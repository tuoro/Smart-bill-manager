//go:build cgo

package services

import (
	"testing"

	"smart-bill-manager/internal/models"
)

func TestRegressionSampleServiceUsesInjectedDatabase(t *testing.T) {
	primaryDB := openServiceTestDB(t)
	secondaryDB := openServiceTestDB(t)

	primarySample := models.RegressionSample{
		ID:           "primary-sample",
		Kind:         "invoice",
		Name:         "primary",
		Origin:       "ui",
		SourceType:   "invoice",
		SourceID:     "invoice-1",
		CreatedBy:    "admin-1",
		RawText:      "primary raw text",
		RawHash:      "primary-hash",
		ExpectedJSON: `{}`,
	}
	if err := primaryDB.Create(&primarySample).Error; err != nil {
		t.Fatalf("写入主数据库回归样例失败: %v", err)
	}

	secondarySample := primarySample
	secondarySample.ID = "secondary-sample"
	secondarySample.Name = "secondary"
	secondarySample.SourceID = "invoice-2"
	secondarySample.RawText = "secondary raw text"
	secondarySample.RawHash = "secondary-hash"
	if err := secondaryDB.Create(&secondarySample).Error; err != nil {
		t.Fatalf("写入次数据库回归样例失败: %v", err)
	}

	service := NewRegressionSampleService(primaryDB)
	rows, total, err := service.List(ListRegressionSamplesParams{})
	if err != nil {
		t.Fatalf("读取回归样例失败: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != primarySample.ID {
		t.Fatalf("回归样例服务未使用注入数据库: total=%d rows=%#v", total, rows)
	}
}
