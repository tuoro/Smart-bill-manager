package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

func shouldLogEachEmailInSync() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_EMAIL_LOG_EACH")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func shouldEmailDiagSearch() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_EMAIL_DIAG_SEARCH")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func formatIMAPLoginError(imapHost string, err error) string {
	base := ""
	if err != nil {
		base = err.Error()
	}
	host := strings.ToLower(strings.TrimSpace(imapHost))
	if host == "" {
		return base
	}

	// QQ Mail commonly requires enabling IMAP/SMTP and using an authorization code (not the login password).
	if strings.Contains(host, "qq.com") {
		lower := strings.ToLower(base)
		if strings.Contains(lower, "login fail") || strings.Contains(lower, "authentication failed") || strings.Contains(lower, "auth") {
			return fmt.Sprintf(
				"%s\n\n提示：QQ邮箱需要在网页版「设置 -> 账户」开启 IMAP/SMTP 服务，并使用生成的“授权码”（不是QQ登录密码）。如果提示登录频率限制/账号异常，建议稍后重试或先网页登录确认账号状态。",
				base,
			)
		}
	}

	return base
}

type EmailService struct {
	repo              *repository.EmailRepository
	invoiceService    *InvoiceService
	uploadsDir        string
	activeConnections map[string]*client.Client
	mu                sync.RWMutex
}

func formatIMAPAddress(a *imap.Address) string {
	if a == nil {
		return ""
	}
	addr := strings.TrimSpace(a.Address())
	name := strings.TrimSpace(a.PersonalName)
	if name == "" {
		return addr
	}
	return fmt.Sprintf("%s <%s>", name, addr)
}

