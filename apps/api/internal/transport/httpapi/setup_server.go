package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	postgresqladapter "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
)

// SetupServer 是数据库尚未配置时的最小 HTTP 表面。它只提供 SPA 与两个接口，
// 不连接数据库、不加载任何业务服务，写入有效配置后即通知调用方转入常规启动。
type SetupServer struct {
	spa               http.Handler
	logger            *slog.Logger
	settingsDirectory string
	migrationsDir     string
	completed         chan struct{}
	once              sync.Once
	writing           sync.Mutex
}

func NewSetupServer(
	webDistPath, settingsDirectory, migrationsDir string,
	logger *slog.Logger,
) (*SetupServer, error) {
	spa, err := newSPAHandler(webDistPath)
	if err != nil {
		return nil, fmt.Errorf("configure web application: %w", err)
	}
	return &SetupServer{
		spa: spa, logger: logger, settingsDirectory: settingsDirectory,
		migrationsDir: migrationsDir, completed: make(chan struct{}),
	}, nil
}

// Completed 在数据库配置成功写入后关闭。
func (s *SetupServer) Completed() <-chan struct{} { return s.completed }

func (s *SetupServer) Handler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /api/v1/setup", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, map[string]any{
			"required": true, "database_required": true,
		})
	})
	router.HandleFunc("POST /api/v1/setup/database", s.configureDatabase)
	router.HandleFunc("POST /api/v1/setup/database/test", s.testDatabase)
	// 未配置数据库时不得报告就绪，避免编排层把它当成可用实例。
	router.HandleFunc("GET /api/v1/ready", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusServiceUnavailable, map[string]any{
			"status": "database_not_configured",
		})
	})
	// 未注册的 API 路径必须返回 JSON。落到 SPA 会让前端把 200 + HTML 当成
	// 有效响应（例如把 /api/v1/session 误判为已登录），从而绕过初始化引导。
	router.HandleFunc("/api/", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]any{
				"code":    "database_not_configured",
				"message": "该部署尚未配置数据库",
			},
		})
	})
	router.Handle("/", s.spa)
	return router
}

type databaseSetupRequest struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"ssl_mode"`
}

// testDatabase 只做一次连接验证，不写入任何持久文件，供页面上的「检测连接」使用。
func (s *SetupServer) testDatabase(response http.ResponseWriter, request *http.Request) {
	input, settings, err := decodeDatabaseSetup(request)
	if err != nil {
		writeSetupFailure(response, http.StatusBadRequest, err.Error())
		return
	}
	password := []byte(input.Password)
	defer clear(password)
	verifyCtx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	if err := postgresqladapter.VerifyConnectionSettings(
		verifyCtx, s.settingsDirectory, settings, password, s.migrationsDir,
	); err != nil {
		writeSetupFailure(response, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"connected": true})
}

func decodeDatabaseSetup(request *http.Request) (databaseSetupRequest, postgresqladapter.ConnectionSettings, error) {
	var input databaseSetupRequest
	decoder := json.NewDecoder(io.LimitReader(request.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, postgresqladapter.ConnectionSettings{}, errors.New("请求格式不正确")
	}
	port, err := postgresqladapter.ParsePort(input.Port)
	if err != nil {
		return input, postgresqladapter.ConnectionSettings{}, err
	}
	sslMode := strings.TrimSpace(input.SSLMode)
	if sslMode == "" {
		sslMode = "disable"
	}
	return input, postgresqladapter.ConnectionSettings{
		Host:     strings.TrimSpace(input.Host),
		Port:     port,
		Database: strings.TrimSpace(input.Database),
		User:     strings.TrimSpace(input.User),
		SSLMode:  sslMode,
	}, nil
}

func (s *SetupServer) configureDatabase(response http.ResponseWriter, request *http.Request) {
	s.writing.Lock()
	defer s.writing.Unlock()
	select {
	case <-s.completed:
		writeSetupFailure(response, http.StatusConflict, "数据库已经配置完成，请刷新页面")
		return
	default:
	}

	input, settings, err := decodeDatabaseSetup(request)
	if err != nil {
		writeSetupFailure(response, http.StatusBadRequest, err.Error())
		return
	}
	password := []byte(input.Password)
	defer clear(password)

	// 先写入受保护的密码文件再验证，使验证走与正式运行完全相同的读取路径。
	if err := postgresqladapter.SaveConnectionSettings(s.settingsDirectory, settings, password); err != nil {
		writeSetupFailure(response, http.StatusBadRequest, err.Error())
		return
	}
	verifyCtx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	probe := settings.Config(
		postgresqladapter.PasswordFilePath(s.settingsDirectory), s.migrationsDir,
	)
	if err := postgresqladapter.VerifyConnection(verifyCtx, probe); err != nil {
		postgresqladapter.RemoveConnectionSettings(s.settingsDirectory)
		writeSetupFailure(response, http.StatusBadRequest, err.Error())
		return
	}
	s.logger.Info("setup: database settings stored", "host", settings.Host, "database", settings.Database)
	s.once.Do(func() { close(s.completed) })
	response.WriteHeader(http.StatusNoContent)
}

func writeSetupFailure(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]any{"code": "setup_database_invalid", "message": message},
	})
}
