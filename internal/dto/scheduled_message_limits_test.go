package dto

import (
	"encoding/json"
	"testing"
)

func TestScheduledMessageListLimitIsNotClientControlledOrSerialized(t *testing.T) {
	var input ScheduledMessageListInput
	if err := json.Unmarshal([]byte(`{"limit":1}`), &input); err != nil {
		t.Fatal(err)
	}
	if input.Limit != 0 {
		t.Fatalf("client set list limit to %d", input.Limit)
	}
	input.Limit = 40
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{}` {
		t.Fatalf("serialized list input = %s, want no private query fields", encoded)
	}
}
