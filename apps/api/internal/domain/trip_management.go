package domain

import (
	"strings"
	"time"
)

const TripTimeAttributionVersion = "trip-time-attribution/1"

// TripDetails 是用户管理的出差范围，不是票面提取结果。
type TripDetails struct {
	Name      string `json:"name"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Timezone  string `json:"timezone"`
	Notes     string `json:"notes"`
}

func NormalizeTripDetails(input TripDetails) (TripDetails, error) {
	value := TripDetails{
		Name: strings.TrimSpace(input.Name), StartDate: strings.TrimSpace(input.StartDate),
		EndDate: strings.TrimSpace(input.EndDate), Timezone: strings.TrimSpace(input.Timezone),
		Notes: strings.TrimSpace(input.Notes),
	}
	if len([]rune(value.Name)) < 1 || len([]rune(value.Name)) > 500 || len([]rune(value.Notes)) > 2000 {
		return TripDetails{}, NewRuleError("invalid_trip_details", "行程名称须为 1 至 500 个字符，备注不能超过 2000 个字符", ErrInvalidInput)
	}
	start, startErr := time.Parse(time.DateOnly, value.StartDate)
	end, endErr := time.Parse(time.DateOnly, value.EndDate)
	if startErr != nil || endErr != nil || start.Year() < 1 || end.Year() > 9998 || end.Before(start) {
		return TripDetails{}, NewRuleError("invalid_trip_dates", "请输入有效起止日期，结束日期不能早于开始日期", ErrInvalidInput)
	}
	if value.Timezone == "" || value.Timezone == "Local" ||
		(value.Timezone != "UTC" && !strings.Contains(value.Timezone, "/")) {
		return TripDetails{}, NewRuleError("invalid_trip_timezone", "请选择明确的 IANA 时区", ErrInvalidInput)
	}
	if _, err := time.LoadLocation(value.Timezone); err != nil {
		return TripDetails{}, NewRuleError("invalid_trip_timezone", "行程时区无效", ErrInvalidInput)
	}
	return value, nil
}

func NormalizeTripReason(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) < 1 || len([]rune(value)) > 500 {
		return "", NewRuleError("invalid_trip_reason", "操作理由须为 1 至 500 个字符", ErrInvalidInput)
	}
	return value, nil
}
