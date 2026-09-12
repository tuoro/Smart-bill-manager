// Package chatdialogue 把「识别完了」变成聊天里的一问一答：推结果卡、改字段、
// 判重复、挑关联，最后收「确认」或「作废」。
//
// 它不自己判断业务，也不自己写库：修订、确认与驳回都走 reviews.Service 的同一条
// 用例，与网页上的按钮完全等价。连接器只负责把文本递进来、把回复送出去。
package chatdialogue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatintake"
	insightapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/insights"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const (
	// 产品决定：20 分钟提醒一次（只这一次），30 分钟收尾放回网页队列。
	RemindAfter = 20 * time.Minute
	ExpireAfter = 30 * time.Minute
	// 一轮扫描最多处理这么多会话，避免单轮无界。
	SweepBatch = 100

	StateAwaitingDecision   = "awaiting_decision"
	StateAwaitingFieldValue = "awaiting_field_value"
	StateAwaitingDuplicate  = "awaiting_duplicate"
	StateAwaitingCandidates = "awaiting_candidates"
	StateAwaitingAmount     = "awaiting_allocation_amount"
)

const (
	replyBound            = "已绑定。以后直接把支付截图或发票发给我即可。"
	replyInvalidCode      = "绑定码无效或已失效，请在网页「账号与密码」重新生成。"
	replyNotLinked        = "还没绑定账号。请先在网页「账号与密码」生成绑定码并发给我。"
	replyWrongTenant      = "这个绑定码属于另一个工作区，请用本工作区生成的绑定码。"
	replyNoSession        = "现在没有待确认的单据。直接把支付截图或发票发给我即可。"
	replyUnparsed         = "没看懂。当前可回复：确认 / 编号 / 作废，或回复「菜单」。"
	replyInternal         = "系统忙，请稍后再试。"
	replyStale            = "这条单据刚被他人修改，请到网页核对。"
	replyDiscarded        = "已作废，不生成记录。"
	replyExpiredSession   = "这份单据已经不在等待状态了，请到网页查看。"
	replyNeedsResolve     = "还有要判断的项目，请先回复「处理」。"
	replyNothingToResolve = "没有要判断的项目，直接回复「确认」保存即可。"
	replyUnavailableHere  = "这项查询暂时不可用，请到网页查看。"
)

type Service struct {
	tx       ports.TransactionManager
	sessions ports.ChatSessionRepository
	reviews  reviews.Service
	intake   chatintake.Service
	notifier ports.ChatNotifier
	clock    ports.Clock
	logger   *slog.Logger
	// 菜单里的两项查询。没接上时只是不提供查询，对话本身照常。
	insights InsightQuery
	jobs     JobQuery
}

// InsightQuery 与 JobQuery 只取聊天要用的那一点点，用接口声明清楚边界：
// 对话服务不碰别的读取面。
type InsightQuery interface {
	Query(ctx context.Context, tenant domain.TenantContext, input insightapp.QueryInput) (insightapp.Page, error)
}

type JobQuery interface {
	ListJobs(ctx context.Context, tenant domain.TenantContext, status *domain.JobStatus) ([]ports.JobSummary, error)
}

// WithQueries 接上菜单里的两项查询。
func (s Service) WithQueries(insights InsightQuery, jobs JobQuery) Service {
	s.insights = insights
	s.jobs = jobs
	return s
}

func NewService(
	tx ports.TransactionManager,
	sessions ports.ChatSessionRepository,
	reviewService reviews.Service,
	intake chatintake.Service,
	notifier ports.ChatNotifier,
	clock ports.Clock,
	logger *slog.Logger,
) Service {
	return Service{tx: tx, sessions: sessions, reviews: reviewService, intake: intake,
		notifier: notifier, clock: clock, logger: logger}
}

