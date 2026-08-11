package main

import (
	"errors"
	"reflect"
	"testing"

	"gorm.io/gorm"
)

func TestPrepareDatabaseWithRunsMigrationBeforeLaterStartupWork(t *testing.T) {
	db := &gorm.DB{}
	steps := make([]string, 0, 2)
	err := prepareDatabaseWith(
		db,
		func(got *gorm.DB) error {
			if got != db {
				t.Fatal("迁移没有收到启动数据库")
			}
			steps = append(steps, "migration")
			return nil
		},
		func(got *gorm.DB) error {
			if got != db {
				t.Fatal("后续启动步骤没有收到启动数据库")
			}
			steps = append(steps, "email-password-encryption")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("启动数据库准备失败: %v", err)
	}
	if want := []string{"migration", "email-password-encryption"}; !reflect.DeepEqual(steps, want) {
		t.Fatalf("启动顺序错误: got=%v want=%v", steps, want)
	}
}

func TestPrepareDatabaseWithFailsClosedWhenMigrationFails(t *testing.T) {
	wantErr := errors.New("synthetic migration failure")
	laterStepCalled := false
	err := prepareDatabaseWith(
		&gorm.DB{},
		func(*gorm.DB) error { return wantErr },
		func(*gorm.DB) error {
			laterStepCalled = true
			return nil
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("应返回迁移错误，实际为 %v", err)
	}
	if laterStepCalled {
		t.Fatal("迁移失败后仍执行了后续启动步骤")
	}
}
