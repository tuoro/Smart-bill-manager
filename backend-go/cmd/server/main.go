package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "time/tzdata"

	"github.com/gin-gonic/gin"

	"smart-bill-manager/internal/config"
	"smart-bill-manager/internal/handlers"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
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
		&models.Task{},
		&models.RegressionSample{},
		&models.Payment{},
		&models.Trip{},
		&models.Invoice{},
		&models.InvoiceAttachment{},
		&models.InvoiceOCRBlob{},
		&models.PaymentOCRBlob{},
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

	// Multi-user backfill: older DBs may not have owner_user_id populated.
	// Assign all legacy rows to the first created user (typically the initial admin).
	var firstUser models.User
	if err := db.Select("id").Order("created_at ASC").First(&firstUser).Error; err == nil {
		defaultOwnerID := strings.TrimSpace(firstUser.ID)
		if defaultOwnerID != "" {
			db.Exec(`UPDATE payments SET owner_user_id = ? WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`, defaultOwnerID)
			db.Exec(`UPDATE trips SET owner_user_id = ? WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`, defaultOwnerID)
			db.Exec(`UPDATE invoices SET owner_user_id = ? WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`, defaultOwnerID)
			db.Exec(`UPDATE email_configs SET owner_user_id = ? WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`, defaultOwnerID)
			// Prefer deriving logs from their config; fallback to default owner.
			db.Exec(`
				UPDATE email_logs
				SET owner_user_id = COALESCE(
					(SELECT owner_user_id FROM email_configs WHERE email_configs.id = email_logs.email_config_id),
					?
	)
				WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''
			`, defaultOwnerID)
			// Tasks: default to created_by; fallback to default owner.
			db.Exec(`
				UPDATE tasks
				SET owner_user_id = COALESCE(NULLIF(TRIM(created_by), ''), ?)
				WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''
			`, defaultOwnerID)
		}
	}

	// Create additional indexes
	db.Exec("CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_invites_code_hash ON invites(code_hash)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invites_created_at ON invites(created_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invites_used_at ON invites(used_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_time ON payments(transaction_time)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_time_ts ON payments(transaction_time_ts)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_owner_draft_time_ts ON payments(owner_user_id, is_draft, transaction_time_ts)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payments_owner_draft_category_time_ts ON payments(owner_user_id, is_draft, category, transaction_time_ts)")
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
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_date_ymd ON invoices(invoice_date_ymd)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_owner_draft_created_at ON invoices(owner_user_id, is_draft, created_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_owner_draft_date_ymd ON invoices(owner_user_id, is_draft, invoice_date_ymd)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_invoice_number ON invoices(invoice_number)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_owner_invoice_number ON invoices(owner_user_id, invoice_number)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_bad_debt ON invoices(bad_debt)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_file_sha256 ON invoices(file_sha256)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_owner_file_sha256 ON invoices(owner_user_id, file_sha256)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoices_dedup_status ON invoices(dedup_status)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoice_ocr_blobs_owner_invoice ON invoice_ocr_blobs(owner_user_id, invoice_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_payment_ocr_blobs_owner_payment ON payment_ocr_blobs(owner_user_id, payment_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_tasks_status_created_at ON tasks(status, created_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_tasks_created_by ON tasks(created_by)")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_regression_samples_source ON regression_samples(source_type, source_id, kind)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_regression_samples_kind_created_at ON regression_samples(kind, created_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_regression_samples_name ON regression_samples(name)")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_regression_samples_kind_rawhash ON regression_samples(kind, raw_hash) WHERE raw_hash != ''")

	// Link rule:
	// - 1 invoice -> 0/1 payment (invoice_id unique)
	// - 1 payment -> 0..N invoices (payment_id non-unique)
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_invoice_payment_links_invoice_id ON invoice_payment_links(invoice_id)")
	// Backward-compatible migration: older builds created a UNIQUE index on payment_id which enforced 1:1.
	db.Exec("DROP INDEX IF EXISTS ux_invoice_payment_links_payment_id")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_invoice_payment_links_payment_id ON invoice_payment_links(payment_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_email_logs_date ON email_logs(created_at)")

	// Backfill invoice_date_ymd (fast path via SQL for common formats, then a Go parser fallback).
	db.Exec(`
		UPDATE invoices
		SET invoice_date_ymd = SUBSTR(invoice_date, 1, 10)
		WHERE (invoice_date_ymd IS NULL OR TRIM(invoice_date_ymd) = '')
		  AND invoice_date IS NOT NULL
		  AND invoice_date LIKE '____-__-__%'
	`)
	db.Exec(`
		UPDATE invoices
		SET invoice_date_ymd = REPLACE(SUBSTR(invoice_date, 1, 10), '/', '-')
		WHERE (invoice_date_ymd IS NULL OR TRIM(invoice_date_ymd) = '')
		  AND invoice_date IS NOT NULL
		  AND invoice_date LIKE '____/__/__%'
	`)

	type invoiceDateRow struct {
		ID          string  `gorm:"column:id"`
		InvoiceDate *string `gorm:"column:invoice_date"`
	}
	var missing []invoiceDateRow
	_ = db.Raw(`
		SELECT id, invoice_date
		FROM invoices
		WHERE invoice_date IS NOT NULL
		  AND TRIM(invoice_date) != ''
		  AND (invoice_date_ymd IS NULL OR TRIM(invoice_date_ymd) = '')
		LIMIT 2000
	`).Scan(&missing).Error
	for _, r := range missing {
		if r.ID == "" || r.InvoiceDate == nil {
			continue
		}
		ymd := utils.NormalizeDateYMD(*r.InvoiceDate)
		if ymd == "" {
			continue
		}
		db.Exec(`UPDATE invoices SET invoice_date_ymd = ? WHERE id = ?`, ymd, r.ID)
	}

	// Backfill OCR blobs tables from legacy columns (best-effort; keeps existing data readable).
	db.Exec(`
		INSERT INTO invoice_ocr_blobs (invoice_id, owner_user_id, extracted_data, raw_text, created_at, updated_at)
		SELECT id, owner_user_id, extracted_data, raw_text, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM invoices
		WHERE (extracted_data IS NOT NULL AND TRIM(extracted_data) != '')
		   OR (raw_text IS NOT NULL AND TRIM(raw_text) != '')
		  AND NOT EXISTS (SELECT 1 FROM invoice_ocr_blobs b WHERE b.invoice_id = invoices.id)
		LIMIT 500
	`)
	db.Exec(`
		INSERT INTO payment_ocr_blobs (payment_id, owner_user_id, extracted_data, created_at, updated_at)
		SELECT id, owner_user_id, extracted_data, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM payments
		WHERE extracted_data IS NOT NULL AND TRIM(extracted_data) != ''
		  AND NOT EXISTS (SELECT 1 FROM payment_ocr_blobs b WHERE b.payment_id = payments.id)
		LIMIT 500
	`)

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
