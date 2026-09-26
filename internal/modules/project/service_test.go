package project

import (
	"errors"
	"strings"
	"testing"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
)

func TestServiceRejectsMissingOwnerBeforeDatabaseAccess(t *testing.T) {
	service := NewService(nil)
	_, createErr := service.Create(t.Context(), "", "project", "")
	_, getErr := service.Get(t.Context(), "", identity.NewID())
	_, listErr := service.List(t.Context(), "", pagination.Params{Limit: 20})
	_, updateErr := service.Update(t.Context(), "", identity.NewID(), "project", "")
	deleteErr := service.Delete(t.Context(), "", identity.NewID())
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

func TestResourceIdentifierValidationBeforeDatabaseAccess(t *testing.T) {
	service := NewService(nil)
	for _, id := range []string{"", " ", "bad\x00id", strings.Repeat("x", 256), strings.Repeat("中", 86)} {
		_, getErr := service.Get(t.Context(), "alice", id)
		_, updateErr := service.Update(t.Context(), "alice", id, "project", "")
		deleteErr := service.Delete(t.Context(), "alice", id)
		for _, err := range []error{getErr, updateErr, deleteErr} {
			if !errors.Is(err, ErrInvalid) {
				t.Fatal("无效标识没有在数据库访问之前被拒绝")
			}
		}
	}
}
