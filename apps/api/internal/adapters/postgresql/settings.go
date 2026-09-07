package postgresqladapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ConnectionSettings 是首启初始化页写入持久卷的数据库连接描述。
// 它只保存非秘密字段；密码始终单独落在同目录树下的 0600 文件里，
// 与 Compose 硬化路径使用同一份 readPasswordFile 契约。
type ConnectionSettings struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	SSLMode  string `json:"ssl_mode"`
}

// ErrConnectionSettingsAbsent 表示该部署尚未通过任何来源配置数据库。
var ErrConnectionSettingsAbsent = errors.New("PostgreSQL connection settings are not configured")

func (s ConnectionSettings) validate() error {
	if strings.TrimSpace(s.Host) == "" {
		return errors.New("数据库地址不能为空")
	}
	if strings.ContainsAny(s.Host, " \t\r\n/@") {
		return errors.New("数据库地址包含无效字符")
	}
	if s.Port == 0 {
		return errors.New("数据库端口必须是 1–65535")
	}
	if strings.TrimSpace(s.Database) == "" || strings.TrimSpace(s.User) == "" {
		return errors.New("数据库名称和账号不能为空")
	}
	if s.SSLMode != "disable" && s.SSLMode != "verify-full" {
		return errors.New("SSL 模式必须是 disable 或 verify-full")
	}
	return nil
}

// LoadConnectionSettings 读取持久卷中的连接描述；文件不存在时返回
// ErrConnectionSettingsAbsent，交由调用方进入首启配置流程。
func LoadConnectionSettings(path string) (ConnectionSettings, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ConnectionSettings{}, ErrConnectionSettingsAbsent
	}
	if err != nil {
		return ConnectionSettings{}, fmt.Errorf("read PostgreSQL settings: %w", err)
	}
	var settings ConnectionSettings
	if err := json.Unmarshal(content, &settings); err != nil {
		return ConnectionSettings{}, fmt.Errorf("parse PostgreSQL settings: %w", err)
	}
	if err := settings.validate(); err != nil {
		return ConnectionSettings{}, fmt.Errorf("stored PostgreSQL settings are invalid: %w", err)
	}
	return settings, nil
}

// SaveConnectionSettings 原子写入连接描述与独立的密码文件。
// 密码文件权限为 0600，路径不进入配置文件之外的任何输出。
func SaveConnectionSettings(directory string, settings ConnectionSettings, password []byte) error {
	if err := settings.validate(); err != nil {
		return err
	}
	if len(password) == 0 || len(password) > 1024 {
		return errors.New("数据库密码长度必须为 1–1024 字节")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	if err := writeProtectedFile(PasswordFilePath(directory), password); err != nil {
		return err
	}
	content, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode PostgreSQL settings: %w", err)
	}
	return writeProtectedFile(SettingsFilePath(directory), content)
}

// SettingsFilePath 与 PasswordFilePath 固定持久卷内的布局，避免调用方各自拼接。
func SettingsFilePath(directory string) string {
	return filepath.Join(directory, "database.json")
}

func PasswordFilePath(directory string) string {
	return filepath.Join(directory, "postgres-password")
}

func writeProtectedFile(path string, content []byte) error {
	candidate, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	defer os.Remove(candidate.Name())
	if err := candidate.Chmod(0o600); err != nil {
		candidate.Close()
		return fmt.Errorf("protect %s: %w", filepath.Base(path), err)
	}
	if _, err := candidate.Write(content); err != nil {
		candidate.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := candidate.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	return os.Rename(candidate.Name(), path)
}

// Config 由连接描述与密码文件路径组合而成，其余字段沿用镜像内固定值。
func (s ConnectionSettings) Config(passwordFile, migrationsDir string) Config {
	return Config{
		Host: s.Host, Port: s.Port, Database: s.Database, User: s.User,
		PasswordFile: passwordFile, SSLMode: s.SSLMode,
		MigrationsDir: migrationsDir, MaxOpenConnections: 32,
	}
}

// VerifyConnection 在写入配置前实际连接一次，避免把不可用的设置固化下来。
// 返回的错误只区分「连不上」与「认证或库名不对」，不透出驱动原始信息。
func VerifyConnection(ctx context.Context, config Config) error {
	db, err := openDatabase(config)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		var opError *net.OpError
		if errors.As(err, &opError) || errors.Is(err, context.DeadlineExceeded) {
			return errors.New("无法连接到该地址和端口，请检查数据库是否可达")
		}
		return errors.New("已连接到数据库，但账号、密码或数据库名称不正确")
	}
	return nil
}

func ParsePort(value string) (uint16, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("数据库端口必须是 1–65535")
	}
	return uint16(port), nil
}

// RemoveConnectionSettings 在验证失败后清除刚写入的配置与密码，
// 使部署回到「未配置」状态而不是固化一份连不上的设置。
func RemoveConnectionSettings(directory string) {
	_ = os.Remove(SettingsFilePath(directory))
	_ = os.Remove(PasswordFilePath(directory))
}

// VerifyConnectionSettings 只验证一组连接信息是否可用，不改动任何持久文件。
// 密码写入同目录下的一次性 0600 文件并在返回前删除，使验证与正式运行走
// 完全相同的读取路径，避免「测试通过但保存后连不上」。
func VerifyConnectionSettings(
	ctx context.Context,
	directory string,
	settings ConnectionSettings,
	password []byte,
	migrationsDir string,
) error {
	if err := settings.validate(); err != nil {
		return err
	}
	if len(password) == 0 || len(password) > 1024 {
		return errors.New("数据库密码长度必须为 1–1024 字节")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	probe, err := os.CreateTemp(directory, "probe-password.*")
	if err != nil {
		return fmt.Errorf("create probe password: %w", err)
	}
	defer os.Remove(probe.Name())
	if err := probe.Chmod(0o600); err != nil {
		probe.Close()
		return fmt.Errorf("protect probe password: %w", err)
	}
	if _, err := probe.Write(password); err != nil {
		probe.Close()
		return fmt.Errorf("write probe password: %w", err)
	}
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close probe password: %w", err)
	}
	return VerifyConnection(ctx, settings.Config(probe.Name(), migrationsDir))
}
