package repositoryinvitation

import (
	"context"
	"errors"
	"testing"
	"time"

	domaininvitation "family-assistant/internal/domain/invitation"
	domainspace "family-assistant/internal/domain/space"

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
		"expires_at", "accepted_at", "accepted_by_user_id", "created_at", "updated_at", "deleted_at", "role_name",
	}).AddRow(
		inv.ID, inv.SpaceID, inv.InvitedEmail, inv.RoleID, inv.InvitedByMemberID, inv.TokenHash, inv.Status,
		inv.ExpiresAt, nil, nil, inv.CreatedAt, inv.UpdatedAt, nil, "space_member",
	)
}

func spaceRows(spaceID, spaceType, status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "type", "category", "status", "created_by_user_id", "created_at", "updated_at", "deleted_at"}).
		AddRow(spaceID, "Shared", spaceType, domainspace.CategoryFamily, status, "owner-1", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), nil, nil)
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
	mock.ExpectQuery(`SELECT .*FROM "spaces".*id = \$1.*type = \$2.*status = \$3.*deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive, 1).
		WillReturnRows(spaceRows(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive))
	mock.ExpectExec(`INSERT INTO "space_members"`).
		WithArgs(sqlmock.AnyArg(), inv.SpaceID, "user-2", inv.RoleID, domainspace.StatusActive, now, sqlmock.AnyArg(), nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "space_invitations"`).
		WithArgs(now, "user-2", domaininvitation.StatusAccepted, now, inv.ID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	acceptance, err := repo.Accept(context.Background(), inv.TokenHash, "user-2", "jane@example.com", now)
	if err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	if acceptance == nil || acceptance.Member == nil || acceptance.Member.SpaceID != inv.SpaceID || acceptance.Member.UserID != "user-2" || acceptance.Member.RoleID != inv.RoleID || acceptance.Member.Status != "ACTIVE" {
		t.Fatalf("unexpected acceptance: %#v", acceptance)
	}
	if acceptance.InvitationID != inv.ID || acceptance.SpaceID != inv.SpaceID || acceptance.RoleID != inv.RoleID || acceptance.RoleName != "space_member" || !acceptance.EmailBound {
		t.Fatalf("unexpected invitation metadata: %#v", acceptance)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAcceptUnboundEmailSucceedsAndReportsUnbound(t *testing.T) {
	db, mock := newInvitationMockDB(t)
	repo := NewRepository(db)
	inv := invitationFixture()
	inv.InvitedEmail = ""
	now := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "space_invitations".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(inv.TokenHash, 1).WillReturnRows(invitationRows(inv))
	mock.ExpectQuery(`SELECT .*FROM "spaces".*id = \$1.*type = \$2.*status = \$3.*deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive, 1).
		WillReturnRows(spaceRows(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive))
	mock.ExpectExec(`INSERT INTO "space_members"`).
		WithArgs(sqlmock.AnyArg(), inv.SpaceID, "user-2", inv.RoleID, domainspace.StatusActive, now, sqlmock.AnyArg(), nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "space_invitations"`).
		WithArgs(now, "user-2", domaininvitation.StatusAccepted, now, inv.ID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	acceptance, err := repo.Accept(context.Background(), inv.TokenHash, "user-2", "different@example.com", now)
	if err != nil {
		t.Fatalf("accept unbound invitation: %v", err)
	}
	if acceptance == nil || acceptance.EmailBound {
		t.Fatalf("acceptance = %#v, want unbound", acceptance)
	}
	if acceptance.RoleName != "space_member" {
		t.Fatalf("role name = %q, want space_member", acceptance.RoleName)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAcceptMapsDuplicateMembershipToConflictAndRollsBack(t *testing.T) {
	db, mock := newInvitationMockDB(t)
	repo := NewRepository(db)
	inv := invitationFixture()
	now := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "space_invitations".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(inv.TokenHash, 1).WillReturnRows(invitationRows(inv))
	mock.ExpectQuery(`SELECT .*FROM "spaces".*id = \$1.*type = \$2.*status = \$3.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive, 1).WillReturnRows(spaceRows(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive))
	mock.ExpectExec(`INSERT INTO "space_members"`).WithArgs(sqlmock.AnyArg(), inv.SpaceID, "user-2", inv.RoleID, domainspace.StatusActive, now, sqlmock.AnyArg(), nil).WillReturnError(gorm.ErrDuplicatedKey)
	mock.ExpectRollback()

	_, err := repo.Accept(context.Background(), inv.TokenHash, "user-2", "jane@example.com", now)
	if !errors.Is(err, domaininvitation.ErrMembershipConflict) {
		t.Fatalf("error = %v, want membership conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAcceptRollsBackWhenMembershipInsertFails(t *testing.T) {
	db, mock := newInvitationMockDB(t)
	repo := NewRepository(db)
	inv := invitationFixture()
	now := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)
	insertErr := errors.New("insert membership")

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "space_invitations".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(inv.TokenHash, 1).WillReturnRows(invitationRows(inv))
	mock.ExpectQuery(`SELECT .*FROM "spaces".*id = \$1.*type = \$2.*status = \$3.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive, 1).WillReturnRows(spaceRows(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive))
	mock.ExpectExec(`INSERT INTO "space_members"`).WithArgs(sqlmock.AnyArg(), inv.SpaceID, "user-2", inv.RoleID, domainspace.StatusActive, now, sqlmock.AnyArg(), nil).WillReturnError(insertErr)
	mock.ExpectRollback()

	_, err := repo.Accept(context.Background(), inv.TokenHash, "user-2", "jane@example.com", now)
	if !errors.Is(err, insertErr) {
		t.Fatalf("error = %v, want insert error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAcceptRollsBackWhenInvitationUpdateFails(t *testing.T) {
	db, mock := newInvitationMockDB(t)
	repo := NewRepository(db)
	inv := invitationFixture()
	now := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)
	updateErr := errors.New("consume invitation")

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "space_invitations".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(inv.TokenHash, 1).WillReturnRows(invitationRows(inv))
	mock.ExpectQuery(`SELECT .*FROM "spaces".*id = \$1.*type = \$2.*status = \$3.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive, 1).WillReturnRows(spaceRows(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive))
	mock.ExpectExec(`INSERT INTO "space_members"`).WithArgs(sqlmock.AnyArg(), inv.SpaceID, "user-2", inv.RoleID, domainspace.StatusActive, now, sqlmock.AnyArg(), nil).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "space_invitations"`).WithArgs(now, "user-2", domaininvitation.StatusAccepted, now, inv.ID).WillReturnError(updateErr)
	mock.ExpectRollback()

	_, err := repo.Accept(context.Background(), inv.TokenHash, "user-2", "jane@example.com", now)
	if !errors.Is(err, updateErr) {
		t.Fatalf("error = %v, want update error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAcceptRejectsInactiveOrPersonalParentSpace(t *testing.T) {
	for _, tt := range []struct {
		name      string
		spaceType string
		status    string
	}{
		{name: "personal", spaceType: domainspace.TypePersonal, status: domainspace.StatusActive},
		{name: "archived", spaceType: domainspace.TypeShared, status: domainspace.StatusArchived},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newInvitationMockDB(t)
			repo := NewRepository(db)
			inv := invitationFixture()
			now := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*FROM "space_invitations".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(inv.TokenHash, 1).WillReturnRows(invitationRows(inv))
			mock.ExpectQuery(`SELECT .*FROM "spaces".*id = \$1.*type = \$2.*status = \$3.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(inv.SpaceID, domainspace.TypeShared, domainspace.StatusActive, 1).WillReturnRows(spaceRows(inv.SpaceID, tt.spaceType, tt.status))
			mock.ExpectRollback()

			_, err := repo.Accept(context.Background(), inv.TokenHash, "user-2", "jane@example.com", now)
			if !errors.Is(err, domaininvitation.ErrInvalidInvitation) {
				t.Fatalf("error = %v, want invalid invitation", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
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
