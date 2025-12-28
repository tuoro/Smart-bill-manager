package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type TripHandler struct {
	tripService *services.TripService
}

func NewTripHandler(tripService *services.TripService) *TripHandler {
	return &TripHandler{tripService: tripService}
}

func (h *TripHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.GetAll)
	r.POST("", h.Create)
	r.GET("/:id", h.GetByID)
	r.PUT("/:id", h.Update)
	r.GET("/:id/summary", h.GetSummary)
	r.GET("/:id/payments", h.GetPayments)
	r.POST("/:id/assign-by-time", h.AssignByTime)
	r.GET("/:id/cascade-preview", h.CascadePreview)
	r.DELETE("/:id", h.DeleteCascade)
}

func (h *TripHandler) GetAll(c *gin.Context) {
	trips, err := h.tripService.GetAll()
	if err != nil {
		utils.Error(c, 500, "获取行程失败", err)
		return
	}
	utils.SuccessData(c, trips)
}

func (h *TripHandler) Create(c *gin.Context) {
	var input services.CreateTripInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}
	trip, err := h.tripService.Create(input)
	if err != nil {
		utils.Error(c, 400, "创建行程失败", err)
		return
	}
	utils.Success(c, 201, "行程创建成功", trip)
}

func (h *TripHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	trip, err := h.tripService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "行程不存在", err)
		return
	}
	utils.SuccessData(c, trip)
}

func (h *TripHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdateTripInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}
	if err := h.tripService.Update(id, input); err != nil {
		utils.Error(c, 400, "更新行程失败", err)
		return
	}
	utils.Success(c, 200, "行程更新成功", nil)
}

func (h *TripHandler) GetSummary(c *gin.Context) {
	id := c.Param("id")
	summary, err := h.tripService.GetSummary(id)
	if err != nil {
		utils.Error(c, 500, "获取统计失败", err)
		return
	}
	utils.SuccessData(c, summary)
}

func (h *TripHandler) GetPayments(c *gin.Context) {
	id := c.Param("id")
	includeInvoices := c.Query("includeInvoices") == "1" || c.Query("includeInvoices") == "true"
	payments, err := h.tripService.GetPayments(id, includeInvoices)
	if err != nil {
		utils.Error(c, 500, "获取支付记录失败", err)
		return
	}
	utils.SuccessData(c, payments)
}

func (h *TripHandler) AssignByTime(c *gin.Context) {
	id := c.Param("id")
	var input services.AssignByTimeInput
	_ = c.ShouldBindJSON(&input)

	out, err := h.tripService.AssignPaymentsByTime(id, input)
	if err != nil {
		utils.Error(c, 400, "同步失败", err)
		return
	}
	utils.SuccessData(c, out)
}

func (h *TripHandler) CascadePreview(c *gin.Context) {
	id := c.Param("id")
	out, _, _, err := h.tripService.GetCascadePreview(id)
	if err != nil {
		utils.Error(c, 500, "获取预览失败", err)
		return
	}
	utils.SuccessData(c, out)
}

func (h *TripHandler) DeleteCascade(c *gin.Context) {
	id := c.Param("id")
	dryRun := c.Query("dryRun")
	if dryRun == "1" || dryRun == "true" {
		out, _, _, err := h.tripService.GetCascadePreview(id)
		if err != nil {
			utils.Error(c, 500, "获取预览失败", err)
			return
		}
		utils.SuccessData(c, out)
		return
	}

	// Optional extra safety: require confirmation flag for API callers.
	if v := c.Query("confirm"); v != "" {
		if ok, _ := strconv.ParseBool(v); !ok {
			utils.Error(c, 400, "需要确认删除", nil)
			return
		}
	}

	out, err := h.tripService.DeleteCascade(id)
	if err != nil {
		utils.Error(c, 500, "删除行程失败", err)
		return
	}
	utils.Success(c, 200, "行程已删除", out)
}
