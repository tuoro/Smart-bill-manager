package services

import (
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
)

// EnsureEmailConfigPasswordsEncrypted migrates legacy plaintext email_configs.password values to encrypted storage.
//
// This is part of the "forced" policy: passwords must never remain plaintext in the DB.
func EnsureEmailConfigPasswordsEncrypted(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	type row struct {
		ID       string `gorm:"column:id"`
		Password string `gorm:"column:password"`
	}

	var rows []row
	if err := db.
		Model(&models.EmailConfig{}).
		Select("id", "password").
		Where("password IS NOT NULL AND TRIM(password) != ''").
		Where("password NOT LIKE ?", emailPasswordEncPrefix+"%").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("query legacy plaintext email configs: %w", err)
	}

	migrated := 0
	for _, r := range rows {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		enc, err := encryptEmailPassword(r.Password)
		if err != nil {
			return fmt.Errorf("encrypt email config password (id=%s): %w", id, err)
		}
		if strings.TrimSpace(enc) == "" || enc == r.Password {
			return fmt.Errorf("unexpected encrypted password result (id=%s)", id)
		}
		if err := db.Model(&models.EmailConfig{}).Where("id = ?", id).Update("password", enc).Error; err != nil {
			return fmt.Errorf("update email config password (id=%s): %w", id, err)
		}
		migrated++
	}

	if migrated > 0 {
		log.Printf("[Email Monitor] migrated %d legacy email password(s) to encrypted storage", migrated)
	}
	return nil
}
