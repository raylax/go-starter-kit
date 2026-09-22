package task

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
	input := Details{Title: "task", Status: Todo}
	_, createErr := service.Create(t.Context(), "", input)
	_, getErr := service.Get(t.Context(), "", uuid.Must(uuid.NewV7()))
	_, listErr := service.List(t.Context(), "", "", pagination.Params{Limit: 20})
	_, updateErr := service.Update(t.Context(), "", uuid.Must(uuid.NewV7()), input)
	deleteErr := service.Delete(t.Context(), "", uuid.Must(uuid.NewV7()))
	for _, err := range []error{createErr, getErr, listErr, updateErr, deleteErr} {
		if !errors.Is(err, apperror.ErrUnauthenticated) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestTaskValidation(t *testing.T) {
	for _, input := range []Details{
		{Title: " \t\n", Status: Todo},
		{Title: strings.Repeat("中", 201), Status: Todo},
		{Title: "title", Description: strings.Repeat("中", 2001), Status: Todo},
		{Title: "title\x00", Status: Todo},
		{Title: "title", Description: "\x00", Status: Todo},
		{Title: "title", Status: "archived"},
		{Title: "title"},
	} {
		if _, err := validate(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid input: %+v", input)
		}
	}
	for _, status := range []Status{Todo, InProgress, Done} {
		input, err := validate(Details{Title: "  中文任务  ", Status: status})
		if err != nil || input.Title != "中文任务" || input.Status != status {
			t.Fatalf("input=%+v error=%v", input, err)
		}
	}
	service := NewService(nil)
	if _, err := service.Update(t.Context(), "alice", uuid.Must(uuid.NewV7()), Details{Title: "title"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("update must require an explicit status")
	}
	for _, filter := range []struct {
		status        Status
		limit, offset int32
	}{
		{"invalid", 20, 0}, {"", 0, 0}, {"", 101, 0}, {"", 20, -1}, {"", 20, 10001},
	} {
		if _, err := service.List(t.Context(), "alice", filter.status, pagination.Params{Limit: filter.limit, Offset: filter.offset}); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid list filter accepted")
		}
	}
}
