package repositoryfinance

import (
	"context"
	"errors"
	"strings"
	"time"

	interfacefinance "family-assistant/internal/interfaces/finance"

	"gorm.io/gorm"
)

var (
	ErrSpaceRequired = errors.New("finance: space id is required")
	ErrIDRequired    = errors.New("finance: id is required")
)

// Repository stores finance data. Every query filters by space_id, and the
// models' gorm.DeletedAt column adds deleted_at IS NULL to every read and
// update, so soft-deleted rows are invisible.
type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) interfacefinance.RepoFinanceInterface {
	return &Repository{DB: db}
}

func requireSpace(spaceID string) error {
	if strings.TrimSpace(spaceID) == "" {
		return ErrSpaceRequired
	}
	return nil
}

func requireSpaceAndID(spaceID, id string) error {
	if err := requireSpace(spaceID); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return ErrIDRequired
	}
	return nil
}

// updateScoped applies an update to the live row (model, id) of the Space and
// reports gorm.ErrRecordNotFound when none matched.
func updateScoped(db *gorm.DB, model any, spaceID, id string, updates map[string]any) error {
	result := db.Model(model).Where("id = ? AND space_id = ?", id, spaceID).Updates(updates)
	return checkAffected(result)
}

// stamp sets the single timestamp used by archive and soft delete.
func stamp(column string, at time.Time) map[string]any {
	return map[string]any{column: at, "updated_at": at}
}

func checkAffected(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) db(ctx context.Context) *gorm.DB { return r.DB.WithContext(ctx) }

var _ interfacefinance.RepoFinanceInterface = (*Repository)(nil)
