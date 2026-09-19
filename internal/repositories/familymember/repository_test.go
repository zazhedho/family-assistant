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
		WithArgs("profile-parent-1", "ACTIVE", "ACTIVE", 2).
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

func TestFindActiveByHermesProfile_RejectsAmbiguousMemberships(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*FROM family_members fm.*u\.hermes_profile_id = \$1.*fm\.status = \$2.*f\.status = \$3.*LIMIT \$4`).
		WithArgs("profile-duplicate", "ACTIVE", "ACTIVE", 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "member_id", "family_id", "role_id", "role_name", "hermes_profile_id",
		}).AddRow("user-1", "member-1", "family-1", "role-1", "parent", "profile-duplicate").
			AddRow("user-1", "member-2", "family-2", "role-2", "parent", "profile-duplicate"))

	_, err := repo.FindActiveByHermesProfile(context.Background(), "profile-duplicate")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ambiguous lookup to be not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByHermesProfile_RejectsInactiveMembership(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*FROM family_members fm.*fm\.status = \$2.*LIMIT \$4`).
		WithArgs("profile-inactive", "ACTIVE", "ACTIVE", 2).
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

func TestFindActiveByUserID_ReturnsResolvedMember(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*fm\.user_id.*fm\.id AS member_id.*fm\.family_id.*fm\.role_id.*COALESCE\(u\.hermes_profile_id, ''\) AS hermes_profile_id.*FROM family_members fm.*JOIN users u ON u\.id = fm\.user_id.*JOIN families f ON f\.id = fm\.family_id.*JOIN roles r ON r\.id = fm\.role_id.*u\.id = \$1.*fm\.status = \$2.*f\.status = \$3.*u\.deleted_at IS NULL.*r\.deleted_at IS NULL.*LIMIT \$4`).
		WithArgs("user-7", "ACTIVE", "ACTIVE", 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "member_id", "family_id", "role_id", "role_name", "hermes_profile_id",
		}).AddRow("user-7", "member-11", "family-13", "role-17", "parent", "profile-parent-1"))

	got, err := repo.FindActiveByUserID(context.Background(), "user-7")
	if err != nil {
		t.Fatalf("find active member by user: %v", err)
	}
	if got.UserID != "user-7" || got.MemberID != "member-11" || got.FamilyID != "family-13" || got.RoleID != "role-17" || got.RoleName != "parent" || got.HermesProfileID != "profile-parent-1" {
		t.Fatalf("unexpected resolved member: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByUserID_RejectsMissingActiveMembership(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*FROM family_members fm.*u\.id = \$1.*fm\.status = \$2.*f\.status = \$3.*LIMIT \$4`).
		WithArgs("user-inactive", "ACTIVE", "ACTIVE", 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "member_id", "family_id", "role_id", "role_name", "hermes_profile_id",
		}))

	_, err := repo.FindActiveByUserID(context.Background(), "user-inactive")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByUserID_AllowsNullHermesProfile(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*COALESCE\(u\.hermes_profile_id, ''\) AS hermes_profile_id.*FROM family_members fm.*u\.id = \$1.*LIMIT \$4`).
		WithArgs("user-http", "ACTIVE", "ACTIVE", 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "member_id", "family_id", "role_id", "role_name", "hermes_profile_id",
		}).AddRow("user-http", "member-http", "family-http", "role-http", "parent", nil))

	got, err := repo.FindActiveByUserID(context.Background(), "user-http")
	if err != nil {
		t.Fatalf("find active HTTP member: %v", err)
	}
	if got.UserID != "user-http" || got.HermesProfileID != "" {
		t.Fatalf("unexpected NULL-profile member: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByUserID_RejectsAmbiguousMemberships(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*FROM family_members fm.*u\.id = \$1.*fm\.status = \$2.*f\.status = \$3.*LIMIT \$4`).
		WithArgs("user-duplicate", "ACTIVE", "ACTIVE", 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "member_id", "family_id", "role_id", "role_name", "hermes_profile_id",
		}).AddRow("user-duplicate", "member-1", "family-1", "role-1", "parent", "").
			AddRow("user-duplicate", "member-2", "family-2", "role-2", "parent", ""))

	_, err := repo.FindActiveByUserID(context.Background(), "user-duplicate")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ambiguous lookup to be not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByID_IsFamilyScoped(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*fm\.id.*r\.name AS role_name.*FROM family_members fm.*JOIN roles r ON r\.id = fm\.role_id.*fm\.family_id = \$1 AND fm\.id = \$2 AND fm\.status = \$3.*r\.deleted_at IS NULL.*LIMIT \$4`).
		WithArgs("family-a", "member-b", "ACTIVE", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "family_id", "user_id", "role_id", "status", "role_name"}))

	_, err := repo.FindActiveByID(context.Background(), "family-a", "member-b")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByIDRejectsInactiveMembershipOrDeletedRole(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*fm\.id.*r\.name AS role_name.*FROM family_members fm.*JOIN roles r ON r\.id = fm\.role_id.*fm\.family_id = \$1 AND fm\.id = \$2 AND fm\.status = \$3.*r\.deleted_at IS NULL.*LIMIT \$4`).
		WithArgs("family-1", "member-inactive", "ACTIVE", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "family_id", "user_id", "role_id", "status", "role_name"}))

	_, err := repo.FindActiveByID(context.Background(), "family-1", "member-inactive")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected inactive/deleted-role lookup to be not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveByIDReturnsTrustedRoleName(t *testing.T) {
	db, mock := newFamilyMemberMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .*fm\.id.*r\.name AS role_name.*FROM family_members fm.*JOIN roles r ON r\.id = fm\.role_id.*fm\.family_id = \$1 AND fm\.id = \$2 AND fm\.status = \$3.*r\.deleted_at IS NULL.*LIMIT \$4`).
		WithArgs("family-1", "member-1", "ACTIVE", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "family_id", "user_id", "role_id", "status", "role_name"}).
			AddRow("member-1", "family-1", "user-1", "role-1", "ACTIVE", "child"))

	got, err := repo.FindActiveByID(context.Background(), "family-1", "member-1")
	if err != nil {
		t.Fatalf("find active member: %v", err)
	}
	if got.RoleName != "child" {
		t.Fatalf("role name = %q, want child", got.RoleName)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
