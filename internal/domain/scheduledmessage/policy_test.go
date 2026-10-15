package scheduledmessage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCanCancel(t *testing.T) {
	tests := []struct {
		name    string
		target  TargetKind
		role    string
		actor   string
		creator string
		want    bool
	}{
		{name: "DM creator can cancel own schedule", target: TargetDM, role: "space_member", actor: "creator", creator: "creator", want: true},
		{name: "DM owner can cancel another schedule", target: TargetDM, role: "space_owner", actor: "owner", creator: "creator", want: true},
		{name: "group owner can cancel", target: TargetGroup, role: "space_owner", actor: "owner", creator: "creator", want: true},
		{name: "group admin can cancel", target: TargetGroup, role: "space_admin", actor: "admin", creator: "creator", want: true},
		{name: "group creator cannot cancel without owner or admin role", target: TargetGroup, role: "space_member", actor: "creator", creator: "creator", want: false},
		{name: "unrelated DM member cannot cancel", target: TargetDM, role: "space_member", actor: "other", creator: "creator", want: false},
		{name: "empty actor cannot cancel", target: TargetDM, role: "space_owner", creator: "creator", want: false},
		{name: "unknown target cannot cancel", target: TargetKind("UNKNOWN"), role: "space_owner", actor: "owner", creator: "creator", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanCancel(tt.target, tt.role, tt.actor, tt.creator); got != tt.want {
				t.Fatalf("CanCancel()=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestCanUpdate(t *testing.T) {
	tests := []struct {
		name    string
		target  TargetKind
		role    string
		actor   string
		creator string
		want    bool
	}{
		{name: "DM creator can update", target: TargetDM, role: "space_member", actor: "creator", creator: "creator", want: true},
		{name: "group creator can update", target: TargetGroup, role: "space_member", actor: "creator", creator: "creator", want: true},
		{name: "DM owner can update another schedule", target: TargetDM, role: "space_owner", actor: "owner", creator: "creator", want: true},
		{name: "group admin can update another schedule", target: TargetGroup, role: "space_admin", actor: "admin", creator: "creator", want: true},
		{name: "unrelated group member cannot update", target: TargetGroup, role: "space_member", actor: "other", creator: "creator", want: false},
		{name: "unrelated DM member cannot update", target: TargetDM, role: "space_member", actor: "other", creator: "creator", want: false},
		{name: "empty creator does not authorize ordinary member", target: TargetGroup, role: "space_member", actor: "member", want: false},
		{name: "empty actor cannot update", target: TargetDM, role: "space_owner", creator: "creator", want: false},
		{name: "unknown target cannot update", target: TargetKind("UNKNOWN"), role: "space_owner", actor: "owner", creator: "creator", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanUpdate(tt.target, tt.role, tt.actor, tt.creator); got != tt.want {
				t.Fatalf("CanUpdate()=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestScheduleDomainModelsDoNotMarshalPrivateContent(t *testing.T) {
	content := "private scheduled body"
	schedule, err := json.Marshal(Schedule{Content: content})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := json.Marshal(Delivery{ContentSnapshot: &content})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(schedule), content) || strings.Contains(string(delivery), content) {
		t.Fatal("domain models must not serialize private message content")
	}
}

func TestCanUnsubscribe(t *testing.T) {
	if !CanUnsubscribe(TargetDM, "recipient", "recipient") {
		t.Fatal("DM recipient should unsubscribe their own schedule")
	}
	if CanUnsubscribe(TargetDM, "other", "recipient") {
		t.Fatal("unrelated member should not unsubscribe a DM schedule")
	}
	if CanUnsubscribe(TargetGroup, "recipient", "recipient") {
		t.Fatal("group schedule does not have a DM unsubscribe target")
	}
	if CanUnsubscribe(TargetDM, "", "") {
		t.Fatal("empty IDs must not authorize unsubscribe")
	}
}

func TestCanRead(t *testing.T) {
	tests := []struct {
		name      string
		target    TargetKind
		role      string
		actor     string
		creator   string
		recipient string
		want      bool
	}{
		{name: "DM creator can read", target: TargetDM, actor: "creator", creator: "creator", recipient: "recipient", want: true},
		{name: "DM recipient can read", target: TargetDM, actor: "recipient", creator: "creator", recipient: "recipient", want: true},
		{name: "owner can read DM", target: TargetDM, role: "space_owner", actor: "owner", creator: "creator", recipient: "recipient", want: true},
		{name: "unrelated member cannot read private DM content", target: TargetDM, role: "space_member", actor: "other", creator: "creator", recipient: "recipient", want: false},
		{name: "group member can read", target: TargetGroup, role: "space_member", actor: "member", creator: "creator", want: true},
		{name: "group viewer can read", target: TargetGroup, role: "space_viewer", actor: "viewer", creator: "creator", want: true},
		{name: "unknown group role cannot read", target: TargetGroup, role: "some_role", actor: "member", creator: "creator", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanRead(tt.target, tt.role, tt.actor, tt.creator, tt.recipient); got != tt.want {
				t.Fatalf("CanRead()=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestUnsubscribedScheduleCannotReactivate(t *testing.T) {
	if CanTransitionStatus(ScheduleStatusUnsubscribed, ScheduleStatusActive) {
		t.Fatal("unsubscribed schedule must not transition back to ACTIVE")
	}
	if !CanTransitionStatus(ScheduleStatusActive, ScheduleStatusUnsubscribed) {
		t.Fatal("ACTIVE schedule should be able to transition to UNSUBSCRIBED")
	}
}
