package repositorygeneric

import (
	"fmt"
	"reflect"
	"slices"
	"starter-kit/pkg/filter"
	"starter-kit/utils"
	"strings"

	"gorm.io/gorm"
)

func applyFilters(query *gorm.DB, filters map[string]any, opts QueryOptions) *gorm.DB {
	if len(opts.AllowedFilters) == 0 {
		return query
	}

	sanitizer := opts.FilterSanitizer
	if sanitizer == nil {
		sanitizer = filter.WhitelistFilter
	}

	safeFilters := sanitizer(filters, opts.AllowedFilters)
	for key, value := range safeFilters {
		query = applyFilter(query, key, value)
	}

	return query
}

func applyFilter(query *gorm.DB, key string, value any) *gorm.DB {
	if value == nil {
		return query
	}
	if !isSafeColumnIdentifier(key) {
		return query
	}

	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return query
		}
		return query.Where(fmt.Sprintf("%s = ?", key), v)
	default:
		if isSliceValue(v) {
			return query.Where(fmt.Sprintf("%s IN ?", key), v)
		}
		return query.Where(fmt.Sprintf("%s = ?", key), v)
	}
}

func applyOrdering(query *gorm.DB, params filter.BaseParams, opts QueryOptions) (*gorm.DB, error) {
	if params.OrderBy != "" && params.OrderDirection != "" {
		if !contains(opts.AllowedOrderColumns, params.OrderBy) {
			return nil, fmt.Errorf("invalid orderBy column: %s", params.OrderBy)
		}
		if err := validateColumnIdentifier(params.OrderBy); err != nil {
			return nil, err
		}
		direction, err := normalizeOrderDirection(params.OrderDirection)
		if err != nil {
			return nil, err
		}

		return query.Order(fmt.Sprintf("%s %s", params.OrderBy, direction)), nil
	}

	for _, order := range opts.DefaultOrders {
		safeOrder, err := safeOrderClause(order)
		if err != nil {
			return nil, err
		}
		query = query.Order(safeOrder)
	}

	return query, nil
}

func contains(values []string, target string) bool {
	return slices.Contains(values, target)
}

func isSliceValue(value any) bool {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return false
	}

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return rv.Type().Elem().Kind() != reflect.Uint8
	default:
		return false
	}
}

func zeroValue[T any]() T {
	var zero T
	return zero
}

func validateColumnIdentifiers(columns []string) error {
	for _, column := range columns {
		if err := validateColumnIdentifier(column); err != nil {
			return err
		}
	}

	return nil
}

func validateColumnIdentifier(column string) error {
	if !isSafeColumnIdentifier(column) {
		return fmt.Errorf("invalid column: %s", column)
	}

	return nil
}

func safeColumnIdentifiers(columns []string) []string {
	safeColumns := make([]string, 0, len(columns))
	for _, column := range columns {
		if isSafeColumnIdentifier(column) {
			safeColumns = append(safeColumns, column)
		}
	}

	return safeColumns
}

func isSafeColumnIdentifier(column string) bool {
	column = strings.TrimSpace(column)
	if column == "" {
		return false
	}

	parts := strings.SplitSeq(column, ".")
	for part := range parts {
		if !isSafeColumnSegment(part) {
			return false
		}
	}

	return true
}

func isSafeColumnSegment(segment string) bool {
	if segment == "" {
		return false
	}

	for i := 0; i < len(segment); i++ {
		char := segment[i]
		if i == 0 {
			if !isASCIILetter(char) && char != '_' {
				return false
			}
			continue
		}
		if !isASCIILetter(char) && !isASCIIDigit(char) && char != '_' {
			return false
		}
	}

	return true
}

func isASCIILetter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func isASCIIDigit(char byte) bool {
	return char >= '0' && char <= '9'
}

func normalizeOrderDirection(direction string) (string, error) {
	normalized := utils.NormalizeUpperKey(direction)
	switch normalized {
	case "ASC", "DESC":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid order direction: %s", direction)
	}
}

func safeOrderClause(order string) (string, error) {
	parts := strings.Fields(order)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid default order: %s", order)
	}
	if err := validateColumnIdentifier(parts[0]); err != nil {
		return "", err
	}
	direction, err := normalizeOrderDirection(parts[1])
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s %s", parts[0], direction), nil
}
