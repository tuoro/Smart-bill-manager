package dingtalk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatintake"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// 回复文案以 docs/design/chat-intake-dialogue.md 为准；改文案先改那份文档。
const (
	replyBound          = "已绑定。以后直接把支付截图或发票发给我即可。"
	replyInvalidCode    = "绑定码无效或已失效，请在网页「账号与密码」重新生成。"
	replyNotLinked      = "还没绑定账号。请先在网页「账号与密码」生成绑定码并发给我。"
	replyForbidden      = "你的账号没有投递单据的权限。"
	replyDuplicate      = "这份文件之前已收过，不再重复识别。"
	replyUnsupportedMsg = "目前只支持发送图片、PDF，或回复绑定码。"
	replyBadContent     = "这个文件不是图片或 PDF，无法识别。"
	replyTooLarge       = "文件超过 20 MiB，无法接收。"
	replyDownloadFailed = "文件下载失败，请重新发送。"
	replyInternal       = "系统暂时无法处理，请稍后重试。"
	replyWrongTenant    = "你的账号绑定在另一个工作区，请使用那个工作区的机器人。"
)

// Downloader 与 Replier 是处理器的两条网络边，抽成接口是为了让处理逻辑不依赖
// 真实钉钉就能测：分派、身份、去重、回复文案都在这一层。
type Downloader interface {
	DownloadMessageFile(ctx context.Context, downloadCode string) ([]byte, error)
}

type Replier interface {
	SimpleReplyText(ctx context.Context, sessionWebhook string, content []byte) error
}

type Handler struct {
	// 这条连接属于哪个工作区。发送者绑在别的工作区就拒绝，在写入前。
	tenantID string
	intake   chatintake.Service
	files    Downloader
	reply    Replier
	logger   *slog.Logger
}

func NewHandler(tenantID string, intake chatintake.Service, files Downloader, reply Replier, logger *slog.Logger) *Handler {
	return &Handler{tenantID: tenantID, intake: intake, files: files, reply: reply, logger: logger}
}

// Handle 是 SDK 回调。无论结果如何都返回成功 ack：处理失败要靠回复告诉用户，
// 而不是让钉钉重投——重投只会让同一份文件再撞一次去重。
func (h *Handler) Handle(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	reply := h.respond(ctx, data)
	if reply != "" {
		if err := h.reply.SimpleReplyText(ctx, data.SessionWebhook, []byte(reply)); err != nil {
			h.logger.Warn("dingtalk reply failed", "msg_id", data.MsgId, "error", err)
		}
	}
	return nil, nil
}

// 发送者标识用 senderStaffId：它是企业内稳定的 userid，也是主动发消息接口
// 认的那个；senderId 是会话级的不透明串，换个会话就变。
func (h *Handler) respond(ctx context.Context, data *chatbot.BotCallbackDataModel) string {
	sender := strings.TrimSpace(data.SenderStaffId)
	switch data.Msgtype {
	case "text":
		return h.handleText(ctx, sender, data.Text.Content)
	case "file":
		content := contentMap(data.Content)
		name := strings.TrimSpace(stringField(content, "fileName"))
		if name == "" {
			name = "file-" + data.MsgId
		}
		return h.handleFile(ctx, sender, stringField(content, "downloadCode"), name)
	case "picture":
		content := contentMap(data.Content)
		return h.handleFile(ctx, sender, stringField(content, "downloadCode"), "image-"+data.MsgId)
	default:
		return replyUnsupportedMsg
	}
}

func (h *Handler) handleText(ctx context.Context, sender, text string) string {
	code := strings.TrimSpace(text)
	if code == "" {
		return replyUnsupportedMsg
	}
	_, err := h.intake.RedeemBindingCode(ctx, domain.ChatPlatformDingTalk, sender, code, h.tenantID)
	switch {
	case err == nil:
		return replyBound
	case errors.Is(err, chatintake.ErrTenantMismatch):
		return replyWrongTenant
	case errors.Is(err, domain.ErrConflict):
		// 账号已绑在别人名下：把规则里那句说明原样给用户。
		return ruleMessage(err, replyInvalidCode)
	case errors.Is(err, domain.ErrInvalidInput):
		return replyInvalidCode
	default:
		h.logger.Error("dingtalk binding failed", "error", err)
		return replyInternal
	}
}

func (h *Handler) handleFile(ctx context.Context, sender, downloadCode, name string) string {
	if downloadCode == "" {
		return replyDownloadFailed
	}
	content, err := h.files.DownloadMessageFile(ctx, downloadCode)
	if err != nil {
		if errors.Is(err, ErrFileTooLarge) {
			return replyTooLarge
		}
		h.logger.Warn("dingtalk download failed", "error", err)
		return replyDownloadFailed
	}
	mime := sniffMIME(content)
	if mime == "" {
		return replyBadContent
	}
	// 上传路径要求文件名后缀、声明类型、字节签名三者一致。图片消息钉钉不给文件名，
	// 名字是这里合成的，按探测结果补后缀；文件消息保留钉钉给的名字，后缀撒谎就让
	// 既有校验拒掉，不在这里替它圆场。
	name = ensureExtension(name, mime)
	_, err = h.intake.Receive(ctx, chatintake.Message{
		Platform:       domain.ChatPlatformDingTalk,
		ExternalUserID: sender,
		FileName:       name,
		MIME:           mime,
		Source:         bytes.NewReader(content),
		TenantID:       h.tenantID,
	})
	var duplicate *domain.DuplicateDocumentError
	switch {
	case err == nil:
		return fmt.Sprintf("已收到 %s，正在识别。", name)
	case errors.Is(err, domain.ErrChatSenderNotLinked):
		return replyNotLinked
	case errors.Is(err, chatintake.ErrTenantMismatch):
		return replyWrongTenant
	case errors.As(err, &duplicate):
		return replyDuplicate
	case errors.Is(err, domain.ErrForbidden):
		return replyForbidden
	case errors.Is(err, domain.ErrInvalidInput):
		return ruleMessage(err, replyBadContent)
	default:
		h.logger.Error("dingtalk intake failed", "error", err)
		return replyInternal
	}
}

func contentMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func ruleMessage(err error, fallback string) string {
	var rule *domain.RuleError
	if errors.As(err, &rule) && rule.Message != "" {
		return rule.Message
	}
	return fallback
}

func ensureExtension(name, mime string) string {
	if strings.Contains(name, ".") {
		return name
	}
	switch mime {
	case "image/jpeg":
		return name + ".jpg"
	case "image/png":
		return name + ".png"
	case "image/webp":
		return name + ".webp"
	case "application/pdf":
		return name + ".pdf"
	}
	return name
}