// countInvoiceAttachments counts attachments in a message body structure.
// It is used for UI display ("附件 X 个") during mailbox sync where we only have BODYSTRUCTURE.
func countInvoiceAttachments(bs *imap.BodyStructure) (hasAttachment int, attachmentCount int) {
	if bs == nil {
		return 0, 0
	}

	extractFilename := func(part *imap.BodyStructure) string {
		if part == nil {
			return ""
		}
		// Prefer the library helper; it handles common Param/DispositionParam variants.
		if v, err := part.Filename(); err == nil {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
		// Fallback: case-insensitive lookup across Params/DispositionParams.
		if part.DispositionParams != nil {
			for k, v := range part.DispositionParams {
				if strings.EqualFold(strings.TrimSpace(k), "filename") || strings.EqualFold(strings.TrimSpace(k), "name") {
					if s := strings.TrimSpace(v); s != "" {
						return s
					}
				}
			}
		}
		if part.Params != nil {
			for k, v := range part.Params {
				if strings.EqualFold(strings.TrimSpace(k), "name") || strings.EqualFold(strings.TrimSpace(k), "filename") {
					if s := strings.TrimSpace(v); s != "" {
						return s
					}
				}
			}
		}
		return ""
	}

	var walk func(part *imap.BodyStructure)
	walk = func(part *imap.BodyStructure) {
		if part == nil {
			return
		}
		if strings.EqualFold(part.MIMEType, "multipart") {
			for _, child := range part.Parts {
				walk(child)
			}
			return
		}
		if strings.EqualFold(part.MIMEType, "message") && strings.EqualFold(part.MIMESubType, "rfc822") && part.BodyStructure != nil {
			walk(part.BodyStructure)
			return
		}

		filename := strings.TrimSpace(extractFilename(part))
		mimeType := strings.ToLower(strings.TrimSpace(part.MIMEType))
		mimeSubType := strings.ToLower(strings.TrimSpace(part.MIMESubType))
		mime := strings.TrimSpace(mimeType + "/" + mimeSubType)
		disp := strings.ToLower(strings.TrimSpace(part.Disposition))

		// Skip typical body parts (text/plain, text/html) unless they explicitly look like attachments.
		if filename == "" && disp != "attachment" && mimeType == "text" && (mimeSubType == "plain" || mimeSubType == "html") {
			return
		}

		// Primary signals for attachments.
		isAttachment := disp == "attachment" || filename != ""

		// Fallback: some servers omit filename/disposition; treat obvious invoice formats as attachments.
		if !isAttachment {
			if mime == "application/pdf" || strings.Contains(mime, "xml") {
				isAttachment = true
			}
		}

		if !isAttachment {
			// As a last resort, count non-text leaf parts as attachments if they have any identifier.
			// (Avoids undercounting when servers strip filename but keep Content-Id/Description.)
			if mimeType != "multipart" && mimeType != "text" && (strings.TrimSpace(part.Id) != "" || strings.TrimSpace(part.Description) != "") {
				isAttachment = true
			}
		}

		if isAttachment {
			attachmentCount++
			hasAttachment = 1
		}
	}

	walk(bs)
	return hasAttachment, attachmentCount
}

// Backward-compatible alias for older tests/callers.
func countPDFAttachments(bs *imap.BodyStructure) (hasAttachment int, attachmentCount int) {
	return countInvoiceAttachments(bs)
}

func readIMAPBodyWithLimit(r io.Reader, maxBytes int64) (string, error) {
	if r == nil || maxBytes <= 0 {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(r, maxBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

var urlRegex = regexp.MustCompile(`(?i)(https?://[^\s"'<>]+|//[^\s"'<>]+)`)

func extractInvoiceLinksFromText(body string) (xmlURL *string, pdfURL *string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil
	}

	// Decode common HTML escaping from text parts.
	body = strings.ReplaceAll(body, "&amp;", "&")

	urls := urlRegex.FindAllString(body, -1)
	if len(urls) == 0 {
		return nil, nil
	}

	cleanURL := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimRight(s, ">)].,;\"'")
		if strings.HasPrefix(s, "//") {
			s = "https:" + s
		}
		return s
	}

	isAssetURL := func(u string) bool {
		l := strings.ToLower(u)
		switch {
		case strings.Contains(l, ".png"),
			strings.Contains(l, ".jpg"),
			strings.Contains(l, ".jpeg"),
			strings.Contains(l, ".gif"),
			strings.Contains(l, ".webp"),
			strings.Contains(l, ".bmp"),
			strings.Contains(l, ".svg"),
			strings.Contains(l, ".css"),
			strings.Contains(l, ".js"):
			return true
		default:
			return false
		}
	}

	isPDFLike := func(u string) bool {
		l := strings.ToLower(u)
		if isAssetURL(l) {
			return false
		}
		if strings.Contains(l, ".pdf") {
			return true
		}
		// Baiwang download endpoint: .../downloadFormat?...&formatType=PDF
		if strings.Contains(l, "formattype=pdf") {
			return true
		}
		return false
	}

	isXMLLike := func(u string) bool {
		l := strings.ToLower(u)
		if isAssetURL(l) {
			return false
		}
		if strings.Contains(l, ".xml") {
			return true
		}
		// Some providers ship XML inside a zip, typically with a /xml/ path segment.
		if strings.Contains(l, ".zip") && strings.Contains(l, "/xml/") {
			return true
		}
		// Baiwang download endpoint: .../downloadFormat?...&formatType=XML
		if strings.Contains(l, "formattype=xml") {
			return true
		}
		return false
	}

	for _, raw := range urls {
		u := cleanURL(raw)
		if u == "" {
			continue
		}
		if xmlURL == nil {
			if isXMLLike(u) {
				xmlURL = ptrString(u)
			}
		}
		if pdfURL == nil {
			if isPDFLike(u) {
				pdfURL = ptrString(u)
			}
		}
		if xmlURL != nil && pdfURL != nil {
			return xmlURL, pdfURL
		}
	}

	return xmlURL, pdfURL
}

func ptrString(s string) *string {
	v := strings.TrimSpace(s)
	if v == "" {
		return nil
	}
	return &v
}

func NewEmailService(uploadsDir string, invoiceService *InvoiceService) *EmailService {
	return &EmailService{
		repo:              repository.NewEmailRepository(),
		invoiceService:    invoiceService,
		uploadsDir:        uploadsDir,
		activeConnections: make(map[string]*client.Client),
	}
}

type CreateEmailConfigInput struct {
	Email    string `json:"email" binding:"required"`
	IMAPHost string `json:"imap_host" binding:"required"`
	IMAPPort int    `json:"imap_port"`
	Password string `json:"password" binding:"required"`
	IsActive int    `json:"is_active"`
}

func (s *EmailService) CreateConfig(ownerUserID string, input CreateEmailConfigInput) (*models.EmailConfig, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, fmt.Errorf("missing owner_user_id")
	}
	port := input.IMAPPort
	if port == 0 {
		port = 993
	}

	isActive := input.IsActive
	if isActive == 0 {
		isActive = 1
	}

	config := &models.EmailConfig{
		ID:          utils.GenerateUUID(),
		OwnerUserID: ownerUserID,
		Email:       input.Email,
		IMAPHost:    input.IMAPHost,
		IMAPPort:    port,
		Password:    input.Password,
		IsActive:    isActive,
	}

	if err := s.repo.CreateConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func (s *EmailService) GetAllConfigs(ownerUserID string) ([]models.EmailConfigResponse, error) {
	return s.GetAllConfigsCtx(context.Background(), ownerUserID)
}

func (s *EmailService) GetAllConfigsCtx(ctx context.Context, ownerUserID string) ([]models.EmailConfigResponse, error) {
	configs, err := s.repo.FindAllConfigsCtx(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}

	var responses []models.EmailConfigResponse
	for _, c := range configs {
		responses = append(responses, c.ToResponse())
	}
	return responses, nil
}

func (s *EmailService) GetConfigByID(ownerUserID string, id string) (*models.EmailConfig, error) {
	return s.GetConfigByIDCtx(context.Background(), ownerUserID, id)
}

func (s *EmailService) GetConfigByIDCtx(ctx context.Context, ownerUserID string, id string) (*models.EmailConfig, error) {
	return s.repo.FindConfigByIDForOwnerCtx(ctx, ownerUserID, id)
}

func (s *EmailService) UpdateConfig(ownerUserID string, id string, data map[string]interface{}) error {
	// Don't update password if it's masked
	if pwd, ok := data["password"]; ok {
		if pwd == "********" {
			delete(data, "password")
		}
	}
	return s.repo.UpdateConfigForOwner(ownerUserID, id, data)
}

func (s *EmailService) DeleteConfig(ownerUserID string, id string) error {
	if _, err := s.repo.FindConfigByIDForOwner(ownerUserID, id); err != nil {
		return err
	}
	s.StopMonitoring(id)
	return s.repo.DeleteConfigForOwnerCascade(ownerUserID, id)
}

func (s *EmailService) ClearLogs(ownerUserID string, configID string) (deleted int64, err error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	if ownerUserID == "" || configID == "" {
		return 0, fmt.Errorf("missing fields")
	}

	// Stop monitoring to avoid concurrent writes while clearing.
	s.StopMonitoring(configID)

	if _, err := s.repo.FindConfigByIDForOwner(ownerUserID, configID); err != nil {
		return 0, err
	}

	deleted, err = s.repo.DeleteLogsByConfigIDForOwner(ownerUserID, configID)
	if err != nil {
		return 0, err
	}

	// Reset last_check so the next sync can safely bootstrap.
	_ = s.repo.UpdateLastCheckForOwner(ownerUserID, configID, "")
	return deleted, nil
}

func (s *EmailService) GetLogs(ownerUserID string, configID string, limit int) ([]models.EmailLog, error) {
	return s.GetLogsCtx(context.Background(), ownerUserID, configID, limit)
}

func (s *EmailService) GetLogsCtx(ctx context.Context, ownerUserID string, configID string, limit int) ([]models.EmailLog, error) {
	if limit == 0 {
		limit = 50
	}
	if strings.TrimSpace(configID) != "" {
		if _, err := s.repo.FindConfigByIDForOwnerCtx(ctx, ownerUserID, configID); err != nil {
			return []models.EmailLog{}, nil
		}
	}
	return s.repo.FindLogsCtx(ctx, ownerUserID, configID, limit)
}

// TestConnection tests IMAP connection
// Note: InsecureSkipVerify is intentionally set to true to support mail servers
// with self-signed certificates, which is common in enterprise environments.
// This matches the behavior of the original Node.js implementation.
func (s *EmailService) TestConnection(email, imapHost string, imapPort int, password string) (bool, string) {
	addr := fmt.Sprintf("%s:%d", imapHost, imapPort)

	// #nosec G402 - InsecureSkipVerify is intentional to support self-signed certs
	c, err := client.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return false, fmt.Sprintf("连接失败: %v", err)
	}
	defer c.Logout()

	if err := c.Login(email, password); err != nil {
		return false, fmt.Sprintf("登录失败: %s", formatIMAPLoginError(imapHost, err))
	}

	return true, "连接成功！"
}

// StartMonitoring starts email monitoring for a config
func (s *EmailService) StartMonitoring(ownerUserID string, configID string) bool {
	config, err := s.repo.FindConfigByIDForOwner(ownerUserID, configID)
	if err != nil || config.IsActive == 0 {
		return false
	}

	// Stop existing connection
	s.StopMonitoring(configID)

	addr := fmt.Sprintf("%s:%d", config.IMAPHost, config.IMAPPort)
	// #nosec G402 - InsecureSkipVerify is intentional to support self-signed certs
	c, err := client.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("[Email Monitor] Connection error for %s: %v", config.Email, err)
		return false
	}

	if err := c.Login(config.Email, config.Password); err != nil {
		log.Printf("[Email Monitor] Login error for %s: %v", config.Email, err)
		c.Logout()
		return false
	}

	s.mu.Lock()
	s.activeConnections[configID] = c
	s.mu.Unlock()

	log.Printf("[Email Monitor] Connected to %s", config.Email)

	// Start monitoring in goroutine.
	// Do NOT auto-run a historical sync on first start; large inboxes easily trigger QQ risk control.
	go s.monitorInbox(configID, strings.TrimSpace(config.OwnerUserID), false, c)

	return true
}

