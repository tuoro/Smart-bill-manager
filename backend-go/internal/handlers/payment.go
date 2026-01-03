package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type PaymentHandler struct {
	paymentService *services.PaymentService
	taskService    *services.TaskService
	uploadsDir     string
}

func NewPaymentHandler(paymentService *services.PaymentService, taskService *services.TaskService) *PaymentHandler {
	return &PaymentHandler{
		paymentService: paymentService,
		taskService:    taskService,
		uploadsDir:     "", // Will be set if needed
	}
}

func (h *PaymentHandler) SetUploadsDir(uploadsDir string) {
	h.uploadsDir = uploadsDir
}

func (h *PaymentHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.GetAll)
	r.GET("/stats", h.GetStats)
	r.GET("/:id", h.GetByID)
	r.GET("/:id/invoices", h.GetLinkedInvoices)
	r.GET("/:id/suggest-invoices", h.SuggestInvoices)
	r.POST("", h.Create)
	r.POST("/upload-screenshot", h.UploadScreenshot)
	r.POST("/upload-screenshot-async", h.UploadScreenshotAsync)
	r.POST("/upload-screenshot/cancel", h.CancelUploadScreenshot)
	r.POST("/:id/reparse", h.ReparseScreenshot)
	r.PUT("/:id", h.Update)
	r.DELETE("/:id", h.Delete)
}

func (h *PaymentHandler) GetAll(c *gin.Context) {
	var filter services.PaymentFilterInput
	if err := c.ShouldBindQuery(&filter); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	payments, err := h.paymentService.GetAllWithInvoiceCounts(filter)
	if err != nil {
		utils.Error(c, 500, "获取支付记录失败", err)
		return
	}

	utils.SuccessData(c, payments)
}

func (h *PaymentHandler) GetStats(c *gin.Context) {
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")

	stats, err := h.paymentService.GetStats(startDate, endDate)
	if err != nil {
		utils.Error(c, 500, "获取统计数据失败", err)
		return
	}

	utils.SuccessData(c, stats)
}

func (h *PaymentHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	payment, err := h.paymentService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "支付记录不存在", nil)
		return
	}

	utils.SuccessData(c, payment)
}

func (h *PaymentHandler) Create(c *gin.Context) {
	var input services.CreatePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "金额和交易时间是必填项", err)
		return
	}

	payment, err := h.paymentService.Create(input)
	if err != nil {
		utils.Error(c, 500, "创建支付记录失败", err)
		return
	}

	utils.Success(c, 201, "支付记录创建成功", payment)
}

func (h *PaymentHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdatePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.paymentService.Update(id, input); err != nil {
		if de, ok := services.AsDuplicateError(err); ok {
			utils.ErrorData(c, 409, "检测到重复，请确认是否仍要保存", de, err)
			return
		}
		utils.Error(c, 404, "支付记录不存在或更新失败", err)
		return
	}

	utils.Success(c, 200, "支付记录更新成功", nil)
}

func (h *PaymentHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	payment, err := h.paymentService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "支付记录不存在", nil)
		return
	}

	// Remove the screenshot file if present (ignore missing file).
	if payment.ScreenshotPath != nil && *payment.ScreenshotPath != "" {
		if absPath, err := resolveUploadsFilePath(h.uploadsDir, *payment.ScreenshotPath); err == nil {
			if rmErr := os.Remove(absPath); rmErr != nil && !os.IsNotExist(rmErr) {
				utils.Error(c, 500, "删除支付截图文件失败", rmErr)
				return
			}
		}
	}

	if err := h.paymentService.Delete(id); err != nil {
		utils.Error(c, 404, "支付记录不存在", nil)
		return
	}

	utils.Success(c, 200, "支付记录删除成功", nil)
}

func (h *PaymentHandler) UploadScreenshot(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, 400, "请上传文件", err)
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		utils.Error(c, 400, "只支持 JPG、JPEG、PNG 格式的图片", nil)
		return
	}
	if file.Size > 10*1024*1024 {
		utils.Error(c, 400, "文件大小不能超过10MB", nil)
		return
	}

	uploadsDir := h.uploadsDir
	if uploadsDir == "" {
		uploadsDir = "uploads"
	}
	if !filepath.IsAbs(uploadsDir) {
		wd, _ := os.Getwd()
		uploadsDir = filepath.Join(wd, uploadsDir)
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		utils.Error(c, 500, "创建上传目录失败", err)
		return
	}

	filename := utils.GenerateUUID() + ext
	filePath := filepath.Join(uploadsDir, filename)

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

	if existing, err := services.FindPaymentByFileSHA256(fileSHA, ""); err != nil {
		_ = os.Remove(filePath)
		utils.Error(c, 500, "重复检查失败", err)
		return
	} else if existing != nil {
		_ = os.Remove(filePath)
		utils.ErrorData(c, 409, "文件内容重复，已存在记录", gin.H{
			"kind":              "hash_duplicate",
			"entity":            "payment",
			"existing_id":       existing.ID,
			"existing_is_draft": existing.IsDraft,
		}, nil)
		return
	}

	relPath := "uploads/" + filename

	payment, extracted, ocrErr := h.paymentService.CreateFromScreenshot(services.CreateFromScreenshotInput{
		ScreenshotPath: relPath,
		FileSHA256:     &fileSHA,
	})

	dedup := interface{}(nil)
	if payment != nil && payment.DedupStatus == services.DedupStatusSuspected {
		if cands, derr := services.FindPaymentCandidatesByAmountTime(payment.Amount, payment.TransactionTimeTs, payment.ID, 5*time.Minute, 5); derr == nil && len(cands) > 0 {
			dedup = gin.H{
				"kind":       "suspected_duplicate",
				"reason":     "amount_time",
				"candidates": cands,
			}
		}
	}

	if ocrErr != nil {
		// Allow continuing the normal OCR flow even if transaction time is missing/unparseable.
		// Keep the uploaded screenshot so the user can manually correct fields in the UI.
		if errors.Is(ocrErr, services.ErrMissingTransactionTime) {
			if extracted == nil {
				extracted = &services.PaymentExtractedData{RawText: "", PrettyText: ""}
			}
			utils.Success(c, 200, "截图上传成功，但无法识别交易时间，请在下方手动选择交易时间", gin.H{
				"payment":         payment,
				"extracted":       extracted,
				"screenshot_path": relPath,
				"ocr_error":       "missing transaction time",
				"dedup":           dedup,
			})
			return
		}

		_ = os.Remove(filePath)
		utils.Error(c, 500, "识别支付截图失败", ocrErr)
		return
	}

	utils.Success(c, 201, "支付截图上传成功", gin.H{
		"payment":         payment,
		"extracted":       extracted,
		"screenshot_path": relPath,
		"dedup":           dedup,
	})
}

