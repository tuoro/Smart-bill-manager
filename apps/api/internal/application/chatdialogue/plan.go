package chatdialogue

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// plan 是一段对话攒下的决定。它只在会话里活着，攒齐后一次性交给与网页同一条
// 确认用例；对话中断就跟着会话一起消失，不会留下半个决定。
type plan struct {
	// 正在等新值的字段路径。
	FieldPath string `json:"field_path,omitempty"`
	// 疑似重复逐笔判断的进度与结果。
	DuplicateIndex int                          `json:"duplicate_index,omitempty"`
	Duplicates     []domain.DuplicateResolution `json:"duplicates,omitempty"`
	// 关联决定：mode 为空表示还没决定。
	Mode        string                     `json:"mode,omitempty"`
	Chosen      []string                   `json:"chosen,omitempty"`
	ChosenIndex int                        `json:"chosen_index,omitempty"`
	Allocations []domain.AllocationRequest `json:"allocations,omitempty"`
}

func decodePlan(raw string) plan {
	var decoded plan
	if raw == "" {
		return decoded
	}
	_ = json.Unmarshal([]byte(raw), &decoded)
	return decoded
}

func (p plan) encode() string {
	encoded, err := json.Marshal(p)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// resolved 回答"这份单据现在可以确认了吗"：该判的重复都判了，该挑的关联挑过了。
func (p plan) resolved(review ports.ReviewSnapshot) bool {
	if len(review.DuplicateCandidates) > 0 && len(p.Duplicates) != len(review.DuplicateCandidates) {
		return false
	}
	if len(review.Candidates) > 0 && p.Mode == "" {
		return false
	}
	return true
}

// parseFieldNumbers 解析"1"或"1,3"这类编号回复，返回 0 基下标。
func parseFieldNumbers(text string, limit int) ([]int, bool) {
	text = strings.NewReplacer("，", ",", " ", "", "、", ",").Replace(strings.TrimSpace(text))
	if text == "" {
		return nil, false
	}
	seen := map[int]bool{}
	result := make([]int, 0, 4)
	for _, part := range strings.Split(text, ",") {
		number, err := strconv.Atoi(part)
		if err != nil || number < 1 || number > limit || seen[number] {
			return nil, false
		}
		seen[number] = true
		result = append(result, number-1)
	}
	return result, true
}

// ChatInstantLayout 是聊天里唯一接受的时间写法。固定格式避免"9/10"「12点45」这类
// 歧义写法引出错误的业务日期与重复判断；时区不在聊天里改。
const ChatInstantLayout = "2006-01-02 15:04"

// parseFieldValue 把用户回的一句话变成该字段的值。看不懂就明确说该怎么写，
// 不猜、不四舍五入、不自动换时区。
func parseFieldValue(review ports.ReviewSnapshot, field ports.ReviewField, text string) (json.RawMessage, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", errBadValue("值不能为空。")
	}
	switch field.ValueType {
	case "string":
		if len([]rune(text)) > 500 {
			return nil, "", errBadValue("内容不能超过 500 个字符。")
		}
		encoded, _ := json.Marshal(text)
		return encoded, text, nil
	case "money_minor":
		currency := domain.Currency(fieldText(review, "currency"))
		money, err := domain.ParseMoney(text, currency)
		if err != nil {
			return nil, "", errBadValue("金额要写成 12.34 这样的数字，且小数位不能超过 " + string(currency) + " 的精度。")
		}
		encoded, _ := json.Marshal(money.MinorUnits)
		return encoded, formatMinorUnits(money.MinorUnits, currency), nil
	case "instant":
		zone := fieldText(review, "source_timezone")
		location, err := time.LoadLocation(zone)
		if err != nil || zone == "" {
			return nil, "", errBadValue("这份单据没有可用的时区，时间请到网页修改。")
		}
		parsed, err := time.ParseInLocation(ChatInstantLayout, text, location)
		if err != nil {
			return nil, "", errBadValue("时间请按 年-月-日 时:分 回复，24 小时制，例如 2026-09-10 12:30。时区保持 " + zone + " 不变。")
		}
		encoded, _ := json.Marshal(parsed.Format(time.RFC3339))
		return encoded, parsed.Format(ChatInstantLayout) + "  " + zone, nil
	case "date":
		parsed, err := time.Parse("2006-01-02", text)
		if err != nil {
			return nil, "", errBadValue("日期请按 年-月-日 回复，例如 2026-09-10。")
		}
		encoded, _ := json.Marshal(parsed.Format("2006-01-02"))
		return encoded, parsed.Format("2006-01-02"), nil
	case "integer":
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, "", errBadValue("请回复一个整数。")
		}
		encoded, _ := json.Marshal(number)
		return encoded, text, nil
	case "decimal":
		if _, err := strconv.ParseFloat(text, 64); err != nil {
			return nil, "", errBadValue("请回复一个数字。")
		}
		encoded, _ := json.Marshal(text)
		return encoded, text, nil
	default:
		return nil, "", errBadValue("这个字段不能在聊天里修改，请到网页处理。")
	}
}

type badValueError struct{ message string }

func (e badValueError) Error() string { return e.message }

func errBadValue(message string) error { return badValueError{message: message} }
