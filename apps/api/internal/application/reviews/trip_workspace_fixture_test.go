package reviews

import (
	"context"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func seedManualTrip(t *testing.T, fixture reviewFixture, label, name, start, end string) ports.TripManagementResult {
	t.Helper()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	result, err := service.Manage(context.Background(), fixture.tenant, "", "create", tripapp.ManagementInput{
		Details: domain.TripDetails{Name: name, StartDate: start, EndDate: end, Timezone: "Asia/Shanghai"},
		Reason:  "创建合成测试行程", IdempotencyKey: label + "-create", RequestID: label + "-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assignmentVersion(t *testing.T, fixture reviewFixture, factType domain.DocumentType, id string) int {
	t.Helper()
	table := "payments"
	if factType == domain.DocumentInvoice {
		table = "invoices"
	}
	var version int
	if err := fixture.store.DB().QueryRowContext(context.Background(), `SELECT version FROM `+table+` WHERE tenant_id = ? AND id = ?`, fixture.tenant.TenantID, id).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

// 人工生命周期测试显式选择禁止自动；自动匹配测试不能调用此 helper。
func blockPaymentAuto(t *testing.T, fixture reviewFixture, paymentID string) {
	t.Helper()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	if err := service.Preference(context.Background(), fixture.tenant, paymentID, "blocked", "synthetic-block-auto",
		assignmentVersion(t, fixture, domain.DocumentPayment, paymentID)); err != nil {
		t.Fatal(err)
	}
}

func deleteManualTrip(t *testing.T, fixture reviewFixture, trip ports.TripManagementResult, requestID string) {
	t.Helper()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	if _, err := service.Manage(context.Background(), fixture.tenant, trip.TripID, "delete", tripapp.ManagementInput{
		ExpectedVersion: trip.Version, Reason: "删除合成测试行程", IdempotencyKey: requestID + "-key", RequestID: requestID,
	}); err != nil {
		t.Fatal(err)
	}
}
