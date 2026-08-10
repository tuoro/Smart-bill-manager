package handlers

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"

	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

const dashboardRecentPaymentLimit = 6

type DashboardHandler struct {
	paymentService *services.PaymentService
	invoiceService *services.InvoiceService
	emailService   *services.EmailService
}

func NewDashboardHandler(
	paymentService *services.PaymentService,
	invoiceService *services.InvoiceService,
	emailService *services.EmailService,
) *DashboardHandler {
	return &DashboardHandler{
		paymentService: paymentService,
		invoiceService: invoiceService,
		emailService:   emailService,
	}
}

func (h *DashboardHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/dashboard", h.Get)
}

func (h *DashboardHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	ownerUserID := middleware.GetEffectiveUserID(c)
	firstDayOfMonth, lastMomentOfMonth := dashboardMonthBounds(time.Now())

	paymentStats, err := h.paymentService.GetStatsCtx(
		ctx,
		ownerUserID,
		firstDayOfMonth.Format(time.RFC3339Nano),
		lastMomentOfMonth.Format(time.RFC3339Nano),
	)
	if err != nil {
		h.internalError(c, "payment stats", err)
		return
	}
	invoiceStats, err := h.invoiceService.GetStatsCtx(ctx, ownerUserID)
	if err != nil {
		h.internalError(c, "invoice stats", err)
		return
	}
	emailStatus, err := h.emailService.GetMonitoringStatusCtx(ctx, ownerUserID)
	if err != nil {
		h.internalError(c, "email status", err)
		return
	}
	recentEmails, err := h.emailService.GetLogsCtx(ctx, ownerUserID, "", 5)
	if err != nil {
		h.internalError(c, "recent email logs", err)
		return
	}

	recentPayments, err := h.paymentService.GetRecentWithInvoiceCountsCtx(
		ctx,
		ownerUserID,
		dashboardRecentPaymentLimit,
	)
	if err != nil {
		h.internalError(c, "recent payments", err)
		return
	}

	utils.SuccessData(c, gin.H{
		"payments": gin.H{
			"totalThisMonth": paymentStats.TotalAmount,
			"countThisMonth": paymentStats.TotalCount,
			"categoryStats":  paymentStats.CategoryStats,
			"dailyStats":     paymentStats.DailyStats,
		},
		"recentPayments": recentPayments,
		"invoices": gin.H{
			"totalCount":  invoiceStats.TotalCount,
			"totalAmount": invoiceStats.TotalAmount,
			"bySource":    invoiceStats.BySource,
		},
		"email": gin.H{
			"monitoringStatus": emailStatus,
			"recentLogs":       recentEmails,
		},
	})
}

func (h *DashboardHandler) internalError(c *gin.Context, operation string, err error) {
	log.Printf("[Dashboard] %s failed: %v", operation, err)
	utils.Error(c, 500, "获取仪表盘数据失败", nil)
}

func dashboardMonthBounds(now time.Time) (time.Time, time.Time) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.UTC
	}
	now = now.In(loc)
	firstDay := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	return firstDay, firstDay.AddDate(0, 1, 0).Add(-time.Millisecond)
}