// Announce 在一份单据识别结束后推回执。不是聊天投进来的单据静默跳过——网页上传
// 的人不该突然收到钉钉消息。
func (s Service) Announce(ctx context.Context, tenantID, jobID string) error {
	recipient, err := s.sessions.FindChatRecipient(ctx, tenantID, jobID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	identity := recipient.Identity
	if recipient.JobStatus != domain.JobNeedsReview {
		// 失败或被取消：说一声就结束，不开会话，也不动别的单据正在进行的对话。
		message := recipient.DocumentName + " 识别失败。可重新拍一张，或到网页转人工录入。"
		if recipient.SafeError != "" {
			message = recipient.DocumentName + " 识别失败：" + recipient.SafeError + "。可重新拍一张，或到网页转人工录入。"
		}
		return s.notifier.Send(ctx, identity.TenantID, identity.Platform, identity.ExternalUserID, message)
	}
	review, err := s.reviews.Get(ctx, identity.TenantContext(), jobID)
	if err != nil {
		return err
	}
	interrupted := ""
	if previous, err := s.sessions.FindChatSession(ctx, identity.Platform, identity.ExternalUserID); err == nil && previous.JobID != jobID {
		interrupted = previous.DocumentName
	}
	now := s.clock.Now()
	session := ports.ChatSession{
		Platform: identity.Platform, ExternalUserID: identity.ExternalUserID,
		TenantID: identity.TenantID, UserID: identity.UserID, JobID: jobID,
		DocumentName: recipient.DocumentName, State: StateAwaitingDecision,
		PlanJSON: plan{}.encode(), ExpectedRevision: review.Revision, StartedAt: now, UpdatedAt: now,
	}
	if err := s.save(ctx, session); err != nil {
		return err
	}
	return s.notifier.Send(ctx, identity.TenantID, identity.Platform, identity.ExternalUserID,
		renderCard(review, recipient.DocumentName, plan{}, interrupted))
}

// Handle 处理一条文本消息：没绑定就当绑定码，绑定了就当对当前单据的答复。
func (s Service) Handle(ctx context.Context, platform, externalUserID, text, tenantID string) string {
	text = strings.TrimSpace(text)
	if externalUserID == "" {
		return replyNotLinked
	}
	session, err := s.sessions.FindChatSession(ctx, platform, externalUserID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return s.handleWithoutSession(ctx, platform, externalUserID, text, tenantID)
	case err != nil:
		s.logger.Error("chat session lookup failed", "error", err)
		return replyInternal
	}
	// 作废在任何一步都能说，包括改字段改到一半。
	if isAny(text, "作废", "丢弃", "删除") {
		return s.reject(ctx, session)
	}
	tenant := domain.TenantContext{TenantID: session.TenantID, UserID: session.UserID, Role: domain.RoleMember}
	if reply, handled := s.menuCommand(ctx, tenant, text); handled {
		return reply
	}
	review, err := s.reviews.Get(ctx, tenant, session.JobID)
	if err != nil {
		return s.decisionError(err, session)
	}
	current := decodePlan(session.PlanJSON)
	switch session.State {
	case StateAwaitingFieldValue:
		return s.applyFieldValue(ctx, session, review, current, text)
	case StateAwaitingDuplicate:
		return s.answerDuplicate(ctx, session, review, current, text)
	case StateAwaitingCandidates:
		return s.answerCandidates(ctx, session, review, current, text)
	case StateAwaitingAmount:
		return s.answerAmount(ctx, session, review, current, text)
	}
	switch {
	case isMenuRequest(text):
		return renderMenu(&session, &review, current)
	case isAny(text, "确认", "确定", "保存"):
		if !current.resolved(review) {
			return replyNeedsResolve
		}
		return s.confirm(ctx, session, review, current)
	case isAny(text, "处理", "逐项处理"):
		return s.startResolving(ctx, session, review, current)
	default:
		if indexes, ok := parseFieldNumbers(text, len(visibleFields(review))); ok && len(indexes) == 1 {
			return s.startFieldEdit(ctx, session, review, current, indexes[0])
		}
		return replyUnparsed
	}
}

// 没有进行中的单据时，文本的含义取决于这个账号绑没绑：没绑就只可能是绑定码；
// 绑了就是没事可做，给一句「现在没有待确认的单据」——不去撞「绑定码无效」那条，
// 那句会让已经绑好的人以为自己掉线了。
func (s Service) handleWithoutSession(ctx context.Context, platform, externalUserID, text, tenantID string) string {
	if identity, err := s.intake.FindIdentity(ctx, platform, externalUserID); err == nil {
		if reply, handled := s.menuCommand(ctx, identity.TenantContext(), text); handled {
			return reply
		}
		if isMenuRequest(text) {
			return renderMenu(nil, nil, plan{})
		}
		return replyNoSession + "\n回复「菜单」看还能做什么。"
	}
	if text == "" {
		return replyNotLinked
	}
	_, err := s.intake.RedeemBindingCode(ctx, platform, externalUserID, text, tenantID)
	switch {
	case err == nil:
		return replyBound
	case errors.Is(err, chatintake.ErrTenantMismatch):
		return replyWrongTenant
	case errors.Is(err, domain.ErrConflict):
		return ruleMessage(err, replyInvalidCode)
	case errors.Is(err, domain.ErrInvalidInput), errors.Is(err, domain.ErrNotFound):
		return replyInvalidCode
	default:
		s.logger.Error("chat binding failed", "error", err)
		return replyInternal
	}
}

// menuCommand 处理与当前单据无关的那几条：漏票、待审核。有单据在手时同样可用
// ——查一眼不改变任何状态。
func (s Service) menuCommand(ctx context.Context, tenant domain.TenantContext, text string) (string, bool) {
	switch {
	case isAny(text, "漏票", "缺发票", "漏发票"):
		return s.gapReport(ctx, tenant), true
	case isAny(text, "待审核", "待确认"):
		return s.pendingReport(ctx, tenant), true
	default:
		return "", false
	}
}

// ── 改字段 ────────────────────────────────────────────────────────────────

func (s Service) startFieldEdit(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan, index int) string {
	field := visibleFields(review)[index]
	// AI 识别的单据只能引用它自己给出的证据；没有证据的字段改不了，如实说明。
	if field.Presence == "present" && len(field.Evidence) == 0 && review.OriginAiRunID != "" {
		return "「" + fieldLabel(field.Path) + "」没有可引用的原件摘录，请到网页修改。"
	}
	current.FieldPath = field.Path
	session.State = StateAwaitingFieldValue
	session.PlanJSON = current.encode()
	if err := s.save(ctx, session); err != nil {
		return replyInternal
	}
	prompt := "把「" + fieldLabel(field.Path) + "」改成什么？当前 " + formatFieldValue(field, review) + "。"
	if field.ValueType == "instant" {
		prompt += "\n请按 年-月-日 时:分 回复，24 小时制，例如 2026-09-10 12:30。时区保持不变。"
	}
	return prompt
}

func (s Service) applyFieldValue(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan, text string) string {
	var target ports.ReviewField
	for _, field := range review.Fields {
		if field.Path == current.FieldPath {
			target = field
		}
	}
	if target.Path == "" {
		return s.backToCard(ctx, session, review, current, "这个字段已经不在当前识别结果里了。")
	}
	value, display, err := parseFieldValue(review, target, text)
	if err != nil {
		var bad badValueError
		if errors.As(err, &bad) {
			return bad.message
		}
		return replyInternal
	}
	tenant := domain.TenantContext{TenantID: session.TenantID, UserID: session.UserID, Role: domain.RoleMember}
	fields := make([]reviews.RevisionFieldInput, 0, len(review.Fields))
	for _, field := range review.Fields {
		// document_type 由修订的独立属性承载，不能混在字段列表里。
		if field.Path == "document_type" {
			continue
		}
		entry := reviews.RevisionFieldInput{
			Path: field.Path, ValueType: field.ValueType, Presence: field.Presence, Value: field.Value,
		}
		for _, evidence := range field.Evidence {
			entry.EvidenceIDs = append(entry.EvidenceIDs, evidence.ID)
		}
		if field.Path == current.FieldPath {
			entry.Value = value
			entry.Presence = "present"
		}
		fields = append(fields, entry)
	}
	updated, err := s.reviews.Revise(ctx, tenant, session.JobID, reviews.RevisionInput{
		ExpectedRevision:          review.Revision,
		ExpectedOptimisticVersion: review.OptimisticVersion,
		DocumentType:              review.DocumentType,
		Fields:                    fields,
	})
	if err != nil {
		var rule *domain.RuleError
		if errors.As(err, &rule) {
			return rule.Message
		}
		return s.decisionError(err, session)
	}
	// 修订换了一版 Claim，重复候选与关联候选都重算过，之前攒的决定随之作废。
	session.ExpectedRevision = updated.Revision
	return s.backToCard(ctx, session, updated, plan{}, fieldLabel(current.FieldPath)+" → "+display+"。")
}

// ── 逐项判断 ──────────────────────────────────────────────────────────────

func (s Service) startResolving(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan) string {
	if current.resolved(review) {
		return replyNothingToResolve
	}
	return s.nextQuestion(ctx, session, review, current, "")
}

// nextQuestion 推进到下一个待判断项：先判重复，再挑关联，都判完回到结果卡。
func (s Service) nextQuestion(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan, prefix string) string {
	if len(current.Duplicates) < len(review.DuplicateCandidates) {
		candidate := review.DuplicateCandidates[len(current.Duplicates)]
		current.DuplicateIndex = len(current.Duplicates)
		session.State = StateAwaitingDuplicate
		session.PlanJSON = current.encode()
		if err := s.save(ctx, session); err != nil {
			return replyInternal
		}
		return prefix + "疑似重复：" + duplicateLabel(candidate) + "。\n这是同一笔吗？回复「同一笔」作废本次，或「不是」保留为独立记录。"
	}
	if len(review.Candidates) > 0 && current.Mode == "" {
		session.State = StateAwaitingCandidates
		session.PlanJSON = current.encode()
		if err := s.save(ctx, session); err != nil {
			return replyInternal
		}
		return prefix + renderCandidateChoices(review)
	}
	return s.backToCard(ctx, session, review, current, prefix)
}

func (s Service) answerDuplicate(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan, text string) string {
	if current.DuplicateIndex >= len(review.DuplicateCandidates) {
		return s.backToCard(ctx, session, review, current, "")
	}
	candidate := review.DuplicateCandidates[current.DuplicateIndex]
	switch {
	case isAny(text, "同一笔", "是同一笔", "重复", "是"):
		// 判定为同一笔就等于这份单据不该存在：与「作废」同一条路。
		return s.reject(ctx, session)
	case isAny(text, "不是", "不同", "保留", "独立"):
		current.Duplicates = append(current.Duplicates, domain.DuplicateResolution{
			CandidateID: candidate.ID, Action: domain.DuplicateKeepDistinct,
		})
		return s.nextQuestion(ctx, session, review, current, "已记为独立记录。\n")
	default:
		return "回复「同一笔」作废本次，或「不是」保留为独立记录。"
	}
}

func (s Service) answerCandidates(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan, text string) string {
	if isAny(text, "不关联", "都不关联", "不用", "跳过") {
		current.Mode = "reject_all"
		current.Allocations = nil
		return s.nextQuestion(ctx, session, review, current, "已记为不关联。\n")
	}
	indexes, ok := parseFieldNumbers(text, len(review.Candidates))
	if !ok {
		return "回复编号关联（可多选，如 1,3），或回复「不关联」。"
	}
	current.Mode = "allocate_candidates"
	current.Chosen = nil
	current.ChosenIndex = 0
	current.Allocations = nil
	for _, index := range indexes {
		current.Chosen = append(current.Chosen, review.Candidates[index].ID)
	}
	// 只挑一张时不必再问金额：能分多少就分多少，说清楚分了多少即可。
	if len(current.Chosen) == 1 {
		candidate := candidateByID(review, current.Chosen[0])
		amount := min64(factAmount(review), candidate.RemainingMinor)
		if amount < 1 {
			return "这张单据已经没有可分配的余额了，请换一张或回复「不关联」。"
		}
		current.Allocations = []domain.AllocationRequest{{CandidateID: candidate.ID, AllocatedMinor: amount}}
		current.Chosen = nil
		return s.nextQuestion(ctx, session, review, current,
			"已关联 "+candidateLabel(candidate)+" "+formatMinorUnits(amount, domain.Currency(candidate.Currency))+"。\n")
	}
	return s.askAmount(ctx, session, review, current, "")
}

func (s Service) askAmount(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan, prefix string) string {
	if current.ChosenIndex >= len(current.Chosen) {
		current.Chosen = nil
		return s.nextQuestion(ctx, session, review, current, prefix)
	}
	candidate := candidateByID(review, current.Chosen[current.ChosenIndex])
	session.State = StateAwaitingAmount
	session.PlanJSON = current.encode()
	if err := s.save(ctx, session); err != nil {
		return replyInternal
	}
	remaining := factAmount(review) - allocatedSoFar(current)
	return prefix + candidateLabel(candidate) + " 分配多少？本单还剩 " +
		formatMinorUnits(remaining, domain.Currency(fieldText(review, "currency"))) +
		"，这张最多可接 " + formatMinorUnits(candidate.RemainingMinor, domain.Currency(candidate.Currency)) + "。"
}

func (s Service) answerAmount(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan, text string) string {
	if current.ChosenIndex >= len(current.Chosen) {
		return s.backToCard(ctx, session, review, current, "")
	}
	candidate := candidateByID(review, current.Chosen[current.ChosenIndex])
	currency := domain.Currency(fieldText(review, "currency"))
	money, err := domain.ParseMoney(strings.TrimSpace(text), currency)
	if err != nil {
		return "金额要写成 12.34 这样的数字。"
	}
	remaining := factAmount(review) - allocatedSoFar(current)
	if money.MinorUnits < 1 || money.MinorUnits > remaining || money.MinorUnits > candidate.RemainingMinor {
		return "分配金额必须大于 0，且不超过本单剩余 " + formatMinorUnits(remaining, currency) +
			" 与这张的可接额度 " + formatMinorUnits(candidate.RemainingMinor, domain.Currency(candidate.Currency)) + "。"
	}
	current.Allocations = append(current.Allocations, domain.AllocationRequest{
		CandidateID: candidate.ID, AllocatedMinor: money.MinorUnits,
	})
	current.ChosenIndex++
	return s.askAmount(ctx, session, review, current,
		"已分配 "+formatMinorUnits(money.MinorUnits, currency)+"。\n")
}

// ── 收尾 ──────────────────────────────────────────────────────────────────

func (s Service) backToCard(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan, prefix string) string {
	current.FieldPath = ""
	session.State = StateAwaitingDecision
	session.PlanJSON = current.encode()
	session.ExpectedRevision = review.Revision
	if err := s.save(ctx, session); err != nil {
		return replyInternal
	}
	return prefix + renderCard(review, session.DocumentName, current, "")
}

func (s Service) confirm(ctx context.Context, session ports.ChatSession, review ports.ReviewSnapshot, current plan) string {
	tenant := domain.TenantContext{TenantID: session.TenantID, UserID: session.UserID, Role: domain.RoleMember}
	input := reviews.ConfirmInput{
		ExpectedRevision:     review.Revision,
		DuplicateResolutions: current.Duplicates,
		IdempotencyKey:       fmt.Sprintf("chat-confirm-%s-%d", session.JobID, review.Revision),
		RequestID:            "chat-confirm-" + session.JobID,
	}
	if input.DuplicateResolutions == nil {
		input.DuplicateResolutions = []domain.DuplicateResolution{}
	}
	// 行程凭证没有关联决定；支付与发票必须明确表态。
	if review.DocumentType != domain.DocumentTrip {
		input.Allocations = current.Allocations
		if input.Allocations == nil {
			input.Allocations = []domain.AllocationRequest{}
		}
		switch {
		case len(review.Candidates) == 0:
			input.AssociationMode = reviews.AssociationNoCandidate
		case current.Mode == "allocate_candidates":
			input.AssociationMode = reviews.AssociationAllocateCandidates
		default:
			input.AssociationMode = reviews.AssociationRejectAll
		}
	}
	if _, err := s.reviews.Confirm(ctx, tenant, session.JobID, input); err != nil {
		var rule *domain.RuleError
		if errors.As(err, &rule) {
			return rule.Message
		}
		return s.decisionError(err, session)
	}
	s.clearSession(ctx, session)
	return "已保存。" + summarize(review, session.DocumentName)
}

func (s Service) reject(ctx context.Context, session ports.ChatSession) string {
	tenant := domain.TenantContext{TenantID: session.TenantID, UserID: session.UserID, Role: domain.RoleMember}
	err := s.reviews.Reject(ctx, tenant, session.JobID, reviews.RejectInput{
		ExpectedRevision: session.ExpectedRevision,
		IdempotencyKey:   fmt.Sprintf("chat-reject-%s-%d", session.JobID, session.ExpectedRevision),
		RequestID:        "chat-reject-" + session.JobID,
	})
	if err != nil {
		return s.decisionError(err, session)
	}
	s.clearSession(ctx, session)
	return replyDiscarded
}

// 决定失败分三种：单据已经不在待审核（多半是网页上先处理了）、版本冲突、其他。
// 前两种都让人去网页核对，不在聊天里重试到底。
func (s Service) decisionError(err error, session ports.ChatSession) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return replyExpiredSession
	case errors.Is(err, domain.ErrVersionConflict), errors.Is(err, domain.ErrConflict):
		return replyStale
	default:
		s.logger.Error("chat decision failed", "job", session.JobID, "error", err)
		return replyInternal
	}
}

