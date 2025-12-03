package services

import (
	"testing"
)

func TestParseInvoiceData_NewlineFormat(t *testing.T) {
	service := NewOCRService()

	// Sample OCR text with newline-separated fields
	sampleText := `电子发票（普通发票）
发票号码：
25312000000336194167
开票日期：
2025年10月21日
购
买
方
信
息
统一社会信用代码/纳税人识别号：
名称：
个人
销
售
方
信
息
统一社会信用代码/纳税人识别号：
92310109MA1KMFLM1K
名称：
上海市虹口区鹏侠百货商店
项目名称
规格型号
单 位
数 量
单 价
金 额
税率/征收率
税 额
*酒*白酒 汾酒青花30
53°*6
瓶
2
841.584158415842
1683.17
1%
16.83
*酒*葡萄酒 奔富407
750ml*6
瓶
2
683.168316831683
1366.34
1%
13.66
合
计
¥
3049.51
¥
30.49
价税合计（大写）
叁仟零捌拾圆整
（小写）
¥
3080.00
备
注
开票人：
江祜璆`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "25312000000336194167" {
		t.Errorf("Expected InvoiceNumber '25312000000336194167', got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2025年10月21日" {
		t.Errorf("Expected InvoiceDate '2025年10月21日', got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 3080.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "上海市虹口区鹏侠百货商店" {
		t.Errorf("Expected SellerName '上海市虹口区鹏侠百货商店', got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "个人" {
		t.Errorf("Expected BuyerName '个人', got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_TraditionalFormat(t *testing.T) {
	service := NewOCRService()

	// Test traditional format (fields on same line) to ensure backward compatibility
	sampleText := `电子发票（普通发票）
发票号码：12345678901234567890
开票日期：2024年12月01日
销售方名称：测试公司
购买方名称：购买公司
价税合计（小写）¥1234.56`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "12345678901234567890" {
		t.Errorf("Expected InvoiceNumber '12345678901234567890', got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2024年12月01日" {
		t.Errorf("Expected InvoiceDate '2024年12月01日', got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 1234.56
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "测试公司" {
		t.Errorf("Expected SellerName '测试公司', got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "购买公司" {
		t.Errorf("Expected BuyerName '购买公司', got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_RealWorldFormat(t *testing.T) {
	service := NewOCRService()

	// EXACT OCR format from real invoice - labels and data are completely separated
	sampleText := `电子发票（普通发票）
发票号码：
开票日期：
购
买
方
信
息
统一社会信用代码/纳税人识别号：
销
售
方
信
息
统一社会信用代码/纳税人识别号：
名称：
名称：
项目名称
规格型号
单 位
数 量
单 价
金 额
税率/征收率
税 额
合
计
价税合计（大写）
（小写）
备
注
开票人：
25312000000336194167
2025年10月21日
个人
上海市虹口区鹏侠百货商店
92310109MA1KMFLM1K
¥
3049.51
¥
30.49
叁仟零捌拾圆整
¥
3080.00
江祜璆
江祜璆
*酒*白酒 汾酒青花30
53°*6
1%
瓶
1683.17
16.83
841.584158415842
2
*酒*葡萄酒 奔富407
750ml*6
1%
瓶
1366.34
13.66
683.168316831683
2`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "25312000000336194167" {
		t.Errorf("Expected InvoiceNumber '25312000000336194167', got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date - SHOULD extract the date from OCR text
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil - should extract '2025年10月21日'")
	} else if *data.InvoiceDate != "2025年10月21日" {
		t.Errorf("Expected InvoiceDate '2025年10月21日', got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 3080.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name - SHOULD extract '上海市虹口区鹏侠百货商店'
	if data.SellerName == nil {
		t.Error("SellerName is nil - should extract '上海市虹口区鹏侠百货商店'")
	} else if *data.SellerName != "上海市虹口区鹏侠百货商店" {
		t.Errorf("Expected SellerName '上海市虹口区鹏侠百货商店', got '%s'", *data.SellerName)
	}

	// Test buyer name - SHOULD extract '个人' NOT '名称：'
	if data.BuyerName == nil {
		t.Error("BuyerName is nil - should extract '个人'")
	} else if *data.BuyerName == "名称：" || *data.BuyerName == "名称:" {
		t.Errorf("BuyerName incorrectly extracted as '%s' - should be '个人'", *data.BuyerName)
	} else if *data.BuyerName != "个人" {
		t.Errorf("Expected BuyerName '个人', got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_DidiInvoice(t *testing.T) {
	service := NewOCRService()

	// Real OCR text from Didi invoice with 8-digit invoice number and full-width ￥ symbol
	sampleText := `合
计
备
注
上海增值税电子普通发票
价税合计（大写）
（小写）
货物或应税劳务、服务名称
规格型号
单位
数　量
单　价
金　额
税率
税　额
购
买
方
销
售
方
收 款 人:
复 核:
开 票 人:
销 售 方:（章）
密
码
区
机器编号:
名　　　　称:
纳税人识别号:
地 址、
开户行及账号:
名　　　　称:
纳税人识别号:
地 址、
开户行及账号:
发票代码:
发票号码:
开票日期:
校
验
码:
电 话:
电 话:
￥19.01
￥0.57
*运输服务*客运服务费
无
次
1
24
24.00
3%
0.72
*运输服务*客运服务费
-4.99
3%
-0.15
499098504973
壹拾玖圆不角扌分
杜洪亮
张唯
于秋红
03<5>/42>5541143639+79737-<*
59765*+75/>89+/47732281674/2
15<5>/42>5541143631>239-3/<5
+7*9>//<310193<219+4/8-4528-
个人
上海滴滴畅行科技有限公司
91310114MA1GW61J6U
上海市静安区万荣路777弄12号202-7室010-83456275
招商银行股份有限公司上海东方支行121932981110606
031002300211
70739906
2024年07月06日
07908 63166 90581 33371
￥19.58`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number - should extract 8-digit number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil - should extract '70739906'")
	} else if *data.InvoiceNumber != "70739906" {
		t.Errorf("Expected InvoiceNumber '70739906', got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2024年07月06日" {
		t.Errorf("Expected InvoiceDate '2024年07月06日', got '%s'", *data.InvoiceDate)
	}

	// Test amount - should extract 19.58 with full-width ￥ symbol
	if data.Amount == nil {
		t.Error("Amount is nil - should extract '19.58'")
	} else {
		expectedAmount := 19.58
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "上海滴滴畅行科技有限公司" {
		t.Errorf("Expected SellerName '上海滴滴畅行科技有限公司', got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "个人" {
		t.Errorf("Expected BuyerName '个人', got '%s'", *data.BuyerName)
	}
}
