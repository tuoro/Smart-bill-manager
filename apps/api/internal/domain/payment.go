package domain

import (
	"fmt"
	"time"
)

// PaymentBusinessDate 生成所有确定性支付匹配与工作流投影共用的不可变本地业务日期。
func PaymentBusinessDate(transactionTime, sourceTimezone string) (string, error) {
	instant, err := time.Parse(time.RFC3339Nano, transactionTime)
	if err != nil {
		return "", fmt.Errorf("parse payment transaction time: %w", err)
	}
	location, err := time.LoadLocation(sourceTimezone)
	if err != nil {
		return "", fmt.Errorf("load payment source timezone: %w", err)
	}
	return instant.In(location).Format("2006-01-02"), nil
}
