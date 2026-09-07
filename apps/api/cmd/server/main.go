package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/emailmime"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/openaicompatible"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/accounts"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/bootstrap"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	applicationemails "github.com/tuoro/smart-bill-manager/apps/api/internal/application/emails"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/insights"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/invoicematerials"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/materialexports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/processing"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/providers"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/transport/httpapi"
)

const version = "m4-dev"

type config struct {
	database             postgresqladapter.Config
	httpAddress          string
	cookieSecure         bool
	sessionTTL           time.Duration
	objectsPath          string
	pdfInfoPath          string
	pdfToPPMPath         string
	masterKeyFile        string
	extractionSchemaPath string
	aiConcurrency        int
	webDistPath          string
	deploymentMode       string
	autoInitialize       bool
}

type runtimeReadiness struct {
	store  *postgresqladapter.Store
	worker *processing.Worker
}

func (r runtimeReadiness) Ready(ctx context.Context) error {
	if err := r.store.Ping(ctx); err != nil {
		return fmt.Errorf("database unavailable: %w", err)
	}
	if !r.worker.Ready() {
		return errors.New("job scheduler is not running")
	}
	return nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if config.autoInitialize {
		if err := autoInitializeSchema(ctx, config, logger); err != nil {
			return err
		}
	}
	store, err := postgresqladapter.Open(ctx, config.database)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.CheckObjectRoot(ctx, config.objectsPath); err != nil {
		return err
	}
	hasher, err := cryptography.NewPasswordHasher(cryptography.DefaultArgon2Params)
	if err != nil {
		return err
	}
	if config.autoInitialize {
		if err := autoInitializeOwner(ctx, store, hasher, logger); err != nil {
			return err
		}
	}
	authService, err := auth.NewService(
		store,
		hasher,
		cryptography.TokenGenerator{},
		system.IDGenerator{},
		system.Clock{},
		config.sessionTTL,
	)
	if err != nil {
		return err
	}
	objects, err := localstorage.New(config.objectsPath)
	if err != nil {
		return err
	}
	if err := objects.InitializeExportSpool(); err != nil {
		return fmt.Errorf("initialize export spool: %w", err)
	}
	exportService := materialexports.NewService(store, objects, objects, system.IDGenerator{})
	defer exportService.Close()
	inspector, err := localstorage.NewInspector(objects, config.pdfInfoPath)
	if err != nil {
		return err
	}
	uploadService := documents.NewUploadService(objects, inspector, store, system.IDGenerator{}, system.Clock{})
	documentQueries := documents.NewQueryService(store, store, objects)
	jobActions := documents.NewActionService(store, store, system.IDGenerator{}, system.Clock{})
	documentDeletions := documents.NewDeletionService(store, objects, store, system.IDGenerator{}, system.Clock{})
	if err := documentDeletions.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile document deletions: %w", err)
	}
	invoiceMaterialService := invoicematerials.NewService(store, store, objects, objects, inspector, system.IDGenerator{}, system.Clock{})
	if err := invoiceMaterialService.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile invoice material publications: %w", err)
	}
	masterKey, err := cryptography.LoadMasterKeyFile(config.masterKeyFile)
	if err != nil {
		return err
	}
	secretCipher, err := cryptography.NewSecretCipher(masterKey)
	clear(masterKey)
	if err != nil {
		return err
	}
	detector, err := openaicompatible.NewDetector(config.extractionSchemaPath)
	if err != nil {
		return err
	}
	normalizer, err := localstorage.NewNormalizer(objects, config.pdfToPPMPath)
	if err != nil {
		return err
	}
	worker, err := processing.NewWorker(
		store,
		store,
		secretCipher,
		normalizer,
		objects,
		detector,
		store,
		system.IDGenerator{},
		system.Clock{},
		logger,
		processing.WorkerConfig{
			Concurrency:   config.aiConcurrency,
			PollInterval:  500 * time.Millisecond,
			JobTimeout:    150 * time.Second,
			LeaseDuration: 165 * time.Second,
		},
	)
	if err != nil {
		return err
	}
	providerService := providers.NewService(
		store,
		store,
		secretCipher,
		detector,
		system.IDGenerator{},
		system.Clock{},
	)
	reviewService := reviews.NewService(store, store, system.IDGenerator{}, system.Clock{}).WithManualEntry(store, normalizer, objects)
	factService := reviews.NewFactService(store, store, system.IDGenerator{}, system.Clock{})
	allocationService := allocations.NewService(store, store, system.IDGenerator{}, system.Clock{})
	emailService := applicationemails.NewService(
		store, store, objects, inspector, emailmime.Parser{}, system.IDGenerator{}, system.Clock{},
	)
	tripService := trips.NewService(store, store, system.IDGenerator{}, system.Clock{})
	reimbursementService := reimbursements.NewService(store, store, system.IDGenerator{}, system.Clock{})
	insightService := insights.NewService(store)
	httpServer, err := httpapi.NewServer(
		authService,
		accounts.NewService(store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{}),
		uploadService,
		documentQueries,
		jobActions,
		documentDeletions,
		providerService,
		reviewService,
		factService,
		invoiceMaterialService,
		allocationService,
		emailService,
		tripService,
		reimbursementService,
		insightService,
		exportService,
		store,
		runtimeReadiness{store: store, worker: worker},
		logger,
		httpapi.Config{
			CookieSecure: config.cookieSecure,
			Version:      version,
			WebDistPath:  config.webDistPath,
		})
	if err != nil {
		return err
	}
	handler := httpServer.Handler()
	server := &http.Server{
		Addr:              config.httpAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serverErrors := make(chan error, 1)
	go worker.Run(ctx)
	go func() {
		logger.Info("server listening", "address", config.httpAddress, "version", version)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	}
}

