package handlers

import (
	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type PaymentHandler struct {
	paymentService *services.PaymentService
}

func NewPaymentHandler(paymentService *services.PaymentService) *PaymentHandler {
	return &PaymentHandler{paymentService: paymentService}
}

func (h *PaymentHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.GetAll)
	r.GET("/stats", h.GetStats)
	r.GET("/:id", h.GetByID)
	r.POST("", h.Create)
	r.PUT("/:id", h.Update)
	r.DELETE("/:id", h.Delete)
}

func (h *PaymentHandler) GetAll(c *gin.Context) {
	var filter services.PaymentFilterInput
	if err := c.ShouldBindQuery(&filter); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	payments, err := h.paymentService.GetAll(filter)
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
		utils.Error(c, 404, "支付记录不存在或更新失败", err)
		return
	}

	utils.Success(c, 200, "支付记录更新成功", nil)
}

func (h *PaymentHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.paymentService.Delete(id); err != nil {
		utils.Error(c, 404, "支付记录不存在", nil)
		return
	}

	utils.Success(c, 200, "支付记录删除成功", nil)
}
