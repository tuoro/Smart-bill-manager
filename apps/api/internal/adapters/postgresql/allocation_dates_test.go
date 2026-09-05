package postgresqladapter

import "testing"

func TestAllocationCalendarDistanceDoesNotOverflowDuration(t *testing.T) {
	for _, dates := range [][2]string{{"0001-01-01", "9999-12-31"}, {"9999-12-31", "0001-01-01"}} {
		got, err := allocationDateDistance(dates[0], dates[1])
		if err != nil || got != 3652058 {
			t.Fatalf("distance %d: %v", got, err)
		}
	}
}
