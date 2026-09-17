package interfacegeneric

import (
	"context"
	"starter-kit/pkg/filter"
)

type GenericRepository[T any] interface {
	Store(ctx context.Context, data T) error
	GetByID(ctx context.Context, id string) (T, error)
	GetAll(ctx context.Context, params filter.BaseParams) ([]T, int64, error)
	Update(ctx context.Context, data T) error
	Delete(ctx context.Context, id string) error
	SoftDelete(ctx context.Context, id, deletedBy string) error
}
