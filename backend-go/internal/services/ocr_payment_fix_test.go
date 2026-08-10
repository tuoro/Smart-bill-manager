package services

import (
	"strings"
	"testing"
)

func TestParsePaymentScreenshot_SyntheticSpacedBillDetail(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
12:34 合 成 标 记
主 全 部 账 单
当 心
A
纯 合 成 便 利 店

当 前 状 态 支 付 成 功

支 付 时 间 2026 年 08 月 03 日 12:34:56

商 品 纯 合 成 便 利 店 ( 纯 合 成 商 贸 有 限 公 司 )

商 户 全 称 纯 合 成 商 贸 有 限 公 司

收 单 机 构 纯 合 成 支 付 服 务 有 限 公 司
由 纯 合 成 清 算 服 务 提 供

支 付 方 式 合 成 银 行 信 用 卡 (9901)
由 纯 合 成 清 算 服 务 提 供

交 易 单 号 999999990000000000000005

商 户 单 号 999999990006`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	// Prefer the short synthetic 商品 value over the legal full name.
	if data.Merchant == nil {
		t.Error("Merchant is nil - should extract merchant name")
	} else {
		t.Logf("Extracted merchant: '%s'", *data.Merchant)
		validMerchants := []string{"纯合成便利店", "纯合成商贸有限公司"}
		found := false
		for _, valid := range validMerchants {
			if *data.Merchant == valid {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected merchant to be one of %v, got '%s'", validMerchants, *data.Merchant)
		}
	}

	// Test transaction time extraction.
	if data.TransactionTime == nil {
		t.Error("TransactionTime is nil for synthetic bill detail")
	} else {
		t.Logf("Extracted time: '%s'", *data.TransactionTime)
		expectedTime := "2026-08-03 12:34:56"
		if *data.TransactionTime != expectedTime {
			t.Errorf("Expected TransactionTime '%s', got '%s'", expectedTime, *data.TransactionTime)
		}
	}

	// Test order number extraction
	if data.OrderNumber == nil {
		t.Error("OrderNumber is nil - should extract order number")
	} else {
		t.Logf("Extracted order number: '%s'", *data.OrderNumber)
		expectedOrderNum := "999999990000000000000005"
		if *data.OrderNumber != expectedOrderNum {
			t.Errorf("Expected OrderNumber '%s', got '%s'", expectedOrderNum, *data.OrderNumber)
		}
	}

	// Test payment method extraction
	if data.PaymentMethod == nil {
		t.Error("PaymentMethod is nil")
	} else {
		t.Logf("Extracted payment method: '%s'", *data.PaymentMethod)
	}
}

func TestParsePaymentScreenshot_WeChatQrPay_PayeeTitleLine(t *testing.T) {
	service := NewOCRService()

	sampleText := syntheticOCRText("微信支付", "扫二维码付款-给纯合成收款方甲", "-45.00", "转账时间 2026年08月08日10:07:30", "转账单号 999999990000000000000007")

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Merchant == nil || *data.Merchant != "纯合成收款方甲" {
		t.Fatalf("expected synthetic payee, got %#v", data.Merchant)
	}
	if data.MerchantConfidence <= 0.0 {
		t.Fatalf("expected MerchantConfidence to be set, got %v", data.MerchantConfidence)
	}
}

func TestParsePaymentScreenshot_WeChatQrPay_PayeeSplitLines(t *testing.T) {
	service := NewOCRService()

	sampleText := syntheticOCRText("微信支付", "扫二维码付款-给", "纯合成收款方甲", "-45.00", "转账时间 2026年08月08日10:07:30")

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Merchant == nil || *data.Merchant != "纯合成收款方甲" {
		t.Fatalf("expected synthetic payee, got %#v", data.Merchant)
	}
}

func TestParsePaymentScreenshot_WeChatBillDetail_LabelListThenValues(t *testing.T) {
	service := NewOCRService()

	// Simulate a layout where OCR outputs all labels first, then values later.
	// Key requirement: do NOT bind the next label as the value (e.g. "商户全称" -> "收单机构").
	sampleText := `SYNTHETIC / 纯合成测试数据
微信支付
全部账单
已支付
纯合成超市
-400.00
交易单号
商品
支付方式
当前状态
支付时间
商户全称
收单机构
商户单号
服务
支付成功
2026年08月09日23:02:47
纯合成超市
合成银行信用卡(9901)
999999990000000000000008
纯合成杂货商店
纯合成支付服务有限公司
由纯合成清算服务提供
可在支持的商户扫码退款
999999990000000000000009
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Merchant == nil {
		t.Fatalf("expected Merchant, got nil")
	}
	// Prefer the user-facing synthetic store title over the legal full name.
	if *data.Merchant != "纯合成超市" {
		t.Fatalf("expected synthetic store title, got %q", *data.Merchant)
	}

	if data.PaymentMethod == nil {
		t.Fatalf("expected PaymentMethod, got nil")
	}
	if *data.PaymentMethod != "合成银行信用卡(9901)" {
		t.Fatalf("expected synthetic payment method, got %q", *data.PaymentMethod)
	}

	if data.TransactionTime == nil {
		t.Fatalf("expected TransactionTime, got nil")
	}
	if *data.TransactionTime != "2026-08-09 23:02:47" {
		t.Fatalf("expected synthetic transaction time, got %q", *data.TransactionTime)
	}

	if data.OrderNumber == nil {
		t.Fatalf("expected OrderNumber, got nil")
	}
	if *data.OrderNumber != "999999990000000000000008" {
		t.Fatalf("expected synthetic order number, got %q", *data.OrderNumber)
	}
}

