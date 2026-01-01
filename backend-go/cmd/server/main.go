package main

import (
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
		&models.Invite{},
		&models.Payment{},
		&models.Trip{},
		&models.Invoice{},
		&models.InvoicePaymentLink{},
		&models.EmailConfig{},
		&models.EmailLog{},
	); err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// Backward-compatible: if older builds created short column names, keep data by copying into new columns.
	// Ignore errors because these legacy columns may not exist.
	db.Exec("UPDATE payments SET trip_assignment_source = COALESCE(trip_assignment_source, trip_assign_src, 'auto')")
	db.Exec("UPDATE payments SET trip_assignment_state = COALESCE(trip_assignment_state, trip_assign_state, 'no_match')")

	// Backfill numeric timestamps for older rows (used for stats and trip auto-assignment).
	db.Exec(`
		UPDATE payments
		SET transaction_time_ts = CAST(strftime('%s', transaction_time) AS INTEGER) * 1000
		WHERE transaction_time_ts = 0
		  AND transaction_time IS NOT NULL
		  AND TRIM(transaction_time) != ''
		  AND strftime('%s', transaction_time) IS NOT NULL
	`)
	db.Exec(`
		UPDATE trips
		SET start_time_ts = CAST(strftime('%s', start_time) AS INTEGER) * 1000
		WHERE start_time_ts = 0
		  AND start_time IS NOT NULL
		  AND TRIM(start_time) != ''
		  AND strftime('%s', start_time) IS NOT NULL
	`)
	db.Exec(`
		UPDATE trips
		SET end_time_ts = CAST(strftime('%s', end_time) AS INTEGER) * 1000
		WHERE end_time_ts = 0
		  AND end_time IS NOT NULL
		  AND TRIM(end_time) != ''
		  AND strftime('%s', end_time) IS NOT NULL
	`)

	// Create additional indexes
	db.Exec("CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_invites_code_hash ON invites(code_hash)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invites_created_at ON invites(created_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invites_used_at ON invites(used_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_time ON payments(transaction_time)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_time_ts ON payments(transaction_time_ts)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_trip_id ON payments(trip_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_bad_debt ON payments(bad_debt)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_trip_assign_src ON payments(trip_assignment_source)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_trip_assign_state ON payments(trip_assignment_state)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_file_sha256 ON payments(file_sha256)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_dedup_status ON payments(dedup_status)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_trips_time ON trips(start_time, end_time)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_trips_time_ts ON trips(start_time_ts, end_time_ts)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_trips_timezone ON trips(timezone)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_trips_reimburse_status ON trips(reimburse_status)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_trips_bad_debt_locked ON trips(bad_debt_locked)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_date ON invoices(invoice_date)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_invoice_number ON invoices(invoice_number)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_bad_debt ON invoices(bad_debt)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_file_sha256 ON invoices(file_sha256)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_dedup_status ON invoices(dedup_status)")

	// Enforce invoice<->payment 1:1 by making each side unique in link table.
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_invoice_payment_links_invoice_id ON invoice_payment_links(invoice_id)")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_invoice_payment_links_payment_id ON invoice_payment_links(payment_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_email_logs_date ON email_logs(created_at)")

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

	// Periodically clean up stale draft uploads (refresh/abandon cases).
	services.StartDraftCleanup(db, uploadsDir)

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
	paymentHandler.SetUploadsDir(uploadsDir)
	paymentHandler.RegisterRoutes(protectedGroup.Group("/payments"))

	// Invoice routes
	invoiceHandler := handlers.NewInvoiceHandler(invoiceService, uploadsDir)
	invoiceHandler.RegisterRoutes(protectedGroup.Group("/invoices"))

	// Email routes
	emailHandler := handlers.NewEmailHandler(emailService)
	emailHandler.RegisterRoutes(protectedGroup.Group("/email"))

	// Trip routes
	tripHandler := handlers.NewTripHandler(tripService)
	tripHandler.RegisterRoutes(protectedGroup.Group("/trips"))

	// Logs routes
	logsHandler := handlers.NewLogsHandler()
	logsHandler.RegisterRoutes(protectedGroup.Group("/logs"))

	// Admin routes
	adminGroup := protectedGroup.Group("/admin")
	adminGroup.Use(middleware.RequireAdmin())
	adminInvitesHandler := handlers.NewAdminInvitesHandler(authService)
	adminInvitesHandler.RegisterRoutes(adminGroup.Group("/invites"))

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
			firstDayOfMonth.Format(time.RFC3339Nano),
			lastMomentOfMonth.Format(time.RFC3339Nano),
		)
		invoiceStats, _ := invoiceService.GetStats()
		emailStatus, _ := emailService.GetMonitoringStatus()
		recentEmails, _ := emailService.GetLogs("", 5)

		// Recent payments with linked invoice status (1:1)
		type recentPaymentRow struct {
			models.Payment
			InvoiceCount int `json:"invoiceCount" gorm:"column:invoice_count"`
		}
		recentPayments := make([]recentPaymentRow, 0)
		_ = db.
			Table("payments AS p").
			Select(`p.*, CASE WHEN l.invoice_id IS NULL THEN 0 ELSE 1 END AS invoice_count`).
			Joins("LEFT JOIN invoice_payment_links AS l ON l.payment_id = p.id").
			Where("p.is_draft = 0").
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
