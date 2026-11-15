package repositoryonboarding

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainonboarding "family-assistant/internal/domain/onboarding"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

func renameLookupQuery() string {
	return `SELECT .*u.id::text AS id.*u.name.*FROM external_identities ei.*JOIN users u ON u.id = ei.user_id.*WHERE ei.provider = \$1.*ei.external_id = \$2.*ei.status = 'ACTIVE'.*ei.deleted_at IS NULL.*u.deleted_at IS NULL.*LIMIT \$3`
}

func TestFindAccountForRenameNormalizesAndReturnsUser(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	mock.ExpectQuery(renameLookupQuery()).
		WithArgs(domainidentity.ProviderHermes, "profile-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("user-1", "Onty Zeia"))

	got, err := repo.FindAccountForRename(context.Background(), " HERMES ", " profile-1 ")
	if err != nil || got != (domainonboarding.UserRef{ID: "user-1", Name: "Onty Zeia"}) {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindAccountForRenameMapsNotFound(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	mock.ExpectQuery(renameLookupQuery()).
		WithArgs(domainidentity.ProviderHermes, "profile-1", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	if _, err := repo.FindAccountForRename(context.Background(), "hermes", "profile-1"); !errors.Is(err, domainidentity.ErrIdentityNotFound) {
		t.Fatalf("err = %v, want ErrIdentityNotFound", err)
	}
}

func TestFindAccountForRenameBlankIdentityIsNotFoundWithoutQuery(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	if _, err := repo.FindAccountForRename(context.Background(), "hermes", " "); !errors.Is(err, domainidentity.ErrIdentityNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func otherMemberNamesQuery() string {
	return `SELECT DISTINCT u.name.*FROM space_members mine.*JOIN space_members theirs ON theirs.space_id = mine.space_id.*JOIN users u ON u.id = theirs.user_id.*JOIN spaces s ON s.id = mine.space_id.*mine.user_id = \$1.*mine.status = 'ACTIVE'.*mine.deleted_at IS NULL.*theirs.user_id <> mine.user_id.*theirs.status = 'ACTIVE'.*theirs.deleted_at IS NULL.*u.deleted_at IS NULL.*s.deleted_at IS NULL`
}

func TestOtherMemberNamesReturnsNames(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	mock.ExpectQuery(otherMemberNamesQuery()).
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("Daddy Zeia").AddRow("Mommy Zeia"))

	names, err := repo.OtherMemberNames(context.Background(), " user-1 ")
	if err != nil || len(names) != 2 || names[0] != "Daddy Zeia" || names[1] != "Mommy Zeia" {
		t.Fatalf("names = %v, err = %v", names, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOtherMemberNamesPropagatesErrorAndBlankUserFails(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	mock.ExpectQuery(otherMemberNamesQuery()).WithArgs("user-1").WillReturnError(errors.New("db down"))
	if _, err := repo.OtherMemberNames(context.Background(), "user-1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := repo.OtherMemberNames(context.Background(), " "); err == nil {
		t.Fatal("blank user id must error, not return no names")
	}
}

func updateUserNameQuery() string {
	return `UPDATE "users" SET "name"=\$1,"updated_at"=\$2 WHERE id = \$3 AND deleted_at IS NULL`
}

func TestUpdateUserNameSetsOnlyNameAndUpdatedAt(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	mock.ExpectExec(updateUserNameQuery()).
		WithArgs("Rheina Putri", now, "user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateUserName(context.Background(), "user-1", "Rheina Putri", now); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateUserNameRequiresExactlyOneRow(t *testing.T) {
	for _, rows := range []int64{0, 2} {
		db, mock := newOnboardingMockDB(t)
		repo := NewRepository(db)
		mock.ExpectExec(updateUserNameQuery()).WillReturnResult(sqlmock.NewResult(0, rows))
		if err := repo.UpdateUserName(context.Background(), "user-1", "Rheina Putri", time.Now()); err == nil {
			t.Fatalf("rows affected %d: expected error", rows)
		}
	}
}

func TestUpdateUserNameRejectsBlankInputsWithoutQuery(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	if err := repo.UpdateUserName(context.Background(), " ", "Rheina Putri", time.Now()); err == nil {
		t.Fatal("blank user id must error")
	}
	if err := repo.UpdateUserName(context.Background(), "user-1", " ", time.Now()); err == nil {
		t.Fatal("blank name must error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateUserNamePropagatesDatabaseError(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	mock.ExpectExec(updateUserNameQuery()).WillReturnError(errors.New("db down"))
	if err := repo.UpdateUserName(context.Background(), "user-1", "Rheina Putri", time.Now()); err == nil {
		t.Fatal("expected error")
	}
}
