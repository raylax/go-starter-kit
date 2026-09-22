package project

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/apperror"
	"github.com/example/go-starter-kit/pagination"
)

func TestServiceRejectsMissingOwnerBeforeDatabaseAccess(t *testing.T) {
	service := NewService(nil)
	_, createErr := service.Create(t.Context(), "", "project", "")
	_, getErr := service.Get(t.Context(), "", uuid.Must(uuid.NewV7()))
	_, listErr := service.List(t.Context(), "", pagination.Params{Limit: 20})
	_, updateErr := service.Update(t.Context(), "", uuid.Must(uuid.NewV7()), "project", "")
	deleteErr := service.Delete(t.Context(), "", uuid.Must(uuid.NewV7()))
	for _, err := range []error{createErr, getErr, listErr, updateErr, deleteErr} {
		if !errors.Is(err, apperror.ErrUnauthenticated) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	for _, name := range []string{"", " \t\n", strings.Repeat("中", 101), "invalid\x00name"} {
		if _, err := validate(name, ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid name %q", name)
		}
	}
	name, err := validate("  中文项目  ", "description")
	if err != nil || name != "中文项目" {
		t.Fatalf("name=%q error=%v", name, err)
	}
}
