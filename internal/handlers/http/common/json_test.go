package handlercommon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"starter-kit/utils"

	"github.com/gin-gonic/gin"
)

type bindJSONTestRequest struct {
	Name string `json:"name" binding:"required"`
}

func TestBindJSONReturnsFalseAndWritesBadRequestOnInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	var req bindJSONTestRequest
	if BindJSON(ctx, utils.GenerateLogId(ctx), "[Test][BindJSON]", &req) {
		t.Fatal("expected BindJSON to return false")
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"message":"Bad Request"`) {
		t.Fatalf("expected invalid request response, got %s", rec.Body.String())
	}
}

func TestBindJSONReturnsTrueOnValidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"name":"starter"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	var req bindJSONTestRequest
	if !BindJSON(ctx, utils.GenerateLogId(ctx), "[Test][BindJSON]", &req) {
		t.Fatal("expected BindJSON to return true")
	}

	if req.Name != "starter" {
		t.Fatalf("expected name to be decoded, got %q", req.Name)
	}
}
