package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

func contentTypeFromInvoiceFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}

func isAllowedInvoiceExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".pdf", ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

type InvoiceHandler struct {
	invoiceService *services.InvoiceService
	uploadsDir     string
}

func NewInvoiceHandler(invoiceService *services.InvoiceService, uploadsDir string) *InvoiceHandler {
	return &InvoiceHandler{
		invoiceService: invoiceService,
		uploadsDir:     uploadsDir,
	}
}

func (h *InvoiceHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.GetAll)
	r.GET("/stats", h.GetStats)
	r.GET("/:id", h.GetByID)
	r.GET("/:id/download", h.Download)
	r.GET("/:id/linked-payments", h.GetLinkedPayments)
	r.GET("/:id/suggest-payments", h.SuggestPayments)
	r.GET("/payment/:paymentId", h.GetByPaymentID)
	r.POST("/upload", h.Upload)
	r.POST("/upload-multiple", h.UploadMultiple)
	r.POST("/:id/link-payment", h.LinkPayment)
	r.POST("/:id/parse", h.Parse)
	r.PUT("/:id", h.Update)
	r.DELETE("/:id", h.Delete)
	r.DELETE("/:id/unlink-payment", h.UnlinkPayment)
}

func (h *InvoiceHandler) GetAll(c *gin.Context) {
	var filter services.InvoiceFilterInput
	if err := c.ShouldBindQuery(&filter); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	invoices, err := h.invoiceService.GetAll(filter)
	if err != nil {
		utils.Error(c, 500, "获取发票列表失败", err)
		return
	}

	utils.SuccessData(c, invoices)
}

func (h *InvoiceHandler) GetStats(c *gin.Context) {
	stats, err := h.invoiceService.GetStats()
	if err != nil {
		utils.Error(c, 500, "获取统计数据失败", err)
		return
	}

	utils.SuccessData(c, stats)
}

func (h *InvoiceHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	invoice, err := h.invoiceService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "发票不存在", nil)
		return
	}

	utils.SuccessData(c, invoice)
}

func (h *InvoiceHandler) Download(c *gin.Context) {
	id := c.Param("id")
	invoice, err := h.invoiceService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "发票不存在", nil)
		return
	}

	filePath := invoice.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(h.uploadsDir, "..", filePath)
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		utils.Error(c, 404, "文件不存在", nil)
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+invoice.OriginalName)
	c.Header("Content-Type", contentTypeFromInvoiceFilename(invoice.Filename))
	c.File(filePath)
}

func (h *InvoiceHandler) GetByPaymentID(c *gin.Context) {
	paymentID := c.Param("paymentId")
	invoices, err := h.invoiceService.GetByPaymentID(paymentID)
	if err != nil {
		utils.Error(c, 500, "获取发票失败", err)
		return
	}

	utils.SuccessData(c, invoices)
}

func (h *InvoiceHandler) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, 400, "请上传文件", err)
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !isAllowedInvoiceExt(ext) {
		utils.Error(c, 400, "只支持 PDF 或图片格式（PNG/JPG）", nil)
		return
	}

	if file.Size > 20*1024*1024 {
		utils.Error(c, 400, "文件大小不能超过20MB", nil)
		return
	}

	if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
		utils.Error(c, 500, "创建上传目录失败", err)
		return
	}

	filename := utils.GenerateUUID() + ext
	filePath := filepath.Join(h.uploadsDir, filename)

	src, err := file.Open()
	if err != nil {
		utils.Error(c, 500, "打开上传文件失败", err)
		return
	}
	defer src.Close()

	dst, err := os.Create(filePath)
	if err != nil {
		utils.Error(c, 500, "保存文件失败", err)
		return
	}
	defer dst.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, hasher), src); err != nil {
		_ = os.Remove(filePath)
		utils.Error(c, 500, "保存文件失败", err)
		return
	}
	fileSHA := hex.EncodeToString(hasher.Sum(nil))

	if existing, err := services.FindInvoiceByFileSHA256(fileSHA, ""); err != nil {
		_ = os.Remove(filePath)
		utils.Error(c, 500, "重复检查失败", err)
		return
	} else if existing != nil {
		_ = os.Remove(filePath)
		utils.ErrorData(c, 409, "文件内容重复，已存在记录", gin.H{
			"kind":              "hash_duplicate",
			"entity":            "invoice",
			"existing_id":       existing.ID,
			"existing_is_draft": existing.IsDraft,
		}, nil)
		return
	}

	var paymentID *string
	if pid := c.PostForm("payment_id"); pid != "" {
		paymentID = &pid
	}

	invoice, err := h.invoiceService.Create(services.CreateInvoiceInput{
		PaymentID:    paymentID,
		Filename:     filename,
		OriginalName: file.Filename,
		FilePath:     "uploads/" + filename,
		FileSize:     file.Size,
		FileSHA256:   &fileSHA,
		Source:       "upload",
		IsDraft:      true,
	})
	if err != nil {
		_ = os.Remove(filePath)
		utils.Error(c, 500, "上传发票失败", err)
		return
	}

	dedup := interface{}(nil)
	if invoice != nil && invoice.DedupStatus == services.DedupStatusSuspected && invoice.InvoiceNumber != nil {
		no := strings.TrimSpace(*invoice.InvoiceNumber)
		if no != "" {
			if cands, derr := services.FindInvoiceCandidatesByInvoiceNumber(no, invoice.ID, 5); derr == nil && len(cands) > 0 {
				dedup = gin.H{
					"kind":       "suspected_duplicate",
					"reason":     "invoice_number",
					"candidates": cands,
				}
			}
		}
	}

	utils.Success(c, 201, "发票上传成功", gin.H{
		"invoice": invoice,
		"dedup":   dedup,
	})
}

