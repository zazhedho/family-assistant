package repositoryonboarding

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainonboarding "family-assistant/internal/domain/onboarding"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	interfaceonboarding "family-assistant/internal/interfaces/onboarding"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newOnboardingMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func validRegistration() domainonboarding.Registration {
	createdAt := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	return domainonboarding.Registration{
		User: domainuser.Users{
			Id:            "user-1",
			Name:          "Hermes User",
			Role:          "viewer",
			LoginProvider: "whatsapp",
			Metadata:      map[string]any{},
			CreatedAt:     createdAt,
		},
		Space: domainspace.Space{
			ID:              "space-1",
			Name:            "Hermes User's Space",
			Type:            domainspace.TypePersonal,
			Category:        domainspace.CategoryPersonal,
			Status:          domainspace.StatusActive,
			CreatedByUserID: "user-1",
			CreatedAt:       createdAt,
		},
		Member: domainspace.Member{
			ID:        "member-1",
			SpaceID:   "space-1",
			UserID:    "user-1",
			RoleID:    "owner-role",
			Status:    domainspace.StatusActive,
			CreatedAt: createdAt,
		},
		Identity: domainidentity.ExternalIdentity{
			ID:         "identity-1",
			UserID:     "user-1",
			Provider:   domainidentity.ProviderHermes,
			ExternalID: "profile-1",
			Status:     domainidentity.StatusActive,
			VerifiedAt: createdAt,
			Metadata:   map[string]any{},
			CreatedAt:  createdAt,
			UpdatedAt:  createdAt,
		},
	}
}

func onboardingUserArgsWithoutCredentials() []driver.Value {
	registration := validRegistration()
	user := registration.User
	return []driver.Value{
		user.Id,
		user.Name,
		user.Role,
		user.RoleId,
		user.EmailVerifiedAt,
		user.PhoneVerifiedAt,
		user.LastLoginAt,
		user.LastLoginIP,
		user.LastLoginUserAgent,
		user.LockedUntil,
		user.LoginProvider,
		user.AvatarURL,
		"{}",
		user.BirthDate,
		user.AgeVerificationMethod,
		user.AgeVerifiedAt,
		user.CreatedAt,
		sqlmock.AnyArg(),
		nil,
	}
}

func onboardingUserInsertQuery() string {
	return `INSERT INTO "users" \("id","name","role","role_id","email_verified_at","phone_verified_at","last_login_at","last_login_ip","last_login_user_agent","locked_until","login_provider","avatar_url","metadata","birth_date","age_verification_method","age_verified_at","created_at","updated_at","deleted_at"\)`
}

func onboardingLookupQuery() string {
	return `SELECT .*FROM external_identities ei.*JOIN spaces s.*s.created_by_user_id = ei.user_id.*s.type = 'PERSONAL'.*s.status = 'ACTIVE'.*s.deleted_at IS NULL.*WHERE ei.provider = \$1.*ei.external_id = \$2.*ei.status = 'ACTIVE'.*ei.deleted_at IS NULL.*LIMIT \$3`
}

func expectCreateBeforeIdentity(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(onboardingUserInsertQuery()).
		WithArgs(onboardingUserArgsWithoutCredentials()...).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "spaces"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "space_members"`).WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestCreateCommitsAllFourRecordsWithNullCredentials(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(onboardingUserInsertQuery()).
		WithArgs(onboardingUserArgsWithoutCredentials()...).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "spaces"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "space_members"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "external_identities"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := repo.Create(context.Background(), validRegistration()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateReturnsIdentityInsertErrorAndRollsBack(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	insertErr := errors.New("insert identity")

	expectCreateBeforeIdentity(mock)
	mock.ExpectExec(`INSERT INTO "external_identities"`).WillReturnError(insertErr)
	mock.ExpectRollback()

	err := repo.Create(context.Background(), validRegistration())
	if !errors.Is(err, insertErr) {
		t.Fatalf("error = %v, want insert identity error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateMapsIdentityConflictAndRollsBack(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)

	expectCreateBeforeIdentity(mock)
	mock.ExpectExec(`INSERT INTO "external_identities"`).WillReturnError(&pgconn.PgError{Code: "23505"})
	mock.ExpectRollback()

	err := repo.Create(context.Background(), validRegistration())
	if !errors.Is(err, domainidentity.ErrIdentityConflict) {
		t.Fatalf("error = %v, want ErrIdentityConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateKeepsNonIdentityDuplicateErrors(t *testing.T) {
	for _, name := range []string{"user", "space", "member"} {
		t.Run(name, func(t *testing.T) {
			db, mock := newOnboardingMockDB(t)
			repo := NewRepository(db)
			duplicateErr := &pgconn.PgError{Code: "23505", ConstraintName: name + "_unique"}

			mock.ExpectBegin()
			switch name {
			case "user":
				mock.ExpectExec(onboardingUserInsertQuery()).
					WithArgs(onboardingUserArgsWithoutCredentials()...).
					WillReturnError(duplicateErr)
			case "space":
				mock.ExpectExec(onboardingUserInsertQuery()).
					WithArgs(onboardingUserArgsWithoutCredentials()...).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`INSERT INTO "spaces"`).WillReturnError(duplicateErr)
			case "member":
				mock.ExpectExec(onboardingUserInsertQuery()).
					WithArgs(onboardingUserArgsWithoutCredentials()...).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`INSERT INTO "spaces"`).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`INSERT INTO "space_members"`).WillReturnError(duplicateErr)
			}
			mock.ExpectRollback()

			err := repo.Create(context.Background(), validRegistration())
			if !errors.Is(err, duplicateErr) {
				t.Fatalf("error = %v, want original duplicate error", err)
			}
			if errors.Is(err, domainidentity.ErrIdentityConflict) {
				t.Fatal("non-identity duplicate was mapped to ErrIdentityConflict")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFindByExternalIdentityNormalizesProviderAndExternalID(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	mock.ExpectQuery(onboardingLookupQuery()).
		WithArgs(domainidentity.ProviderHermes, "profile-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "space_id"}).AddRow("user-1", "space-1"))

	got, err := repo.FindByExternalIdentity(context.Background(), " HERMES ", " profile-1 ")
	if err != nil {
		t.Fatalf("find by external identity: %v", err)
	}
	if got != (domainonboarding.AccountRef{UserID: "user-1", SpaceID: "space-1"}) {
		t.Fatalf("account ref = %#v, want user-1/space-1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByExternalIdentityMapsRecordNotFound(t *testing.T) {
	db, mock := newOnboardingMockDB(t)
	repo := NewRepository(db)
	mock.ExpectQuery(onboardingLookupQuery()).
		WithArgs(domainidentity.ProviderHermes, "profile-1", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.FindByExternalIdentity(context.Background(), " HERMES ", " profile-1 ")
	if !errors.Is(err, domainidentity.ErrIdentityNotFound) {
		t.Fatalf("error = %v, want ErrIdentityNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositorySatisfiesInterface(t *testing.T) {
	var _ interfaceonboarding.RepoOnboardingInterface = (*Repository)(nil)
}
