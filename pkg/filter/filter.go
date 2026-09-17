package filter

import (
	"encoding/json"
	"fmt"
	"starter-kit/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

type BaseParams struct {
	Search         string         `json:"search" form:"search"`
	Filters        map[string]any `json:"filters" form:"filters"`
	OrderBy        string         `json:"order_by" form:"order_by"`
	OrderDirection string         `json:"order_direction" form:"order_direction"`
	Page           int            `json:"page" form:"page"`
	Limit          int            `json:"limit" form:"limit"`
	Offset         int            `json:"offset" form:"offset"`
	Columns        []string       `json:"columns" form:"columns"`
}

func GetBaseParams(ctx *gin.Context, defOrderBy, defOrderDirection string, defLimit int) (req BaseParams, err error) {
	err = ctx.Bind(&req)
	if err != nil {
		return
	}

	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit == -1 {
		req.Page = 1
		req.Offset = 0
	} else {
		if req.Limit < 1 || req.Limit > 10000 {
			req.Limit = defLimit
		}
		req.Offset = (req.Page - 1) * req.Limit
	}
	if req.OrderBy == "" {
		req.OrderBy = defOrderBy
	}
	validDirs := map[string]bool{"asc": true, "desc": true}
	if !validDirs[utils.NormalizeKey(req.OrderDirection)] {
		req.OrderDirection = defOrderDirection
	}

	if req.Filters == nil {
		req.Filters = make(map[string]any)
	}
	if filters, ok := ctx.GetQueryMap("filters"); ok {
		for k, v := range filters {
			var jsonVal any
			if err := json.Unmarshal([]byte(v), &jsonVal); err == nil {
				req.Filters[k] = jsonVal
			} else {
				req.Filters[k] = v
			}
		}
	}
	return
}

func whitelistTransform(
	filters map[string]any,
	allowed []string,
	transform func(any) any,
) map[string]any {
	if filters == nil {
		return nil
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, k := range allowed {
		allowedSet[k] = struct{}{}
	}

	out := make(map[string]any, len(allowedSet))
	for k, v := range filters {
		if _, ok := allowedSet[k]; ok {
			if transform != nil {
				out[k] = transform(v)
			} else {
				out[k] = v
			}
		}
	}
	return out
}

func WhitelistFilter(filters map[string]any, allowed []string) map[string]any {
	return whitelistTransform(filters, allowed, nil)
}

func WhitelistStringFilter(filters map[string]any, allowed []string) map[string]any {
	return whitelistTransform(filters, allowed, func(v any) any {
		switch t := v.(type) {
		case nil:
			return ""
		case string:
			return t
		case fmt.Stringer:
			return t.String()
		case []string:
			return strings.Join(t, ",")
		default:
			return fmt.Sprintf("%v", v)
		}
	})
}
