package regressionfixtures

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	SyntheticMarker = "SYNTHETIC / 纯合成测试数据"

	ScenarioPayment     = "synthetic_payment"
	ScenarioBasic       = "synthetic_basic_invoice"
	ScenarioMultiItem   = "synthetic_multi_item_invoice"
	ScenarioAirTicket   = "synthetic_air_ticket"
	ScenarioRailTicket  = "synthetic_rail_ticket"
	SyntheticAirOrigin  = "星河市"
	SyntheticAirDest    = "云海市"
	SyntheticFlightNo   = "ZZ9001"
	SyntheticRailOrigin = "星河"
	SyntheticRailDest   = "云海"
	SyntheticTrainNo    = "G9001"
)

type expectedItem struct {
	NameContains []string `json:"name_contains"`
	Unit         string   `json:"unit,omitempty"`
	Quantity     *float64 `json:"quantity,omitempty"`
}

type expectedData struct {
	Amount          *float64       `json:"amount,omitempty"`
	Merchant        *string        `json:"merchant,omitempty"`
	TransactionTime *string        `json:"transaction_time,omitempty"`
	PaymentMethod   *string        `json:"payment_method,omitempty"`
	OrderNumber     *string        `json:"order_number,omitempty"`
	InvoiceNumber   *string        `json:"invoice_number,omitempty"`
	InvoiceDate     *string        `json:"invoice_date,omitempty"`
	TaxAmount       *float64       `json:"tax_amount,omitempty"`
	SellerName      *string        `json:"seller_name,omitempty"`
	BuyerName       *string        `json:"buyer_name,omitempty"`
	Items           []expectedItem `json:"items,omitempty"`
}

type fixtureDocument struct {
	Schema     int          `json:"schema"`
	Synthetic  bool         `json:"synthetic"`
	Provenance string       `json:"provenance"`
	Scenario   string       `json:"scenario"`
	Kind       string       `json:"kind"`
	Name       string       `json:"name"`
	RawText    string       `json:"raw_text"`
	Expected   expectedData `json:"expected"`
}

type fixtureSpec struct {
	path string
	doc  fixtureDocument
}

