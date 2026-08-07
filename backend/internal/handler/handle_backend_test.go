package handler

import (
	"testing"
	"time"
)

func TestValidateBackendAPIKeysAllowsEmptyList(t *testing.T) {
	apiKeys, err := validateBackendAPIKeys(nil, "", nil, nil)
	if err != nil {
		t.Fatalf("validate empty api key list: %v", err)
	}
	if len(apiKeys) != 0 {
		t.Fatalf("expected empty api key list, got %d items", len(apiKeys))
	}
}

func TestSameConsoleDateDoesNotTreatPreviousLocalDayAsToday(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	previousDay := time.Date(2026, time.August, 7, 23, 30, 0, 0, location)
	today := time.Date(2026, time.August, 8, 0, 30, 0, 0, location)

	if sameConsoleDate(previousDay, today) {
		t.Fatal("expected timestamps on different local dates to require another checkin")
	}
}

func TestSameConsoleDateTreatsSameLocalDayAsToday(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	morning := time.Date(2026, time.August, 8, 0, 10, 0, 0, location)
	evening := time.Date(2026, time.August, 8, 23, 50, 0, 0, location)

	if !sameConsoleDate(morning, evening) {
		t.Fatal("expected timestamps on the same local date to be treated as checked in today")
	}
}