func TestParsePaymentScreenshot_WeChatBillDetail_PaymentMethodShouldNotBeBarcode(t *testing.T) {
	service := NewOCRService()

	// A synthetic layout-aware postprocess case may output:
	// - a card value paired to 服务
	// - "支付方式：10016..." (barcode / merchant id got paired to "支付方式")
	// We should still extract the actual payment method (the card), not the long digits.
	sampleText := `SYNTHETIC / 纯合成测试数据
微信支付
全部账单
已支付
纯合成超市
-400.00
当前状态：支付成功
支付时间：2026年08月09日23:02:47
商品：纯合成超市
商户全称：纯合成杂货商店
收单机构：纯合成支付服务有限公司
服务：合成银行信用卡(9901)
由纯合成清算服务提供
支付方式：999999990000000000000009
交易单号：999999990000000000000008
商户单号：可在支持的商户扫码退款
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.PaymentMethod == nil {
		t.Fatalf("expected PaymentMethod, got nil")
	}
	if *data.PaymentMethod != "合成银行信用卡(9901)" {
		t.Fatalf("expected synthetic payment method, got %q", *data.PaymentMethod)
	}
}

func TestParsePaymentScreenshot_WeChatBillDetail_MerchantShouldPreferTitleOverGenericItem(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
微信支付
11:26
全部账单
合成银行
纯合成长名称门店
-3420.00
当前状态：支付成功
支付时间：2026年08月10日11:26:26
商品：商户收款
商户全称：纯合成商贸有限公司
收单机构：纯合成商业银行有限公司
支付方式：合成银行信用卡(9901)
交易单号：999999990000000000000010
商户单号：999999990000000000000011
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Merchant == nil {
		t.Fatalf("expected Merchant, got nil")
	}
	if *data.Merchant != "纯合成长名称门店" {
		t.Fatalf("expected synthetic title merchant, got %q", *data.Merchant)
	}
}

func TestParsePaymentScreenshot_Alipay_BillDetail_BasicFields(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
账单详情
纯合成外卖服务
-52.00
支付时间
2026年08月11日20:13:28
付款方式
合成银行信用卡(9901)
交易号
999999990000000000000012
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 52.00 {
		t.Fatalf("expected synthetic amount, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant != "纯合成外卖服务" {
		t.Fatalf("expected synthetic merchant, got %#v", data.Merchant)
	}
	if data.TransactionTime == nil || *data.TransactionTime != "2026-08-11 20:13:28" {
		t.Fatalf("expected synthetic transaction time, got %#v", data.TransactionTime)
	}
	if data.PaymentMethod == nil || *data.PaymentMethod != "合成银行信用卡(9901)" {
		t.Fatalf("expected synthetic payment method, got %#v", data.PaymentMethod)
	}
	if data.OrderNumber == nil || *data.OrderNumber != "999999990000000000000012" {
		t.Fatalf("expected synthetic order number, got %#v", data.OrderNumber)
	}
}

func TestParsePaymentScreenshot_JDPay_BillDetail_ShouldExtractTimeAndOrder(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
8:22
账单详情
5+
纯合成平台商户
-13,897.00
交易成功
支付方式
合成银行信用卡（9901）>
创建时间
2026-08-12 14:51:37
总订单编号
999999990013
商户单号
999999990000000000000014
服务详情
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 13897.00 {
		t.Fatalf("expected Amount=13897.00, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant != "纯合成平台商户" {
		t.Fatalf("expected synthetic merchant, got %#v", data.Merchant)
	}
	if data.PaymentMethod == nil || *data.PaymentMethod != "合成银行信用卡(9901)" {
		t.Fatalf("expected synthetic payment method, got %#v", data.PaymentMethod)
	}
	if data.TransactionTime == nil || *data.TransactionTime != "2026-08-12 14:51:37" {
		t.Fatalf("expected synthetic transaction time, got %#v", data.TransactionTime)
	}
	// Prefer merchant order id when no explicit "交易单号/交易号" is present.
	if data.OrderNumber == nil || *data.OrderNumber != "999999990000000000000014" {
		t.Fatalf("expected synthetic order number, got %#v", data.OrderNumber)
	}
}

func TestParsePaymentScreenshot_JDPay_BillDetail_WithWeChatPayMethod_ShouldStillExtractTime(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
8:22
账单详情
纯合成平台商户
-622.00
交易成功
支付方式
微信支付
创建时间
2026-08-13 17:23:17
总订单编号
999999990015
商户单号
999999990000000000000016
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Amount == nil || *data.Amount != 622.00 {
		t.Fatalf("expected Amount=622.00, got %#v", data.Amount)
	}
	if data.PaymentMethod == nil || *data.PaymentMethod != "微信支付" {
		t.Fatalf("expected PaymentMethod=微信支付, got %#v", data.PaymentMethod)
	}
	if data.PaymentMethodSource != "jd_method" {
		t.Fatalf("expected PaymentMethodSource=jd_method, got %q", data.PaymentMethodSource)
	}
	if data.TransactionTime == nil || *data.TransactionTime != "2026-08-13 17:23:17" {
		t.Fatalf("expected synthetic transaction time, got %#v", data.TransactionTime)
	}
	if data.TransactionTimeSource != "jd_time" {
		t.Fatalf("expected TransactionTimeSource=jd_time, got %q", data.TransactionTimeSource)
	}
	if data.OrderNumber == nil || *data.OrderNumber != "999999990000000000000016" {
		t.Fatalf("expected synthetic order number, got %#v", data.OrderNumber)
	}
}

func TestParsePaymentScreenshot_UnionPay_BillDetail_ShouldUseUnionPaySources(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
账单详情
纯合成航空服务 (航空客票）
-￥1,301.00
当前状态
交易成功
订单金额
￥1,301.00
付款方式
合成银行银联储蓄卡[9902]
订单时间
2026年8月14日17:21:58
订单编号
999999990000000017
商户订单号
999999990018
在此商户的交易
点击查看>`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Amount == nil || *data.Amount != 1301.00 {
		t.Fatalf("expected Amount=1301.00, got %#v", data.Amount)
	}
	if data.AmountSource != "unionpay_amount_label" {
		t.Fatalf("expected AmountSource=unionpay_amount_label, got %q", data.AmountSource)
	}
	if data.Merchant == nil || *data.Merchant != "纯合成航空服务 (航空客票）" {
		t.Fatalf("expected synthetic merchant, got %#v", data.Merchant)
	}
	if data.MerchantSource != "unionpay_bill_detail" {
		t.Fatalf("expected MerchantSource=unionpay_bill_detail, got %q", data.MerchantSource)
	}
	if data.PaymentMethod == nil || *data.PaymentMethod != "合成银行银联储蓄卡[9902]" {
		t.Fatalf("expected synthetic payment method, got %#v", data.PaymentMethod)
	}
	if data.PaymentMethodSource != "unionpay_method_label" {
		t.Fatalf("expected PaymentMethodSource=unionpay_method_label, got %q", data.PaymentMethodSource)
	}
	if data.TransactionTime == nil || *data.TransactionTime != "2026-8-14 17:21:58" {
		t.Fatalf("expected synthetic transaction time, got %#v", data.TransactionTime)
	}
	if data.TransactionTimeSource != "unionpay_time_label" {
		t.Fatalf("expected TransactionTimeSource=unionpay_time_label, got %q", data.TransactionTimeSource)
	}
	if data.OrderNumber == nil || *data.OrderNumber != "999999990018" {
		t.Fatalf("expected synthetic order number, got %#v", data.OrderNumber)
	}
	if data.OrderNumberSource != "unionpay_merchant_order" {
		t.Fatalf("expected OrderNumberSource=unionpay_merchant_order, got %q", data.OrderNumberSource)
	}
}

