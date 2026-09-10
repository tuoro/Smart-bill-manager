package domain

import "time"

const (
	ChatConnectorDetectionPending = "pending"
	ChatConnectorDetectionPassed  = "passed"
	ChatConnectorDetectionFailed  = "failed"
)

// ChatConnector 是一个工作区在某个平台上的机器人凭据。密文只在服务层解密，
// 对外（HTTP、日志）只暴露 HasSecret。
type ChatConnector struct {
	TenantID           string
	Platform           string
	AppKey             string
	EncryptedAppSecret []byte
	DetectionStatus    string
	DetectionCheckedAt *time.Time
	DetectionMessage   string
	Active             bool
	Version            int64
	UpdatedByUserID    string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func ChatConnectorDetectionRequired() error {
	return NewRuleError("chat_connector_detection_required", "凭据必须先通过检测才能启用", ErrConflict)
}
