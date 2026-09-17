package repositoryfamilymember

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newFamilyMemberMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func TestFindActiveByHermesProfile_ReturnsResolvedMember(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*fm\.user_id.*fm\.id AS member_id.*fm\.family_id.*fm\.role_id.*r\.name AS role_name.*u\.hermes_profile_id.*FROM family_members fm.*JOIN users u ON u\.id = fm\.user_id.*JOIN families f ON f\.id = fm\.family_id.*JOIN roles r ON r\.id = fm\.role_id.*u\.hermes_profile_id = \$1.*fm\.status = \$2.*f\.status = \$3.*u\.deleted_at IS NULL.*r\.deleted_at IS NULL.*LIMIT \$4`).
		WithArgs("profile-parent-1", "ACTIVE", "ACTIVE", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "member_id", "family_id", "role_id", "role_name", "hermes_profile_id",
		}).AddRow("user-7", "member-11", "family-13", "role-17", "parent", "profile-parent-1"))

	got, err := repo.FindActiveByHermesProfile(context.Background(), "profile-parent-1")
	if err != nil {
		t.Fatalf("find active member: %v", err)
	}
	if got.UserID != "user-7" || got.MemberID != "member-11" || got.FamilyID != "family-13" || got.RoleID != "role-17" || got.RoleName != "parent" || got.HermesProfileID != "profile-parent-1" {
		t.Fatalf("unexpected resolved member: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByHermesProfile_RejectsInactiveMembership(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*FROM family_members fm.*fm\.status = \$2.*LIMIT \$4`).
		WithArgs("profile-inactive", "ACTIVE", "ACTIVE", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "member_id", "family_id", "role_id", "role_name", "hermes_profile_id",
		}))

	_, err := repo.FindActiveByHermesProfile(context.Background(), "profile-inactive")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByID_IsFamilyScoped(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT \* FROM "family_members" WHERE family_id = \$1 AND id = \$2 AND status = \$3.*LIMIT \$4`).
		WithArgs("family-a", "member-b", "ACTIVE", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "family_id", "user_id", "role_id", "status"}))

	_, err := repo.FindActiveByID(context.Background(), "family-a", "member-b")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
