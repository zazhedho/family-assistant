package domainlocation

import "testing"

func TestTableNames(t *testing.T) {
	tests := map[string]string{
		"provinces":          (Province{}).TableName(),
		"cities":             (City{}).TableName(),
		"districts":          (District{}).TableName(),
		"villages":           (Village{}).TableName(),
		"location_sync_jobs": (SyncJob{}).TableName(),
	}

	for want, got := range tests {
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	}
}
