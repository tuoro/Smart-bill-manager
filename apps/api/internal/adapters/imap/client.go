// Package imap 是 ports.MailboxClient 的 go-imap 实现：登录、只读打开收件箱、按 UID
// 增量取原始邮件。它不做任何解析，也不改邮箱里的任何东西（不打已读、不移动、不删）。
package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/mailsync"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// 首次接入只回溯这么久：用户要的是最近的票，不是整个邮箱的历史。
const InitialLookback = 30 * 24 * time.Hour

type Client struct {
	// TLSConfig 允许测试注入自签证书；生产用系统根证书。
	TLSConfig *tls.Config
	// AllowInsecure 只给测试用：对内存 IMAP 服务器走明文。生产永远为 false。
	AllowInsecure bool
	Now           func() time.Time
}

func New() *Client { return &Client{Now: time.Now} }

func (c *Client) Probe(ctx context.Context, credentials ports.MailboxCredentials) error {
	client, err := c.connect(ctx, credentials)
	if err != nil {
		return err
	}
	defer client.Close()
	if _, err := client.Select("INBOX", &goimap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return fmt.Errorf("%w: open INBOX: %v", mailsync.ErrUnreachable, err)
	}
	_ = client.Logout().Wait()
	return nil
}

func (c *Client) FetchNew(
	ctx context.Context,
	credentials ports.MailboxCredentials,
	state ports.MailboxState,
	limit int,
	handle func(ports.MailboxMessage) error,
) (ports.MailboxState, error) {
	client, err := c.connect(ctx, credentials)
	if err != nil {
		return state, err
	}
	defer client.Close()
	selected, err := client.Select("INBOX", &goimap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return state, fmt.Errorf("%w: open INBOX: %v", mailsync.ErrUnreachable, err)
	}
	// UIDVALIDITY 变了说明服务器重编了号，旧游标作废，按首次接入处理。
	criteria := &goimap.SearchCriteria{}
	next := ports.MailboxState{UIDValidity: selected.UIDValidity, LastUID: state.LastUID}
	if state.UIDValidity != selected.UIDValidity || state.LastUID == 0 {
		next.LastUID = 0
		now := time.Now
		if c.Now != nil {
			now = c.Now
		}
		criteria.Since = now().Add(-InitialLookback)
	} else {
		criteria.UID = []goimap.UIDSet{{{Start: goimap.UID(state.LastUID + 1), Stop: 0}}}
	}
	found, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return state, fmt.Errorf("%w: search: %v", mailsync.ErrUnreachable, err)
	}
	uids := found.AllUIDs()
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if next.LastUID == 0 && len(uids) == 0 {
		// 首次接入且回溯窗口里没有邮件：游标定在当前 UIDNEXT 之前，之后只看新邮件。
		if selected.UIDNext > 0 {
			next.LastUID = uint32(selected.UIDNext) - 1
		}
		return next, nil
	}
	var pending []goimap.UID
	for _, uid := range uids {
		if uint32(uid) > next.LastUID {
			pending = append(pending, uid)
		}
	}
	if len(pending) > limit {
		pending = pending[:limit]
	}
	section := &goimap.FetchItemBodySection{Peek: true}
	for _, uid := range pending {
		options := &goimap.FetchOptions{UID: true, InternalDate: true, RFC822Size: true, BodySection: []*goimap.FetchItemBodySection{section}}
		buffers, err := client.Fetch(goimap.UIDSetNum(uid), options).Collect()
		if err != nil {
			return next, fmt.Errorf("%w: fetch: %v", mailsync.ErrUnreachable, err)
		}
		for _, buffer := range buffers {
			raw := buffer.FindBodySection(section)
			if buffer.RFC822Size > domain.MaxEmailMessageBytes || int64(len(raw)) > domain.MaxEmailMessageBytes || len(raw) == 0 {
				// 超限或空邮件无法安全归档：跳过并推进游标，不让它卡住后面的邮件。
				next.LastUID = uint32(uid)
				continue
			}
			if err := handle(ports.MailboxMessage{UID: uint32(uid), UIDValidity: selected.UIDValidity, InternalDate: buffer.InternalDate, Raw: raw}); err != nil {
				return next, err
			}
			next.LastUID = uint32(uid)
		}
	}
	_ = client.Logout().Wait()
	return next, nil
}

func (c *Client) connect(ctx context.Context, credentials ports.MailboxCredentials) (*imapclient.Client, error) {
	address := net.JoinHostPort(credentials.Host, strconv.Itoa(credentials.Port))
	deadline := 30 * time.Second
	if until, ok := ctx.Deadline(); ok {
		deadline = time.Until(until)
	}
	options := &imapclient.Options{
		TLSConfig: c.TLSConfig,
		Dialer:    &net.Dialer{Timeout: deadline},
	}
	var client *imapclient.Client
	var err error
	switch {
	case c.AllowInsecure:
		client, err = imapclient.DialInsecure(address, options)
	case credentials.TransportSecurity == domain.EmailTransportSTARTTLS:
		client, err = imapclient.DialStartTLS(address, options)
	default:
		client, err = imapclient.DialTLS(address, options)
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return nil, fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
		}
		return nil, fmt.Errorf("%w: dial: %v", mailsync.ErrUnreachable, err)
	}
	if err := client.Login(credentials.Username, string(credentials.Password)).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("%w: %v", mailsync.ErrCredentialsRejected, sanitize(err))
	}
	return client, nil
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// 服务器的 NO 响应文本可能回显用户名，日志里只留前几十个字符的类别信息。
func sanitize(err error) string {
	text := err.Error()
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	if len(text) > 80 {
		text = text[:80]
	}
	return text
}
