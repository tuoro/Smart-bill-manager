//go:build cgo

package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"smart-bill-manager/internal/models"
)

func TestFileAccessHTTPContracts(t *testing.T) {
	application := newTestApplication(t)
	admin := setupContractAdmin(t, application)
	member := registerContractMember(t, application, admin.Token)

	uploadsDir := application.uploadsDir
	ownerDir := filepath.Join(uploadsDir, admin.ID)
	if err := os.MkdirAll(ownerDir, 0o755); err != nil {
		t.Fatalf("创建测试上传目录失败: %v", err)
	}
	invoiceContent := []byte("invoice-content")
	attachmentContent := []byte("attachment-content")
	screenshotContent := []byte("screenshot-content")
	writeContractFile(t, filepath.Join(ownerDir, "invoice.pdf"), invoiceContent)
	writeContractFile(t, filepath.Join(ownerDir, "attachment.pdf"), attachmentContent)
	writeContractFile(t, filepath.Join(ownerDir, "screenshot.png"), screenshotContent)

	screenshotPath := filepath.ToSlash(filepath.Join("uploads", admin.ID, "screenshot.png"))
	payment := models.Payment{
		ID:                "payment-file-owner",
		OwnerUserID:       admin.ID,
		Amount:            1.23,
		TransactionTime:   "2026-08-03T00:00:00Z",
		TransactionTimeTs: 1785715200000,
		ScreenshotPath:    &screenshotPath,
		TripAssignSrc:     "auto",
		TripAssignState:   "no_match",
		DedupStatus:       "ok",
	}
	invoice := models.Invoice{
		ID:           "invoice-file-owner",
		OwnerUserID:  admin.ID,
		Filename:     "invoice.pdf",
		OriginalName: "原始发票.pdf",
		FilePath:     filepath.ToSlash(filepath.Join("uploads", admin.ID, "invoice.pdf")),
		ParseStatus:  "success",
		Source:       "upload",
		DedupStatus:  "ok",
	}
	attachment := models.InvoiceAttachment{
		ID:           "attachment-file-owner",
		OwnerUserID:  admin.ID,
		InvoiceID:    invoice.ID,
		Kind:         "attachment",
		Filename:     "attachment.pdf",
		OriginalName: "附件.pdf",
		FilePath:     filepath.ToSlash(filepath.Join("uploads", admin.ID, "attachment.pdf")),
		Source:       "upload",
	}
	if err := application.db.Create(&payment).Error; err != nil {
		t.Fatalf("创建测试支付失败: %v", err)
	}
	if err := application.db.Create(&invoice).Error; err != nil {
		t.Fatalf("创建测试发票失败: %v", err)
	}
	if err := application.db.Create(&attachment).Error; err != nil {
		t.Fatalf("创建测试附件失败: %v", err)
	}

	tests := []struct {
		name        string
		path        string
		contentType string
		content     []byte
	}{
		{name: "发票预览", path: "/api/invoices/" + invoice.ID + "/file", contentType: "application/pdf", content: invoiceContent},
		{name: "发票下载", path: "/api/invoices/" + invoice.ID + "/download", contentType: "application/pdf", content: invoiceContent},
		{name: "附件下载", path: "/api/invoices/" + invoice.ID + "/attachments/" + attachment.ID + "/download", contentType: "application/pdf", content: attachmentContent},
		{name: "支付截图", path: "/api/payments/" + payment.ID + "/screenshot", contentType: "image/png", content: screenshotContent},
	}
	for _, test := range tests {
		t.Run(test.name+"本人可访问", func(t *testing.T) {
			response := performRawContractRequest(t, application, test.path, admin.Token)
			if response.status != http.StatusOK || response.body != string(test.content) {
				t.Fatalf("合法文件响应异常: status=%d body=%q", response.status, response.body)
			}
			if got := response.contentType; !strings.HasPrefix(got, test.contentType) {
				t.Fatalf("Content-Type 应以 %q 开头，实际为 %q", test.contentType, got)
			}
		})

		t.Run(test.name+"跨租户隐藏资源", func(t *testing.T) {
			response := performRawContractRequest(t, application, test.path, member.Token)
			if response.status != http.StatusNotFound || strings.Contains(response.body, "content") {
				t.Fatalf("跨租户文件访问应返回 404 且不泄露内容: status=%d body=%q", response.status, response.body)
			}
		})
	}
}

