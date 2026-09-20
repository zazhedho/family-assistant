package repositoryinvitation

import (
	"context"
	"errors"
	"testing"
	"time"

	domaininvitation "family-assistant/internal/domain/invitation"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newInvitationMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return db, mock
}

func invitationFixture() *domaininvitation.Invitation {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	return &domaininvitation.Invitation{
		ID:                "invitation-1",
		SpaceID:           "space-1",
		InvitedEmail:      "jane@example.com",
		RoleID:            "role-member",
		InvitedByMemberID: "member-owner",
		TokenHash:         "hash-1",
		Status:            domaininvitation.StatusPending,
		ExpiresAt:         now.Add(72 * time.Hour),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func invitationRows(inv *domaininvitation.Invitation) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "space_id", "invited_email", "role_id", "invited_by_member_id", "token_hash", "status",
		"expires_at", "accepted_at", "accepted_by_user_id", "created_at", "updated_at", "deleted_at",
	}).AddRow(
		inv.ID, inv.SpaceID, inv.InvitedEmail, inv.RoleID, inv.InvitedByMemberID, inv.TokenHash, inv.Status,
		inv.ExpiresAt, nil, nil, inv.CreatedAt, inv.UpdatedAt, nil,
	)
}

func TestAcceptLocksInvitationAndAtomicallyAddsMembership(t *testing.T) {
	db, mock := newInvitationMockDB(t)
	repo := NewRepository(db)
	inv := invitationFixture()
	now := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "space_invitations".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(inv.TokenHash, 1).
		WillReturnRows(invitationRows(inv))
	mock.ExpectExec(`INSERT INTO "space_members"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "space_invitations"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	member, err := repo.Accept(context.Background(), inv.TokenHash, "user-2", "jane@example.com", now)
	if err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	if member == nil || member.SpaceID != inv.SpaceID || member.UserID != "user-2" || member.RoleID != inv.RoleID || member.Status != "ACTIVE" {
		t.Fatalf("unexpected member: %#v", member)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAcceptInvalidInvitationStatesUseSameSafeError(t *testing.T) {
	tests := []struct {
		name   string
		status string
		expiry time.Time
		email  string
	}{
		{name: "replayed", status: domaininvitation.StatusAccepted, expiry: time.Now().UTC().Add(time.Hour), email: "jane@example.com"},
		{name: "expired", status: domaininvitation.StatusPending, expiry: time.Now().UTC().Add(-time.Hour), email: "jane@example.com"},
		{name: "email mismatch", status: domaininvitation.StatusPending, expiry: time.Now().UTC().Add(time.Hour), email: "other@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newInvitationMockDB(t)
			repo := NewRepository(db)
			inv := invitationFixture()
			inv.Status, inv.ExpiresAt, inv.InvitedEmail = tt.status, tt.expiry, tt.email
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*FROM "space_invitations".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).
				WithArgs(inv.TokenHash, 1).
				WillReturnRows(invitationRows(inv))
			mock.ExpectRollback()

			_, err := repo.Accept(context.Background(), inv.TokenHash, "user-2", "jane@example.com", time.Now().UTC())
			if !errors.Is(err, domaininvitation.ErrInvalidInvitation) {
				t.Fatalf("error = %v, want ErrInvalidInvitation", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
	}
}

func TestAcceptGuessedTokenUsesSameSafeError(t *testing.T) {
	db, mock := newInvitationMockDB(t)
	repo := NewRepository(db)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "space_invitations".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).
		WithArgs("guessed-hash", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "space_id", "invited_email", "role_id", "invited_by_member_id", "token_hash", "status",
			"expires_at", "accepted_at", "accepted_by_user_id", "created_at", "updated_at", "deleted_at",
		}))
	mock.ExpectRollback()

	_, err := repo.Accept(context.Background(), "guessed-hash", "user-2", "jane@example.com", time.Now().UTC())
	if !errors.Is(err, domaininvitation.ErrInvalidInvitation) {
		t.Fatalf("error = %v, want ErrInvalidInvitation", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
