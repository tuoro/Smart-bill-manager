package domain

import "testing"

func TestPaymentBusinessDateUsesSourceTimezone(t *testing.T) {
	t.Parallel()

	date, err := PaymentBusinessDate("2026-08-27T23:30:00Z", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if date != "2026-08-28" {
		t.Fatalf("business date = %q, want 2026-08-28", date)
	}
}

func TestPaymentBusinessDateRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	for name, input := range map[string][2]string{
		"invalid instant":  {"not-an-instant", "Asia/Shanghai"},
		"invalid timezone": {"2026-08-27T23:30:00Z", "Mars/Olympus"},
	} {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := PaymentBusinessDate(input[0], input[1]); err == nil {
				t.Fatal("invalid payment date input unexpectedly succeeded")
			}
		})
	}
}