func (s *EmailService) monitorInbox(configID string, ownerUserID string, fullSync bool, c *client.Client) {
	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Printf("[Email Monitor] Error selecting inbox: %v", err)
		s.StopMonitoring(configID)
		return
	}

	log.Printf("[Email Monitor] Inbox opened. %d total messages", mbox.Messages)

	// On first run (no last_check), do NOT scan the whole mailbox (risk control on huge inboxes).
	// Instead, fast-forward to the current UIDNEXT and only start logging new emails.
	if fullSync {
		if _, err := s.fetchEmails(ownerUserID, configID, c, true, nil); err != nil {
			log.Printf("[Email Monitor] Full sync error: %v", err)
		}
	} else {
		if cfg, err := s.repo.FindConfigByIDCtx(context.Background(), configID); err == nil && cfg != nil {
			if cfg.LastCheck == nil || strings.TrimSpace(*cfg.LastCheck) == "" {
				now := time.Now().Format(time.RFC3339)
				_ = s.repo.UpdateLastCheckForOwner(ownerUserID, configID, now)
				log.Printf("[Email Monitor] Initialized last_check=%s; skip historical scan (uidNext=%d)", now, mbox.UidNext)
			}
		}
		if _, err := s.fetchEmails(ownerUserID, configID, c, false, nil); err != nil {
			log.Printf("[Email Monitor] Fetch error: %v", err)
		}
	}

	// Set up IDLE for real-time notifications
	updates := make(chan client.Update)
	c.Updates = updates

	done := make(chan error, 1)
	stop := make(chan struct{})

	// Debounce mailbox update bursts (common in high-volume inboxes) to avoid frequent IMAP commands.
	go func() {
		const debounce = 5 * time.Second
		timer := time.NewTimer(debounce)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		pending := false
		for {
			select {
			case update := <-updates:
				switch update.(type) {
				case *client.MailboxUpdate:
					log.Println("[Email Monitor] New mail received!")
					pending = true
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(debounce)
				}
			case <-timer.C:
				if pending {
					pending = false
					if _, err := s.fetchEmails(ownerUserID, configID, c, false, nil); err != nil {
						log.Printf("[Email Monitor] Fetch error: %v", err)
					}
				}
			case <-stop:
				return
			}
		}
	}()

	// Start IDLE
	go func() {
		done <- c.Idle(stop, nil)
	}()

	// Wait for done or connection closed
	if err := <-done; err != nil {
		log.Printf("[Email Monitor] IDLE error: %v", err)
	}

	close(stop)
	s.mu.Lock()
	delete(s.activeConnections, configID)
	s.mu.Unlock()
}

