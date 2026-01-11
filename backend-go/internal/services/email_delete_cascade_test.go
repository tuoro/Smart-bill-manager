//go:build cgo
// +build cgo

package services

import (
	"os"
	"path/filepath"
	"testing"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
)

func TestEmailDeleteConfig_DeletesLogs(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0755)

	db := database.Init(dir)
	if err := db.AutoMigrate(&models.EmailConfig{}, &models.EmailLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	owner := "u1"
	cfg1 := &models.EmailConfig{
		ID:          "c1",
		OwnerUserID: owner,
		Email:       "a@example.com",
		IMAPHost:    "imap.example.com",
		IMAPPort:    993,
		Password:    "pw",
		IsActive:    1,
	}
	cfg2 := &models.EmailConfig{
		ID:          "c2",
		OwnerUserID: owner,
		Email:       "b@example.com",
		IMAPHost:    "imap.example.com",
		IMAPPort:    993,
		Password:    "pw",
		IsActive:    1,
	}
	if err := db.Create(cfg1).Error; err != nil {
		t.Fatalf("create cfg1: %v", err)
	}
	if err := db.Create(cfg2).Error; err != nil {
		t.Fatalf("create cfg2: %v", err)
	}

	subject := "s"
	log1 := &models.EmailLog{ID: "l1", OwnerUserID: owner, EmailConfigID: "c1", Mailbox: "INBOX", MessageUID: 1, Subject: &subject}
	log2 := &models.EmailLog{ID: "l2", OwnerUserID: owner, EmailConfigID: "c1", Mailbox: "INBOX", MessageUID: 2, Subject: &subject}
	logOther := &models.EmailLog{ID: "l3", OwnerUserID: owner, EmailConfigID: "c2", Mailbox: "INBOX", MessageUID: 3, Subject: &subject}
	if err := db.Create(log1).Error; err != nil {
		t.Fatalf("create log1: %v", err)
	}
	if err := db.Create(log2).Error; err != nil {
		t.Fatalf("create log2: %v", err)
	}
	if err := db.Create(logOther).Error; err != nil {
		t.Fatalf("create logOther: %v", err)
	}

	svc := NewEmailService(filepath.Join(dir, "uploads"), nil)
	if err := svc.DeleteConfig(owner, "c1"); err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}

	var cnt int64
	if err := db.Model(&models.EmailLog{}).Where("email_config_id = ?", "c1").Count(&cnt).Error; err != nil {
		t.Fatalf("count logs c1: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("expected logs for config c1 deleted, got %d", cnt)
	}

	if err := db.Model(&models.EmailLog{}).Where("email_config_id = ?", "c2").Count(&cnt).Error; err != nil {
		t.Fatalf("count logs c2: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected logs for config c2 kept, got %d", cnt)
	}

	if err := db.Model(&models.EmailConfig{}).Where("id = ?", "c1").Count(&cnt).Error; err != nil {
		t.Fatalf("count config c1: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("expected config c1 deleted, got %d", cnt)
	}
}
