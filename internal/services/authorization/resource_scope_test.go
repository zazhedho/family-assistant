package authorization

import (
	"errors"
	"testing"
)

func TestValidateResourceRequiresSpace(t *testing.T) {
	for _, resource := range []Resource{
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

func TestValidateResourceAcceptsSpaceResourceFields(t *testing.T) {
	err := ValidateResource(Resource{
		SpaceID:           "space-1",
		CreatedByMemberID: "member-1",
		AssigneeMemberID:  "member-2",
	})
	if err != nil {
		t.Fatalf("ValidateResource() error = %v", err)
	}
}
