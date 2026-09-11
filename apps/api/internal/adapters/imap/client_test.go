package imap

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/mailsync"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type literal struct{ *bytes.Reader }

func (l literal) Size() int64 { return int64(l.Reader.Len()) }

func rawMail(subject string) []byte {
	return []byte("From: a@example.invalid\r\nTo: b@example.invalid\r\nSubject: " + subject + "\r\n\r\nbody\r\n")
}

// 起一个内存 IMAP 服务器：明文、一个用户、INBOX 里放几封不同日期的邮件。
func startMemServer(t *testing.T, appended ...func(*imapmemserver.User)) (string, int) {
	t.Helper()
	user := imapmemserver.NewUser("me@example.invalid", "secret-app-password")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	for _, fn := range appended {
		fn(user)
	}
	mem := imapmemserver.New()
	mem.AddUser(user)
	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapIMAP4rev2: {}},
		InsecureAuth: true,
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	address := listener.Addr().(*net.TCPAddr)
	return address.IP.String(), address.Port
}

func appendMail(t *testing.T, user *imapmemserver.User, subject string, when time.Time) {
	t.Helper()
	raw := rawMail(subject)
	if _, err := user.Append("INBOX", literal{bytes.NewReader(raw)}, &goimap.AppendOptions{Time: when}); err != nil {
		t.Fatal(err)
	}
}

func TestProbeDistinguishesBadPasswordFromUnreachable(t *testing.T) {
	host, port := startMemServer(t)
	client := &Client{AllowInsecure: true, Now: time.Now}
	ctx := context.Background()
	good := ports.MailboxCredentials{Host: host, Port: port, TransportSecurity: "implicit_tls", Username: "me@example.invalid", Password: []byte("secret-app-password")}
	if err := client.Probe(ctx, good); err != nil {
		t.Fatalf("probe = %v", err)
	}
	bad := good
	bad.Password = []byte("nope")
	if err := client.Probe(ctx, bad); !errors.Is(err, mailsync.ErrCredentialsRejected) {
		t.Fatalf("bad password error = %v", err)
	}
	if strings.Contains(errorText(client.Probe(ctx, bad)), "nope") {
		t.Fatal("password leaked into error")
	}
	unreachable := good
	unreachable.Port = 1
	if err := client.Probe(ctx, unreachable); !errors.Is(err, mailsync.ErrUnreachable) {
		t.Fatalf("unreachable error = %v", err)
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// 首次接入只回溯 30 天；之后按 UID 增量；游标只推进到最后一封成功处理的邮件。
func TestFetchNewLooksBackThenContinuesByUID(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	var user *imapmemserver.User
	host, port := startMemServer(t, func(u *imapmemserver.User) {
		user = u
		appendMail(t, u, "ancient", now.Add(-90*24*time.Hour))
		appendMail(t, u, "recent-1", now.Add(-2*24*time.Hour))
		appendMail(t, u, "recent-2", now.Add(-1*24*time.Hour))
	})
	client := &Client{AllowInsecure: true, Now: func() time.Time { return now }}
	credentials := ports.MailboxCredentials{Host: host, Port: port, TransportSecurity: "implicit_tls", Username: "me@example.invalid", Password: []byte("secret-app-password")}
	ctx := context.Background()
	var seen []string
	state, err := client.FetchNew(ctx, credentials, ports.MailboxState{}, 50, func(message ports.MailboxMessage) error {
		seen = append(seen, subjectOf(message.Raw))
		if message.UID == 0 || message.UIDValidity == 0 {
			t.Fatalf("message identity missing: %#v", message)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, ",") != "recent-1,recent-2" || state.LastUID != 3 {
		t.Fatalf("first sync seen=%v state=%#v", seen, state)
	}
	// 新邮件到达后只拿新的。
	appendMail(t, user, "recent-3", now)
	seen = nil
	state, err = client.FetchNew(ctx, credentials, state, 50, func(message ports.MailboxMessage) error {
		seen = append(seen, subjectOf(message.Raw))
		return nil
	})
	if err != nil || strings.Join(seen, ",") != "recent-3" || state.LastUID != 4 {
		t.Fatalf("incremental sync seen=%v state=%#v err=%v", seen, state, err)
	}
	// 处理失败：游标停在失败之前那封。
	appendMail(t, user, "recent-4", now)
	appendMail(t, user, "recent-5", now)
	failing := errors.New("archive failed")
	seen = nil
	state, err = client.FetchNew(ctx, credentials, state, 50, func(message ports.MailboxMessage) error {
		seen = append(seen, subjectOf(message.Raw))
		if subjectOf(message.Raw) == "recent-5" {
			return failing
		}
		return nil
	})
	if !errors.Is(err, failing) || state.LastUID != 5 {
		t.Fatalf("failure handling seen=%v state=%#v err=%v", seen, state, err)
	}
	// limit 限制一轮数量。
	seen = nil
	state, err = client.FetchNew(ctx, credentials, ports.MailboxState{UIDValidity: state.UIDValidity, LastUID: 0}, 1, func(message ports.MailboxMessage) error {
		seen = append(seen, subjectOf(message.Raw))
		return nil
	})
	if err != nil || len(seen) != 1 {
		t.Fatalf("limit seen=%v err=%v", seen, err)
	}
}

func subjectOf(raw []byte) string {
	for _, line := range strings.Split(string(raw), "\r\n") {
		if strings.HasPrefix(line, "Subject: ") {
			return strings.TrimPrefix(line, "Subject: ")
		}
	}
	return ""
}
