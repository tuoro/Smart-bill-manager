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
	Name string `json:"name"`
}

type bulkDeleteInput struct {
	IDs []string `json:"ids"`
}

func (h *AdminRegressionSamplesHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("/payments/:id", h.MarkPayment)
	r.POST("/invoices/:id", h.MarkInvoice)
	r.GET("", h.List)
	r.POST("/sync", h.SyncFromRepo)
	r.GET("/export", h.Export)
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
	sample, err := h.svc.CreateOrUpdateFromPayment(id, adminID, input.Name)
	if err != nil {
		if err == services.ErrNotFound {
			utils.Error(c, 404, "支付记录不存在", err)
			return
		}
		utils.Error(c, 400, "标记回归样本失败", err)
		return
	}
	utils.SuccessData(c, sample)
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
	sample, err := h.svc.CreateOrUpdateFromInvoice(id, adminID, input.Name)
	if err != nil {
		if err == services.ErrNotFound {
			utils.Error(c, 404, "发票不存在", err)
			return
		}
		utils.Error(c, 400, "标记回归样本失败", err)
		return
	}
	utils.SuccessData(c, sample)
}

func (h *AdminRegressionSamplesHandler) List(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
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
	b, filename, err := h.svc.ExportZip(kind)
	if err != nil {
		utils.Error(c, 400, "导出失败", err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(200, "application/zip", b)
}

func (h *AdminRegressionSamplesHandler) SyncFromRepo(c *gin.Context) {
	mode := strings.TrimSpace(c.Query("mode"))
	syncMode := services.RepoSyncModeRepoOnly
	if mode != "" {
		switch mode {
		case string(services.RepoSyncModeRepoOnly):
			syncMode = services.RepoSyncModeRepoOnly
		case string(services.RepoSyncModeOverwrite):
			syncMode = services.RepoSyncModeOverwrite
		default:
			utils.Error(c, 400, "mode 仅支持 repo_only/overwrite", nil)
			return
		}
	}

	res, err := h.svc.SyncFromRepoDir(syncMode)
	if err != nil {
		utils.Error(c, 400, "同步失败", err)
		return
	}
	utils.SuccessData(c, res)
}
