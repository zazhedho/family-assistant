package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
)

func TestSpaceChannelBindInputHidesVerifiedContextFromPublicJSON(t *testing.T) {
	input := SpaceChannelBindInput{
		Space:           "space-1",
		CurrentChannel:  "whatsapp",
		CurrentChatID:   "123@g.us",
		CurrentChatType: "group",
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"whatsapp", "123@g.us", "group"} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("trusted context %q leaked into public JSON: %s", value, encoded)
		}
	}
}

func TestScheduledMessageTargetOutputCarriesScopedIdentifiers(t *testing.T) {
	memberID := "member-1"
	output := ScheduledMessageTargetOutput{
		Kind:        domainscheduledmessage.TargetDM,
		MemberID:    &memberID,
		DisplayName: "Alex",
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"kind":"DM"`, `"member_id":"member-1"`, `"display_name":"Alex"`} {
		if !strings.Contains(string(encoded), required) {
			t.Fatalf("target output %s missing %s", encoded, required)
		}
	}
	if strings.Contains(string(encoded), "external_chat_id") {
		t.Fatalf("target output exposes a raw chat address: %s", encoded)
	}
}

func TestScheduledMessageGenerationInputCarriesBoundedContext(t *testing.T) {
	scheduledAt := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	input := ScheduledMessageGenerationInput{
		Theme:         "A warm reminder in Bahasa Indonesia",
		RecipientName: "Alex",
		ScheduledAt:   scheduledAt,
		Timezone:      "Asia/Jakarta",
		RecentMessages: []string{
			"Morning, Alex. Take a break.",
		},
	}
	if input.Theme == "" || input.RecipientName != "Alex" || !input.ScheduledAt.Equal(scheduledAt) ||
		input.Timezone != "Asia/Jakarta" || len(input.RecentMessages) != 1 {
		t.Fatalf("incomplete generation context: %#v", input)
	}
}