type FullSyncOptions struct {
	Limit     int
	BeforeUID uint32
	Mode      string
}

func clampFullSyncLimit(v int) int {
	const defaultLimit = 500
	const maxLimit = 5000
	if v <= 0 {
		return defaultLimit
	}
	if v > maxLimit {
		return maxLimit
	}
	return v
}

func getFullSyncSafetyMaxUIDs() int {
	// Hard cap to avoid accidentally trying to sync an extremely large mailbox in one request.
	// Still rate-limited, but could otherwise run for hours and look "stuck" in UI.
	const defaultMax = 50000
	v := strings.TrimSpace(os.Getenv("SBM_EMAIL_FULLSYNC_MAX_UIDS"))
	if v == "" {
		return defaultMax
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultMax
	}
	return n
}

func (s *EmailService) fetchEmails(ownerUserID string, configID string, c *client.Client, full bool, fullOpts *FullSyncOptions) (int, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	if ownerUserID == "" || configID == "" || c == nil {
		return 0, fmt.Errorf("missing fields")
	}

	// Only fetch metadata. The detailed email body is read later during "解析" to avoid
	// slow/fragile full-sync behavior on some IMAP servers.
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchBodyStructure, imap.FetchInternalDate}

	newLogs := 0
	fetchChunk := func(seqSet *imap.SeqSet) error {
		if seqSet == nil {
			return fmt.Errorf("nil seqSet")
		}
		messages := make(chan *imap.Message, 32)
		errCh := make(chan error, 1)
		go func() {
			errCh <- c.UidFetch(seqSet, items, messages)
		}()
		for msg := range messages {
			if s.processMessage(ownerUserID, configID, msg, nil, full) {
				newLogs++
			}
		}
		return <-errCh
	}

	if full {
		// Log mailbox stats to verify UID range and help diagnose "missing year" reports.
		var inboxStatus *imap.MailboxStatus
		if mbox, err := c.Select("INBOX", false); err == nil && mbox != nil {
			inboxStatus = mbox
			log.Printf(
				"[Email Monitor] Full sync mailbox: messages=%d uidNext=%d (config=%s)",
				mbox.Messages,
				mbox.UidNext,
				configID,
			)
		} else if err != nil {
			log.Printf("[Email Monitor] Full sync Select INBOX error: %v (config=%s)", err, configID)
		}

		// Some IMAP servers behave unexpectedly with "UID FETCH 1:*" for large mailboxes.
		// Use a two-step approach: UID SEARCH ALL -> chunked UID FETCH.
		criteria := imap.NewSearchCriteria() // defaults to ALL
		uids, err := c.UidSearch(criteria)
		if err != nil {
			log.Printf("[Email Monitor] Full sync UidSearch error: %v (falling back to UID FETCH 1:*)", err)
			seqSet := new(imap.SeqSet)
			seqSet.AddRange(1, 0) // 1:* (all UIDs)
			if err := fetchChunk(seqSet); err != nil {
				log.Printf("[Email Monitor] UidFetch error: %v", err)
				return newLogs, err
			}
		} else {
			// IMAP servers are not required to return UIDs in sorted order.
			// We rely on ascending order for paging (beforeUID) and tail-slicing (newest window).
			if len(uids) > 1 {
				sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
			}

			if len(uids) == 0 {
				_ = s.reconcileDeletedLogs(ownerUserID, configID, "INBOX", uids)
			} else {
				allMin := uids[0]
				allMax := uids[len(uids)-1]
				log.Printf(
					"[Email Monitor] Full sync: search returned uidCount=%d uidMin=%d uidMax=%d (config=%s)",
					len(uids),
					allMin,
					allMax,
					configID,
				)
				if inboxStatus != nil && inboxStatus.UidNext > 0 && allMax != inboxStatus.UidNext-1 {
					log.Printf(
						"[Email Monitor] Full sync warning: uidMax=%d != uidNext-1=%d (server may hide mailboxes or truncate SEARCH) (config=%s)",
						allMax,
						inboxStatus.UidNext-1,
						configID,
					)
				}

				uidsAll := uids

				if shouldEmailDiagSearch() {
					// IMAP SINCE/BEFORE uses INTERNALDATE (server receive date).
					diag := func(label string, since, before time.Time) {
						crit := imap.NewSearchCriteria()
						crit.Since = since
						crit.Before = before
						duids, derr := c.UidSearch(crit)
						if derr != nil {
							log.Printf("[Email Monitor] Full sync diag %s: UidSearch error: %v (config=%s)", label, derr, configID)
							return
						}
						if len(duids) > 1 {
							sort.Slice(duids, func(i, j int) bool { return duids[i] < duids[j] })
						}
						if len(duids) == 0 {
							log.Printf("[Email Monitor] Full sync diag %s: count=0 (config=%s)", label, configID)
							return
						}
						log.Printf(
							"[Email Monitor] Full sync diag %s: count=%d uidMin=%d uidMax=%d (config=%s)",
							label,
							len(duids),
							duids[0],
							duids[len(duids)-1],
							configID,
						)
					}

					diag("Y2026", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
					diag("Y2025", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
					diag("Y2024", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
				}

				mode := "latest"
				limit := 0
				var beforeUID uint32
				if fullOpts != nil {
					limit = fullOpts.Limit
					beforeUID = fullOpts.BeforeUID
					if v := strings.TrimSpace(fullOpts.Mode); v != "" {
						mode = strings.ToLower(v)
					}
				}
				if mode != "all" && mode != "backfill" {
					mode = "latest"
				}
				limit = clampFullSyncLimit(limit)

				// If requested, only fetch UIDs older than beforeUID (paged full sync).
				if beforeUID > 0 {
					// UIDs are ascending; find first >= beforeUID, keep everything before it.
					cut := sort.Search(len(uids), func(i int) bool { return uids[i] >= beforeUID })
					uids = uids[:cut]
					log.Printf(
						"[Email Monitor] Full sync paging: beforeUID=%d cutIndex=%d remaining=%d (config=%s)",
						beforeUID,
						cut,
						len(uids),
						configID,
					)
				}

				// Scope selection:
				// - latest: only sync the newest `limit` messages (default 500) so one click gives "latest-first" UI.
				// - all: sync everything (still rate-limited; with a safety cap).
				// - backfill: typically used with beforeUID to sync older history in chunks.
				if mode == "all" {
					maxUIDs := getFullSyncSafetyMaxUIDs()
					if len(uids) > maxUIDs {
						log.Printf(
							"[Email Monitor] Full sync safety cap: uidCount=%d cap=%d (set SBM_EMAIL_FULLSYNC_MAX_UIDS to override) (config=%s)",
							len(uids),
							maxUIDs,
							configID,
						)
						uids = uids[len(uids)-maxUIDs:]
					}
				} else {
					if len(uids) > limit {
						uids = uids[len(uids)-limit:]
					}
				}
				if len(uids) == 0 {
					log.Printf("[Email Monitor] Full sync: no UIDs to fetch after paging/limit (config=%s)", configID)
					return newLogs, nil
				}
				winMin := uids[0]
				winMax := uids[len(uids)-1]

				// Rate-limited pipeline: fetch in small batches and sleep between batches to reduce IMAP bursts.
				const batchSize = 20
				const baseDelay = 350 * time.Millisecond
				const jitter = 250 * time.Millisecond
				rng := rand.New(rand.NewSource(time.Now().UnixNano()))
				progressEvery := 30 * time.Second
				lastProgress := time.Now()
				log.Printf(
					"[Email Monitor] Full sync plan: mode=%s fetchCount=%d uidMin=%d uidMax=%d beforeUID=%d batchSize=%d delay=%s jitter=%s newestFirst=true (config=%s)",
					mode,
					len(uids),
					winMin,
					winMax,
					beforeUID,
					batchSize,
					baseDelay,
					jitter,
					configID,
				)

				for end := len(uids); end > 0; end -= batchSize {
					start := end - batchSize
					if start < 0 {
						start = 0
					}
					seqSet := new(imap.SeqSet)
					seqSet.AddNum(uids[start:end]...)
					if end == len(uids) || start == 0 {
						log.Printf(
							"[Email Monitor] Full sync batch: uidMin=%d uidMax=%d count=%d (config=%s)",
							uids[start],
							uids[end-1],
							end-start,
							configID,
						)
					}
					if err := fetchChunk(seqSet); err != nil {
						log.Printf("[Email Monitor] UidFetch error: %v", err)
						return newLogs, err
					}
					if time.Since(lastProgress) >= progressEvery && start > 0 {
						done := len(uids) - start
						log.Printf(
							"[Email Monitor] Full sync progress: mode=%s done=%d/%d nextOlderUID=%d (config=%s)",
							mode,
							done,
							len(uids),
							uids[start-1],
							configID,
						)
						lastProgress = time.Now()
					}
					if start > 0 {
						sleep := baseDelay
						if jitter > 0 {
							sleep += time.Duration(rng.Int63n(int64(jitter)))
						}
						time.Sleep(sleep)
					}
				}

				// Reconcile deletions only when we effectively scanned the whole mailbox.
				// Paged/limited syncs should not mark unseen older logs as deleted.
				if beforeUID == 0 && len(uids) == len(uidsAll) {
					_ = s.reconcileDeletedLogs(ownerUserID, configID, "INBOX", uidsAll)
				}
			}
		}
		// Summary to confirm paging progress (helps diagnose "stuck" ranges).
		minUID, minErr := s.repo.GetMinUIDForMailboxCtx(context.Background(), ownerUserID, configID, "INBOX")
		maxUID, maxErr := s.repo.GetMaxUIDForMailboxCtx(context.Background(), ownerUserID, configID, "INBOX")
		if minErr == nil && maxErr == nil {
			log.Printf(
				"[Email Monitor] Full sync done: newLogs=%d dbMinUID=%d dbMaxUID=%d (config=%s)",
				newLogs,
				minUID,
				maxUID,
				configID,
			)
		}
	} else {
		// Incremental sync: avoid "SEARCH UNSEEN" on large inboxes; it can match a huge backlog and trigger risk control.
		mbox, err := c.Select("INBOX", false)
		if err != nil {
			log.Printf("[Email Monitor] Select error: %v", err)
			return 0, err
		}
		if mbox == nil || mbox.UidNext == 0 {
			return 0, nil
		}
		uidNext := mbox.UidNext
		lastUID, err := s.repo.GetMaxUIDForMailboxCtx(context.Background(), ownerUserID, configID, "INBOX")
		if err != nil {
			log.Printf("[Email Monitor] Max UID query error: %v", err)
			return 0, err
		}

		// Bootstrap when we have no cursor/logs yet: fast-forward to current UIDNEXT and only log new emails.
		// Use status="deleted" so it does not show up in UI lists, but still advances MAX(message_uid).
		if lastUID == 0 {
			baseline := uint32(0)
			if uidNext > 0 {
				baseline = uidNext - 1
			}
			if baseline > 0 {
				_ = s.repo.CreateLog(&models.EmailLog{
					ID:            utils.GenerateUUID(),
					OwnerUserID:   strings.TrimSpace(ownerUserID),
					EmailConfigID: configID,
					Mailbox:       "INBOX",
					MessageUID:    baseline,
					HasAttachment: 0,
					Status:        "deleted",
				})
			}
			now := time.Now().Format(time.RFC3339)
			_ = s.repo.UpdateLastCheckForOwner(ownerUserID, configID, now)
			log.Printf("[Email Monitor] Baseline cursor set (uid=%d uidNext=%d); skip historical scan", baseline, uidNext)
			return 0, nil
		}
		start := lastUID + 1
		end := uidNext - 1
		if start == 0 || end == 0 || start > end {
			return 0, nil
		}
		seqSet := new(imap.SeqSet)
		seqSet.AddRange(start, end)
		if err := fetchChunk(seqSet); err != nil {
			log.Printf("[Email Monitor] UidFetch error: %v", err)
			return newLogs, err
		}
	}

	now := time.Now().Format(time.RFC3339)
	_ = s.repo.UpdateLastCheckForOwner(ownerUserID, configID, now)
	return newLogs, nil
}

func (s *EmailService) reconcileDeletedLogs(ownerUserID string, configID string, mailbox string, serverUIDs []uint32) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if ownerUserID == "" || configID == "" {
		return nil
	}

	serverSet := make(map[uint32]struct{}, len(serverUIDs))
	for _, uid := range serverUIDs {
		if uid == 0 {
			continue
		}
		serverSet[uid] = struct{}{}
	}

	logs, err := s.repo.FindLogsForMailboxReconcileCtx(context.Background(), ownerUserID, configID, mailbox)
	if err != nil {
		return err
	}
	if len(logs) == 0 {
		return nil
	}

	missingIDs := make([]string, 0, 32)
	for _, row := range logs {
		if row.MessageUID == 0 {
			continue
		}
		if _, ok := serverSet[row.MessageUID]; ok {
			continue
		}
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		missingIDs = append(missingIDs, row.ID)
	}

	if len(missingIDs) == 0 {
		return nil
	}

	affected, err := s.repo.MarkLogsDeletedByIDs(missingIDs)
	if err != nil {
		return err
	}
	if affected > 0 {
		log.Printf("[Email Monitor] Reconciled deletions: %d logs marked deleted (config=%s mailbox=%s)", affected, configID, mailbox)
	}
	return nil
}

func computeEmailLogMetadataUpdates(existing *models.EmailLog, hasAttachment int, attachmentCount int, xmlURL *string, pdfURL *string, forceRefresh bool) map[string]interface{} {
	if existing == nil {
		return map[string]interface{}{}
	}
	updates := map[string]interface{}{}

	// If an email previously got marked as deleted but re-appears in mailbox (e.g. transient sync issue),
	// restore it so it becomes visible again.
	if strings.TrimSpace(existing.Status) == "deleted" {
		if existing.ParsedInvoiceID != nil && strings.TrimSpace(*existing.ParsedInvoiceID) != "" {
			updates["status"] = "parsed"
		} else {
			updates["status"] = "received"
		}
	}

	if forceRefresh {
		if existing.HasAttachment != hasAttachment {
			updates["has_attachment"] = hasAttachment
		}
		if existing.AttachmentCount != attachmentCount {
			updates["attachment_count"] = attachmentCount
		}
	} else {
		if hasAttachment > existing.HasAttachment {
			updates["has_attachment"] = hasAttachment
		}
		if attachmentCount > existing.AttachmentCount {
			updates["attachment_count"] = attachmentCount
		}
	}

	if xmlURL != nil && strings.TrimSpace(*xmlURL) != "" {
		if existing.InvoiceXMLURL == nil || strings.TrimSpace(*existing.InvoiceXMLURL) == "" {
			updates["invoice_xml_url"] = strings.TrimSpace(*xmlURL)
		}
	}
	if pdfURL != nil && strings.TrimSpace(*pdfURL) != "" {
		if existing.InvoicePDFURL == nil || strings.TrimSpace(*existing.InvoicePDFURL) == "" {
			updates["invoice_pdf_url"] = strings.TrimSpace(*pdfURL)
		}
	}

	return updates
}

func (s *EmailService) markSeenByUID(c *client.Client, uid uint32) {
	if c == nil || uid == 0 {
		return
	}
	seenSet := new(imap.SeqSet)
	seenSet.AddNum(uid)
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.SeenFlag}
	if err := c.UidStore(seenSet, item, flags, nil); err != nil {
		log.Printf("[Email Monitor] Error marking as seen: %v", err)
	}
}

