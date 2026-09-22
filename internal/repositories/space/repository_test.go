package repositoryspace

import (
	"context"
	"errors"
	"testing"
	"time"

	domainspace "family-assistant/internal/domain/space"
	interfacespace "family-assistant/internal/interfaces/space"

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

func TestUpdateScopesActiveSharedSpaceAndWritesOnlyPatch(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := &Repository{DB: db}
	name := "Updated"
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "spaces" SET "name"=\$1,"updated_at"=\$2 WHERE \(id = \$3 AND status = \$4 AND deleted_at IS NULL\) AND "spaces"\."deleted_at" IS NULL`).
		WithArgs(name, now, "space-1", domainspace.StatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Update(context.Background(), "space-1", domainspace.SpaceUpdateFields{Name: &name}, now); err != nil {
		t.Fatalf("update space: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveSetsArchivedStatusAndDeletedAt(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := &Repository{DB: db}
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "spaces" SET "deleted_at"=\$1,"status"=\$2,"updated_at"=\$3 WHERE \(id = \$4 AND status = \$5 AND deleted_at IS NULL\) AND "spaces"\."deleted_at" IS NULL`).
		WithArgs(now, domainspace.StatusArchived, now, "space-1", domainspace.StatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Archive(context.Background(), "space-1", now); err != nil {
		t.Fatalf("archive space: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateMemberRoleScopesActiveMembership(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := &Repository{DB: db}
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "space_members" SET "role_id"=\$1,"updated_at"=\$2 WHERE \(id = \$3 AND space_id = \$4 AND status = \$5 AND deleted_at IS NULL\) AND "space_members"\."deleted_at" IS NULL`).
		WithArgs("role-member", now, "member-2", "space-1", domainspace.StatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateMemberRole(context.Background(), "space-1", "member-2", "role-member", now); err != nil {
		t.Fatalf("update member role: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveMemberSetsInactiveAndDeletedAt(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := &Repository{DB: db}
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "space_members" SET "deleted_at"=\$1,"status"=\$2,"updated_at"=\$3 WHERE \(id = \$4 AND space_id = \$5 AND status = \$6 AND deleted_at IS NULL\) AND "space_members"\."deleted_at" IS NULL`).
		WithArgs(now, domainspace.StatusInactive, now, "member-2", "space-1", domainspace.StatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.RemoveMember(context.Background(), "space-1", "member-2", now); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCountActiveOwnersScopesSpace(t *testing.T) {
	db, mock := newSpaceMockDB(t)
	repo := &Repository{DB: db}

	mock.ExpectQuery(`SELECT count\(\*\) FROM space_members sm JOIN roles r ON r\.id = sm\.role_id WHERE sm\.space_id = \$1 AND r\.name = \$2 AND sm\.status = \$3 AND sm\.deleted_at IS NULL AND r\.deleted_at IS NULL`).
		WithArgs("space-1", "space_owner", domainspace.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	count, err := repo.CountActiveOwners(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("count owners: %v", err)
	}
	if count != 1 {
		t.Fatalf("owner count = %d, want 1", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositorySatisfiesInterface(t *testing.T) {
	var _ interfacespace.RepoSpaceInterface = (*Repository)(nil)
}
