package migrations

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
)

type schemaMigration struct {
	Version   int64     `gorm:"primaryKey"`
	Name      string    `gorm:"not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigration) TableName() string {
	return "schema_migrations"
}

type migration struct {
	version int64
	name    string
	up      func(*gorm.DB) error
}

var registeredMigrations = []migration{
	{version: 2026080301, name: "legacy_data_and_indexes", up: migrateLegacyDataAndIndexes},
	{version: 2026080302, name: "money_cents", up: migrateMoneyCents},
}

// Run 先同步表结构，再按版本顺序执行尚未应用的数据迁移。
func Run(db *gorm.DB) error {
	if db == nil {
		return errors.New("数据库连接为空")
	}
	if err := validateRegistry(registeredMigrations); err != nil {
		return err
	}
	if err := migrateSchema(db); err != nil {
		return fmt.Errorf("同步数据库结构失败: %w", err)
	}
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		return fmt.Errorf("创建迁移记录表失败: %w", err)
	}

	var latestApplied int64
	if err := db.Model(&schemaMigration{}).Select("COALESCE(MAX(version), 0)").Scan(&latestApplied).Error; err != nil {
		return fmt.Errorf("读取数据库迁移版本失败: %w", err)
	}
	latestKnown := registeredMigrations[len(registeredMigrations)-1].version
	if latestApplied > latestKnown {
		return fmt.Errorf("数据库版本 %d 高于程序支持的最新版本 %d", latestApplied, latestKnown)
	}

	for _, item := range registeredMigrations {
		var applied schemaMigration
		err := db.Where("version = ?", item.version).First(&applied).Error
		switch {
		case err == nil:
			if applied.Name != item.name {
				return fmt.Errorf("迁移版本 %d 名称不一致: 数据库=%q 程序=%q", item.version, applied.Name, item.name)
			}
			continue
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("检查迁移 %d 失败: %w", item.version, err)
		}

		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := item.up(tx); err != nil {
				return err
			}
			return tx.Create(&schemaMigration{
				Version:   item.version,
				Name:      item.name,
				AppliedAt: time.Now().UTC(),
			}).Error
		}); err != nil {
			return fmt.Errorf("执行迁移 %d (%s) 失败: %w", item.version, item.name, err)
		}
	}

	return nil
}

func validateRegistry(items []migration) error {
	if len(items) == 0 {
		return errors.New("未注册数据库迁移")
	}
	versions := make(map[int64]struct{}, len(items))
	for i, item := range items {
		if item.version <= 0 || item.name == "" || item.up == nil {
			return fmt.Errorf("第 %d 个迁移定义无效", i+1)
		}
		if _, exists := versions[item.version]; exists {
			return fmt.Errorf("迁移版本重复: %d", item.version)
		}
		versions[item.version] = struct{}{}
	}
	if !sort.SliceIsSorted(items, func(i, j int) bool { return items[i].version < items[j].version }) {
		return errors.New("迁移必须按版本升序注册")
	}
	return nil
}