func (s *EmailService) processMessage(ownerUserID string, configID string, msg *imap.Message, section *imap.BodySectionName, forceRefresh bool) bool {
	if msg == nil {
		return false
	}
	if msg.Uid == 0 {
		return false
	}

	// If the log already exists, still backfill attachment/link metadata (older builds may have missed it).
	if existing, err := s.repo.FindLogByUIDCtx(context.Background(), ownerUserID, configID, "INBOX", msg.Uid); err == nil && existing != nil {
		hasAttachment, attachmentCount := countInvoiceAttachments(msg.BodyStructure)

		var bodyText string
		if section != nil {
			if r := msg.GetBody(section); r != nil {
				if s, err := readIMAPBodyWithLimit(r, 256*1024); err == nil {
					bodyText = s
				}
			}
		}
		xmlURL, pdfURL := extractInvoiceLinksFromText(bodyText)

		updates := computeEmailLogMetadataUpdates(existing, hasAttachment, attachmentCount, xmlURL, pdfURL, forceRefresh)
		// Backfill basic metadata for cursor/older rows (e.g. baseline cursor log) when missing.
		if envelope := msg.Envelope; envelope != nil {
			subject := strings.TrimSpace(envelope.Subject)
			if subject == "" {
				subject = "(无主题)"
			}
			from := ""
			if len(envelope.From) > 0 {
				from = formatIMAPAddress(envelope.From[0])
			}
			receivedAt := envelope.Date
			if !msg.InternalDate.IsZero() {
				receivedAt = msg.InternalDate
			}
			dateStr := receivedAt.Format(time.RFC3339)

			if existing.Subject == nil || strings.TrimSpace(*existing.Subject) == "" {
				updates["subject"] = subject
			}
			if existing.FromAddress == nil || strings.TrimSpace(*existing.FromAddress) == "" {
				updates["from_address"] = from
			}
			// Always prefer INTERNALDATE for received_date (server receive time), override header Date when present.
			if existing.ReceivedDate == nil || strings.TrimSpace(*existing.ReceivedDate) != dateStr {
				updates["received_date"] = dateStr
			}
		}
		if len(updates) > 0 {
			_ = s.repo.UpdateLog(existing.ID, updates)
		}
		return false
	}

	// New behavior: only log metadata + (optional) URLs, do not download PDF attachments or create invoices automatically.
	envelope := msg.Envelope
	if envelope != nil {
		subject := strings.TrimSpace(envelope.Subject)
		if subject == "" {
			subject = "(无主题)"
		}

		from := ""
		if len(envelope.From) > 0 {
			from = formatIMAPAddress(envelope.From[0])
		}

		receivedDate := envelope.Date
		if !msg.InternalDate.IsZero() {
			receivedDate = msg.InternalDate
		}
		dateStr := receivedDate.Format(time.RFC3339)

		hasAttachment, attachmentCount := countInvoiceAttachments(msg.BodyStructure)

		var bodyText string
		if section != nil {
			if r := msg.GetBody(section); r != nil {
				if s, err := readIMAPBodyWithLimit(r, 256*1024); err == nil {
					bodyText = s
				}
			}
		}
		xmlURL, pdfURL := extractInvoiceLinksFromText(bodyText)

		emailLog := &models.EmailLog{
			ID:              utils.GenerateUUID(),
			OwnerUserID:     strings.TrimSpace(ownerUserID),
			EmailConfigID:   configID,
			Mailbox:         "INBOX",
			MessageUID:      msg.Uid,
			Subject:         &subject,
			FromAddress:     &from,
			ReceivedDate:    &dateStr,
			HasAttachment:   hasAttachment,
			AttachmentCount: attachmentCount,
			InvoiceXMLURL:   xmlURL,
			InvoicePDFURL:   pdfURL,
			Status:          "received",
		}
		_ = s.repo.CreateLog(emailLog)

		if shouldLogEachEmailInSync() {
			log.Printf("[Email Monitor] Email logged: %s", subject)
		}
		return true
	}

	r := msg.GetBody(section)
	if r == nil {
		return false
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		log.Printf("[Email Monitor] Error creating mail reader: %v", err)
		return false
	}

	var subject, from string
	var receivedDate time.Time

	header := mr.Header
	if s, err := header.Subject(); err == nil {
		subject = s
	}
	if addrs, err := header.AddressList("From"); err == nil && len(addrs) > 0 {
		from = addrs[0].String()
	}
	// Prefer INTERNALDATE as the "received time" (server receive time), fall back to Date header.
	if !msg.InternalDate.IsZero() {
		receivedDate = msg.InternalDate
	} else if d, err := header.Date(); err == nil {
		receivedDate = d
	}

	var attachmentCount int
	var hasAttachment int

	// Process parts
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[Email Monitor] Error reading part: %v", err)
			break
		}

		switch h := p.Header.(type) {
		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			if strings.HasSuffix(strings.ToLower(filename), ".pdf") {
				content, err := readWithLimit(p.Body, emailParseMaxPDFBytes)
				if err != nil {
					log.Printf("[Email Monitor] Error reading attachment: %v", err)
					continue
				}
				s.saveAttachment(ownerUserID, filename, content)
				attachmentCount++
				hasAttachment = 1
			}
		}
	}

	// Log the email
	dateStr := receivedDate.Format(time.RFC3339)
	if subject == "" {
		subject = "(无主题)"
	}
	emailLog := &models.EmailLog{
		ID:              utils.GenerateUUID(),
		OwnerUserID:     strings.TrimSpace(ownerUserID),
		EmailConfigID:   configID,
		Mailbox:         "INBOX",
		MessageUID:      msg.Uid,
		Subject:         &subject,
		FromAddress:     &from,
		ReceivedDate:    &dateStr,
		HasAttachment:   hasAttachment,
		AttachmentCount: attachmentCount,
		Status:          "processed",
	}
	s.repo.CreateLog(emailLog)

	if shouldLogEachEmailInSync() {
		log.Printf("[Email Monitor] Email logged: %s", subject)
	}
	return true
}

