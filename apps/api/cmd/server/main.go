package main

import (
	"context"
	"errors"
	"fmt"
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// 数据库未配置时先只提供首启配置服务；写入成功后再按常规路径完整启动。
	for {
		config, err := loadConfig()
		if errors.Is(err, postgresqladapter.ErrConnectionSettingsAbsent) {
			if err := runDatabaseSetup(ctx, logger); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		if err != nil {
			return err
		}
		return runApplication(ctx, config, logger)
	}
}

func runApplication(ctx context.Context, config config, logger *slog.Logger) error {
	status, err := postgresqladapter.InspectMigrations(ctx, config.database)
	if err != nil {
		if databaseFromStoredSettings {
			return fmt.Errorf(
				"无法使用已保存的数据库连接（%s:%d/%s）。该配置由初始化页写入 %s；"+
					"数据库地址变化时可用 SBM_POSTGRES_HOST、SBM_POSTGRES_USER、"+
					"SBM_POSTGRES_PASSWORD 等环境变量覆盖它，或删除该文件后重新配置。原始错误：%w",
				config.database.Host, config.database.Port, config.database.Database,
				postgresqladapter.SettingsFilePath(settingsDirectory()), err,
			)
		}
		return err
	}
	switch {
	case status.LedgerMissing:
		// 全新数据库：没有任何数据可能被迁移损坏，直接初始化。
		if err := autoInitializeSchema(ctx, config, logger); err != nil {
			return err
		}
	case status.Pending():
		// 已有数据的库上执行迁移会原地修改数据，且无法回滚到迁移前的状态。
		// 因此要求显式声明，等价于 Compose 路径的 upgrade --backup-confirmed。
		if !allowMigration() {
			return fmt.Errorf(
				"检测到 %d 条未执行的数据库迁移（已应用 %d / 共 %d）。"+
					"迁移会原地修改现有数据且不可回滚，请先创建并验证备份，"+
					"然后设置 SBM_ALLOW_MIGRATION=true 重新启动以应用它们",
				status.Expected-status.Applied, status.Applied, status.Expected,
			)
		}
		logger.Info("applying pending migrations",
			"applied", status.Applied, "expected", status.Expected)
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
		bootstrap.NewService(store, hasher, system.IDGenerator{}, system.Clock{}),
		store,
		httpapi.DatabaseSettingsStore{
			Directory:     settingsDirectory(),
			MigrationsDir: os.Getenv("SBM_MIGRATIONS_DIR"),
			// 连接由环境变量固定时页面只读：改文件不会生效，环境变量优先级更高。
			Managed: databaseFromStoredSettings,
		},
		config.database,
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

// settingsDirectory 是持久卷内保存首启数据库配置与密码的目录。它由应用自身写入，
// 因此与只读穿越的主密钥目录 /var/lib/sbm/secrets 分开。
func settingsDirectory() string {
	return environmentOrDefault("SBM_SETTINGS_DIR", "/var/lib/sbm/config")
}

// resolveDatabase 优先使用环境变量；未提供时回退到持久卷中的首启配置。
// 两者都没有时返回 ErrConnectionSettingsAbsent，由调用方进入配置流程。
//
// 判断依据是口令文件是否真实存在，而不是 SBM_POSTGRES_PASSWORD_FILE 是否被设置：
// 镜像 ENV 无条件给出该路径，只看变量会让首启配置永远无法生效。
func resolveDatabase() (postgresqladapter.Config, error) {
	passwordFile := strings.TrimSpace(os.Getenv("SBM_POSTGRES_PASSWORD_FILE"))
	if passwordFile != "" {
		if _, err := os.Stat(passwordFile); err == nil {
			return postgresqladapter.RuntimeConfigFromEnvironment()
		}
	}
	directory := settingsDirectory()
	settings, err := postgresqladapter.LoadConnectionSettings(
		postgresqladapter.SettingsFilePath(directory),
	)
	if err != nil {
		return postgresqladapter.Config{}, err
	}
	databaseFromStoredSettings = true
	return settings.Config(
		postgresqladapter.PasswordFilePath(directory),
		os.Getenv("SBM_MIGRATIONS_DIR"),
	), nil
}

// databaseFromStoredSettings 记录本次连接配置是否来自初始化页写入的文件，
// 用于在连接失败时给出针对性的修复指引。
var databaseFromStoredSettings bool

func loadConfig() (config, error) {
	database, err := resolveDatabase()
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


// allowMigration 是升级前的确认门禁：迁移不可回滚，必须由部署者显式声明。
func allowMigration() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SBM_ALLOW_MIGRATION")), "true")
}

func environmentOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// runDatabaseSetup 在数据库尚未配置时提供最小 HTTP 表面，等待用户在浏览器里
// 填写连接信息。写入并验证成功后返回，由 run 重新解析配置并完整启动。
func runDatabaseSetup(ctx context.Context, logger *slog.Logger) error {
	address := os.Getenv("SBM_HTTP_ADDRESS")
	if strings.TrimSpace(address) == "" {
		return errors.New("SBM_HTTP_ADDRESS is required")
	}
	setupServer, err := httpapi.NewSetupServer(
		os.Getenv("SBM_WEB_DIST_PATH"),
		settingsDirectory(),
		os.Getenv("SBM_MIGRATIONS_DIR"),
		logger,
	)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              address,
		Handler:           setupServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("setup: waiting for database configuration", "address", address)
		serverErrors <- server.ListenAndServe()
	}()
	shutdown := func() error {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
	select {
	case <-setupServer.Completed():
		if err := shutdown(); err != nil {
			return fmt.Errorf("shutdown setup server: %w", err)
		}
		return nil
	case <-ctx.Done():
		if err := shutdown(); err != nil {
			return fmt.Errorf("shutdown setup server: %w", err)
		}
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve setup HTTP: %w", err)
	}
}