var fixedFixtures = []fixtureSpec{
	{
		path: filepath.Join("payments", ScenarioPayment+".json"),
		doc: fixtureDocument{
			Schema: 1, Synthetic: true, Provenance: "SYNTHETIC_FIXED_CONSTANTS", Scenario: ScenarioPayment,
			Kind: "payment_screenshot", Name: ScenarioPayment,
			RawText: SyntheticMarker + `
支付成功
¥42.36
商户全称：纯合成便利店
支付时间：2026-08-03 12:34:56
付款方式：纯合成测试余额
订单号：999999990001`,
			Expected: expectedData{
				Amount: number(42.36), Merchant: text("纯合成便利店"),
				TransactionTime: text("2026-08-03 12:34:56"), PaymentMethod: text("纯合成测试余额"),
				OrderNumber: text("999999990001"),
			},
		},
	},
	{
		path: filepath.Join("invoices", ScenarioBasic+".json"),
		doc: fixtureDocument{
			Schema: 1, Synthetic: true, Provenance: "SYNTHETIC_FIXED_CONSTANTS", Scenario: ScenarioBasic,
			Kind: "invoice", Name: ScenarioBasic,
			RawText: SyntheticMarker + `
电子发票（普通发票）
发票号码：99000000000000000001
开票日期：2026年08月01日
购买方名称：纯合成采购方甲
销售方名称：纯合成开票服务有限公司
项目名称 规格型号 单位 数量 单价 金额 税率 税额
*纯合成服务*基础服务 标准 套 1 100.00 100.00 6% 6.00
合计 ¥100.00 ¥6.00
价税合计（小写）¥106.00`,
			Expected: expectedData{
				InvoiceNumber: text("99000000000000000001"), InvoiceDate: text("2026年08月01日"),
				Amount: number(106), TaxAmount: number(6), SellerName: text("纯合成开票服务有限公司"),
				BuyerName: text("纯合成采购方甲"),
				Items:     []expectedItem{{NameContains: []string{"纯合成服务", "基础服务"}, Unit: "套", Quantity: number(1)}},
			},
		},
	},
	{
		path: filepath.Join("invoices", ScenarioMultiItem+".json"),
		doc: fixtureDocument{
			Schema: 1, Synthetic: true, Provenance: "SYNTHETIC_FIXED_CONSTANTS", Scenario: ScenarioMultiItem,
			Kind: "invoice", Name: ScenarioMultiItem,
			RawText: SyntheticMarker + `
电子发票（普通发票）
发票号码：99000000000000000002
开票日期：2026年08月02日
购买方名称：纯合成采购方乙
销售方名称：纯合成多项目商店
项目名称 规格型号 单位 数量 单价 金额 税率 税额
*纯合成用品*测试记录本 A1 本 2 12.00 24.00 3% 0.72
*纯合成服务*测试配送 标准 次 1 8.00 8.00 3% 0.24
合计 ¥32.00 ¥0.96
价税合计（小写）¥32.96`,
			Expected: expectedData{
				InvoiceNumber: text("99000000000000000002"), InvoiceDate: text("2026年08月02日"),
				Amount: number(32.96), TaxAmount: number(0.96), SellerName: text("纯合成多项目商店"),
				BuyerName: text("纯合成采购方乙"),
				Items: []expectedItem{
					{NameContains: []string{"纯合成用品", "测试记录本"}},
					{NameContains: []string{"纯合成服务", "测试配送"}},
				},
			},
		},
	},
	{
		path: filepath.Join("invoices", ScenarioAirTicket+".json"),
		doc: fixtureDocument{
			Schema: 1, Synthetic: true, Provenance: "SYNTHETIC_FIXED_CONSTANTS", Scenario: ScenarioAirTicket,
			Kind: "invoice", Name: ScenarioAirTicket,
			RawText: SyntheticMarker + `
航空运输电子客票行程单
发票号码：99000000000000000003
电子客票号码：SYNTHETIC-AIR-0001
旅客姓名：纯合成旅客甲
自：星河市
至：云海市
航班 ZZ9001
ZZ9001 2026年08月02日 09:30
填开单位：纯合成航空服务有限公司
填开日期：2026年08月01日
合计 CNY 456.78
增值税税额 CNY 12.34`,
			Expected: expectedData{
				InvoiceNumber: text("99000000000000000003"), InvoiceDate: text("2026年08月01日"),
				Amount: number(456.78), TaxAmount: number(12.34), SellerName: text("纯合成航空服务有限公司"),
				BuyerName: text("纯合成旅客甲"),
				Items: []expectedItem{{
					NameContains: []string{SyntheticAirOrigin, SyntheticAirDest, SyntheticFlightNo}, Unit: "次", Quantity: number(1),
				}},
			},
		},
	},
	{
		path: filepath.Join("invoices", ScenarioRailTicket+".json"),
		doc: fixtureDocument{
			Schema: 1, Synthetic: true, Provenance: "SYNTHETIC_FIXED_CONSTANTS", Scenario: ScenarioRailTicket,
			Kind: "invoice", Name: ScenarioRailTicket,
			RawText: SyntheticMarker + `
电子发票（铁路电子客票）
发票号码：99000000000000000004
开票日期：2026年08月04日
购买方名称：纯合成旅客乙
星河站
云海站
车次 G9001
二等座
票价：¥88.00`,
			Expected: expectedData{
				InvoiceNumber: text("99000000000000000004"), InvoiceDate: text("2026年08月04日"),
				Amount: number(88), BuyerName: text("纯合成旅客乙"),
				Items: []expectedItem{{
					NameContains: []string{SyntheticRailOrigin, SyntheticRailDest, SyntheticTrainNo}, Unit: "次", Quantity: number(1),
				}},
			},
		},
	},
}

func text(value string) *string { return &value }

func number(value float64) *float64 { return &value }

// Generate writes the complete deterministic fixture set from fixed synthetic constants.
// It never reads a database, uploads, OCR output, mail, environment variables, the network, or input files.
func Generate(outputDir string) error {
	if outputDir == "" {
		return errors.New("output directory is required")
	}
	for _, fixture := range fixedFixtures {
		payload, err := json.MarshalIndent(fixture.doc, "", "  ")
		if err != nil {
			return err
		}
		payload = append(payload, '\n')
		path := filepath.Join(outputDir, fixture.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			return err
		}
	}
	return nil
}
