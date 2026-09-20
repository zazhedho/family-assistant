package repositoryspace

import (
	"context"
	"errors"
	"testing"

	domainspace "family-assistant/internal/domain/space"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newSpaceMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return db, mock
}

func spaceFixture() *domainspace.Space {
	return &domainspace.Space{
		ID:              "space-1",
		Name:            "Home",
		Type:            domainspace.TypeShared,
		Category:        domainspace.CategoryFamily,
		Status:          domainspace.StatusActive,
		CreatedByUserID: "user-1",
	}
}

func memberFixture() *domainspace.Member {
	return &domainspace.Member{
		ID:      "member-1",
		SpaceID: "space-1",
		UserID:  "user-1",
		RoleID:  "role-1",
		Status:  domainspace.StatusActive,
	}
}

func resolvedMembershipRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "space_id", "space_name", "space_type", "space_category",
		"user_id", "user_name", "user_email", "role_id", "role_name", "status",
	})
}

func TestCreateWithOwnerRollsBackMembershipFailure(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := NewRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "spaces"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "space_members"`).WillReturnError(errors.New("insert member"))
	mock.ExpectRollback()

	err := repo.CreateWithOwner(context.Background(), spaceFixture(), memberFixture())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWithOwnerCommitsAfterBothInserts(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := NewRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "spaces"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "space_members"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := repo.CreateWithOwner(context.Background(), spaceFixture(), memberFixture()); err != nil {
		t.Fatalf("create with owner: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWithOwnerPropagatesSpaceInsertFailure(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := NewRepository(db)
	wantErr := errors.New("insert space")

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "spaces"`).WillReturnError(wantErr)
	mock.ExpectRollback()

	err := repo.CreateWithOwner(context.Background(), spaceFixture(), memberFixture())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected space insert error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWithOwnerRejectsNilInputs(t *testing.T) {
	tests := []struct {
		name   string
		space  *domainspace.Space
		member *domainspace.Member
		want   error
	}{
		{name: "nil space", member: memberFixture(), want: errSpaceRequired},
		{name: "nil member", space: spaceFixture(), want: errMemberRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newSpaceMockDB(t)
			repo := NewRepository(db)

			err := repo.CreateWithOwner(context.Background(), tt.space, tt.member)
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCreateWithOwnerRejectsCrossSpaceMember(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := NewRepository(db)
	space := spaceFixture()
	member := memberFixture()
	member.SpaceID = "another-space"

	err := repo.CreateWithOwner(context.Background(), space, member)
	if !errors.Is(err, errMemberSpaceMismatch) {
		t.Fatalf("expected Space mismatch error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWithOwnerGeneratesAndBindsBlankIDs(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := NewRepository(db)
	space := spaceFixture()
	space.ID = ""
	member := memberFixture()
	member.ID = ""
	member.SpaceID = ""

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "spaces"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "space_members"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := repo.CreateWithOwner(context.Background(), space, member); err != nil {
		t.Fatalf("create with generated IDs: %v", err)
	}
	if _, err := uuid.Parse(space.ID); err != nil {
		t.Fatalf("space ID = %q: %v", space.ID, err)
	}
	if _, err := uuid.Parse(member.ID); err != nil {
		t.Fatalf("member ID = %q: %v", member.ID, err)
	}
	if member.SpaceID != space.ID {
		t.Fatalf("member Space ID = %q, want %q", member.SpaceID, space.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListActiveByUserIDFiltersDeletedAndInactiveRows(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*FROM space_members sm.*JOIN spaces s ON s\.id = sm\.space_id.*JOIN users u ON u\.id = sm\.user_id.*JOIN roles r ON r\.id = sm\.role_id.*sm\.user_id = \$1.*sm\.status = \$2.*sm\.deleted_at IS NULL.*s\.status = \$3.*s\.deleted_at IS NULL`).
		WithArgs("user-1", domainspace.StatusActive, domainspace.StatusActive).
		WillReturnRows(resolvedMembershipRows().AddRow(
			"member-1", "space-1", "Home", "SHARED", "family",
			"user-1", "User One", "user@example.com", "role-1", "space_owner", "ACTIVE",
		))

	got, err := repo.ListActiveByUserID(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("list active memberships: %v", err)
	}
	if len(got) != 1 || got[0].ID != "member-1" || got[0].SpaceID != "space-1" || got[0].UserEmail != "user@example.com" {
		t.Fatalf("unexpected memberships: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindActiveMembershipFiltersDeletedAndInactiveRows(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*FROM space_members sm.*JOIN spaces s ON s\.id = sm\.space_id.*sm\.user_id = \$1 AND sm\.space_id = \$2.*sm\.status = \$3.*sm\.deleted_at IS NULL.*s\.status = \$4.*s\.deleted_at IS NULL.*LIMIT \$5`).
		WithArgs("user-1", "space-1", domainspace.StatusActive, domainspace.StatusActive, 1).
		WillReturnRows(resolvedMembershipRows().AddRow(
			"member-1", "space-1", "Home", "SHARED", "family",
			"user-1", "User One", "user@example.com", "role-1", "space_owner", "ACTIVE",
		))

	got, err := repo.FindActiveMembership(context.Background(), "user-1", "space-1")
	if err != nil {
		t.Fatalf("find active membership: %v", err)
	}
	if got.ID != "member-1" || got.SpaceName != "Home" || got.RoleName != "space_owner" {
		t.Fatalf("unexpected membership: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListActiveMembersFiltersDeletedAndInactiveRows(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*FROM space_members sm.*JOIN spaces s ON s\.id = sm\.space_id.*sm\.space_id = \$1.*sm\.status = \$2.*sm\.deleted_at IS NULL.*s\.status = \$3.*s\.deleted_at IS NULL`).
		WithArgs("space-1", domainspace.StatusActive, domainspace.StatusActive).
		WillReturnRows(resolvedMembershipRows().AddRow(
			"member-1", "space-1", "Home", "SHARED", "family",
			"user-1", "User One", "user@example.com", "role-1", "space_owner", "ACTIVE",
		))

	got, err := repo.ListActiveMembers(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("list active members: %v", err)
	}
	if len(got) != 1 || got[0].UserID != "user-1" || got[0].RoleID != "role-1" {
		t.Fatalf("unexpected members: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositorySatisfiesDomainInterface(t *testing.T) {
	var _ domainspace.Repository = (*Repository)(nil)
}
