//go:build cgo

package services

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
)

func TestTripServiceUsesInjectedDatabase(t *testing.T) {
	primaryDB := openServiceTestDB(t)
	secondaryDB := openServiceTestDB(t)

	service := NewTripService(primaryDB, t.TempDir())
	trip, _, err := service.Create("owner-1", CreateTripInput{
		Name:      "上海出差",
		StartTime: "2026-08-01T08:00:00+08:00",
		EndTime:   "2026-08-02T18:00:00+08:00",
	})
	if err != nil {
		t.Fatalf("创建行程失败: %v", err)
	}
	if trip.OwnerUserID != "owner-1" || trip.Name != "上海出差" {
		t.Fatalf("行程内容异常: %#v", trip)
	}

	assertTripCount(t, primaryDB, 1)
	assertTripCount(t, secondaryDB, 0)
}

func TestPrepareTripExportZipReturnsNamedErrorWhenTripIsEmpty(t *testing.T) {
	db := openServiceTestDB(t)
	service := NewTripService(db, t.TempDir())
	trip, _, err := service.Create("owner-1", CreateTripInput{
		Name:      "空行程",
		StartTime: "2026-08-01T08:00:00+08:00",
		EndTime:   "2026-08-02T18:00:00+08:00",
	})
	if err != nil {
		t.Fatalf("创建空行程失败: %v", err)
	}

	_, err = service.PrepareTripExportZip(context.Background(), "owner-1", trip.ID)
	if !errors.Is(err, ErrTripHasNoPaymentsToExport) {
		t.Fatalf("空行程导出应返回具名错误，实际为: %v", err)
	}
}

func assertTripCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.Trip{}).Count(&count).Error; err != nil {
		t.Fatalf("统计行程记录失败: %v", err)
	}
	if count != want {
		t.Fatalf("行程记录数应为 %d，实际为 %d", want, count)
	}
}
