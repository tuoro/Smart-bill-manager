//go:build cgo

package app

import (
	"net/http"
	"testing"

	"smart-bill-manager/internal/models"
)

func TestPaymentCRUDPreservesMoneyAndTenantIsolation(t *testing.T) {
	application := newTestApplication(t)
	admin := setupContractAdmin(t, application)
	member := registerContractMember(t, application, admin.Token)

	created := performContractRequest(t, application, http.MethodPost, "/api/payments", admin.Token, map[string]any{
		"amount":           10.01,
		"merchant":         "金额精度商户",
		"transaction_time": "2026-08-03T10:00:00+08:00",
	}, nil)
	assertContractStatus(t, created, http.StatusCreated)
	var payment models.Payment
	decodeContractData(t, created, &payment)
	if payment.OwnerUserID != admin.ID || payment.Amount != 10.01 {
		t.Fatalf("支付创建响应异常: %#v", payment)
	}

	var stored models.Payment
	if err := application.db.Where("id = ?", payment.ID).First(&stored).Error; err != nil {
		t.Fatalf("读取支付记录失败: %v", err)
	}
	if stored.AmountCents != 1001 || stored.Amount != 10.01 {
		t.Fatalf("金额未按整数分持久化: %#v", stored)
	}

	ownerRead := performContractRequest(t, application, http.MethodGet, "/api/payments/"+payment.ID, admin.Token, nil, nil)
	assertContractStatus(t, ownerRead, http.StatusOK)
	var ownerPayment models.Payment
	decodeContractData(t, ownerRead, &ownerPayment)
	if ownerPayment.Amount != 10.01 {
		t.Fatalf("金额 API 往返后发生漂移: %#v", ownerPayment)
	}

	memberList := performContractRequest(t, application, http.MethodGet, "/api/payments", member.Token, nil, nil)
	assertContractStatus(t, memberList, http.StatusOK)
	var list struct {
		Items []models.Payment `json:"items"`
		Total int64            `json:"total"`
	}
	decodeContractData(t, memberList, &list)
	if list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("其他租户列表不应包含支付记录: %#v", list)
	}

	memberRead := performContractRequest(t, application, http.MethodGet, "/api/payments/"+payment.ID, member.Token, nil, nil)
	assertContractStatus(t, memberRead, http.StatusNotFound)

	memberUpdate := performContractRequest(t, application, http.MethodPut, "/api/payments/"+payment.ID, member.Token, map[string]any{
		"amount":   99.99,
		"merchant": "越权修改",
	}, nil)
	assertContractStatus(t, memberUpdate, http.StatusNotFound)

	memberDelete := performContractRequest(t, application, http.MethodDelete, "/api/payments/"+payment.ID, member.Token, nil, nil)
	assertContractStatus(t, memberDelete, http.StatusNotFound)

	if err := application.db.Where("id = ?", payment.ID).First(&stored).Error; err != nil {
		t.Fatalf("越权操作后支付记录不应被删除: %v", err)
	}
	if stored.OwnerUserID != admin.ID || stored.AmountCents != 1001 || stored.Merchant == nil || *stored.Merchant != "金额精度商户" {
		t.Fatalf("越权操作改变了支付记录: %#v", stored)
	}

	ownerUpdate := performContractRequest(t, application, http.MethodPut, "/api/payments/"+payment.ID, admin.Token, map[string]any{
		"amount": 10.02,
	}, nil)
	assertContractStatus(t, ownerUpdate, http.StatusOK)
	if err := application.db.Where("id = ?", payment.ID).First(&stored).Error; err != nil {
		t.Fatalf("读取更新后支付记录失败: %v", err)
	}
	if stored.AmountCents != 1002 || stored.Amount != 10.02 {
		t.Fatalf("更新金额未同步整数分字段: %#v", stored)
	}
}

func TestInvoicePaymentLinkIsTenantScopedAndTransactional(t *testing.T) {
	application := newTestApplication(t)
	admin := setupContractAdmin(t, application)
	member := registerContractMember(t, application, admin.Token)

	adminPayment := seedContractPayment(t, application, "payment-admin", admin.ID, 20.01)
	memberPayment := seedContractPayment(t, application, "payment-member", member.ID, 30.02)
	adminInvoice := models.Invoice{
		ID:           "invoice-admin",
		OwnerUserID:  admin.ID,
		Filename:     "invoice.pdf",
		OriginalName: "invoice.pdf",
		FilePath:     "uploads/" + admin.ID + "/invoice.pdf",
		ParseStatus:  "success",
		Source:       "upload",
		DedupStatus:  "ok",
	}
	if err := application.db.Create(&adminInvoice).Error; err != nil {
		t.Fatalf("创建测试发票失败: %v", err)
	}

	crossTenant := performContractRequest(t, application, http.MethodPost, "/api/invoices/"+adminInvoice.ID+"/link-payment", admin.Token, map[string]any{
		"payment_id": memberPayment.ID,
	}, nil)
	assertContractStatus(t, crossTenant, http.StatusNotFound)
	assertInvoiceLinkState(t, application, adminInvoice.ID, "", 0)

	memberAttempt := performContractRequest(t, application, http.MethodPost, "/api/invoices/"+adminInvoice.ID+"/link-payment", member.Token, map[string]any{
		"payment_id": memberPayment.ID,
	}, nil)
	assertContractStatus(t, memberAttempt, http.StatusNotFound)
	assertInvoiceLinkState(t, application, adminInvoice.ID, "", 0)

	linked := performContractRequest(t, application, http.MethodPost, "/api/invoices/"+adminInvoice.ID+"/link-payment", admin.Token, map[string]any{
		"payment_id": adminPayment.ID,
	}, nil)
	assertContractStatus(t, linked, http.StatusOK)
	assertInvoiceLinkState(t, application, adminInvoice.ID, adminPayment.ID, 1)
}

func seedContractPayment(t *testing.T, application *Application, id, ownerID string, amount float64) models.Payment {
	t.Helper()
	payment := models.Payment{
		ID:                id,
		OwnerUserID:       ownerID,
		Amount:            amount,
		TransactionTime:   "2026-08-03T02:00:00Z",
		TransactionTimeTs: 1785722400000,
		TripAssignSrc:     "auto",
		TripAssignState:   "no_match",
		DedupStatus:       "ok",
	}
	if err := application.db.Create(&payment).Error; err != nil {
		t.Fatalf("创建测试支付 %s 失败: %v", id, err)
	}
	return payment
}

func assertInvoiceLinkState(t *testing.T, application *Application, invoiceID, paymentID string, wantLinks int64) {
	t.Helper()
	var count int64
	if err := application.db.Model(&models.InvoicePaymentLink{}).Where("invoice_id = ?", invoiceID).Count(&count).Error; err != nil {
		t.Fatalf("统计发票支付关联失败: %v", err)
	}
	if count != wantLinks {
		t.Fatalf("关联数应为 %d，实际为 %d", wantLinks, count)
	}
	var invoice models.Invoice
	if err := application.db.Where("id = ?", invoiceID).First(&invoice).Error; err != nil {
		t.Fatalf("读取发票失败: %v", err)
	}
	if paymentID == "" {
		if invoice.PaymentID != nil {
			t.Fatalf("失败关联不应更新兼容 payment_id: %#v", invoice.PaymentID)
		}
		return
	}
	if invoice.PaymentID == nil || *invoice.PaymentID != paymentID {
		t.Fatalf("成功关联未同步兼容 payment_id: %#v", invoice.PaymentID)
	}
}
