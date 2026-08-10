//go:build cgo

package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/services"
)

func TestDashboardRecentPaymentsAreTenantScopedAndKeepContract(t *testing.T) {
	const expectedDashboardRecentPaymentLimit = 6

	application := newTestApplication(t)
	admin := setupContractAdmin(t, application)
	member := registerContractMember(t, application, admin.Token)

	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载上海时区失败: %v", err)
	}
	now := time.Now().In(shanghai)
	baseTime := time.Date(now.Year(), now.Month(), 15, 12, 0, 0, 0, shanghai)
	adminPayments := make([]models.Payment, 0, 7)
	for i := 0; i < 7; i++ {
		adminPayments = append(adminPayments, seedArchitecturePayment(
			t,
			application,
			"dashboard-admin-"+strconv.Itoa(i),
			admin.ID,
			baseTime.Add(-time.Duration(i)*time.Minute),
			false,
		))
	}
	memberPayment := seedArchitecturePayment(
		t,
		application,
		"dashboard-member",
		member.ID,
		baseTime.Add(time.Hour),
		false,
	)
	seedArchitecturePayment(
		t,
		application,
		"dashboard-admin-draft",
		admin.ID,
		baseTime.Add(2*time.Hour),
		true,
	)

	adminInvoiceOne := seedArchitectureInvoice(t, application, "dashboard-admin-invoice-1", admin.ID)
	adminInvoiceTwo := seedArchitectureInvoice(t, application, "dashboard-admin-invoice-2", admin.ID)
	memberCrossOwnerInvoice := seedArchitectureInvoice(t, application, "dashboard-member-cross-owner-invoice", member.ID)
	memberOwnInvoice := seedArchitectureInvoice(t, application, "dashboard-member-own-invoice", member.ID)
	links := []models.InvoicePaymentLink{
		{InvoiceID: adminInvoiceOne.ID, PaymentID: adminPayments[0].ID},
		{InvoiceID: adminInvoiceTwo.ID, PaymentID: adminPayments[0].ID},
		// 构造一条越权脏关联，Dashboard 计数仍必须以发票所有者为边界。
		{InvoiceID: memberCrossOwnerInvoice.ID, PaymentID: adminPayments[0].ID},
		{InvoiceID: memberOwnInvoice.ID, PaymentID: memberPayment.ID},
	}
	if err := application.db.Create(&links).Error; err != nil {
		t.Fatalf("创建 Dashboard 测试关联失败: %v", err)
	}

	adminResponse := performContractRequest(t, application, http.MethodGet, "/api/dashboard", admin.Token, nil, nil)
	assertContractStatus(t, adminResponse, http.StatusOK)
	adminDashboard := decodeDashboardContract(t, adminResponse)
	if adminDashboard.Payments.CountThisMonth != 7 {
		t.Fatalf("管理员本月支付数量应为 7，实际为 %d", adminDashboard.Payments.CountThisMonth)
	}
	if adminDashboard.Invoices.TotalCount != 2 {
		t.Fatalf("管理员发票数量应为 2，实际为 %d", adminDashboard.Invoices.TotalCount)
	}
	if len(adminDashboard.RecentPayments) != expectedDashboardRecentPaymentLimit {
		t.Fatalf("最近支付应保持 %d 条上限，实际为 %d", expectedDashboardRecentPaymentLimit, len(adminDashboard.RecentPayments))
	}
	if adminDashboard.RecentPayments[0].ID != adminPayments[0].ID || adminDashboard.RecentPayments[0].InvoiceCount != 2 {
		t.Fatalf("最近支付排序或同所有者发票计数错误: %#v", adminDashboard.RecentPayments[0])
	}
	for _, payment := range adminDashboard.RecentPayments {
		if payment.OwnerUserID != admin.ID || payment.IsDraft {
			t.Fatalf("Dashboard 泄露其他所有者或草稿支付: %#v", payment)
		}
		if payment.ID == adminPayments[6].ID || payment.ID == memberPayment.ID {
			t.Fatalf("Dashboard 最近支付边界错误: %#v", payment)
		}
	}

	memberResponse := performContractRequest(t, application, http.MethodGet, "/api/dashboard", member.Token, nil, nil)
	assertContractStatus(t, memberResponse, http.StatusOK)
	memberDashboard := decodeDashboardContract(t, memberResponse)
	if memberDashboard.Payments.CountThisMonth != 1 || memberDashboard.Invoices.TotalCount != 2 {
		t.Fatalf("成员 Dashboard 汇总越权: %#v", memberDashboard)
	}
	if len(memberDashboard.RecentPayments) != 1 ||
		memberDashboard.RecentPayments[0].ID != memberPayment.ID ||
		memberDashboard.RecentPayments[0].InvoiceCount != 1 {
		t.Fatalf("成员最近支付响应错误: %#v", memberDashboard.RecentPayments)
	}
}