func (s *EmailService) saveAttachment(ownerUserID string, filename string, content []byte) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	safeFilename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeFilename(filename))

	targetDir := s.uploadsDir
	if ownerUserID != "" {
		targetDir = filepath.Join(s.uploadsDir, ownerUserID)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Printf("[Email Monitor] Error creating uploads dir: %v", err)
		return
	}

	filePath := filepath.Join(targetDir, safeFilename)
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		log.Printf("[Email Monitor] Error saving attachment: %v", err)
		return
	}

	log.Printf("[Email Monitor] Saved attachment: %s", safeFilename)
}

func sanitizeFilename(filename string) string {
	// Replace unsafe characters
	unsafe := []string{"/", "\\", "..", " "}
	result := filename
	for _, u := range unsafe {
		result = strings.ReplaceAll(result, u, "_")
	}
	return result
}

// StopMonitoring stops email monitoring for a config
func (s *EmailService) StopMonitoring(configID string) bool {
	configID = strings.TrimSpace(configID)
	if configID == "" {
		return false
	}

	s.mu.Lock()
	c, exists := s.activeConnections[configID]
	if exists {
		delete(s.activeConnections, configID)
	}
	s.mu.Unlock()

	if !exists || c == nil {
		return false
	}

	// Return quickly to the API caller: Terminate closes the TCP connection immediately and
	// unblocks any ongoing IDLE/FETCH calls; do not perform network I/O while holding locks.
	if err := c.Terminate(); err != nil {
		log.Printf("[Email Monitor] Terminate error for %s: %v", configID, err)
	}
	return true
}