func loadConfig() (config, error) {
	database, err := postgresqladapter.RuntimeConfigFromEnvironment()
	if err != nil {
		return config{}, err
	}
	value := config{
		database:             database,
		httpAddress:          os.Getenv("SBM_HTTP_ADDRESS"),
		objectsPath:          os.Getenv("SBM_OBJECTS_PATH"),
		pdfInfoPath:          os.Getenv("SBM_PDFINFO_PATH"),
		pdfToPPMPath:         os.Getenv("SBM_PDFTOPPM_PATH"),
		masterKeyFile:        os.Getenv("SBM_MASTER_KEY_FILE"),
		extractionSchemaPath: os.Getenv("SBM_EXTRACTION_SCHEMA_PATH"),
		webDistPath:          os.Getenv("SBM_WEB_DIST_PATH"),
		deploymentMode:       os.Getenv("SBM_DEPLOYMENT_MODE"),
		// 交出 Owner 凭据即表示要求自初始化；Compose 硬化路径从不设置它。
		autoInitialize: strings.TrimSpace(os.Getenv("SBM_OWNER_EMAIL")) != "",
	}
	for name, entry := range map[string]string{
		"SBM_HTTP_ADDRESS":           value.httpAddress,
		"SBM_SESSION_TTL":            os.Getenv("SBM_SESSION_TTL"),
		"SBM_OBJECTS_PATH":           value.objectsPath,
		"SBM_PDFINFO_PATH":           value.pdfInfoPath,
		"SBM_PDFTOPPM_PATH":          value.pdfToPPMPath,
		"SBM_MASTER_KEY_FILE":        value.masterKeyFile,
		"SBM_EXTRACTION_SCHEMA_PATH": value.extractionSchemaPath,
		"SBM_AI_CONCURRENCY":         os.Getenv("SBM_AI_CONCURRENCY"),
		"SBM_WEB_DIST_PATH":          value.webDistPath,
		"SBM_DEPLOYMENT_MODE":        value.deploymentMode,
	} {
		if entry == "" {
			return config{}, fmt.Errorf("%s is required", name)
		}
	}
	sessionTTL, err := time.ParseDuration(os.Getenv("SBM_SESSION_TTL"))
	if err != nil {
		return config{}, fmt.Errorf("parse SBM_SESSION_TTL: %w", err)
	}
	if value.deploymentMode != "local" && value.deploymentMode != "production" {
		return config{}, errors.New("SBM_DEPLOYMENT_MODE must be local or production")
	}
	// Secure cookie 默认由部署模式推导：local 走明文回环，production 必须经 TLS。
	// 只有在 local 模式下由外部反向代理终止 TLS 时才需要显式覆盖。
	cookieSecure := value.deploymentMode == "production"
	if raw := strings.TrimSpace(os.Getenv("SBM_COOKIE_SECURE")); raw != "" {
		cookieSecure, err = strconv.ParseBool(raw)
		if err != nil {
			return config{}, errors.New("SBM_COOKIE_SECURE must be true or false")
		}
		if value.deploymentMode == "production" && !cookieSecure {
			return config{}, errors.New("SBM_COOKIE_SECURE must be true in production mode")
		}
	}
	value.cookieSecure = cookieSecure
	value.sessionTTL = sessionTTL
	aiConcurrency, err := strconv.Atoi(os.Getenv("SBM_AI_CONCURRENCY"))
	if err != nil || aiConcurrency < 1 || aiConcurrency > 8 {
		return config{}, errors.New("SBM_AI_CONCURRENCY must be an integer between 1 and 8")
	}
	value.aiConcurrency = aiConcurrency
	return value, nil
}

