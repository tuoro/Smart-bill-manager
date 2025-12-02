package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"smart-bill-manager/internal/config"
	"smart-bill-manager/internal/handlers"
	"smart-bill-manager/internal/middleware"
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

	// Run migrations
	if err := db.AutoMigrate(
		&models.User{},
		&models.Payment{},
		&models.Invoice{},
		&models.EmailConfig{},
		&models.EmailLog{},
		&models.DingtalkConfig{},
		&models.DingtalkLog{},
	); err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// Create additional indexes
	db.Exec("CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_time ON payments(transaction_time)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_date ON invoices(invoice_date)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_email_logs_date ON email_logs(created_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_dingtalk_logs_date ON dingtalk_logs(created_at)")

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
	paymentService := services.NewPaymentService()
	invoiceService := services.NewInvoiceService(uploadsDir)
	emailService := services.NewEmailService(uploadsDir, invoiceService)
	dingtalkService := services.NewDingtalkService(uploadsDir, invoiceService)

	// Ensure admin exists
	if err := authService.EnsureAdminExists(); err != nil {
		log.Fatal("Failed to ensure admin exists:", err)
	}

	// Set Gin mode
	if cfg.NodeEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin router
	r := gin.Default()

	// Middleware
	r.Use(middleware.CORSMiddleware())

	// Serve uploaded files
	r.Static("/uploads", uploadsDir)

	// API routes
	api := r.Group("/api")

	// Health check (public)
	api.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":    "ok",
			"timestamp": time.Now().Format(time.RFC3339),
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

	// Payment routes
	paymentHandler := handlers.NewPaymentHandler(paymentService)
	paymentHandler.RegisterRoutes(protectedGroup.Group("/payments"))

	// Invoice routes
	invoiceHandler := handlers.NewInvoiceHandler(invoiceService, uploadsDir)
	invoiceHandler.RegisterRoutes(protectedGroup.Group("/invoices"))

	// Email routes
	emailHandler := handlers.NewEmailHandler(emailService)
	emailHandler.RegisterRoutes(protectedGroup.Group("/email"))

	// DingTalk routes
	dingtalkHandler := handlers.NewDingtalkHandler(dingtalkService, invoiceService, uploadsDir)
	dingtalkHandler.RegisterRoutes(protectedGroup.Group("/dingtalk"))

	// Dashboard endpoint
	protectedGroup.GET("/dashboard", func(c *gin.Context) {
		now := time.Now()
		firstDayOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		lastDayOfMonth := firstDayOfMonth.AddDate(0, 1, -1)

		paymentStats, _ := paymentService.GetStats(
			firstDayOfMonth.Format(time.RFC3339),
			lastDayOfMonth.Format(time.RFC3339),
		)
		invoiceStats, _ := invoiceService.GetStats()
		emailStatus, _ := emailService.GetMonitoringStatus()
		recentEmails, _ := emailService.GetLogs("", 5)

		c.JSON(200, gin.H{
			"success": true,
			"data": gin.H{
				"payments": gin.H{
					"totalThisMonth": paymentStats.TotalAmount,
					"countThisMonth": paymentStats.TotalCount,
					"categoryStats":  paymentStats.CategoryStats,
					"dailyStats":     paymentStats.DailyStats,
				},
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
	log.Println("🤖 DingTalk webhook ready at /api/dingtalk/webhook")
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
