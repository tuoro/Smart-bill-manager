package chatdialogue

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// 卡片上的字段顺序与标签。顺序固定，编号才稳定——用户回编号改字段时不能因为
// 识别结果多了一项就指到别的字段上。未列出的字段按路径排在后面。
var fieldOrder = map[domain.DocumentType][]string{
	domain.DocumentPayment: {"merchant", "merchant_full_name", "amount_minor", "transaction_time", "payment_method", "order_number", "category"},
	domain.DocumentInvoice: {"seller_name", "buyer_name", "total_minor", "invoice_date", "invoice_number", "invoice_code"},
	domain.DocumentTrip:    {"carrier", "origin", "destination", "departure_time", "arrival_time", "passenger_name", "ticket_number"},
}

var fieldLabels = map[string]string{
	"merchant": "商户", "merchant_full_name": "商户全称", "amount_minor": "金额",
	"transaction_time": "时间", "payment_method": "支付方式", "order_number": "订单号",
	"category": "分类", "seller_name": "销售方", "buyer_name": "购买方", "total_minor": "金额",
	"invoice_date": "开票日期", "invoice_number": "发票号", "invoice_code": "发票代码",
	"carrier": "承运", "origin": "出发", "destination": "到达", "departure_time": "出发时间",
	"arrival_time": "到达时间", "passenger_name": "乘车人", "ticket_number": "票号",
	"currency": "币种", "source_timezone": "时区",
}

// 这几项不单独占一行：币种并进金额、时区并进时间、类型是卡片本身的标题。
var foldedFields = map[string]bool{
	"currency": true, "source_timezone": true, "document_type": true, "supplementary_fields": true,
}

func documentTypeLabel(kind domain.DocumentType) string {
	switch kind {
	case domain.DocumentPayment:
		return "支付"
	case domain.DocumentInvoice:
		return "发票"
	case domain.DocumentTrip:
		return "行程"
	default:
		return "单据"
	}
}

// visibleFields 按固定顺序挑出要显示的字段，编号即它们在结果里的位置。
func visibleFields(review ports.ReviewSnapshot) []ports.ReviewField {
	byPath := make(map[string]ports.ReviewField, len(review.Fields))
	for _, field := range review.Fields {
		byPath[field.Path] = field
	}
	result := make([]ports.ReviewField, 0, len(review.Fields))
	seen := map[string]bool{}
	for _, path := range fieldOrder[review.DocumentType] {
		if field, ok := byPath[path]; ok && !foldedFields[path] {
			result = append(result, field)
			seen[path] = true
		}
	}
	for _, field := range review.Fields {
		if !seen[field.Path] && !foldedFields[field.Path] {
			result = append(result, field)
		}
	}
	return result
}

func stringValue(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return strings.TrimSpace(string(raw))
}

// formatMinorUnits 按币种精度还原成人看的金额。币种缺失时只报数字，不猜。
func formatMinorUnits(minor int64, currency domain.Currency) string {
	exponent, ok := currency.Exponent()
	if !ok {
		return strconv.FormatInt(minor, 10)
	}
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	if exponent == 0 {
		return fmt.Sprintf("%s%s %d", sign, currency, minor)
	}
	scale := int64(1)
	for range exponent {
		scale *= 10
	}
	return fmt.Sprintf("%s%s %d.%0*d", sign, currency, minor/scale, exponent, minor%scale)
}

// formatFieldValue 把一个字段渲染成一行值。金额要币种、时刻要时区，两者都从
// 同一份识别结果里取，取不到就退回原样显示，不臆造。
func formatFieldValue(field ports.ReviewField, review ports.ReviewSnapshot) string {
	if field.Presence != "present" {
		return "（未识别）"
	}
	switch field.ValueType {
	case "money_minor":
		var minor int64
		if json.Unmarshal(field.Value, &minor) != nil {
			return stringValue(field.Value)
		}
		return formatMinorUnits(minor, domain.Currency(fieldText(review, "currency")))
	case "instant":
		instant, err := time.Parse(time.RFC3339Nano, stringValue(field.Value))
		if err != nil {
			return stringValue(field.Value)
		}
		zone := fieldText(review, "source_timezone")
		location, err := time.LoadLocation(zone)
		if err != nil || zone == "" {
			return instant.UTC().Format("2006-01-02 15:04") + " UTC"
		}
		return instant.In(location).Format("2006-01-02 15:04") + "  " + zone
	default:
		text := stringValue(field.Value)
		if text == "" {
			return "（空）"
		}
		return text
	}
}

