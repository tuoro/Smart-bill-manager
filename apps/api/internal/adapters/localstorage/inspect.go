package localstorage

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Inspector struct {
	store       *Store
	pdfInfoPath string
}

func NewInspector(store *Store, pdfInfoPath string) (Inspector, error) {
	if store == nil {
		return Inspector{}, errors.New("object store is required")
	}
	if strings.TrimSpace(pdfInfoPath) == "" {
		return Inspector{}, errors.New("pdfinfo path is required")
	}
	return Inspector{store: store, pdfInfoPath: pdfInfoPath}, nil
}

func (i Inspector) InspectStaged(
	ctx context.Context,
	staged ports.StagedObject,
	originalName string,
	declaredMIME string,
) (ports.DocumentInspection, error) {
	if !supportedDocumentMIME(declaredMIME) {
		return ports.DocumentInspection{}, domain.NewRuleError(
			"unsupported_document",
			"仅支持 JPEG、PNG、WebP 和 PDF",
			domain.ErrInvalidInput,
		)
	}
	location, err := i.store.stagePath(staged.ID)
	if err != nil {
		return ports.DocumentInspection{}, err
	}
	file, err := os.Open(location)
	if err != nil {
		return ports.DocumentInspection{}, fmt.Errorf("open staged document: %w", err)
	}
	header := make([]byte, 16)
	read, readErr := file.Read(header)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		file.Close()
		return ports.DocumentInspection{}, fmt.Errorf("read document signature: %w", readErr)
	}
	header = header[:read]
	detectedMIME := detectMIME(header)
	if detectedMIME == "" || detectedMIME != declaredMIME || !extensionMatches(originalName, detectedMIME) {
		file.Close()
		return ports.DocumentInspection{}, domain.NewRuleError(
			"document_signature_mismatch",
			"文件扩展名、声明类型和文件签名不一致",
			domain.ErrInvalidInput,
		)
	}
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		return ports.DocumentInspection{}, fmt.Errorf("rewind staged document: %w", err)
	}
	if detectedMIME == "application/pdf" {
		file.Close()
		return i.inspectPDF(ctx, location)
	}
	config, _, err := image.DecodeConfig(bufio.NewReader(file))
	closeErr := file.Close()
	if err != nil {
		return ports.DocumentInspection{}, domain.NewRuleError("corrupt_document", "图片已损坏或无法解码", domain.ErrInvalidInput)
	}
	if closeErr != nil {
		return ports.DocumentInspection{}, fmt.Errorf("close staged document: %w", closeErr)
	}
	if config.Width < 1 || config.Height < 1 {
		return ports.DocumentInspection{}, domain.NewRuleError("invalid_image_dimensions", "图片尺寸不正确", domain.ErrInvalidInput)
	}
	return ports.DocumentInspection{
		DetectedMIME: detectedMIME,
		PageCount:    1,
		Width:        config.Width,
		Height:       config.Height,
	}, nil
}

func supportedDocumentMIME(value string) bool {
	switch value {
	case "image/jpeg", "image/png", "image/webp", "application/pdf":
		return true
	default:
		return false
	}
}

func (i Inspector) inspectPDF(ctx context.Context, location string) (ports.DocumentInspection, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, i.pdfInfoPath, location)
	output, err := command.Output()
	if commandCtx.Err() != nil {
		return ports.DocumentInspection{}, domain.NewRuleError("pdf_inspection_timeout", "PDF 检查超时", domain.ErrInvalidInput)
	}
	if err != nil {
		return ports.DocumentInspection{}, domain.NewRuleError("corrupt_pdf", "PDF 已损坏、加密或无法读取", domain.ErrInvalidInput)
	}
	properties := parsePDFInfo(output)
	if strings.EqualFold(properties["Encrypted"], "yes") {
		return ports.DocumentInspection{}, domain.NewRuleError("encrypted_pdf", "PDF 已加密，请先解除密码保护", domain.ErrInvalidInput)
	}
	pages, err := strconv.Atoi(properties["Pages"])
	if err != nil || pages < 1 {
		return ports.DocumentInspection{}, domain.NewRuleError("corrupt_pdf", "PDF 页数无法读取", domain.ErrInvalidInput)
	}
	if pages > 20 {
		return ports.DocumentInspection{}, domain.NewRuleError("pdf_page_limit", "PDF 不能超过 20 页", domain.ErrInvalidInput)
	}
	return ports.DocumentInspection{DetectedMIME: "application/pdf", PageCount: pages}, nil
}

func detectMIME(header []byte) string {
	switch {
	case len(header) >= 3 && bytes.Equal(header[:3], []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case len(header) >= 8 && bytes.Equal(header[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png"
	case len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WEBP")):
		return "image/webp"
	case len(header) >= 5 && bytes.Equal(header[:5], []byte("%PDF-")):
		return "application/pdf"
	default:
		return ""
	}
}

func extensionMatches(name, mime string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	switch mime {
	case "image/jpeg":
		return extension == ".jpg" || extension == ".jpeg"
	case "image/png":
		return extension == ".png"
	case "image/webp":
		return extension == ".webp"
	case "application/pdf":
		return extension == ".pdf"
	default:
		return false
	}
}

func parsePDFInfo(output []byte) map[string]string {
	properties := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		properties[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return properties
}
