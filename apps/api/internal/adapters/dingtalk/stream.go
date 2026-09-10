package dingtalk

import (
	"context"
	"log/slog"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
)

// Run 维持一条到钉钉的出站长连接（Stream 模式），直到 ctx 结束。
// 不需要公网入站：系统主动连出去，与 IMAP 轮询同一个形状。
//
// SDK 自带断线重连；这里只兜首次握手失败——比如密钥错、网络不通——按退避重试，
// 而不是让整个服务进程因为钉钉连不上而退出：收单通道断了不该拖垮记账本身。
func Run(ctx context.Context, appKey, appSecret string, handler *Handler, logger *slog.Logger) {
	backoff := 5 * time.Second
	for {
		cli := client.NewStreamClient(client.WithAppCredential(client.NewAppCredentialConfig(appKey, appSecret)))
		cli.RegisterChatBotCallbackRouter(chatbot.IChatBotMessageHandler(handler.Handle))
		err := cli.Start(ctx)
		if err == nil {
			logger.Info("dingtalk stream connected")
			<-ctx.Done()
			cli.Close()
			return
		}
		logger.Error("dingtalk stream start failed", "error", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 5*time.Minute {
			backoff *= 2
		}
	}
}