func (s Service) save(ctx context.Context, session ports.ChatSession) error {
	session.UpdatedAt = s.clock.Now()
	return s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.UpsertChatSession(ctx, session)
	})
}

func (s Service) clearSession(ctx context.Context, session ports.ChatSession) {
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.DeleteChatSession(ctx, session.Platform, session.ExternalUserID)
	}); err != nil {
		s.logger.Warn("clear chat session failed", "error", err)
	}
}

// Sweep 跑提醒与超时：20 分钟提醒一次，30 分钟结束对话。单据本身不动，它一直
// 都在网页的待审核队列里。
func (s Service) Sweep(ctx context.Context) error {
	now := s.clock.Now()
	stale, err := s.sessions.ListStaleChatSessions(ctx, now.Add(-RemindAfter), SweepBatch)
	if err != nil {
		return err
	}
	for _, session := range stale {
		age := now.Sub(session.StartedAt)
		switch {
		case age >= ExpireAfter:
			if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
				return t.DeleteChatSession(ctx, session.Platform, session.ExternalUserID)
			}); err != nil {
				return err
			}
			s.send(ctx, session, session.DocumentName+" 超过 30 分钟没有确认，已留在网页待审核队列。")
		case !session.Reminded:
			if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
				return t.MarkChatSessionReminded(ctx, session.Platform, session.ExternalUserID, now)
			}); err != nil {
				return err
			}
			s.send(ctx, session, session.DocumentName+" 还没确认，10 分钟后将放回网页待审核。")
		}
	}
	return nil
}

