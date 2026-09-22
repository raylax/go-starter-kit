package task

import (
	"context"
	"net/http"

	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
)

func Routes() []httpapi.Route[*Service] {
	op := httpapi.ProtectedOperations("Tasks")
	create := op("create-task", http.MethodPost, "/v1/tasks", "Create a task")
	return []httpapi.Route[*Service]{
		httpapi.CreatedEndpoint(create, createTask, toDTO, func(item Task) string { return "/v1/tasks/" + item.ID.String() }),
		httpapi.ItemEndpoint(op("get-task", http.MethodGet, "/v1/tasks/{id}", "Get an owned task"), getTask, toDTO),
		httpapi.PageEndpoint[TaskListResponse](op("list-tasks", http.MethodGet, "/v1/tasks", "List owned tasks, optionally filtered by status"), listTasks, toDTO),
		httpapi.ItemEndpoint(op("update-task", http.MethodPut, "/v1/tasks/{id}", "Replace an owned task"), updateTask, toDTO),
		httpapi.NoContentEndpoint(op("delete-task", http.MethodDelete, "/v1/tasks/{id}", "Delete an owned task"), deleteTask),
	}
}

func createTask(service *Service, ctx context.Context, input *TaskCreateInput) (Record, error) {
	return service.Create(ctx, identity.Subject(ctx), Details{Title: input.Body.Title, Description: input.Body.Description, Status: input.Body.Status})
}

func getTask(service *Service, ctx context.Context, input *TaskIDInput) (Record, error) {
	return service.Get(ctx, identity.Subject(ctx), input.ID)
}

func listTasks(service *Service, ctx context.Context, input *TaskListInput) (pagination.Result[Record], error) {
	return service.List(ctx, identity.Subject(ctx), input.Status, input.PageQuery.Params())
}

func updateTask(service *Service, ctx context.Context, input *TaskUpdateInput) (Record, error) {
	return service.Update(ctx, identity.Subject(ctx), input.ID, Details{Title: input.Body.Title, Description: input.Body.Description, Status: input.Body.Status})
}

func deleteTask(service *Service, ctx context.Context, input *TaskIDInput) error {
	return service.Delete(ctx, identity.Subject(ctx), input.ID)
}