func TestFileDownloadRejectsPathsOutsideUploadsRoot(t *testing.T) {
	application := newTestApplication(t)
	admin := setupContractAdmin(t, application)

	outsidePath := filepath.Join(t.TempDir(), "outside-secret.pdf")
	outsideContent := "outside-secret-content"
	writeContractFile(t, outsidePath, []byte(outsideContent))

	unsafeInvoice := models.Invoice{
		ID:           "invoice-unsafe-path",
		OwnerUserID:  admin.ID,
		Filename:     "outside-secret.pdf",
		OriginalName: "outside-secret.pdf",
		FilePath:     outsidePath,
		ParseStatus:  "success",
		Source:       "upload",
		DedupStatus:  "ok",
	}
	unsafeAttachment := models.InvoiceAttachment{
		ID:           "attachment-unsafe-path",
		OwnerUserID:  admin.ID,
		InvoiceID:    unsafeInvoice.ID,
		Kind:         "attachment",
		Filename:     "outside-secret.pdf",
		OriginalName: "outside-secret.pdf",
		FilePath:     outsidePath,
		Source:       "upload",
	}
	unsafeScreenshot := outsidePath
	unsafePayment := models.Payment{
		ID:                "payment-unsafe-path",
		OwnerUserID:       admin.ID,
		Amount:            1,
		TransactionTime:   "2026-08-03T00:00:00Z",
		TransactionTimeTs: 1785715200000,
		ScreenshotPath:    &unsafeScreenshot,
		TripAssignSrc:     "auto",
		TripAssignState:   "no_match",
		DedupStatus:       "ok",
	}
	if err := application.db.Create(&unsafeInvoice).Error; err != nil {
		t.Fatalf("创建不安全路径发票失败: %v", err)
	}
	if err := application.db.Create(&unsafeAttachment).Error; err != nil {
		t.Fatalf("创建不安全路径附件失败: %v", err)
	}
	if err := application.db.Create(&unsafePayment).Error; err != nil {
		t.Fatalf("创建不安全路径支付失败: %v", err)
	}

	paths := []string{
		"/api/invoices/" + unsafeInvoice.ID + "/file",
		"/api/invoices/" + unsafeInvoice.ID + "/download",
		"/api/invoices/" + unsafeInvoice.ID + "/attachments/" + unsafeAttachment.ID + "/download",
		"/api/payments/" + unsafePayment.ID + "/screenshot",
	}
	for _, path := range paths {
		response := performRawContractRequest(t, application, path, admin.Token)
		if response.status != http.StatusBadRequest {
			t.Fatalf("外部绝对路径应返回 400: path=%s status=%d body=%q", path, response.status, response.body)
		}
		if strings.Contains(response.body, outsideContent) {
			t.Fatalf("外部路径内容被泄露: path=%s body=%q", path, response.body)
		}
	}

	traversalPath := "uploads/../" + filepath.Base(outsidePath)
	if err := application.db.Model(&unsafeInvoice).Update("file_path", traversalPath).Error; err != nil {
		t.Fatalf("更新目录穿越路径失败: %v", err)
	}
	response := performRawContractRequest(t, application, "/api/invoices/"+unsafeInvoice.ID+"/download", admin.Token)
	if response.status != http.StatusBadRequest || strings.Contains(response.body, outsideContent) {
		t.Fatalf("目录穿越路径未被阻断: status=%d body=%q", response.status, response.body)
	}
}

type rawContractResponse struct {
	status      int
	body        string
	contentType string
}

func performRawContractRequest(t *testing.T, application *Application, path, token string) rawContractResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("创建文件请求失败: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	application.Router.ServeHTTP(recorder, request)
	return rawContractResponse{
		status:      recorder.Code,
		body:        recorder.Body.String(),
		contentType: recorder.Header().Get("Content-Type"),
	}
}

func writeContractFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
}
