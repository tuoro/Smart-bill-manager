package migrations

import (
	"testing"

	"gorm.io/gorm"
)

func TestValidateRegistry(t *testing.T) {
	noop := func(*gorm.DB) error { return nil }
	tests := []struct {
		name  string
		items []migration
	}{
		{name: "空注册表", items: nil},
		{name: "重复版本", items: []migration{{version: 1, name: "a", up: noop}, {version: 1, name: "b", up: noop}}},
		{name: "版本未排序", items: []migration{{version: 2, name: "b", up: noop}, {version: 1, name: "a", up: noop}}},
		{name: "定义不完整", items: []migration{{version: 1, name: "", up: noop}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateRegistry(test.items); err == nil {
				t.Fatal("应返回迁移定义错误")
			}
		})
	}
	if err := validateRegistry([]migration{{version: 1, name: "ok", up: noop}}); err != nil {
		t.Fatalf("有效迁移注册表不应报错: %v", err)
	}
}
