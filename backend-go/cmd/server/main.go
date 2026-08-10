package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "time/tzdata"

	"smart-bill-manager/internal/app"
	"smart-bill-manager/internal/config"
	"smart-bill-manager/internal/migrations"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/pkg/database"
)

const shutdownTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal("服务退出: ", err)
	}
}

func run() error {
	log.Println("正在启动 Smart Bill Manager...")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取工作目录失败: %w", err)
	}
	log.Printf("运行环境: %s", cfg.NodeEnv)
	log.Printf("工作目录: %s", workingDir)
	if cfg.JWTSecretGenerated {
		log.Println("当前进程使用临时开发 JWT 密钥，服务重启后已有会话将失效")
	}

	db, err := database.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取数据库连接句柄失败: %w", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("关闭数据库连接失败: %v", err)
		}
	}()

	if err := migrations.Run(db); err != nil {
		return fmt.Errorf("执行数据库迁移失败: %w", err)
	}
	if err := services.EnsureEmailConfigPasswordsEncrypted(db); err != nil {
		return fmt.Errorf("加密历史邮箱密码失败: %w", err)
	}

	uploadsDir := cfg.UploadsDir
	if !filepath.IsAbs(uploadsDir) {
		uploadsDir = filepath.Join(workingDir, uploadsDir)
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return fmt.Errorf("创建上传目录失败: %w", err)
	}
	log.Printf("上传目录: %s", uploadsDir)

	application, err := app.New(cfg, db, uploadsDir)
	if err != nil {
		return fmt.Errorf("初始化应用失败: %w", err)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	application.Start(rootCtx)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           application.Router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErr <- err
	}()

	log.Printf("Smart Bill Manager API 已启动: http://localhost:%s", cfg.Port)
	select {
	case err := <-serverErr:
		if err != nil {
			log.Printf("HTTP 服务异常退出: %v", err)
		}
		stop()
		return errors.Join(err, shutdown(server, application))
	case <-rootCtx.Done():
		log.Println("收到退出信号，开始优雅停机")
		stop()
		return shutdown(server, application)
	}
}

func shutdown(server *http.Server, application *app.Application) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	httpErr := server.Shutdown(ctx)
	workerErr := application.Wait(ctx)
	if errors.Is(workerErr, context.Canceled) {
		workerErr = nil
	}
	return errors.Join(httpErr, workerErr)
}