func (s Service) send(ctx context.Context, session ports.ChatSession, text string) {
	if err := s.notifier.Send(ctx, session.TenantID, session.Platform, session.ExternalUserID, text); err != nil {
		s.logger.Warn("chat notify failed", "platform", session.Platform, "error", err)
	}
}

// summarize 是保存后的一句回执：类型、名字、金额，够人一眼对上就行。
func summarize(review ports.ReviewSnapshot, documentName string) string {
	parts := []string{documentTypeLabel(review.DocumentType)}
	for _, path := range []string{"merchant", "seller_name", "destination"} {
		if text := fieldText(review, path); text != "" {
			parts = append(parts, text)
			break
		}
	}
	for _, field := range review.Fields {
		if field.ValueType == "money_minor" && field.Presence == "present" {
			parts = append(parts, formatFieldValue(field, review))
			break
		}
	}
	if len(parts) == 1 {
		return documentName + " 已入账。"
	}
	return strings.Join(parts, " · ") + "。"
}

func ruleMessage(err error, fallback string) string {
	var rule *domain.RuleError
	if errors.As(err, &rule) && rule.Message != "" {
		return rule.Message
	}
	return fallback
}

func isAny(text string, options ...string) bool {
	for _, option := range options {
		if text == option {
			return true
		}
	}
	return false
}

func candidateByID(review ports.ReviewSnapshot, id string) ports.LinkCandidate {
	for _, candidate := range review.Candidates {
		if candidate.ID == id {
			return candidate
		}
	}
	return ports.LinkCandidate{}
}

// factAmount 是这份单据自己的金额，分配总额不能超过它。
func factAmount(review ports.ReviewSnapshot) int64 {
	for _, path := range []string{"amount_minor", "total_minor"} {
		for _, field := range review.Fields {
			if field.Path == path && field.Presence == "present" {
				var minor int64
				if jsonNumber(field.Value, &minor) {
					return minor
				}
			}
		}
	}
	return 0
}

func allocatedSoFar(current plan) int64 {
	var total int64
	for _, allocation := range current.Allocations {
		total += allocation.AllocatedMinor
	}
	return total
}

func min64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
