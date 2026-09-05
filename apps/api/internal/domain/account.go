package domain

import (
	"encoding/base64"
	"net/mail"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const MaxPendingInvitations = 100

func NormalizeLoginEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return "", NewRuleError("invalid_email", "登录邮箱格式不正确", ErrInvalidInput)
	}
	return email, nil
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
	if !utf8.ValidString(reason) || reason == "" || utf8.RuneCountInString(reason) > 500 {
		return "", NewRuleError("invalid_reason", "操作理由长度必须为 1–500 个字符", ErrInvalidInput)
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
