package domain

import "errors"

// 目前只有钉钉。它是唯一一个既能收文件、又能用出站长连接（Stream 模式）接收
// 事件的通道——本系统只监听回环，开不了公网回调。
const ChatPlatformDingTalk = "dingtalk"

// 未绑定的发送者一律拒收，不落任何默认租户。能给机器人发消息不等于有权往账
// 目里投单据：这条通道绕开了登录会话，身份必须显式建立过才算数。
var ErrChatSenderNotLinked = errors.New("chat sender is not linked to a member")

func ValidChatPlatform(platform string) bool {
	return platform == ChatPlatformDingTalk
}

// ChatIdentity 把一个外部账号解析成确定的租户成员。一个外部账号最多对应一个
// 成员，歧义在收单路径上不可接受。
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
