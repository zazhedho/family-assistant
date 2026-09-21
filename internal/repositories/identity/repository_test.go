package repositoryidentity

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	interfaceidentity "family-assistant/internal/interfaces/identity"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newIdentityMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func linkTokenFixture() *domainidentity.LinkToken {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	return &domainidentity.LinkToken{
		ID:        "token-1",
		UserID:    "user-1",
		Provider:  domainidentity.ProviderHermes,
		TokenHash: "hash-1",
		ExpiresAt: now.Add(10 * time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func linkTokenRows(token *domainidentity.LinkToken) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "user_id", "provider", "token_hash", "expires_at", "consumed_at", "created_at", "updated_at", "deleted_at",
	}).AddRow(token.ID, token.UserID, token.Provider, token.TokenHash, token.ExpiresAt, token.ConsumedAt, token.CreatedAt, token.UpdatedAt, nil)
}

func expectConsumeAndLink(t *testing.T, mock sqlmock.Sqlmock, token *domainidentity.LinkToken, externalID string, now time.Time) {
	t.Helper()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "identity_link_tokens".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(token.TokenHash, 1).WillReturnRows(linkTokenRows(token))
	mock.ExpectExec(`INSERT INTO "external_identities"`).
		WithArgs(sqlmock.AnyArg(), token.UserID, domainidentity.ProviderHermes, externalID, domainidentity.StatusActive, now, sqlmock.AnyArg(), now, now, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "identity_link_tokens" SET .*consumed_at.*updated_at.*WHERE .*"id" = \$3`).
		WithArgs(now, now, token.ID).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}

func TestCreateLinkTokenPersistsExactFields(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	token := linkTokenFixture()

	mock.ExpectExec(`INSERT INTO "identity_link_tokens"`).
		WithArgs(token.ID, token.UserID, token.Provider, token.TokenHash, token.ExpiresAt, nil, token.CreatedAt, token.UpdatedAt, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.CreateLinkToken(context.Background(), token); err != nil {
		t.Fatalf("create link token: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestConsumeAndLinkLocksCodeAndCreatesIdentityAtomically(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	token := linkTokenFixture()
	now := time.Date(2026, 9, 20, 10, 5, 0, 0, time.UTC)

	expectConsumeAndLink(t, mock, token, "profile-a", now)

	identity, err := repo.ConsumeAndLink(context.Background(), token.TokenHash, " HERMES ", "profile-a", now)
	if err != nil {
		t.Fatalf("consume and link: %v", err)
	}
	if identity == nil || identity.UserID != token.UserID || identity.Provider != domainidentity.ProviderHermes || identity.ExternalID != "profile-a" || identity.Status != domainidentity.StatusActive {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	if identity.VerifiedAt != now {
		t.Fatalf("verified_at = %s, want %s", identity.VerifiedAt, now)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestConsumeAndLinkRollsBackWhenIdentityInsertFails(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	token := linkTokenFixture()
	now := time.Date(2026, 9, 20, 10, 5, 0, 0, time.UTC)
	insertErr := errors.New("insert identity")

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "identity_link_tokens".*FOR UPDATE`).WithArgs(token.TokenHash, 1).WillReturnRows(linkTokenRows(token))
	mock.ExpectExec(`INSERT INTO "external_identities"`).
		WithArgs(sqlmock.AnyArg(), token.UserID, domainidentity.ProviderHermes, "profile-a", domainidentity.StatusActive, now, sqlmock.AnyArg(), now, now, nil).
		WillReturnError(insertErr)
	mock.ExpectRollback()

	_, err := repo.ConsumeAndLink(context.Background(), token.TokenHash, domainidentity.ProviderHermes, "profile-a", now)
	if !errors.Is(err, insertErr) {
		t.Fatalf("error = %v, want insert error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestConsumeAndLinkRollsBackWhenTokenConsumptionFails(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	token := linkTokenFixture()
	now := time.Date(2026, 9, 20, 10, 5, 0, 0, time.UTC)
	updateErr := errors.New("consume token")

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "identity_link_tokens".*FOR UPDATE`).WithArgs(token.TokenHash, 1).WillReturnRows(linkTokenRows(token))
	mock.ExpectExec(`INSERT INTO "external_identities"`).
		WithArgs(sqlmock.AnyArg(), token.UserID, domainidentity.ProviderHermes, "profile-a", domainidentity.StatusActive, now, sqlmock.AnyArg(), now, now, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "identity_link_tokens" SET .*consumed_at.*updated_at.*WHERE .*"id" = \$3`).
		WithArgs(now, now, token.ID).WillReturnError(updateErr)
	mock.ExpectRollback()

	_, err := repo.ConsumeAndLink(context.Background(), token.TokenHash, domainidentity.ProviderHermes, "profile-a", now)
	if !errors.Is(err, updateErr) {
		t.Fatalf("error = %v, want consume error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestConsumeAndLinkRejectsReplayExpiryAndGuessWithSameError(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 5, 0, 0, time.UTC)
	for _, tt := range []struct {
		name  string
		setup func(*domainidentity.LinkToken)
		rows  bool
	}{
		{name: "replayed", setup: func(token *domainidentity.LinkToken) { consumed := now.Add(-time.Minute); token.ConsumedAt = &consumed }, rows: true},
		{name: "expired", setup: func(token *domainidentity.LinkToken) { token.ExpiresAt = now.Add(-time.Minute) }, rows: true},
		{name: "guessed", setup: func(*domainidentity.LinkToken) {}, rows: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newIdentityMockDB(t)
			repo := NewRepository(db)
			token := linkTokenFixture()
			tt.setup(token)
			mock.ExpectBegin()
			query := mock.ExpectQuery(`SELECT .*FROM "identity_link_tokens".*token_hash = \$1.*deleted_at IS NULL.*FOR UPDATE`).WithArgs(token.TokenHash, 1)
			if tt.rows {
				query.WillReturnRows(linkTokenRows(token))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{
					"id", "user_id", "provider", "token_hash", "expires_at", "consumed_at", "created_at", "updated_at", "deleted_at",
				}))
			}
			mock.ExpectRollback()

			_, err := repo.ConsumeAndLink(context.Background(), token.TokenHash, domainidentity.ProviderHermes, "profile-a", now)
			if !errors.Is(err, domainidentity.ErrInvalidLinkToken) {
				t.Fatalf("error = %v, want ErrInvalidLinkToken", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
	}
}

func TestConsumeAndLinkRollsBackWhenActiveIdentityConflicts(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	token := linkTokenFixture()
	now := time.Date(2026, 9, 20, 10, 5, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "identity_link_tokens".*FOR UPDATE`).WithArgs(token.TokenHash, 1).WillReturnRows(linkTokenRows(token))
	mock.ExpectExec(`INSERT INTO "external_identities"`).WillReturnError(gorm.ErrDuplicatedKey)
	mock.ExpectRollback()

	_, err := repo.ConsumeAndLink(context.Background(), token.TokenHash, domainidentity.ProviderHermes, "profile-a", now)
	if !errors.Is(err, domainidentity.ErrIdentityConflict) {
		t.Fatalf("error = %v, want ErrIdentityConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestFindActiveUsesNormalizedProviderAndExternalID(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	mock.ExpectQuery(`SELECT .*FROM "external_identities".*provider = \$1.*external_id = \$2.*status = \$3.*deleted_at IS NULL`).
		WithArgs(domainidentity.ProviderHermes, "profile-a", domainidentity.StatusActive, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "provider", "external_id", "status", "verified_at", "metadata", "created_at", "updated_at", "deleted_at"}).
			AddRow("identity-1", "user-1", domainidentity.ProviderHermes, "profile-a", domainidentity.StatusActive, time.Now().UTC(), []byte(`{}`), time.Now().UTC(), time.Now().UTC(), nil))

	identity, err := repo.FindActive(context.Background(), " HERMES ", " profile-a ")
	if err != nil {
		t.Fatalf("find active: %v", err)
	}
	if identity == nil || identity.ID != "identity-1" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSameUserCanLinkTwoDistinctExternalIDs(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 9, 20, 10, 5, 0, 0, time.UTC)
	first := linkTokenFixture()
	second := linkTokenFixture()
	second.ID, second.TokenHash = "token-2", "hash-2"

	expectConsumeAndLink(t, mock, first, "profile-a", now)
	expectConsumeAndLink(t, mock, second, "profile-b", now)

	firstIdentity, err := repo.ConsumeAndLink(context.Background(), first.TokenHash, first.Provider, "profile-a", now)
	if err != nil {
		t.Fatalf("link first identity: %v", err)
	}
	secondIdentity, err := repo.ConsumeAndLink(context.Background(), second.TokenHash, second.Provider, "profile-b", now)
	if err != nil {
		t.Fatalf("link second identity: %v", err)
	}
	if firstIdentity.UserID != secondIdentity.UserID || firstIdentity.ExternalID == secondIdentity.ExternalID {
		t.Fatalf("unexpected same-user identities: %#v %#v", firstIdentity, secondIdentity)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRevokeChangesOnlyMatchingActiveIdentityStatus(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	mock.ExpectExec(`UPDATE "external_identities" SET .*status.*WHERE .*user_id = \$3.*provider = \$4.*external_id = \$5.*status = \$6.*deleted_at IS NULL`).
		WithArgs(domainidentity.StatusRevoked, sqlmock.AnyArg(), "user-1", domainidentity.ProviderHermes, "profile-a", domainidentity.StatusActive).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.Revoke(context.Background(), "user-1", " HERMES ", " profile-a "); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRevokeReturnsNotFoundWithoutDeleting(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	mock.ExpectExec(`UPDATE "external_identities" SET .*status.*WHERE .*user_id = \$3.*provider = \$4.*external_id = \$5.*status = \$6.*deleted_at IS NULL`).
		WithArgs(domainidentity.StatusRevoked, sqlmock.AnyArg(), "user-1", domainidentity.ProviderHermes, "profile-a", domainidentity.StatusActive).
		WillReturnResult(sqlmock.NewResult(1, 0))

	if err := repo.Revoke(context.Background(), "user-1", domainidentity.ProviderHermes, "profile-a"); !errors.Is(err, domainidentity.ErrIdentityNotFound) {
		t.Fatalf("error = %v, want ErrIdentityNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRevokedIdentityCanBeRelinked(t *testing.T) {
	db, mock := newIdentityMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 9, 20, 10, 5, 0, 0, time.UTC)
	token := linkTokenFixture()
	relinkToken := linkTokenFixture()
	relinkToken.ID, relinkToken.TokenHash = "token-2", "hash-2"

	mock.ExpectExec(`UPDATE "external_identities" SET .*status.*WHERE .*user_id = \$3.*provider = \$4.*external_id = \$5.*status = \$6.*deleted_at IS NULL`).
		WithArgs(domainidentity.StatusRevoked, sqlmock.AnyArg(), token.UserID, domainidentity.ProviderHermes, "profile-a", domainidentity.StatusActive).
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectConsumeAndLink(t, mock, relinkToken, "profile-a", now)

	if err := repo.Revoke(context.Background(), token.UserID, token.Provider, "profile-a"); err != nil {
		t.Fatalf("revoke identity: %v", err)
	}
	relinked, err := repo.ConsumeAndLink(context.Background(), relinkToken.TokenHash, relinkToken.Provider, "profile-a", now)
	if err != nil {
		t.Fatalf("relink identity: %v", err)
	}
	if relinked == nil || relinked.Status != domainidentity.StatusActive || relinked.ExternalID != "profile-a" {
		t.Fatalf("unexpected relinked identity: %#v", relinked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositorySatisfiesInterface(t *testing.T) {
	var _ interfaceidentity.RepoIdentityInterface = (*Repository)(nil)
}