func TestTripExportNamedEmptyErrorMapsToBadRequest(t *testing.T) {
	application := newTestApplication(t)
	admin := setupContractAdmin(t, application)
	member := registerContractMember(t, application, admin.Token)

	tripStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	tripEnd := tripStart.Add(24 * time.Hour)
	trip := models.Trip{
		ID:              "empty-export-trip",
		OwnerUserID:     admin.ID,
		Name:            "空行程",
		StartTime:       tripStart.Format(time.RFC3339),
		EndTime:         tripEnd.Format(time.RFC3339),
		StartTimeTs:     tripStart.UnixMilli(),
		EndTimeTs:       tripEnd.UnixMilli(),
		Timezone:        "Asia/Shanghai",
		ReimburseStatus: "unreimbursed",
	}
	if err := application.db.Create(&trip).Error; err != nil {
		t.Fatalf("创建空行程失败: %v", err)
	}

	response := performContractRequest(
		t,
		application,
		http.MethodGet,
		"/api/trips/"+trip.ID+"/export",
		admin.Token,
		nil,
		nil,
	)
	assertContractStatus(t, response, http.StatusBadRequest)
	if response.body.Error != services.ErrTripHasNoPaymentsToExport.Error() {
		t.Fatalf("空行程导出错误契约异常: %#v", response.body)
	}

	otherOwner := performContractRequest(
		t,
		application,
		http.MethodGet,
		"/api/trips/"+trip.ID+"/export",
		member.Token,
		nil,
		nil,
	)
	assertContractStatus(t, otherOwner, http.StatusNotFound)
}

type dashboardContract struct {
	Payments struct {
		TotalThisMonth float64            `json:"totalThisMonth"`
		CountThisMonth int                `json:"countThisMonth"`
		CategoryStats  map[string]float64 `json:"categoryStats"`
		DailyStats     map[string]float64 `json:"dailyStats"`
	} `json:"payments"`
	RecentPayments []struct {
		models.Payment
		InvoiceCount int `json:"invoiceCount"`
	} `json:"recentPayments"`
	Invoices struct {
		TotalCount  int            `json:"totalCount"`
		TotalAmount float64        `json:"totalAmount"`
		BySource    map[string]int `json:"bySource"`
	} `json:"invoices"`
	Email struct {
		MonitoringStatus json.RawMessage `json:"monitoringStatus"`
		RecentLogs       json.RawMessage `json:"recentLogs"`
	} `json:"email"`
}

func decodeDashboardContract(t *testing.T, response recordedContractResponse) dashboardContract {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(response.body.Data, &root); err != nil {
		t.Fatalf("解析 Dashboard 根响应失败: %v", err)
	}
	for _, key := range []string{"payments", "recentPayments", "invoices", "email"} {
		if _, ok := root[key]; !ok {
			t.Fatalf("Dashboard 响应缺少字段 %q: %s", key, response.raw)
		}
	}

	var dashboard dashboardContract
	if err := json.Unmarshal(response.body.Data, &dashboard); err != nil {
		t.Fatalf("解析 Dashboard 响应失败: %v", err)
	}
	if dashboard.Payments.CategoryStats == nil || dashboard.Payments.DailyStats == nil || dashboard.Invoices.BySource == nil {
		t.Fatalf("Dashboard 汇总映射字段不应缺失: %#v", dashboard)
	}
	if dashboard.Email.MonitoringStatus == nil || dashboard.Email.RecentLogs == nil {
		t.Fatalf("Dashboard 邮箱字段不应缺失: %#v", dashboard.Email)
	}
	return dashboard
}

func seedArchitecturePayment(
	t *testing.T,
	application *Application,
	id string,
	ownerUserID string,
	transactionTime time.Time,
	isDraft bool,
) models.Payment {
	t.Helper()
	payment := models.Payment{
		ID:                id,
		OwnerUserID:       ownerUserID,
		IsDraft:           isDraft,
		Amount:            10,
		TransactionTime:   transactionTime.Format(time.RFC3339),
		TransactionTimeTs: transactionTime.UnixMilli(),
		TripAssignSrc:     "auto",
		TripAssignState:   "no_match",
		DedupStatus:       "ok",
		CreatedAt:         transactionTime,
	}
	if err := application.db.Create(&payment).Error; err != nil {
		t.Fatalf("创建 Dashboard 测试支付 %s 失败: %v", id, err)
	}
	return payment
}

func seedArchitectureInvoice(t *testing.T, application *Application, id string, ownerUserID string) models.Invoice {
	t.Helper()
	amount := 5.0
	invoice := models.Invoice{
		ID:           id,
		OwnerUserID:  ownerUserID,
		Filename:     id + ".pdf",
		OriginalName: id + ".pdf",
		FilePath:     "uploads/" + ownerUserID + "/" + id + ".pdf",
		Amount:       &amount,
		ParseStatus:  "success",
		Source:       "upload",
		DedupStatus:  "ok",
	}
	if err := application.db.Create(&invoice).Error; err != nil {
		t.Fatalf("创建 Dashboard 测试发票 %s 失败: %v", id, err)
	}
	return invoice
}