func (h *PaymentHandler) UploadScreenshotAsync(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, 400, "请上传文件", err)
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		utils.Error(c, 400, "只支持 JPG、JPEG、PNG 格式的图片", nil)
		return
	}
	if file.Size > 10*1024*1024 {
		utils.Error(c, 400, "文件大小不能超过10MB", nil)
		return
	}

	uploadsDir := h.uploadsDir
	if uploadsDir == "" {
		uploadsDir = "uploads"
	}
	if !filepath.IsAbs(uploadsDir) {
		wd, _ := os.Getwd()
		uploadsDir = filepath.Join(wd, uploadsDir)
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		utils.Error(c, 500, "创建上传目录失败", err)
		return
	}

	filename := utils.GenerateUUID() + ext
	filePath := filepath.Join(uploadsDir, filename)

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

	if existing, err := services.FindPaymentByFileSHA256(fileSHA, ""); err != nil {
		_ = os.Remove(filePath)
		utils.Error(c, 500, "重复检查失败", err)
		return
	} else if existing != nil {
		_ = os.Remove(filePath)
		utils.ErrorData(c, 409, "文件内容重复，已存在记录", gin.H{
			"kind":              "hash_duplicate",
			"entity":            "payment",
			"existing_id":       existing.ID,
			"existing_is_draft": existing.IsDraft,
		}, nil)
		return
	}

	relPath := "uploads/" + filename
	userID := middleware.GetUserID(c)
	if userID == "" {
		_ = os.Remove(filePath)
		utils.Error(c, 401, "未授权，请先登录", nil)
		return
	}

	payment, err := h.paymentService.CreateDraftFromScreenshotUpload(relPath, &fileSHA)
	if err != nil {
		_ = os.Remove(filePath)
		utils.Error(c, 500, "创建草稿失败", err)
		return
	}

	task, err := h.taskService.CreateTask(services.TaskTypePaymentOCR, userID, payment.ID, &fileSHA)
	if err != nil {
		utils.Error(c, 500, "创建识别任务失败", err)
		return
	}

	utils.Success(c, 201, "截图上传成功，正在识别", gin.H{
		"taskId":          task.ID,
		"payment":         payment,
		"screenshot_path": relPath,
	})
}

func (h *PaymentHandler) CancelUploadScreenshot(c *gin.Context) {
	var input struct {
		ScreenshotPath string `json:"screenshot_path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	absPath, err := resolveUploadsFilePath(h.uploadsDir, input.ScreenshotPath)
	if err != nil {
		utils.Error(c, 400, "截图路径错误", err)
		return
	}

	if rmErr := os.Remove(absPath); rmErr != nil && !os.IsNotExist(rmErr) {
		utils.Error(c, 500, "删除截图文件失败", rmErr)
		return
	}

	// Best-effort: remove any leftover draft row that references this screenshot.
	// This helps when the frontend loses the draft payment id but still wants to discard the upload.
	if h.paymentService != nil {
		_ = h.paymentService.DeleteDraftByScreenshotPath(strings.TrimSpace(input.ScreenshotPath))
	}

	utils.Success(c, 200, "已取消上传", nil)
}

func (h *PaymentHandler) GetLinkedInvoices(c *gin.Context) {
	id := c.Param("id")
	invoices, err := h.paymentService.GetLinkedInvoices(id)
	if err != nil {
		utils.Error(c, 500, "获取关联发票失败", err)
		return
	}

	utils.SuccessData(c, invoices)
}

func (h *PaymentHandler) SuggestInvoices(c *gin.Context) {
	id := c.Param("id")
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	debug := c.Query("debug") == "1" || c.Query("debug") == "true"

	log.Printf("[MATCH] suggest-invoices payment_id=%s limit=%d debug=%t", id, limit, debug)

	invoices, err := h.paymentService.SuggestInvoices(id, limit, debug)
	if err != nil {
		utils.Error(c, 500, "获取建议发票失败", err)
		return
	}

	log.Printf("[MATCH] suggest-invoices payment_id=%s -> %d results", id, len(invoices))
	utils.SuccessData(c, invoices)
}

func (h *PaymentHandler) ReparseScreenshot(c *gin.Context) {
	id := c.Param("id")

	payment, err := h.paymentService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "支付记录不存在", nil)
		return
	}
	if payment.ScreenshotPath == nil || *payment.ScreenshotPath == "" {
		utils.Error(c, 400, "该支付记录没有截图", nil)
		return
	}

	extracted, err := h.paymentService.ReparseScreenshot(id)
	if err != nil {
		utils.Error(c, 500, "重新解析失败", err)
		return
	}

	utils.Success(c, 200, "重新解析成功", extracted)
}