func (h *InvoiceHandler) UploadMultiple(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		utils.Error(c, 400, "请上传文件", err)
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		utils.Error(c, 400, "请上传文件", nil)
		return
	}
	if len(files) > 10 {
		utils.Error(c, 400, "最多同时上传 10 个文件", nil)
		return
	}

	if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
		utils.Error(c, 500, "创建上传目录失败", err)
		return
	}

	var paymentID *string
	if pid := c.PostForm("payment_id"); pid != "" {
		paymentID = &pid
	}

	invoices := make([]interface{}, 0, len(files))
	skippedDuplicates := 0

	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if !isAllowedInvoiceExt(ext) {
			continue
		}
		if file.Size > 20*1024*1024 {
			continue
		}

		filename := utils.GenerateUUID() + ext
		filePath := filepath.Join(h.uploadsDir, filename)

		src, err := file.Open()
		if err != nil {
			continue
		}
		func() {
			defer src.Close()

			dst, err := os.Create(filePath)
			if err != nil {
				return
			}
			defer dst.Close()

			hasher := sha256.New()
			if _, err := io.Copy(io.MultiWriter(dst, hasher), src); err != nil {
				_ = os.Remove(filePath)
				return
			}
			fileSHA := hex.EncodeToString(hasher.Sum(nil))

			if existing, err := services.FindInvoiceByFileSHA256(fileSHA, ""); err != nil {
				_ = os.Remove(filePath)
				return
			} else if existing != nil {
				skippedDuplicates++
				_ = os.Remove(filePath)
				return
			}

			invoice, err := h.invoiceService.Create(services.CreateInvoiceInput{
				PaymentID:    paymentID,
				Filename:     filename,
				OriginalName: file.Filename,
				FilePath:     "uploads/" + filename,
				FileSize:     file.Size,
				FileSHA256:   &fileSHA,
				Source:       "upload",
				IsDraft:      true,
			})
			if err != nil {
				_ = os.Remove(filePath)
				return
			}

			invoices = append(invoices, invoice)
		}()
	}

	msg := fmt.Sprintf("成功上传 %d 个发票", len(invoices))
	if skippedDuplicates > 0 {
		msg = fmt.Sprintf("%s，跳过 %d 个重复文件", msg, skippedDuplicates)
	}
	utils.Success(c, 201, msg, invoices)
}

func (h *InvoiceHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdateInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.invoiceService.Update(id, input); err != nil {
		if de, ok := services.AsDuplicateError(err); ok {
			utils.ErrorData(c, 409, "检测到重复，请确认是否仍要保存", de, err)
			return
		}
		utils.Error(c, 404, "发票不存在或更新失败", err)
		return
	}

	utils.Success(c, 200, "发票更新成功", nil)
}

func (h *InvoiceHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.invoiceService.Delete(id); err != nil {
		utils.Error(c, 404, "发票不存在", nil)
		return
	}

	utils.Success(c, 200, "发票删除成功", nil)
}

func (h *InvoiceHandler) LinkPayment(c *gin.Context) {
	id := c.Param("id")

	var input struct {
		PaymentID string `json:"payment_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.invoiceService.LinkPayment(id, input.PaymentID); err != nil {
		utils.Error(c, 500, "关联支付记录失败", err)
		return
	}

	utils.Success(c, 200, "关联支付记录成功", nil)
}

func (h *InvoiceHandler) UnlinkPayment(c *gin.Context) {
	id := c.Param("id")
	paymentID := c.Query("payment_id")

	if paymentID == "" {
		utils.Error(c, 400, "缺少 payment_id 参数", nil)
		return
	}

	if err := h.invoiceService.UnlinkPayment(id, paymentID); err != nil {
		utils.Error(c, 500, "取消关联失败", err)
		return
	}

	utils.Success(c, 200, "取消关联成功", nil)
}

func (h *InvoiceHandler) GetLinkedPayments(c *gin.Context) {
	id := c.Param("id")

	payments, err := h.invoiceService.GetLinkedPayments(id)
	if err != nil {
		utils.Error(c, 500, "获取关联支付记录失败", err)
		return
	}

	utils.SuccessData(c, payments)
}

func (h *InvoiceHandler) SuggestPayments(c *gin.Context) {
	id := c.Param("id")
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	debug := c.Query("debug") == "1" || c.Query("debug") == "true"

	log.Printf("[MATCH] suggest-payments invoice_id=%s limit=%d debug=%t", id, limit, debug)

	payments, err := h.invoiceService.SuggestPayments(id, limit, debug)
	if err != nil {
		utils.Error(c, 500, "获取建议支付记录失败", err)
		return
	}

	log.Printf("[MATCH] suggest-payments invoice_id=%s -> %d results", id, len(payments))
	utils.SuccessData(c, payments)
}

func (h *InvoiceHandler) Parse(c *gin.Context) {
	id := c.Param("id")

	invoice, err := h.invoiceService.Reparse(id)
	if err != nil {
		utils.Error(c, 500, "解析发票失败", err)
		return
	}

	utils.Success(c, 200, "发票解析完成", invoice)
}