// autoInitializeSchema 在单角色部署里用运行身份直接应用未执行的迁移。
// 硬化部署仍由独立的 provision 与 migrate 入口按最小权限顺序完成。
func autoInitializeSchema(ctx context.Context, value config, logger *slog.Logger) error {
	migrationConfig := value.database
	migrationConfig.RuntimeRole = value.database.User
	migrationCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := postgresqladapter.Migrate(migrationCtx, migrationConfig); err != nil {
		return fmt.Errorf("auto initialize schema: %w", err)
	}
	logger.Info("auto initialize: schema is up to date")
	return nil
}

// autoInitializeOwner 只在数据库仍为空时创建唯一 Owner；已初始化的部署重启不受影响。
func autoInitializeOwner(
	ctx context.Context,
	store *postgresqladapter.Store,
	hasher cryptography.PasswordHasher,
	logger *slog.Logger,
) error {
	email := strings.TrimSpace(os.Getenv("SBM_OWNER_EMAIL"))
	if email == "" {
		return nil
	}
	ownerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	empty, err := store.IdentityIsEmpty(ownerCtx)
	if err != nil {
		return fmt.Errorf("auto initialize owner: %w", err)
	}
	if !empty {
		logger.Info("auto initialize: an owner already exists, skipping owner creation")
		return nil
	}
	password, err := autoInitializeOwnerPassword()
	if err != nil {
		return err
	}
	defer clear(password)
	service := bootstrap.NewService(store, hasher, system.IDGenerator{}, system.Clock{})
	_, err = service.Execute(ownerCtx, bootstrap.Input{
		Email:           email,
		Password:        password,
		DisplayName:     environmentOrDefault("SBM_OWNER_DISPLAY_NAME", "Owner"),
		TenantName:      environmentOrDefault("SBM_TENANT_NAME", "My Workspace"),
		DefaultCurrency: domain.Currency(environmentOrDefault("SBM_DEFAULT_CURRENCY", "CNY")),
		Timezone:        environmentOrDefault("SBM_TIMEZONE", "Asia/Shanghai"),
	})
	if errors.Is(err, domain.ErrBootstrapNotEmpty) {
		logger.Info("auto initialize: an owner already exists, skipping owner creation")
		return nil
	}
	if err != nil {
		return fmt.Errorf("auto initialize owner: %w", err)
	}
	logger.Info("auto initialize: owner created", "email", email)
	return nil
}

func autoInitializeOwnerPassword() ([]byte, error) {
	if path := strings.TrimSpace(os.Getenv("SBM_OWNER_PASSWORD_FILE")); path != "" {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect owner password file: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("owner password file must be regular and accessible only by its owner")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open owner password file: %w", err)
		}
		defer file.Close()
		password, err := io.ReadAll(io.LimitReader(file, 1026))
		if err != nil {
			return nil, fmt.Errorf("read owner password file: %w", err)
		}
		if len(password) > 1025 {
			return nil, errors.New("owner password file exceeds 1024 bytes")
		}
		if len(password) > 0 && password[len(password)-1] == '\n' {
			password = password[:len(password)-1]
			if len(password) > 0 && password[len(password)-1] == '\r' {
				password = password[:len(password)-1]
			}
		}
		return password, nil
	}
	password := os.Getenv("SBM_OWNER_PASSWORD")
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("SBM_OWNER_PASSWORD or SBM_OWNER_PASSWORD_FILE is required when SBM_OWNER_EMAIL is set")
	}
	return []byte(password), nil
}

func environmentOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
