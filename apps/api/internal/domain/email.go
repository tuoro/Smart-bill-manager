package domain

import (
	"net"
	"net/mail"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	EmailSourceRegistrationVersion       = "email-source-registration/1"
	EmailArchiveVersion                  = "email-message-archive/1"
	EmailMIMEArchiveVersion              = "email-mime-archive/1"
	EmailSourcePendingConnection         = "pending_connection"
	EmailSourceActive                    = "active"
	EmailTransportImplicitTLS            = "implicit_tls"
	EmailTransportSTARTTLS               = "starttls"
	EmailMessageArchived                 = "archived"
	EmailMessageBlocked                  = "blocked"
	EmailAttachmentQueued                = "queued"
	EmailAttachmentExisting              = "existing_document"
	EmailAttachmentArchivedOnly          = "archived_only"
	DocumentIngestionUpload              = "upload"
	DocumentIngestionEmail               = "email_attachment"
	DocumentObjectOwnerDocument          = "document"
	DocumentObjectOwnerEmail             = "email_attachment"
	MaxEmailMessageBytes           int64 = 32 * 1024 * 1024
	MaxEmailMIMEDepth                    = 10
	MaxEmailMIMEParts                    = 200
	MaxEmailAttachments                  = 50
)

type EmailSourceRegistration struct {
	DisplayName       string `json:"display_name"`
	MailboxAddress    string `json:"mailbox_address"`
	IMAPHost          string `json:"imap_host"`
	IMAPPort          int    `json:"imap_port"`
	TransportSecurity string `json:"transport_security"`
}

func CanonicalEmailSourceRegistration(
	input EmailSourceRegistration,
) (EmailSourceRegistration, string, error) {
	displayName, err := normalizeBoundedText(input.DisplayName, 100, "invalid_email_source_name", "邮箱来源名称长度必须为 1–100 个字符")
	if err != nil {
		return EmailSourceRegistration{}, "", err
	}
	mailboxAddress, err := normalizeMailboxAddress(input.MailboxAddress)
	if err != nil {
		return EmailSourceRegistration{}, "", err
	}
	host, err := normalizeIMAPHost(input.IMAPHost)
	if err != nil {
		return EmailSourceRegistration{}, "", err
	}
	if input.IMAPPort < 1 || input.IMAPPort > 65535 {
		return EmailSourceRegistration{}, "", NewRuleError("invalid_imap_port", "IMAP 端口必须为 1–65535", ErrInvalidInput)
	}
	security := strings.TrimSpace(input.TransportSecurity)
	if security != EmailTransportImplicitTLS && security != EmailTransportSTARTTLS {
		return EmailSourceRegistration{}, "", NewRuleError("invalid_email_transport_security", "邮箱传输安全模式必须是 implicit_tls 或 starttls", ErrInvalidInput)
	}
	canonical := EmailSourceRegistration{
		DisplayName:       displayName,
		MailboxAddress:    mailboxAddress,
		IMAPHost:          host,
		IMAPPort:          input.IMAPPort,
		TransportSecurity: security,
	}
	payload := struct {
		Version string `json:"version"`
		EmailSourceRegistration
	}{EmailSourceRegistrationVersion, canonical}
	digest, err := hashJSON(payload)
	if err != nil {
		return EmailSourceRegistration{}, "", err
	}
	return canonical, digest, nil
}

func ValidateExternalMessageKey(value string) error {
	if !ValidSHA256Hex(value) {
		return NewRuleError("invalid_external_message_key", "外部邮件键必须是 64 位小写十六进制", ErrInvalidInput)
	}
	return nil
}

func normalizeMailboxAddress(value string) (string, error) {
	value = strings.TrimSpace(norm.NFKC.String(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || parsed.Name != "" || len([]rune(value)) < 3 || len([]rune(value)) > 254 {
		return "", NewRuleError("invalid_mailbox_address", "邮箱地址格式不正确", ErrInvalidInput)
	}
	parts := strings.Split(parsed.Address, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", NewRuleError("invalid_mailbox_address", "邮箱地址格式不正确", ErrInvalidInput)
	}
	return strings.ToLower(parts[0]) + "@" + strings.ToLower(parts[1]), nil
}

func normalizeIMAPHost(value string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
	if len(host) < 1 || len(host) > 253 || strings.ContainsAny(host, "/\\@?#[]") {
		return "", NewRuleError("invalid_imap_host", "IMAP 主机格式不正确", ErrInvalidInput)
	}
	for _, character := range host {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return "", NewRuleError("invalid_imap_host", "IMAP 主机格式不正确", ErrInvalidInput)
		}
	}
	if net.ParseIP(host) != nil {
		return host, nil
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", NewRuleError("invalid_imap_host", "IMAP 主机格式不正确", ErrInvalidInput)
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", NewRuleError("invalid_imap_host", "IMAP 主机格式不正确", ErrInvalidInput)
			}
		}
	}
	return host, nil
}

func normalizeBoundedText(value string, maximum int, code, message string) (string, error) {
	value = strings.TrimSpace(norm.NFKC.String(value))
	length := len([]rune(value))
	if length < 1 || length > maximum {
		return "", NewRuleError(code, message, ErrInvalidInput)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", NewRuleError(code, message, ErrInvalidInput)
		}
	}
	return value, nil
}
