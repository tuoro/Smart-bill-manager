package materialexports

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const exportReadme = "Smart Bill Manager 材料交付包\n\n请先阅读 manifest.json 的 scope、trip、warnings 与 references。\n当前行程包包含准备时的活动费用、行程凭证与发票辅助材料；固定报销包只包含该报销快照已知的原件与固定辅助材料，不包含未捕获的行程凭证。\n历史未捕获的 Review 身份或辅助集合不会回填。共享文件在 materials/ 中只出现一次；references 保留每个业务引用，按 document_id 对应 files 的 path。\n金额均为整数最小货币单位，不能把支付与发票金额直接相加作为报销合计。\n原件不会因后续人工纠错被改写；正式字段以 references 的指定版本为准。\n此包是已确认预览时点的副本，不是数据库备份，也不是下载时的实时列表。\n材料含所选范围的业务信息，请只交付给有权接收者。\n"

func writeArchive(ctx context.Context, file ports.ExportFile, objects ports.ObjectStore, inventory ports.ExportInventory) (int64, error) {
	output := &limitedExportWriter{writer: file, remaining: domain.MaxExportZIPBytes}
	writer := zip.NewWriter(output)
	// 任意失败只关闭底层临时文件，不把部分 ZIP 当作可下载结果。
	buffer := make([]byte, 32*1024)
	for _, item := range inventory.Manifest.Files {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		key := inventory.StorageKeys[item.DocumentID]
		if key == "" {
			return 0, domain.ExportObjectUnavailable(item.DocumentID)
		}
		if err := writeExportObject(ctx, writer, objects, key, item, buffer); err != nil {
			return 0, err
		}
	}
	manifest, err := json.MarshalIndent(inventory.Manifest, "", "  ")
	if err != nil {
		return 0, err
	}
	for _, item := range []struct {
		name  string
		bytes []byte
	}{{"manifest.json", manifest}, {"说明.txt", []byte(exportReadme)}} {
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: item.name, Method: zip.Store})
		if err != nil {
			return 0, exportUnavailable()
		}
		if _, err := entry.Write(item.bytes); err != nil {
			return 0, archiveWriteError(err)
		}
	}
	if err := writer.Close(); err != nil {
		return 0, archiveWriteError(err)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return domain.MaxExportZIPBytes - output.remaining, nil
}

func writeExportObject(ctx context.Context, writer *zip.Writer, objects ports.ObjectStore, key string, item domain.ExportFile, buffer []byte) error {
	source, err := objects.Open(ctx, key)
	if err != nil {
		return domain.ExportObjectUnavailable(item.DocumentID)
	}
	defer source.Close()
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: item.Path, Method: zip.Store})
	if err != nil {
		return archiveWriteError(err)
	}
	hash := sha256.New()
	// 独立跟踪源读取错误与目标写错误，磁盘满不能误报成原件损坏。
	count := int64(0)
	reader := io.LimitReader(source, item.SizeBytes+1)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			count += int64(n)
			if count > item.SizeBytes {
				return domain.ExportObjectUnavailable(item.DocumentID)
			}
			hash.Write(buffer[:n])
			if written, err := entry.Write(buffer[:n]); err != nil || written != n {
				if err == nil {
					err = io.ErrShortWrite
				}
				return archiveWriteError(err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return domain.ExportObjectUnavailable(item.DocumentID)
		}
	}
	if count != item.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return domain.ExportObjectUnavailable(item.DocumentID)
	}
	return nil
}

type limitedExportWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *limitedExportWriter) Write(buffer []byte) (int, error) {
	if int64(len(buffer)) > w.remaining {
		return 0, domain.ExportLimit()
	}
	n, err := w.writer.Write(buffer)
	w.remaining -= int64(n)
	return n, err
}
func archiveWriteError(err error) error {
	if errors.Is(err, domain.ErrPayloadTooLarge) {
		return err
	}
	return exportUnavailable()
}
