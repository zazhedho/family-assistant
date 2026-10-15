package repositoryscheduledmessage

import (
	"context"
	"testing"

	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	"family-assistant/internal/dto"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestListUsesRequestedLimitAndCapsAtOneHundred(t *testing.T) {
	for _, test := range []struct {
		requested int
		want      int
	}{
		{requested: 7, want: 7},
		{requested: 101, want: 100},
	} {
		db, mock := newScheduledMessageMockDB(t)
		repo := NewRepository(db)
		spaceID := "00000000-0000-4000-8000-000000000201"
		viewerID := "00000000-0000-4000-8000-000000000301"
		mock.ExpectQuery(`(?s)SELECT .*scheduled_messages.space_id = .*scheduled_messages.target <> .*created_by_member_id = .*recipient_member_id = .*LIMIT \$`).
			WithArgs(spaceID, "ACTIVE", domainscheduledmessage.TargetDM, false, viewerID, viewerID, test.want).
			WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target"}))

		if _, err := repo.List(context.Background(), dto.ScheduledMessageListInput{
			Space: spaceID, ViewerMemberID: viewerID, Limit: test.requested,
		}); err != nil {
			t.Fatalf("list with requested limit %d: %v", test.requested, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("requested limit %d SQL expectations: %v", test.requested, err)
		}
	}
}