// GetMonitoringStatus returns status of all configs
func (s *EmailService) GetMonitoringStatus(ownerUserID string) ([]models.MonitorStatus, error) {
	return s.GetMonitoringStatusCtx(context.Background(), ownerUserID)
}

func (s *EmailService) GetMonitoringStatusCtx(ctx context.Context, ownerUserID string) ([]models.MonitorStatus, error) {
	ids, err := s.repo.GetConfigIDsCtx(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var statuses []models.MonitorStatus
	for _, id := range ids {
		status := "stopped"
		if _, exists := s.activeConnections[id]; exists {
			status = "running"
		}
		statuses = append(statuses, models.MonitorStatus{
			ConfigID: id,
			Status:   status,
		})
	}

	return statuses, nil
}

// ManualCheck performs a manual email check
func (s *EmailService) ManualCheck(ownerUserID string, configID string, full bool) (bool, string, int) {
	return s.ManualCheckWithOptions(ownerUserID, configID, full, nil)
}

func (s *EmailService) ManualCheckWithOptions(ownerUserID string, configID string, full bool, fullOpts *FullSyncOptions) (bool, string, int) {
	config, err := s.repo.FindConfigByIDForOwner(ownerUserID, configID)
	if err != nil {
		return false, "配置不存在", 0
	}

	// Auto-paging for full sync: when the caller does not specify beforeUid, continue syncing older messages
	// based on the oldest UID already logged in DB. This avoids relying on the UI's limited log window.
	if full {
		needsAutoBefore := fullOpts != nil && strings.EqualFold(strings.TrimSpace(fullOpts.Mode), "backfill") && fullOpts.BeforeUID == 0
		if needsAutoBefore {
			if minUID, err := s.repo.GetMinUIDForMailboxCtx(context.Background(), ownerUserID, configID, "INBOX"); err == nil && minUID > 0 {
				fullOpts.BeforeUID = minUID
				log.Printf("[Email Monitor] Full sync auto-beforeUID=%d (config=%s)", minUID, configID)
			}
		}
	}

	addr := fmt.Sprintf("%s:%d", config.IMAPHost, config.IMAPPort)
	// #nosec G402 - InsecureSkipVerify is intentional to support self-signed certs
	c, err := client.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return false, fmt.Sprintf("连接失败: %v", err), 0
	}
	defer c.Logout()

	if err := c.Login(config.Email, config.Password); err != nil {
		return false, fmt.Sprintf("登录失败: %s", formatIMAPLoginError(config.IMAPHost, err)), 0
	}

	_, err = c.Select("INBOX", false)
	if err != nil {
		return false, fmt.Sprintf("打开收件箱失败: %v", err), 0
	}

	count, fetchErr := s.fetchEmails(ownerUserID, configID, c, full, fullOpts)
	if fetchErr != nil {
		if full {
			return false, fmt.Sprintf("全量同步失败: %v", fetchErr), 0
		}
		return false, fmt.Sprintf("同步失败: %v", fetchErr), 0
	}

	// Update last check time
	now := time.Now().Format(time.RFC3339)
	_ = s.repo.UpdateLastCheckForOwner(ownerUserID, configID, now)

	if count == 0 {
		return true, "没有新邮件", 0
	}
	if full {
		return true, fmt.Sprintf("成功同步 %d 封邮件", count), count
	}
	return true, fmt.Sprintf("成功处理 %d 封邮件", count), count
}
