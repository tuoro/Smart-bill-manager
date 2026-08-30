package domain

import "testing"

func TestStableItemPath(t *testing.T) {
	t.Parallel()

	path, err := StableItemPath("item_01", "amount_minor")
	if err != nil || path != "items[item_01].amount_minor" {
		t.Fatalf("StableItemPath() = %q, %v", path, err)
	}
	for _, token := range []string{"", "../item", "item.1", "item]"} {
		if _, err := StableItemPath(token, "amount_minor"); err == nil {
			t.Errorf("unsafe item key %q accepted", token)
		}
	}
}
