package httpapi

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// 两种上传共享文件上限；辅助上传只额外允许明确列出的少量文本字段。
func readDocumentMultipart(response http.ResponseWriter, request *http.Request, allowed ...string) (documents.UploadInput, map[string]string, error) {
	var file documents.UploadInput
	fields := make(map[string]string, len(allowed))
	permitted := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		permitted[name] = true
	}
	request.Body = http.MaxBytesReader(response, request.Body, multipartOverheadLimit)
	reader, err := request.MultipartReader()
	if err != nil {
		return file, nil, domain.ErrInvalidInput
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return file, nil, domain.ErrInvalidInput
		}
		name := part.FormName()
		if name == "file" {
			if file.Source != nil || part.FileName() == "" {
				part.Close()
				return file, nil, domain.ErrInvalidInput
			}
			content, err := readMultipartPart(part, ports.MaxDocumentBytes)
			if err != nil {
				return file, nil, err
			}
			file = documents.UploadInput{Name: part.FileName(), MIME: part.Header.Get("Content-Type"), Source: bytes.NewReader(content)}
			continue
		}
		_, duplicate := fields[name]
		if !permitted[name] || duplicate || part.FileName() != "" {
			part.Close()
			return file, nil, domain.ErrInvalidInput
		}
		content, err := readMultipartPart(part, 4096)
		if err != nil {
			return file, nil, err
		}
		fields[name] = string(content)
	}
	if file.Source == nil || len(fields) != len(permitted) {
		return file, nil, domain.ErrInvalidInput
	}
	return file, fields, nil
}

func readMultipartPart(part *multipart.Part, limit int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(part, limit+1))
	if err = errors.Join(err, part.Close()); err != nil {
		return nil, domain.ErrInvalidInput
	}
	if int64(len(content)) > limit {
		if limit == ports.MaxDocumentBytes {
			return nil, domain.ErrPayloadTooLarge
		}
		return nil, domain.ErrInvalidInput
	}
	return content, nil
}
