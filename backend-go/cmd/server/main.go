package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "time/tzdata"

	"github.com/gin-gonic/gin"

	"smart-bill-manager/internal/config"
	"smart-bill-manager/internal/handlers"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/migrations"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/pkg/database"
)

func main() {
	log.Println("Starting Smart Bill Manager...")

	// Load configuration
	cfg := config.Load()
	log.Printf("Environment: %s", cfg.NodeEnv)
	log.Printf("Working directory: %s", mustGetWd())

	// Initialize database
	db := database.Init(cfg.DataDir)

	// 执行版本化数据库迁移。
	if err := migrations.Run(db); err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// 邮件日志去重仍是可重复执行的兼容性维护任务，不阻断启动。
	services.EnsureEmailLogUniqueIndex(db)

	// 强制加密旧版数据库中的 IMAP 明文密码。
	if err := services.EnsureEmailConfigPasswordsEncrypted(db); err != nil {
		log.Fatal("Failed to enforce email password encryption:", err)
	}

	// Ensure uploads directory exists
	uploadsDir := cfg.UploadsDir
	if !filepath.IsAbs(uploadsDir) {
		uploadsDir = filepath.Join(mustGetWd(), uploadsDir)
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		log.Fatal("Failed to create uploads directory:", err)
	}
	log.Printf("Uploads directory: %s", uploadsDir)

	// Initialize services
	authService := services.NewAuthService()
	paymentService := services.NewPaymentService(uploadsDir)
	invoiceService := services.NewInvoiceService(uploadsDir)
	emailService := services.NewEmailService(uploadsDir, invoiceService)
	tripService := services.NewTripService(uploadsDir)
	taskService := services.NewTaskService(db, paymentService, invoiceService)
	taskService.StartWorker()
	if started, err := services.StartOCRWorkerIfEnabled(); err != nil {
		log.Printf("[OCR] worker not started: %v", err)
	} else if started {
		log.Printf("[OCR] worker mode: enabled")
	}

	// Periodically clean up stale draft uploads (refresh/abandon cases).
	services.StartDraftCleanup(db, uploadsDir)

	// Import built-in regression samples (repo/docker-bundled) into DB.
	// Mode B: if a matching local (ui) sample exists, it will be promoted as origin=repo.
	if res, err := services.NewRegressionSampleService().ImportRepoSamples(); err != nil {
		if !errors.Is(err, services.ErrRepoSampleDirNotFound) {
			log.Printf("[Regression] repo sample import failed: %v", err)
		}
	} else if res != nil {
		if res.Files > 0 || res.Inserted > 0 || res.Updated > 0 || res.Promoted > 0 {
			log.Printf("[Regression] repo samples: scanned=%d inserted=%d updated=%d promoted=%d errors=%d", res.Files, res.Inserted, res.Updated, res.Promoted, res.Errors)
		}
	}

	// No longer automatically creating admin - use setup page instead
	log.Println("System ready. Use setup page for initial configuration.")

	// Set Gin mode
	if cfg.NodeEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin router
	r := gin.Default()

	// Middleware
	r.Use(middleware.CORSMiddleware())

	// Serve uploaded files
	// Do not expose uploads statically; files are served via authenticated endpoints.

	// API routes
	api := r.Group("/api")

	// Health check (public)
	api.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":    "ok",
			"timestamp": time.Now().Format(time.RFC3339),
			// Bump this when shipping parsing fixes so deployments can be verified quickly.
			"items_parser_rev":   3,
			"invoice_parser_rev": 1,
		})
	})

	// Auth routes (public) with rate limiting
	authGroup := api.Group("/auth")
	authGroup.Use(middleware.AuthRateLimitMiddleware())
	authHandler := handlers.NewAuthHandler(authService)
	authHandler.RegisterRoutes(authGroup)

	// Protected routes with rate limiting
	protectedGroup := api.Group("")
	protectedGroup.Use(middleware.APIRateLimitMiddleware())
	protectedGroup.Use(middleware.AuthMiddleware(authService))
	protectedGroup.Use(middleware.ActAsConfirmMiddleware())

	// Payment routes
	paymentHandler := handlers.NewPaymentHandler(paymentService, taskService)
	paymentHandler.SetUploadsDir(uploadsDir)
	paymentHandler.RegisterRoutes(protectedGroup.Group("/payments"))

	// Invoice routes
	invoiceHandler := handlers.NewInvoiceHandler(invoiceService, taskService, uploadsDir)
	invoiceHandler.RegisterRoutes(protectedGroup.Group("/invoices"))

	// Email routes
	emailHandler := handlers.NewEmailHandler(emailService)
	emailHandler.RegisterRoutes(protectedGroup.Group("/email"))

	// Trip routes
	tripHandler := handlers.NewTripHandler(tripService)
	tripHandler.RegisterRoutes(protectedGroup.Group("/trips"))

	// Logs routes
	logsHandler := handlers.NewLogsHandler()
	logsGroup := protectedGroup.Group("/logs")
	logsGroup.Use(middleware.RequireAdmin())
	logsHandler.RegisterRoutes(logsGroup)

	// Tasks routes
	taskHandler := handlers.NewTaskHandler(taskService)
	taskHandler.RegisterRoutes(protectedGroup.Group("/tasks"))

	// Admin routes
	adminGroup := protectedGroup.Group("/admin")
	adminGroup.Use(middleware.RequireAdmin())
	adminInvitesHandler := handlers.NewAdminInvitesHandler(authService)
	adminInvitesHandler.RegisterRoutes(adminGroup.Group("/invites"))
	adminUsersHandler := handlers.NewAdminUsersHandler(authService, uploadsDir)
	adminUsersHandler.RegisterRoutes(adminGroup.Group("/users"))
	adminRegressionHandler := handlers.NewAdminRegressionSamplesHandler(services.NewRegressionSampleService())
	adminRegressionHandler.RegisterRoutes(adminGroup.Group("/regression-samples"))

	// Dashboard endpoint
	protectedGroup.GET("/dashboard", func(c *gin.Context) {
		// Use Asia/Shanghai for "本月" boundaries to match OCR/default time parsing.
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		firstDayOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		firstDayNextMonth := firstDayOfMonth.AddDate(0, 1, 0)
		// Inclusive upper bound for APIs that use <= endDate.
		lastMomentOfMonth := firstDayNextMonth.Add(-time.Millisecond)

		paymentStats, _ := paymentService.GetStats(
			middleware.GetEffectiveUserID(c),
			firstDayOfMonth.Format(time.RFC3339Nano),
			lastMomentOfMonth.Format(time.RFC3339Nano),
		)
		invoiceStats, _ := invoiceService.GetStats(middleware.GetEffectiveUserID(c))
		emailStatus, _ := emailService.GetMonitoringStatus(middleware.GetEffectiveUserID(c))
		recentEmails, _ := emailService.GetLogs(middleware.GetEffectiveUserID(c), "", 5)

		// Recent payments with linked invoice count
		type recentPaymentRow struct {
			models.Payment
			InvoiceCount int `json:"invoiceCount" gorm:"column:invoice_count"`
		}
		recentPayments := make([]recentPaymentRow, 0)
		_ = db.
			Table("payments AS p").
			Select(`p.*, COUNT(l.invoice_id) AS invoice_count`).
			Joins("LEFT JOIN invoice_payment_links AS l ON l.payment_id = p.id").
			Where("p.is_draft = 0").
			Where("p.owner_user_id = ?", middleware.GetEffectiveUserID(c)).
			Group("p.id").
			Order("p.transaction_time_ts DESC, p.created_at DESC").
			Limit(6).
			Scan(&recentPayments).Error

		c.JSON(200, gin.H{
			"success": true,
			"data": gin.H{
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
			},
		})
	})

	// Start server
	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🚀 Smart Bill Manager API running on port %s", cfg.Port)
	log.Printf("📊 Dashboard: http://localhost:%s", cfg.Port)
	log.Println("📬 Email monitoring ready")
	log.Println("🔐 Auth system enabled")

	if err := r.Run(addr); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func mustGetWd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
