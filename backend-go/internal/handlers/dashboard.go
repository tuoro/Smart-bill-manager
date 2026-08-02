package handlers

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type DashboardHandler struct {
	db             *gorm.DB
	paymentService *services.PaymentService
	invoiceService *services.InvoiceService
	emailService   *services.EmailService
}

func NewDashboardHandler(
	db *gorm.DB,
	paymentService *services.PaymentService,
	invoiceService *services.InvoiceService,
	emailService *services.EmailService,
) *DashboardHandler {
	return &DashboardHandler{
		db:             db,
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

	type recentPaymentRow struct {
		models.Payment
		InvoiceCount int `json:"invoiceCount" gorm:"column:invoice_count"`
	}
	recentPayments := make([]recentPaymentRow, 0)
	if err := h.db.WithContext(ctx).
		Table("payments AS p").
		Select(`p.*, COUNT(l.invoice_id) AS invoice_count`).
		Joins("LEFT JOIN invoice_payment_links AS l ON l.payment_id = p.id").
		Where("p.is_draft = 0").
		Where("p.owner_user_id = ?", ownerUserID).
		Group("p.id").
		Order("p.transaction_time_ts DESC, p.created_at DESC").
		Limit(6).
		Scan(&recentPayments).Error; err != nil {
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
