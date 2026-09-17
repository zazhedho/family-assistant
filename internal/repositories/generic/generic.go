package repositorygeneric

import (
	"context"
	"errors"
	"fmt"
	"starter-kit/pkg/filter"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SearchFunc func(query *gorm.DB, search string) *gorm.DB
type QueryFunc func(query *gorm.DB) *gorm.DB
type FilterSanitizer func(filters map[string]any, allowed []string) map[string]any

type QueryOptions struct {
	BaseQuery           QueryFunc
	Search              SearchFunc
	AllowedFilters      []string
	FilterSanitizer     FilterSanitizer
	AllowedOrderColumns []string
	DefaultOrders       []string
}

type GenericRepository[T any] struct {
	DB *gorm.DB
}

func New[T any](db *gorm.DB) *GenericRepository[T] {
	return &GenericRepository[T]{DB: db}
}

func (r *GenericRepository[T]) Store(ctx context.Context, m T) error {
	return r.DB.WithContext(ctx).Create(&m).Error
}

func (r *GenericRepository[T]) Upsert(ctx context.Context, values []T, conflictColumns, updateColumns []string) error {
	if len(values) == 0 {
		return nil
	}
	if len(conflictColumns) == 0 {
		return errors.New("conflict columns are required")
	}
	if len(updateColumns) == 0 {
		return errors.New("update columns are required")
	}
	if err := validateColumnIdentifiers(conflictColumns); err != nil {
		return err
	}
	if err := validateColumnIdentifiers(updateColumns); err != nil {
		return err
	}

	columns := make([]clause.Column, 0, len(conflictColumns))
	for _, column := range conflictColumns {
		columns = append(columns, clause.Column{Name: column})
	}

	return r.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   columns,
		DoUpdates: clause.AssignmentColumns(updateColumns),
	}).Create(&values).Error
}

func (r *GenericRepository[T]) GetByID(ctx context.Context, id string) (ret T, err error) {
	err = r.DB.WithContext(ctx).Where("id = ?", id).First(&ret).Error
	if err != nil {
		return zeroValue[T](), err
	}

	return ret, nil
}

func (r *GenericRepository[T]) GetOneByField(ctx context.Context, field string, value any) (ret T, err error) {
	if err = validateColumnIdentifier(field); err != nil {
		return zeroValue[T](), err
	}

	err = r.DB.WithContext(ctx).Where(fmt.Sprintf("%s = ?", field), value).First(&ret).Error
	if err != nil {
		return zeroValue[T](), err
	}

	return ret, nil
}

func (r *GenericRepository[T]) GetManyByField(ctx context.Context, field string, value any) (ret []T, err error) {
	if err = validateColumnIdentifier(field); err != nil {
		return nil, err
	}

	err = r.DB.WithContext(ctx).Where(fmt.Sprintf("%s = ?", field), value).Find(&ret).Error
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *GenericRepository[T]) ExistsByField(ctx context.Context, field string, value any) (exists bool, err error) {
	if err = validateColumnIdentifier(field); err != nil {
		return false, err
	}

	var count int64
	err = r.DB.WithContext(ctx).Model(new(T)).Where(fmt.Sprintf("%s = ?", field), value).Count(&count).Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *GenericRepository[T]) ExistsByFields(ctx context.Context, filters map[string]any) (exists bool, err error) {
	query := r.DB.WithContext(ctx).Model(new(T))
	for key, value := range filters {
		if err = validateColumnIdentifier(key); err != nil {
			return false, err
		}
		query = query.Where(fmt.Sprintf("%s = ?", key), value)
	}

	var count int64
	err = query.Count(&count).Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *GenericRepository[T]) GetAll(ctx context.Context, params filter.BaseParams, opts QueryOptions) (ret []T, totalData int64, err error) {
	query := r.DB.WithContext(ctx).Model(new(T))
	if opts.BaseQuery != nil {
		query = opts.BaseQuery(query)
	}

	if params.Search != "" && opts.Search != nil {
		query = opts.Search(query, params.Search)
	}

	query = applyFilters(query, params.Filters, opts)

	if err = query.Count(&totalData).Error; err != nil {
		return nil, 0, err
	}

	query, err = applyOrdering(query, params, opts)
	if err != nil {
		return nil, 0, err
	}

	if err = query.Offset(params.Offset).Limit(params.Limit).Find(&ret).Error; err != nil {
		return nil, 0, err
	}

	return ret, totalData, nil
}

func (r *GenericRepository[T]) Update(ctx context.Context, m T) error {
	return r.DB.WithContext(ctx).Save(&m).Error
}

func (r *GenericRepository[T]) Delete(ctx context.Context, id string) error {
	return r.DB.WithContext(ctx).Where("id = ?", id).Delete(new(T)).Error
}

func (r *GenericRepository[T]) SoftDelete(ctx context.Context, id, deletedBy string) error {
	return r.DB.WithContext(ctx).Model(new(T)).Where("id = ?", id).Updates(map[string]any{
		"deleted_by": deletedBy,
		"deleted_at": time.Now(),
	}).Error
}

func BuildSearchFunc(columns ...string) SearchFunc {
	safeColumns := safeColumnIdentifiers(columns)

	return func(query *gorm.DB, search string) *gorm.DB {
		if len(safeColumns) == 0 {
			return query
		}

		searchPattern := "%" + search + "%"
		parts := make([]string, 0, len(safeColumns))
		args := make([]any, 0, len(safeColumns))

		for _, column := range safeColumns {
			parts = append(parts, fmt.Sprintf("LOWER(%s) LIKE LOWER(?)", column))
			args = append(args, searchPattern)
		}

		return query.Where(strings.Join(parts, " OR "), args...)
	}
}
