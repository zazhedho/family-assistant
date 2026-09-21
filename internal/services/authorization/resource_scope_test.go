package authorization

import (
	"errors"
	"testing"

	domainauthorization "family-assistant/internal/domain/authorization"
)

func TestValidateResourceRequiresSpace(t *testing.T) {
	for _, resource := range []domainauthorization.Resource{
		{},
		{SpaceID: " \t"},
	} {
		err := ValidateResource(resource)
		if err == nil {
			t.Fatalf("ValidateResource(%+v) error = nil", resource)
		}
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "space_id" {
			t.Fatalf("ValidateResource(%+v) error = %T %v, want space_id ValidationError", resource, err, err)
		}
	}
}

func TestValidateResourceAcceptsSpaceResource(t *testing.T) {
	err := ValidateResource(domainauthorization.Resource{
		SpaceID: "space-1",
	})
	if err != nil {
		t.Fatalf("ValidateResource() error = %v", err)
	}
}
