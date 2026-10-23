package repositoryidentity

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGroupSpaceRepositoryReturnsActiveBinding(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	mock.ExpectQuery(`SELECT .*space_id.* FROM "space_channels" JOIN spaces.*space_channels.channel = \$1 AND space_channels.external_chat_id = \$2 AND space_channels.status = \$3 AND spaces.status = \$4 AND spaces.deleted_at IS NULL`).
		WithArgs("whatsapp", "120363@g.us", "ACTIVE", "ACTIVE", 1).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}).AddRow("space-1"))

	spaceID, found, err := NewGroupSpaceRepository(db).ActiveSpaceIDForChat(context.Background(), " WhatsApp ", " 120363@g.us ")
	if err != nil || !found || spaceID != "space-1" {
		t.Fatalf("ActiveSpaceIDForChat = %q, %v, %v", spaceID, found, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGroupSpaceRepositoryReportsUnboundChat(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	mock.ExpectQuery(`FROM "space_channels"`).WillReturnRows(sqlmock.NewRows([]string{"space_id"}))

	spaceID, found, err := NewGroupSpaceRepository(db).ActiveSpaceIDForChat(context.Background(), "whatsapp", "120363@g.us")
	if err != nil || found || spaceID != "" {
		t.Fatalf("ActiveSpaceIDForChat = %q, %v, %v", spaceID, found, err)
	}
}

func TestGroupSpaceRepositoryBlankChatSkipsQuery(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	_, found, err := NewGroupSpaceRepository(db).ActiveSpaceIDForChat(context.Background(), "whatsapp", " ")
	if err != nil || found {
		t.Fatalf("blank chat = %v, %v", found, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGroupSpaceRepositoryPropagatesDatabaseError(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	boom := errors.New("db down")
	mock.ExpectQuery(`FROM "space_channels"`).WillReturnError(boom)

	_, _, err := NewGroupSpaceRepository(db).ActiveSpaceIDForChat(context.Background(), "whatsapp", "120363@g.us")
	if !errors.Is(err, boom) {
		t.Fatalf("expected db error, got %v", err)
	}
}
