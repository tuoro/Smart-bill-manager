package materialexports

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const (
	PrepareTimeout  = 2 * time.Minute
	ReadyLifetime   = 5 * time.Minute
	DownloadTimeout = 10 * time.Minute
	MaxConcurrent   = 2
)

type Actor struct {
	Tenant    domain.TenantContext
	SessionID string
}

type Prepared struct {
	ID           string    `json:"id"`
	FileName     string    `json:"file_name"`
	SizeBytes    int64     `json:"size_bytes"`
	ManifestHash string    `json:"manifest_hash"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type readyPackage struct {
	actor    Actor
	scope    domain.ExportScope
	prepared Prepared
	lease    *fileLease
	timer    *time.Timer
}

type Service struct {
	repository    ports.MaterialExportRepository
	objects       ports.ObjectStore
	spool         ports.ExportSpool
	ids           ports.IDGenerator
	ctx           context.Context
	cancel        context.CancelFunc
	slots         chan struct{}
	mu            sync.Mutex
	closed        bool
	ready         map[string]*readyPackage
	active        map[string]*fileLease
	readyLifetime time.Duration
}

func NewService(repository ports.MaterialExportRepository, objects ports.ObjectStore, spool ports.ExportSpool, ids ports.IDGenerator) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{repository: repository, objects: objects, spool: spool, ids: ids, ctx: ctx, cancel: cancel, slots: make(chan struct{}, MaxConcurrent), ready: map[string]*readyPackage{}, active: map[string]*fileLease{}, readyLifetime: ReadyLifetime}
}

func (s *Service) Preview(ctx context.Context, tenant domain.TenantContext, scope domain.ExportScope) (domain.ExportManifest, error) {
	inventory, err := s.inventory(ctx, tenant, scope)
	return inventory.Manifest, err
}

func (s *Service) inventory(ctx context.Context, tenant domain.TenantContext, scope domain.ExportScope) (ports.ExportInventory, error) {
	if err := domain.RequireMaterialExport(tenant, scope); err != nil {
		return ports.ExportInventory{}, err
	}
	inventory, err := s.repository.BuildMaterialExport(ctx, tenant.TenantID, scope)
	if err != nil {
		return ports.ExportInventory{}, err
	}
	if inventory.Manifest.Scope != scope {
		return ports.ExportInventory{}, errors.New("export repository scope mismatch")
	}
	inventory.Manifest, err = domain.CanonicalExportManifest(inventory.Manifest)
	return inventory, err
}

func (s *Service) Prepare(ctx context.Context, actor Actor, scope domain.ExportScope, expectedHash string, acknowledgedWarnings bool) (Prepared, error) {
	if err := domain.RequireMaterialExport(actor.Tenant, scope); err != nil {
		return Prepared{}, err
	}
	if actor.SessionID == "" {
		return Prepared{}, domain.ErrUnauthenticated
	}
	if !domain.ValidSHA256Hex(expectedHash) {
		return Prepared{}, domain.ErrInvalidInput
	}
	if err := s.ctx.Err(); err != nil {
		return Prepared{}, exportUnavailable()
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return Prepared{}, domain.NewRuleError("export_busy", "已有两个材料包正在准备、等待或下载，请完成或取消后重试", domain.ErrUnavailable)
	}
	// 槽从查询/准备开始持有，成功后由句柄统一释放，避免等待队列扩大资源。
	transferred := false
	defer func() {
		if !transferred {
			<-s.slots
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, PrepareTimeout)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	inventory, err := s.inventory(ctx, actor.Tenant, scope)
	if err != nil {
		return Prepared{}, err
	}
	if inventory.Manifest.ManifestHash != expectedHash {
		return Prepared{}, domain.NewRuleError("export_preview_stale", "材料范围或版本已变化，请重新预览并核对", domain.ErrConflict)
	}
	if len(inventory.Manifest.Warnings) > 0 && !acknowledgedWarnings {
		return Prepared{}, domain.NewRuleError("export_acknowledgement_required", "请明确确认历史材料未捕获等限制后再准备", domain.ErrInvalidInput)
	}
	id, err := s.ids.NewID()
	if err != nil {
		return Prepared{}, err
	}
	file, err := s.spool.CreateExportFile(ctx)
	if err != nil {
		return Prepared{}, exportUnavailable()
	}
	lease := &fileLease{file: file}
	lease.release = func() {
		s.mu.Lock()
		if s.active[id] == lease {
			delete(s.active, id)
		}
		if current, ok := s.ready[id]; ok && current.lease == lease {
			current.timer.Stop()
			delete(s.ready, id)
		}
		s.mu.Unlock()
		<-s.slots
	}
	transferred = true
	accepted := false
	defer func() {
		if !accepted {
			lease.Close()
		}
	}()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Prepared{}, exportUnavailable()
	}
	if _, exists := s.active[id]; exists {
		s.mu.Unlock()
		return Prepared{}, errors.New("export identity collision")
	}
	s.active[id] = lease
	s.mu.Unlock()
	size, err := writeArchive(ctx, file, s.objects, inventory)
	if err != nil {
		return Prepared{}, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return Prepared{}, exportUnavailable()
	}
	prepared := Prepared{ID: id, FileName: "smart-bill-" + scope.Kind + "-" + id + ".zip", SizeBytes: size, ManifestHash: expectedHash, ExpiresAt: time.Now().UTC().Add(s.readyLifetime)}
	entry := &readyPackage{actor: actor, scope: scope, prepared: prepared, lease: lease}
	s.mu.Lock()
	if s.closed || ctx.Err() != nil {
		s.mu.Unlock()
		return Prepared{}, exportUnavailable()
	}
	s.ready[id] = entry
	entry.timer = time.AfterFunc(s.readyLifetime, func() {
		s.mu.Lock()
		current := s.ready[id] == entry
		if current {
			delete(s.ready, id)
		}
		s.mu.Unlock()
		if current {
			lease.Close()
		}
	})
	accepted = true
	s.mu.Unlock()
	return prepared, nil
}

type Download struct {
	Prepared
	Body io.ReadCloser
}

func (s *Service) Take(actor Actor, id string) (Download, error) {
	entry, err := s.removeReady(actor, id)
	if err != nil {
		return Download{}, err
	}
	return Download{Prepared: entry.prepared, Body: entry.lease}, nil
}

func (s *Service) Cancel(actor Actor, id string) error {
	entry, err := s.removeReady(actor, id)
	if err != nil {
		return err
	}
	return entry.lease.Close()
}

func (s *Service) removeReady(actor Actor, id string) (*readyPackage, error) {
	if !domain.ValidExportID(id) {
		return nil, domain.ErrInvalidInput
	}
	// 即使包不存在，也先验证来源读取权限，不能成为跨角色探测入口。
	if err := domain.RequireMaterialExport(actor.Tenant, domain.ExportScope{Kind: "trip", ID: id}); err != nil {
		return nil, err
	}
	if actor.SessionID == "" {
		return nil, domain.ErrUnauthenticated
	}
	s.mu.Lock()
	entry, ok := s.ready[id]
	if s.closed {
		s.mu.Unlock()
		return nil, exportUnavailable()
	}
	if !ok || entry.actor.Tenant.TenantID != actor.Tenant.TenantID || entry.actor.Tenant.UserID != actor.Tenant.UserID || entry.actor.SessionID != actor.SessionID {
		s.mu.Unlock()
		return nil, domain.ErrNotFound
	}
	if err := domain.RequireMaterialExport(actor.Tenant, entry.scope); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if !time.Now().Before(entry.prepared.ExpiresAt) {
		s.mu.Unlock()
		entry.lease.Close()
		return nil, domain.ErrNotFound
	}
	delete(s.ready, id)
	entry.timer.Stop()
	s.mu.Unlock()
	return entry, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	leases := make([]*fileLease, 0, len(s.active))
	for _, lease := range s.active {
		leases = append(leases, lease)
	}
	s.mu.Unlock()
	s.cancel()
	var result error
	for _, lease := range leases {
		result = errors.Join(result, lease.Close())
	}
	return result
}

type fileLease struct {
	file    ports.ExportFile
	once    sync.Once
	err     error
	release func()
}

func (l *fileLease) Read(buffer []byte) (int, error) { return l.file.Read(buffer) }
func (l *fileLease) Close() error {
	l.once.Do(func() { l.err = l.file.Close(); l.release() })
	return l.err
}

func exportUnavailable() error {
	return domain.NewRuleError("export_unavailable", "材料包准备失败或服务已关闭，请检查磁盘空间后重试", domain.ErrUnavailable)
}
