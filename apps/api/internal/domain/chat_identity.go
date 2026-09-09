package domain

import (
	"errors"
	"time"
)

// 目前只有钉钉。它是唯一一个既能收文件、又能用出站长连接（Stream 模式）接收
// 事件的通道——本系统只监听回环，开不了公网回调。
const ChatPlatformDingTalk = "dingtalk"

// 未绑定的发送者一律拒收，不落任何默认租户。能给机器人发消息不等于有权往账
// 目里投单据：这条通道绕开了登录会话，身份必须显式建立过才算数。
var ErrChatSenderNotLinked = errors.New("chat sender is not linked to a member")

// 绑定码要经由聊天软件传递，暴露面比会话令牌大，所以有效期取得很短。
const ChatBindingCodeTTL = 10 * time.Minute

// 无效、过期、已用过，对外都是同一句话：不告诉尝试者他猜到了哪一步。
func InvalidChatBindingCode() error {
	return NewRuleError(
		"invalid_chat_binding_code",
		"绑定码无效或已失效，请在网页重新生成",
		ErrInvalidInput,
	)
}

// 换绑必须是明确动作，不能靠持有一张码就把别人名下的账号挪过来。
func ChatAccountAlreadyBound() error {
	return NewRuleError(
		"chat_account_already_bound",
		"该聊天账号已绑定到其他成员，请先由对方解除绑定",
		ErrConflict,
	)
}

func ValidChatPlatform(platform string) bool {
	return platform == ChatPlatformDingTalk
}

// ChatBinding 是展示给成员看的当前绑定。外部账号标识原样给出而不做遮罩：这是
// 他自己的账号，看得见才好确认绑的是不是手上这一个。
type ChatBinding struct {
	Platform       string
	ExternalUserID string
	CreatedAt      time.Time
}

// ChatIdentity 把一个外部账号解析成确定的租户成员。一个外部账号最多对应一个
// 成员，歧义在收单路径上不可接受。
// ChatBindingCode 是待兑换的一次性凭据；只有哈希会落库。
type ChatBindingCode struct {
	ID        string
	TenantID  string
	UserID    string
	Platform  string
	CodeHash  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type ChatIdentity struct {
	Platform       string
	ExternalUserID string
	TenantID       string
	UserID         string
	Role           Role
}

// 投递按该成员在租户里的真实角色鉴权，与网页上传同一套能力判定：只读成员
// 通过聊天同样投不进来。
func (i ChatIdentity) TenantContext() TenantContext {
	return TenantContext{TenantID: i.TenantID, UserID: i.UserID, Role: i.Role}
}
