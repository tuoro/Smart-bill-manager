package domain

import (
	"encoding/base64"
	"net/mail"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const MaxPendingInvitations = 100

// NormalizeLoginIdentifier 规范化登录标识符。系统不发送任何邮件，因此标识符
// 可以是邮箱，也可以是纯用户名；两者共用同一列与唯一索引，已有邮箱账号不受影响。
// 含 "@" 时按邮箱严格校验，否则按用户名字符集校验。
func NormalizeLoginIdentifier(value string) (string, error) {
	identifier := strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
	if strings.Contains(identifier, "@") {
		address, err := mail.ParseAddress(identifier)
		if err != nil || address.Address != identifier || len(identifier) > 254 {
			return "", newInvalidLoginIdentifier()
		}
		return identifier, nil
	}
	if len(identifier) < 3 || len(identifier) > 64 {
		return "", newInvalidLoginIdentifier()
	}
	for index, character := range identifier {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
		case (character == '.' || character == '_' || character == '-') && index > 0:
		default:
			return "", newInvalidLoginIdentifier()
		}
	}
	return identifier, nil
}

func newInvalidLoginIdentifier() error {
	return NewRuleError(
		"invalid_login_identifier",
		"登录标识符必须是邮箱，或 3–64 位小写字母、数字、点、下划线、连字符（不能以符号开头）",
		ErrInvalidInput,
	)
}

func NormalizeAccountName(value string) (string, error) {
	name := strings.TrimSpace(norm.NFKC.String(value))
	if !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > 100 {
		return "", NewRuleError("invalid_display_name", "姓名长度必须为 1–100 个字符", ErrInvalidInput)
	}
	return name, nil
}

func NormalizeAccountReason(value string) (string, error) {
	reason := strings.TrimSpace(value)
	// 理由可选：操作者对自己的操作负责。
	if !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > 500 {
		return "", NewRuleError("invalid_reason", "理由不能超过 500 个字符", ErrInvalidInput)
	}
	return reason, nil
}

func ValidInvitationToken(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}

func InvalidInvitation() error {
	return NewRuleError("invalid_invitation", "邀请代码无效或已失效，请联系工作区管理员", ErrInvalidInput)
}

func InvalidCredentials() error {
	return NewRuleError("invalid_credentials", "邮箱或密码不正确，请重新核对", ErrUnauthenticated)
}
