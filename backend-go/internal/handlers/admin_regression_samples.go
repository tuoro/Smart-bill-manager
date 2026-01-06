package handlers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type AdminRegressionSamplesHandler struct {
	svc *services.RegressionSampleService
}

func NewAdminRegressionSamplesHandler(svc *services.RegressionSampleService) *AdminRegressionSamplesHandler {
	return &AdminRegressionSamplesHandler{svc: svc}
}

type markSampleInput struct {
	Name  string `json:"name"`
	Force bool   `json:"force"`
}

type bulkDeleteInput struct {
	IDs []string `json:"ids"`
}

type exportSamplesInput struct {
	IDs    []string `json:"ids"`
	Kind   string   `json:"kind"`
	Origin string   `json:"origin"` // ui | repo
	Redact bool     `json:"redact"`
}

func (h *AdminRegressionSamplesHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("/payments/:id", h.MarkPayment)
	r.POST("/invoices/:id", h.MarkInvoice)
	r.GET("", h.List)
	r.GET("/export", h.Export)
	r.POST("/export", h.ExportSelected)
	r.DELETE("/:id", h.Delete)
	r.POST("/bulk-delete", h.BulkDelete)
}

func (h *AdminRegressionSamplesHandler) MarkPayment(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		utils.Error(c, 400, "缺少 id", nil)
		return
	}

	var input markSampleInput
	_ = c.ShouldBindJSON(&input)

	adminID := middleware.GetUserID(c)
	sample, issues, err := h.svc.CreateOrUpdateFromPayment(id, adminID, input.Name, input.Force)
	if err != nil {
		if qerr, ok := err.(*services.SampleQualityError); ok {
			utils.ErrorData(c, 422, "样本质量检查未通过", gin.H{
				"issues":    qerr.Issues,
				"can_force": true,
			}, err)
			return
		}
		if err == services.ErrNotFound {
			utils.Error(c, 404, "支付记录不存在", err)
			return
		}
		utils.Error(c, 400, "标记回归样本失败", err)
		return
	}
	utils.SuccessData(c, gin.H{"sample": sample, "issues": issues})
}

func (h *AdminRegressionSamplesHandler) MarkInvoice(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		utils.Error(c, 400, "缺少 id", nil)
		return
	}

	var input markSampleInput
	_ = c.ShouldBindJSON(&input)

	adminID := middleware.GetUserID(c)
	sample, issues, err := h.svc.CreateOrUpdateFromInvoice(id, adminID, input.Name, input.Force)
	if err != nil {
		if qerr, ok := err.(*services.SampleQualityError); ok {
			utils.ErrorData(c, 422, "样本质量检查未通过", gin.H{
				"issues":    qerr.Issues,
				"can_force": true,
			}, err)
			return
		}
		if err == services.ErrNotFound {
			utils.Error(c, 404, "发票不存在", err)
			return
		}
		utils.Error(c, 400, "标记回归样本失败", err)
		return
	}
	utils.SuccessData(c, gin.H{"sample": sample, "issues": issues})
}

func (h *AdminRegressionSamplesHandler) List(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	origin := strings.TrimSpace(c.Query("origin"))
	search := strings.TrimSpace(c.Query("search"))

	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	items, total, err := h.svc.List(services.ListRegressionSamplesParams{
		Kind:   kind,
		Origin: origin,
		Search: search,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		utils.Error(c, 500, "获取回归样本失败", err)
		return
	}

	utils.SuccessData(c, gin.H{
		"items": items,
		"total": total,
	})
}

func (h *AdminRegressionSamplesHandler) Delete(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		utils.Error(c, 400, "缺少 id", nil)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		if err == services.ErrSampleNotFound {
			utils.Error(c, 404, "回归样本不存在", err)
			return
		}
		utils.Error(c, 500, "删除回归样本失败", err)
		return
	}
	utils.SuccessData(c, gin.H{"deleted": true})
}

func (h *AdminRegressionSamplesHandler) BulkDelete(c *gin.Context) {
	var input bulkDeleteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}
	deleted, err := h.svc.BulkDelete(input.IDs)
	if err != nil {
		utils.Error(c, 500, "批量删除失败", err)
		return
	}
	utils.SuccessData(c, gin.H{"deleted": deleted})
}

func (h *AdminRegressionSamplesHandler) Export(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	origin := strings.TrimSpace(c.Query("origin"))
	redact := strings.TrimSpace(c.Query("redact")) == "1"

	release, err := services.AcquireZipExport(c.Request.Context())
	if err != nil {
		utils.Error(c, 429, "导出服务繁忙，请稍后重试", err)
		return
	}
	defer release()

	plan, err := h.svc.PrepareExportZip(c.Request.Context(), services.ExportRegressionSamplesParams{
		Kind:   kind,
		Origin: origin,
		Redact: redact,
	})
	if err != nil {
		utils.Error(c, 400, "导出失败", err)
		return
	}
	filename := strings.ReplaceAll(plan.Filename, "\n", "")
	filename = strings.ReplaceAll(filename, "\r", "")
	filename = strings.ReplaceAll(filename, "\"", "")
	if strings.TrimSpace(filename) == "" {
		filename = "regression_samples.zip"
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Status(200)
	_ = plan.Write(c.Writer)
}

func (h *AdminRegressionSamplesHandler) ExportSelected(c *gin.Context) {
	var input exportSamplesInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	release, err := services.AcquireZipExport(c.Request.Context())
	if err != nil {
		utils.Error(c, 429, "导出服务繁忙，请稍后重试", err)
		return
	}
	defer release()

	plan, err := h.svc.PrepareExportZip(c.Request.Context(), services.ExportRegressionSamplesParams{
		Kind:   strings.TrimSpace(input.Kind),
		Origin: strings.TrimSpace(input.Origin),
		IDs:    input.IDs,
		Redact: input.Redact,
	})
	if err != nil {
		utils.Error(c, 400, "导出失败", err)
		return
	}
	filename := strings.ReplaceAll(plan.Filename, "\n", "")
	filename = strings.ReplaceAll(filename, "\r", "")
	filename = strings.ReplaceAll(filename, "\"", "")
	if strings.TrimSpace(filename) == "" {
		filename = "regression_samples.zip"
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Status(200)
	_ = plan.Write(c.Writer)
}
