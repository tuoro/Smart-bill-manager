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
	"syscall"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/openaicompatible"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/processing"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/providers"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/transport/httpapi"
)

const version = "m1-dev"

type config struct {
	databasePath         string
	migrationsDir        string
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
	store  *sqliteadapter.Store
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
	store, err := sqliteadapter.Open(ctx, sqliteadapter.Config{
		DatabasePath:  config.databasePath,
		MigrationsDir: config.migrationsDir,
	})
	if err != nil {
		return err
	}
	defer store.Close()
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
	reviewService := reviews.NewService(store, store, system.IDGenerator{}, system.Clock{})
	factService := reviews.NewFactService(store, store, system.IDGenerator{}, system.Clock{})
	allocationService := allocations.NewService(store, store, system.IDGenerator{}, system.Clock{})
	httpServer, err := httpapi.NewServer(
		authService,
		uploadService,
		documentQueries,
		jobActions,
		documentDeletions,
		providerService,
		reviewService,
		factService,
		allocationService,
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
	value := config{
		databasePath:         os.Getenv("SBM_DATABASE_PATH"),
		migrationsDir:        os.Getenv("SBM_MIGRATIONS_DIR"),
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
		"SBM_DATABASE_PATH":          value.databasePath,
		"SBM_MIGRATIONS_DIR":         value.migrationsDir,
		"SBM_HTTP_ADDRESS":           value.httpAddress,
		"SBM_COOKIE_SECURE":          os.Getenv("SBM_COOKIE_SECURE"),
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
	cookieSecure, err := strconv.ParseBool(os.Getenv("SBM_COOKIE_SECURE"))
	if err != nil {
		return config{}, errors.New("SBM_COOKIE_SECURE must be true or false")
	}
	sessionTTL, err := time.ParseDuration(os.Getenv("SBM_SESSION_TTL"))
	if err != nil {
		return config{}, fmt.Errorf("parse SBM_SESSION_TTL: %w", err)
	}
	value.cookieSecure = cookieSecure
	if value.deploymentMode != "local" && value.deploymentMode != "production" {
		return config{}, errors.New("SBM_DEPLOYMENT_MODE must be local or production")
	}
	if value.deploymentMode == "production" && !cookieSecure {
		return config{}, errors.New("SBM_COOKIE_SECURE must be true in production mode")
	}
	value.sessionTTL = sessionTTL
	aiConcurrency, err := strconv.Atoi(os.Getenv("SBM_AI_CONCURRENCY"))
	if err != nil || aiConcurrency < 1 || aiConcurrency > 8 {
		return config{}, errors.New("SBM_AI_CONCURRENCY must be an integer between 1 and 8")
	}
	value.aiConcurrency = aiConcurrency
	return value, nil
}
