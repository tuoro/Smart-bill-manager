package localstorage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	imagedraw "image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"golang.org/x/image/draw"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const normalizedLongestEdge = 8000

type Normalizer struct {
	store        *Store
	pdfToPPMPath string
}

func NewNormalizer(store *Store, pdfToPPMPath string) (Normalizer, error) {
	if store == nil {
		return Normalizer{}, errors.New("object store is required")
	}
	if pdfToPPMPath == "" {
		return Normalizer{}, errors.New("pdftoppm path is required")
	}
	return Normalizer{store: store, pdfToPPMPath: pdfToPPMPath}, nil
}

func (n Normalizer) Normalize(ctx context.Context, document ports.ProcessingDocument) ([]ports.NormalizedPage, error) {
	location, err := n.store.objectPath(document.StorageKey)
	if err != nil {
		return nil, err
	}
	var sourceFiles []string
	var cleanup func()
	if document.MIME == "application/pdf" {
		sourceFiles, cleanup, err = n.renderPDF(ctx, location, document.PageCount)
		if err != nil {
			return nil, err
		}
		defer cleanup()
	} else {
		sourceFiles = []string{location}
	}
	pages := make([]ports.NormalizedPage, 0, len(sourceFiles))
	for index, sourceFile := range sourceFiles {
		page, err := n.normalizeImage(ctx, document, index+1, sourceFile)
		if err != nil {
			_ = n.DeleteNormalized(context.WithoutCancel(ctx), pages)
			return nil, err
		}
		pages = append(pages, page)
	}
	if len(pages) != document.PageCount {
		_ = n.DeleteNormalized(context.WithoutCancel(ctx), pages)
		return nil, domain.NewRuleError("pdf_page_count_changed", "PDF 渲染页数与上传检查不一致", domain.ErrInvalidInput)
	}
	return pages, nil
}

func (n Normalizer) DeleteNormalized(ctx context.Context, pages []ports.NormalizedPage) error {
	var joined error
	for _, page := range pages {
		joined = errors.Join(joined, n.store.Delete(ctx, page.StorageKey))
	}
	return joined
}

func (n Normalizer) normalizeImage(
	ctx context.Context,
	document ports.ProcessingDocument,
	pageNumber int,
	sourceFile string,
) (ports.NormalizedPage, error) {
	file, err := os.Open(sourceFile)
	if err != nil {
		return ports.NormalizedPage{}, fmt.Errorf("open source page: %w", err)
	}
	decoded, _, err := image.Decode(file)
	closeErr := file.Close()
	if err != nil {
		return ports.NormalizedPage{}, domain.NewRuleError("document_quality_insufficient", "文档页面无法解码", domain.ErrInvalidInput)
	}
	if closeErr != nil {
		return ports.NormalizedPage{}, fmt.Errorf("close source page: %w", closeErr)
	}
	decoded = resizeImage(decoded, normalizedLongestEdge)
	providerImage := toEightBitRGBA(decoded)
	bounds := providerImage.Bounds()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, providerImage); err != nil {
		return ports.NormalizedPage{}, fmt.Errorf("encode normalized page: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return ports.NormalizedPage{}, err
	}
	staged, err := n.store.Stage(ctx, bytes.NewReader(encoded.Bytes()), ports.MaxDerivedPageBytes)
	if err != nil {
		return ports.NormalizedPage{}, err
	}
	storageKey := fmt.Sprintf(
		"tenants/%s/documents/%s/pages/%04d.png",
		document.TenantID,
		document.DocumentID,
		pageNumber,
	)
	if err := n.store.Commit(ctx, staged, storageKey); err != nil {
		_ = n.store.Abort(context.WithoutCancel(ctx), staged)
		return ports.NormalizedPage{}, err
	}
	hash := sha256.Sum256(encoded.Bytes())
	return ports.NormalizedPage{
		PageImage: ports.PageImage{
			PageNumber: pageNumber,
			MIME:       "image/png",
			Data:       bytes.Clone(encoded.Bytes()),
			SHA256:     hex.EncodeToString(hash[:]),
		},
		StorageKey: storageKey,
		Width:      bounds.Dx(),
		Height:     bounds.Dy(),
	}, nil
}

func toEightBitRGBA(source image.Image) *image.RGBA {
	// Go 会把 JPEG 解码为 YCbCr；直接交给 PNG 编码器会生成 16 位 RGB，
	// 部分兼容接口虽返回成功却无法读取像素。模型输入先收敛到 8 位 RGBA 像素缓冲再编码。
	bounds := source.Bounds()
	destination := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	imagedraw.Draw(destination, destination.Bounds(), source, bounds.Min, imagedraw.Src)
	return destination
}

func (n Normalizer) renderPDF(ctx context.Context, location string, expectedPages int) ([]string, func(), error) {
	temporary, err := os.MkdirTemp(filepath.Join(n.store.root, "staging"), "pdf-render-")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create PDF render directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(temporary) }
	prefix := filepath.Join(temporary, "page")
	command := exec.CommandContext(
		ctx,
		n.pdfToPPMPath,
		"-f", "1",
		"-l", fmt.Sprintf("%d", expectedPages),
		"-r", "144",
		"-png",
		location,
		prefix,
	)
	if output, err := command.CombinedOutput(); err != nil {
		cleanup()
		if ctx.Err() != nil {
			return nil, func() {}, ctx.Err()
		}
		_ = output
		return nil, func() {}, domain.NewRuleError("corrupt_pdf", "PDF 页面无法渲染", domain.ErrInvalidInput)
	}
	files, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("list rendered PDF pages: %w", err)
	}
	sort.Strings(files)
	if len(files) != expectedPages {
		cleanup()
		return nil, func() {}, domain.NewRuleError("pdf_page_count_changed", "PDF 渲染页数与上传检查不一致", domain.ErrInvalidInput)
	}
	return files, cleanup, nil
}

func resizeImage(source image.Image, longestEdge int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= longestEdge && height <= longestEdge {
		return source
	}
	newWidth, newHeight := width, height
	if width >= height {
		newWidth = longestEdge
		newHeight = max(1, height*longestEdge/width)
	} else {
		newHeight = longestEdge
		newWidth = max(1, width*longestEdge/height)
	}
	destination := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.CatmullRom.Scale(destination, destination.Bounds(), source, bounds, draw.Over, nil)
	return destination
}
