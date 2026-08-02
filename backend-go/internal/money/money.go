package money

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
)

const (
	scale        = int64(100)
	maxSafeCents = int64(9_007_199_254_740_991)
)

var ErrInvalidAmount = errors.New("金额无效")

// FromMajor 将元单位金额四舍五入为整数分，并限制在可精确表示的安全范围内。
func FromMajor(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, ErrInvalidAmount
	}
	if math.Abs(value) > float64(maxSafeCents)/float64(scale) {
		return 0, fmt.Errorf("%w: 超出安全范围", ErrInvalidAmount)
	}

	decimal := strconv.FormatFloat(value, 'f', -1, 64)
	rational, ok := new(big.Rat).SetString(decimal)
	if !ok {
		return 0, ErrInvalidAmount
	}
	scaled := new(big.Rat).Mul(rational, big.NewRat(scale, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(scaled.Num(), scaled.Denom(), remainder)

	twiceRemainder := new(big.Int).Lsh(new(big.Int).Abs(remainder), 1)
	if twiceRemainder.Cmp(scaled.Denom()) >= 0 {
		if scaled.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() || quotient.Int64() > maxSafeCents || quotient.Int64() < -maxSafeCents {
		return 0, fmt.Errorf("%w: 超出安全范围", ErrInvalidAmount)
	}
	return quotient.Int64(), nil
}

// ToMajor 将整数分转换为 API 使用的元单位金额。
func ToMajor(cents int64) float64 {
	return float64(cents) / float64(scale)
}

func FromMajorPointer(value *float64) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	cents, err := FromMajor(*value)
	if err != nil {
		return nil, err
	}
	return &cents, nil
}

func ToMajorPointer(cents *int64) *float64 {
	if cents == nil {
		return nil
	}
	value := ToMajor(*cents)
	return &value
}

// SyncUpdateMap 规范化更新参数，并同步写入整数分字段和旧版元字段。
func SyncUpdateMap(data map[string]any, majorKey, centsKey string, nullable bool) error {
	value, exists := data[majorKey]
	if !exists {
		return nil
	}
	if value == nil {
		if !nullable {
			return fmt.Errorf("%w: %s 不能为空", ErrInvalidAmount, majorKey)
		}
		data[centsKey] = nil
		return nil
	}

	major, ok := numericValue(value)
	if !ok {
		return fmt.Errorf("%w: %s 类型不受支持", ErrInvalidAmount, majorKey)
	}
	cents, err := FromMajor(major)
	if err != nil {
		return fmt.Errorf("%s: %w", majorKey, err)
	}
	data[majorKey] = ToMajor(cents)
	data[centsKey] = cents
	return nil
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case int32:
		return float64(number), true
	default:
		return 0, false
	}
}