func TestParsePaymentScreenshot_BankReceipt_ICBC_ShouldExtractAmountTimeOrderPayee(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
ICBC
中国工商银行
境内汇款电子回单
收款银行
收款户名
收款卡号
9900****0000
纯合成收款银行
纯合成绿化服务中心
收款金额
手续费
合计
免费
肆仟零壹拾元整
4,010.00元（人民币）
付款户名
付款卡号
付款银行
*纯合成付款方
9901****9999
中国工商银行
指令序号
回单编号
交易时间
附言
纯合成采购
SYNTH-0007-0000-0000-0001
2026/08/15 15:21
999999990000000000000019
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 4010.00 {
		t.Fatalf("expected Amount=4010.00, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant != "纯合成绿化服务中心" {
		t.Fatalf("expected synthetic merchant, got %#v", data.Merchant)
	}
	if data.TransactionTime == nil || *data.TransactionTime != "2026-08-15 15:21" {
		t.Fatalf("expected synthetic transaction time, got %#v", data.TransactionTime)
	}
	if data.OrderNumber == nil || *data.OrderNumber != "SYNTH-0007-0000-0000-0001" {
		t.Fatalf("expected synthetic order number, got %#v", data.OrderNumber)
	}
	if data.PaymentMethod == nil || *data.PaymentMethod != "中国工商银行(9999)" {
		t.Fatalf("expected masked synthetic payment method, got %#v", data.PaymentMethod)
	}
}

func TestParsePaymentScreenshot_AlipayTransferVoucher_ShouldExtractPayeeTimeAndVoucherNo(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
转账凭证
款项已经转出成功，凭证仅供参考，请以收方账户
￥600
实际到账为准。
支付宝（中国）
收款方姓名
纯合成收款方乙
收款方账号
************9999
收款方银行
合成银行
付款方姓名
纯合成付款方乙
付款方账号
synthetic***@example.invalid
转账时间
2026-08-1612:57
凭证编号
99999999000000000000
000020
转账附言
转账
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Amount == nil || *data.Amount != 600.00 {
		t.Fatalf("expected synthetic amount, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant != "纯合成收款方乙" {
		t.Fatalf("expected synthetic payee, got %#v", data.Merchant)
	}
	if data.MerchantSource != "alipay_transfer_payee" {
		t.Fatalf("expected MerchantSource=alipay_transfer_payee, got %q", data.MerchantSource)
	}
	if data.TransactionTime == nil || *data.TransactionTime != "2026-08-16 12:57" {
		t.Fatalf("expected synthetic transaction time, got %#v", data.TransactionTime)
	}
	if data.TransactionTimeSource != "alipay_transfer_time" {
		t.Fatalf("expected TransactionTimeSource=alipay_transfer_time, got %q", data.TransactionTimeSource)
	}
	if data.OrderNumber == nil || *data.OrderNumber != "99999999000000000000000020" {
		t.Fatalf("expected synthetic voucher number, got %#v", data.OrderNumber)
	}
	if data.OrderNumberSource != "alipay_transfer_voucher_no" {
		t.Fatalf("expected OrderNumberSource=alipay_transfer_voucher_no, got %q", data.OrderNumberSource)
	}
	if data.PaymentMethod == nil || *data.PaymentMethod != "支付宝转账" {
		t.Fatalf("expected PaymentMethod=支付宝转账, got %#v", data.PaymentMethod)
	}
	if data.PaymentMethodSource != "alipay_transfer" {
		t.Fatalf("expected PaymentMethodSource=alipay_transfer, got %q", data.PaymentMethodSource)
	}
}

// TestRemoveChineseSpaces_PreserveTimeSpace tests the fix for preserving space after 日
func TestRemoveChineseSpaces_PreserveTimeSpace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Date with spaces - preserve space before time",
			input:    "2026 年 08 月 03 日 12:34:56",
			expected: "2026年08月03日 12:34:56",
		},
		{
			name:     "Synthetic payment time text",
			input:    "支 付 时 间 2026 年 08 月 03 日 12:34:56",
			expected: "支付时间2026年08月03日 12:34:56",
		},
		{
			name:     "Date only - remove all spaces",
			input:    "2026 年 08 月 03 日",
			expected: "2026年08月03日",
		},
		{
			name:     "Time with different format",
			input:    "支 付 时 间 2026 年 08 月 03 日 09:30:15",
			expected: "支付时间2026年08月03日 09:30:15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeChineseSpaces(tt.input)
			if result != tt.expected {
				t.Errorf("removeChineseSpaces(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestConvertChineseDateToISO_BothFormats tests the improved convertChineseDateToISO
func TestConvertChineseDateToISO_BothFormats(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Date and time with space",
			input:    "2026年08月03日 12:34:56",
			expected: "2026-08-03 12:34:56",
		},
		{
			name:     "Date and time without space",
			input:    "2026年08月03日12:34:56",
			expected: "2026-08-03 12:34:56",
		},
		{
			name:     "Date only",
			input:    "2026年08月03日",
			expected: "2026-08-03",
		},
		{
			name:     "Single digit month and day with time",
			input:    "2026年8月3日 9:30:46",
			expected: "2026-8-3 9:30:46",
		},
		{
			name:     "Single digit month and day without space",
			input:    "2026年8月3日9:30:46",
			expected: "2026-8-3 9:30:46",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertChineseDateToISO(tt.input)
			if result != tt.expected {
				t.Errorf("convertChineseDateToISO(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestParsePaymentScreenshot_WithNegativeAmount tests parsing with negative amount
func TestParsePaymentScreenshot_WithNegativeAmount(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
支 付 成 功
-42.36
支 付 时 间 2026 年 08 月 03 日 12:34:56
商 品 纯 合 成 便 利 店
交 易 单 号 999999990000000000000021`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	// Test amount extraction
	if data.Amount == nil {
		t.Error("Amount is nil for synthetic negative amount")
	} else {
		expectedAmount := 42.36
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test time extraction
	if data.TransactionTime == nil {
		t.Error("TransactionTime is nil")
	} else {
		expectedTime := "2026-08-03 12:34:56"
		if *data.TransactionTime != expectedTime {
			t.Errorf("Expected TransactionTime '%s', got '%s'", expectedTime, *data.TransactionTime)
		}
	}
}

// Helper function to check if string contains any of the substrings
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
