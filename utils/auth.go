package utils

import "github.com/gin-gonic/gin"

func GetAuthData(ctx *gin.Context) map[string]any {
	jwtClaims, _ := ctx.Get(CtxKeyAuthData)
	if jwtClaims != nil {
		return jwtClaims.(map[string]any)
	}
	return nil
}
