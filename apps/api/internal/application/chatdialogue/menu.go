package chatdialogue

import (
	"context"
	"fmt"
	"strings"

	insightapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/insights"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// 聊天里只放"站着掏出手机就想知道"的那几件事。列表要翻要筛要对照的，留在网页——
// 在这里重做一遍只会更难用，还要维护第二套读取口径。
const (
	menuGapLimit     = 5
	menuPendingLimit = 3
)

// 菜单项用词不用数字：有单据在手时数字是"改第几个字段"，两者不能撞。
func isMenuRequest(text string) bool {
	return isAny(text, "菜单", "帮助", "help", "?", "？")
}

// renderMenu 按当前状态给出能做的事。手上有单据时先说这份单据怎么处理，
// 不把无关选项堆上来。
func renderMenu(session *ports.ChatSession, review *ports.ReviewSnapshot, current plan) string {
	var builder strings.Builder
	if session != nil && review != nil {
		builder.WriteString("当前在处理：" + session.DocumentName + "\n")
		if !current.resolved(*review) {
			builder.WriteString("· 处理 —— 逐项判断疑似重复与可关联的单据\n")
		} else {
			builder.WriteString("· 确认 —— 保存这份单据\n")
		}
		builder.WriteString("· 编号（如 3）—— 修改对应字段\n")
		builder.WriteString("· 作废 —— 丢弃这份单据\n\n")
	}
	builder.WriteString("随时可以：\n")
	builder.WriteString("· 直接发支付截图或发票 —— 进识别队列\n")
	builder.WriteString("· 漏票 —— 最近哪几笔支付还没有发票\n")
	builder.WriteString("· 待审核 —— 还有几份没确认")
	return builder.String()
}

// gapReport 回答"哪几笔支付还差发票"。口径与网页的漏票清单同一个用例，
// 不在这里另算一遍。
func (s Service) gapReport(ctx context.Context, tenant domain.TenantContext) string {
	if s.insights == nil {
		return replyUnavailableHere
	}
	page, err := s.insights.Query(ctx, tenant, insightapp.QueryInput{
		Filter: domain.InsightFilter{
			FactType:         string(domain.DocumentPayment),
			AllocationStatus: domain.InsightStatusIncomplete,
		},
		Limit: menuGapLimit + 1,
	})
	if err != nil {
		s.logger.Warn("chat gap report failed", "error", err)
		return replyInternal
	}
	if len(page.Items) == 0 {
		return "没有缺发票的支付，都齐了。"
	}
	var builder strings.Builder
	builder.WriteString("还缺发票的支付：\n")
	for index, item := range page.Items {
		if index == menuGapLimit {
			break
		}
		builder.WriteString(fmt.Sprintf("· %s %s %s（还差 %s）\n",
			item.BusinessDate, item.DisplayName,
			formatMinorUnits(item.AmountMinor, item.Currency),
			formatMinorUnits(item.RemainingMinor, item.Currency)))
	}
	if len(page.Items) > menuGapLimit {
		builder.WriteString("……还有更多，完整清单在网页「数据洞察」。")
	} else {
		builder.WriteString("把对应发票发给我即可补上。")
	}
	return strings.TrimRight(builder.String(), "\n")
}

// pendingReport 回答"还有几份没确认"。
func (s Service) pendingReport(ctx context.Context, tenant domain.TenantContext) string {
	if s.jobs == nil {
		return replyUnavailableHere
	}
	status := domain.JobNeedsReview
	items, err := s.jobs.ListJobs(ctx, tenant, &status)
	if err != nil {
		s.logger.Warn("chat pending report failed", "error", err)
		return replyInternal
	}
	if len(items) == 0 {
		return "没有待审核的单据。"
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("还有 %d 份待审核：\n", len(items)))
	for index, item := range items {
		if index == menuPendingLimit {
			builder.WriteString("……其余的在网页「AI 收件箱」。")
			return builder.String()
		}
		builder.WriteString("· " + item.OriginalName + "\n")
	}
	builder.WriteString("最近一份可以直接发「确认」处理——重新发一次它的截图也行。")
	return builder.String()
}
