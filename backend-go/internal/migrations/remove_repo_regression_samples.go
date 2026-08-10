package migrations

import "gorm.io/gorm"

const (
	legacyRepoRegressionOrigin    = "repo"
	legacyRepoRegressionSource    = "repo"
	legacyRepoRegressionCreatedBy = "repo_sync"
)

// removeRepoRegressionSamples 只清理旧版启动同步自动创建的仓库样本。
// 三个标记必须同时完全匹配，避免影响用户手工创建或曾被同步流程更新的行。
func removeRepoRegressionSamples(db *gorm.DB) error {
	if !db.Migrator().HasTable("regression_samples") {
		return nil
	}
	return db.Exec(
		`DELETE FROM regression_samples
		 WHERE origin = ? AND source_type = ? AND created_by = ?`,
		legacyRepoRegressionOrigin,
		legacyRepoRegressionSource,
		legacyRepoRegressionCreatedBy,
	).Error
}
