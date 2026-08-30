package system

import (
	"regexp"
	"testing"
)

func TestIDGeneratorCreatesUUIDv4(t *testing.T) {
	t.Parallel()

	id, err := (IDGenerator{}).NewID()
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("id %q is not UUIDv4", id)
	}
}