func fieldText(review ports.ReviewSnapshot, path string) string {
	for _, field := range review.Fields {
		if field.Path == path && field.Presence == "present" {
			return stringValue(field.Value)
		}
	}
	return ""
}

func fieldLabel(path string) string {
	if label, ok := fieldLabels[path]; ok {
		return label
	}
	return path
}

// renderCard 是识别完推给用户的那张结果卡，也是每次改完字段、判完重复后回到的
// 那一屏。编号固定，用户回编号就能改对应字段。
func renderCard(review ports.ReviewSnapshot, documentName string, current plan, interrupted string) string {
	var builder strings.Builder
	if interrupted != "" {
		builder.WriteString("上一份 " + interrupted + " 未确认，已留在网页待审核。\n\n")
	}
	builder.WriteString(documentName + " 识别结果（" + documentTypeLabel(review.DocumentType) + "）：\n")
	for index, field := range visibleFields(review) {
		builder.WriteString(fmt.Sprintf("%d. %s  %s\n", index+1, fieldLabel(field.Path), formatFieldValue(field, review)))
	}
	pending := make([]string, 0, 2)
	if len(review.DuplicateCandidates) > 0 && len(current.Duplicates) != len(review.DuplicateCandidates) {
		pending = append(pending, fmt.Sprintf("· 有 %d 笔疑似重复", len(review.DuplicateCandidates)))
	}
	if len(review.Candidates) > 0 && current.Mode == "" {
		pending = append(pending, fmt.Sprintf("· 找到 %d 张可关联的单据", len(review.Candidates)))
	}
	if len(pending) > 0 {
		builder.WriteString("\n需要你判断：\n" + strings.Join(pending, "\n"))
		builder.WriteString("\n回复「处理」逐项处理；回复编号修改字段；回复「作废」丢弃。")
		return builder.String()
	}
	if summary := renderPlanSummary(review, current); summary != "" {
		builder.WriteString("\n" + summary)
	}
	builder.WriteString("\n回复「确认」保存；回复编号修改字段；回复「作废」丢弃。")
	return builder.String()
}

// renderPlanSummary 把已经做出的关联与重复判断复述一遍，确认前让人再看一眼。
func renderPlanSummary(review ports.ReviewSnapshot, current plan) string {
	lines := make([]string, 0, 3)
	if len(current.Duplicates) > 0 {
		lines = append(lines, fmt.Sprintf("· 已判定 %d 笔疑似重复为独立记录", len(current.Duplicates)))
	}
	switch current.Mode {
	case "reject_all":
		lines = append(lines, "· 不关联任何单据")
	case "allocate_candidates":
		for _, allocation := range current.Allocations {
			for _, candidate := range review.Candidates {
				if candidate.ID == allocation.CandidateID {
					lines = append(lines, "· 关联 "+candidateLabel(candidate)+" "+
						formatMinorUnits(allocation.AllocatedMinor, domain.Currency(candidate.Currency)))
				}
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func candidateLabel(candidate ports.LinkCandidate) string {
	parts := []string{}
	if candidate.BusinessDate != "" {
		parts = append(parts, candidate.BusinessDate)
	}
	if candidate.DisplayName != "" {
		parts = append(parts, candidate.DisplayName)
	}
	parts = append(parts, formatMinorUnits(candidate.AmountMinor, domain.Currency(candidate.Currency)))
	return strings.Join(parts, " ")
}

func duplicateLabel(candidate ports.DuplicateCandidate) string {
	parts := []string{}
	if candidate.BusinessDate != "" {
		parts = append(parts, candidate.BusinessDate)
	}
	if candidate.DisplayName != "" {
		parts = append(parts, candidate.DisplayName)
	}
	if candidate.AmountMinor != nil {
		parts = append(parts, formatMinorUnits(*candidate.AmountMinor, domain.Currency(candidate.Currency)))
	}
	if len(parts) == 0 {
		return "一份内容高度相似的单据"
	}
	return strings.Join(parts, " ")
}

// renderCandidateChoices 列出可关联的单据，编号即回复时用的编号。
func renderCandidateChoices(review ports.ReviewSnapshot) string {
	var builder strings.Builder
	builder.WriteString("可关联的单据：\n")
	for index, candidate := range review.Candidates {
		builder.WriteString(fmt.Sprintf("%d. %s · 剩余 %s\n", index+1, candidateLabel(candidate),
			formatMinorUnits(candidate.RemainingMinor, domain.Currency(candidate.Currency))))
	}
	builder.WriteString("回复编号关联（可多选，如 1,3），或回复「不关联」。")
	return builder.String()
}

func jsonNumber(raw json.RawMessage, target *int64) bool {
	return json.Unmarshal(raw, target) == nil
}
