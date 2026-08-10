package services

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
)

// EnsureEmailConfigPasswordsEncrypted 将旧版明文邮箱密码迁移为加密存储。
// 该修复依赖数据库外部密钥，因此保留在启动流程中；所有数据库写入必须在同一事务内完成。
func EnsureEmailConfigPasswordsEncrypted(db *gorm.DB) error {
	if db == nil {
		return errors.New("邮箱密码迁移的数据库连接为空")
	}

	migrated := 0
	if err := db.Transaction(func(tx *gorm.DB) error {
		type row struct {
			ID       string `gorm:"column:id"`
			Password string `gorm:"column:password"`
		}

		var rows []row
		if err := tx.
			Model(&models.EmailConfig{}).
			Select("id", "password").
			Where("password IS NOT NULL AND TRIM(password) != ''").
			Where("password NOT LIKE ?", emailPasswordEncPrefix+"%").
			Order("id ASC").
			Find(&rows).Error; err != nil {
			return fmt.Errorf("读取旧版明文邮箱密码失败: %w", err)
		}

		for _, row := range rows {
			if strings.TrimSpace(row.ID) == "" {
				return errors.New("旧版明文邮箱密码记录缺少 ID")
			}
			encrypted, err := encryptEmailPassword(row.Password)
			if err != nil {
				return fmt.Errorf("加密邮箱密码失败 (id=%s): %w", row.ID, err)
			}
			if strings.TrimSpace(encrypted) == "" || encrypted == row.Password {
				return fmt.Errorf("邮箱密码加密结果无效 (id=%s)", row.ID)
			}
			result := tx.Model(&models.EmailConfig{}).Where("id = ?", row.ID).Update("password", encrypted)
			if result.Error != nil {
				return fmt.Errorf("更新加密邮箱密码失败 (id=%s): %w", row.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("更新加密邮箱密码失败 (id=%s): 更新行数=%d", row.ID, result.RowsAffected)
			}
			migrated++
		}
		return nil
	}); err != nil {
		return err
	}

	if migrated > 0 {
		log.Printf("[Email Monitor] migrated %d legacy email password(s) to encrypted storage", migrated)
	}
	return nil
}
