package response

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestResponseSetsStatusFromCode(t *testing.T) {
	logID := uuid.New()

	success := Response(http.StatusCreated, "created", logID, map[string]string{"id": "1"})
	if !success.Status || success.Id != logID || success.Message != "created" {
		t.Fatalf("unexpected success response: %+v", success)
	}

	failed := Response(http.StatusBadRequest, "bad request", logID, nil)
	if failed.Status {
		t.Fatalf("expected failed status for 400, got %+v", failed)
	}
	if failed.Message != "Bad Request" || failed.Error.(Errors).Message != "bad request" {
		t.Fatalf("unexpected failed response message: %+v", failed)
	}
}

func TestErrorHelpersHideInternalDetails(t *testing.T) {
	logID := uuid.New()

	got := InternalServerError(logID)
	if got.Status {
		t.Fatalf("expected error status, got %+v", got)
	}

	errBody, ok := got.Error.(Errors)
	if !ok {
		t.Fatalf("expected response.Errors, got %#v", got.Error)
	}
	expectedMessage := fmt.Sprintf("Something went wrong. Please contact support with the log ID: %s.", logID)
	if errBody.Code != http.StatusInternalServerError || errBody.Message != expectedMessage {
		t.Fatalf("unexpected error body: %+v", errBody)
	}
	if got.Message != "Something went wrong" {
		t.Fatalf("unexpected internal server error title: %+v", got)
	}

	unauthorized := Unauthorized(logID, "login required")
	if unauthorized.Status || unauthorized.Error.(Errors).Code != http.StatusUnauthorized {
		t.Fatalf("unexpected unauthorized response: %+v", unauthorized)
	}
	if unauthorized.Message != "Unauthorized" || unauthorized.Error.(Errors).Message != "login required" {
		t.Fatalf("unexpected unauthorized message: %+v", unauthorized)
	}
	forbidden := Forbidden(logID, "denied")
	if forbidden.Status || forbidden.Error.(Errors).Code != http.StatusForbidden {
		t.Fatalf("unexpected forbidden response: %+v", forbidden)
	}
	if forbidden.Message != "Forbidden" || forbidden.Error.(Errors).Message != "denied" {
		t.Fatalf("unexpected forbidden message: %+v", forbidden)
	}
}

func TestPaginationResponseCalculatesPageState(t *testing.T) {
	got := PaginationResponse(http.StatusOK, 55, 2, 20, uuid.New(), []string{"item"})

	if got.TotalPages != 3 {
		t.Fatalf("expected 3 total pages, got %d", got.TotalPages)
	}
	if !got.NextPage || !got.PrevPage {
		t.Fatalf("expected next and previous page flags, got %+v", got)
	}
	if got.Limit != 20 || got.TotalData != 55 {
		t.Fatalf("unexpected pagination metadata: %+v", got)
	}
}
