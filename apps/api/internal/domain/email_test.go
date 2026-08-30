package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalEmailSourceRegistrationNormalizesAndHashes(t *testing.T) {
	t.Parallel()
	canonical, first, err := CanonicalEmailSourceRegistration(EmailSourceRegistration{
		DisplayName: "  财务邮箱  ", MailboxAddress: "Bills@Example.Invalid",
		IMAPHost: "IMAP.Example.Invalid", IMAPPort: 993, TransportSecurity: EmailTransportImplicitTLS,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := CanonicalEmailSourceRegistration(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.DisplayName != "财务邮箱" || canonical.MailboxAddress != "bills@example.invalid" ||
		canonical.IMAPHost != "imap.example.invalid" || first != second || !ValidSHA256Hex(first) {
		t.Fatalf("canonical = %#v, hashes %q %q", canonical, first, second)
	}
}

func TestCanonicalEmailSourceRegistrationRejectsUnsafeBoundaries(t *testing.T) {
	t.Parallel()
	valid := EmailSourceRegistration{
		DisplayName: "财务邮箱", MailboxAddress: "bills@example.invalid",
		IMAPHost: "imap.example.invalid", IMAPPort: 993, TransportSecurity: EmailTransportImplicitTLS,
	}
	tests := []struct {
		name   string
		mutate func(*EmailSourceRegistration)
	}{
		{"blank name", func(value *EmailSourceRegistration) { value.DisplayName = " " }},
		{"display address", func(value *EmailSourceRegistration) { value.MailboxAddress = "Bills <bills@example.invalid>" }},
		{"host scheme", func(value *EmailSourceRegistration) { value.IMAPHost = "https://imap.example.invalid" }},
		{"host credential", func(value *EmailSourceRegistration) { value.IMAPHost = "user@imap.example.invalid" }},
		{"host label", func(value *EmailSourceRegistration) { value.IMAPHost = "-imap.example.invalid" }},
		{"zero port", func(value *EmailSourceRegistration) { value.IMAPPort = 0 }},
		{"plain transport", func(value *EmailSourceRegistration) { value.TransportSecurity = "plain" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if _, _, err := CanonicalEmailSourceRegistration(value); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateExternalMessageKey(t *testing.T) {
	t.Parallel()
	if err := ValidateExternalMessageKey(strings.Repeat("A", 64)); err == nil {
		t.Fatal("invalid key accepted")
	}
	if err := ValidateExternalMessageKey(strings.Repeat("a", 64)); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
}
