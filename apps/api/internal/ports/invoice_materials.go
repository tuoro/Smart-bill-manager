package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type InvoiceMaterial struct {
	ID           string    `json:"id"`
	DocumentID   string    `json:"document_id"`
	OriginalName string    `json:"original_name"`
	MIME         string    `json:"mime"`
	SizeBytes    int64     `json:"size_bytes"`
	PageCount    int       `json:"page_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type InvoiceMaterialWorkspace struct {
	InvoiceID string            `json:"invoice_id"`
	Version   int               `json:"version"`
	Items     []InvoiceMaterial `json:"items"`
}

type MaterialDocumentQuery struct {
	Query string
	After *domain.FactSortKey
	Limit int
}

type InvoiceMaterialResult struct {
	InvoiceID  string   `json:"invoice_id"`
	LinkID     string   `json:"link_id"`
	DocumentID string   `json:"document_id"`
	Version    int      `json:"version"`
	Replayed   bool     `json:"replayed"`
	Document   Document `json:"-"`
}

type InvoiceMaterialCommand struct {
	domain.InvoiceMaterialRequest
	TenantID, ActorUserID, RequestHash, DecisionID, NewLinkID, AuditEventID, RequestID string
	CreatedAt                                                                          time.Time
	UploadDocument                                                                     *Document
}

type MaterialPublication struct {
	ID         string       `json:"id"`
	TenantID   string       `json:"tenant_id"`
	DocumentID string       `json:"document_id"`
	StorageKey string       `json:"storage_key"`
	Staged     StagedObject `json:"staged"`
}

// 意图只覆盖本次随机新对象，不能用于清理共享文件或全库扫描。
type MaterialPublicationStore interface {
	RecordMaterialPublication(context.Context, MaterialPublication) error
	GetMaterialPublication(context.Context, string) (MaterialPublication, error)
	PendingMaterialPublications(context.Context, int) ([]MaterialPublication, error)
	FinishMaterialPublication(context.Context, string) error
}

type InvoiceMaterialRepository interface {
	GetInvoiceMaterials(context.Context, string, string) (InvoiceMaterialWorkspace, error)
	ListMaterialDocuments(context.Context, string, string, MaterialDocumentQuery) (FactPage[InvoiceMaterial], error)
}

type InvoiceMaterialTransaction interface {
	LockMaterialPublication(context.Context, string) error
	MaterialPublicationCommitted(context.Context, MaterialPublication) (bool, error)
	ChangeInvoiceMaterial(context.Context, InvoiceMaterialCommand) (InvoiceMaterialResult, error)
}
