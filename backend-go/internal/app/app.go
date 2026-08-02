package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"smart-bill-manager/internal/config"
	"smart-bill-manager/internal/handlers"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

const (
	itemsParserRevision   = 3
	invoiceParserRevision = 1
)

type Application struct {
	Router      *gin.Engine
	db          *gorm.DB
	uploadsDir  string
	taskService *services.TaskService
	startOnce   sync.Once
	done        chan struct{}
}

func New(cfg *config.Config, db *gorm.DB, uploadsDir string) (*Application, error) {
	if cfg == nil {
		return nil, errors.New("应用配置不能为空")
	}
	if db == nil {
		return nil, errors.New("数据库连接不能为空")
	}

	tokenManager, err := utils.NewTokenManager(cfg.JWTSecret, cfg.JWTExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("初始化 JWT 管理器失败: %w", err)
	}

	authService := services.NewAuthService(db, tokenManager)
	paymentService := services.NewPaymentService(db, uploadsDir)
	invoiceService := services.NewInvoiceService(db, uploadsDir)
	emailService := services.NewEmailService(db, uploadsDir, invoiceService)
	tripService := services.NewTripService(db, uploadsDir)
	taskService := services.NewTaskService(db, paymentService, invoiceService)

	if cfg.NodeEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	if err := router.SetTrustedProxies([]string{"127.0.0.1", "::1"}); err != nil {
		return nil, fmt.Errorf("配置可信代理失败: %w", err)
	}
	router.Use(middleware.CORSMiddleware(cfg.CORSAllowedOrigins))

	api := router.Group("/api")
	api.GET("/health", healthCheck)

	authGroup := api.Group("/auth")
	authGroup.Use(middleware.AuthRateLimitMiddleware())
	handlers.NewAuthHandler(authService).RegisterRoutes(authGroup)

	protectedGroup := api.Group("")
	protectedGroup.Use(middleware.APIRateLimitMiddleware())
	protectedGroup.Use(middleware.AuthMiddleware(authService))
	protectedGroup.Use(middleware.ActAsConfirmMiddleware())

	paymentHandler := handlers.NewPaymentHandler(paymentService, taskService)
	paymentHandler.SetUploadsDir(uploadsDir)
	paymentHandler.RegisterRoutes(protectedGroup.Group("/payments"))
	handlers.NewInvoiceHandler(invoiceService, taskService, uploadsDir).RegisterRoutes(protectedGroup.Group("/invoices"))
	handlers.NewEmailHandler(emailService).RegisterRoutes(protectedGroup.Group("/email"))
	handlers.NewTripHandler(tripService).RegisterRoutes(protectedGroup.Group("/trips"))
	handlers.NewTaskHandler(taskService).RegisterRoutes(protectedGroup.Group("/tasks"))
	handlers.NewDashboardHandler(db, paymentService, invoiceService, emailService).RegisterRoutes(protectedGroup)

	logsGroup := protectedGroup.Group("/logs")
	logsGroup.Use(middleware.RequireAdmin())
	handlers.NewLogsHandler().RegisterRoutes(logsGroup)

	adminGroup := protectedGroup.Group("/admin")
	adminGroup.Use(middleware.RequireAdmin())
	handlers.NewAdminInvitesHandler(authService).RegisterRoutes(adminGroup.Group("/invites"))
	handlers.NewAdminUsersHandler(authService, uploadsDir).RegisterRoutes(adminGroup.Group("/users"))
	handlers.NewAdminRegressionSamplesHandler(services.NewRegressionSampleService()).RegisterRoutes(adminGroup.Group("/regression-samples"))

	return &Application{
		Router:      router,
		db:          db,
		uploadsDir:  uploadsDir,
		taskService: taskService,
		done:        make(chan struct{}),
	}, nil
}

func (a *Application) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.startOnce.Do(func() {
		taskDone := a.taskService.StartWorker(ctx)
		cleanupDone := services.StartDraftCleanup(ctx, a.db, a.uploadsDir)

		if started, err := services.StartOCRWorkerIfEnabled(); err != nil {
			log.Printf("[OCR] worker not started: %v", err)
		} else if started {
			log.Printf("[OCR] worker mode: enabled")
		}
		a.importRegressionSamples()

		ocrDone := make(chan struct{})
		go func() {
			defer close(ocrDone)
			<-ctx.Done()
			services.StopOCRWorker()
		}()
		go func() {
			<-taskDone
			<-cleanupDone
			<-ocrDone
			close(a.done)
		}()
	})
}

func (a *Application) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-a.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Application) importRegressionSamples() {
	result, err := services.NewRegressionSampleService().ImportRepoSamples()
	if err != nil {
		if !errors.Is(err, services.ErrRepoSampleDirNotFound) {
			log.Printf("[Regression] repo sample import failed: %v", err)
		}
		return
	}
	if result != nil && (result.Files > 0 || result.Inserted > 0 || result.Updated > 0 || result.Promoted > 0) {
		log.Printf(
			"[Regression] repo samples: scanned=%d inserted=%d updated=%d promoted=%d errors=%d",
			result.Files,
			result.Inserted,
			result.Updated,
			result.Promoted,
			result.Errors,
		)
	}
}

func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":             "ok",
		"timestamp":          time.Now().Format(time.RFC3339),
		"items_parser_rev":   itemsParserRevision,
		"invoice_parser_rev": invoiceParserRevision,
	})
}
