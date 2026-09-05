package ports

import (
	"context"
	"io"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type ExportInventory struct {
	Manifest    domain.ExportManifest
	StorageKeys map[string]string
}

type MaterialExportRepository interface {
	BuildMaterialExport(context.Context, string, domain.ExportScope) (ExportInventory, error)
}

// 文件创建后已从目录解除链接，调用方 Close 即释放磁盘，不能按路径重开。
type ExportFile interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
}

type ExportSpool interface {
	CreateExportFile(context.Context) (ExportFile, error)
}
