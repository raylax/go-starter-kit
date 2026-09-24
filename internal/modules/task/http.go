package task

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
)

func Routes() []httpapi.Route[*Service] {
	op := httpapi.ProtectedOperations("Tasks")
	return []httpapi.Route[*Service]{
		httpapi.CreatedEndpoint(
			op(huma.Operation{
				OperationID: "create-task",
				Method:      http.MethodPost,
				Path:        "/v1/tasks",
				Summary:     "Create a task",
			}),
			createTask,
			toDTO,
			taskLocation,
		),
		httpapi.ItemEndpoint(
			op(huma.Operation{
				OperationID: "get-task",
				Method:      http.MethodGet,
				Path:        "/v1/tasks/{id}",
				Summary:     "Get an owned task",
			}),
			getTask,
			toDTO,
		),
		httpapi.PageEndpoint[TaskListResponse](
			op(huma.Operation{
				OperationID: "list-tasks",
				Method:      http.MethodGet,
				Path:        "/v1/tasks",
				Summary:     "List owned tasks, optionally filtered by status",
			}),
			listTasks,
			toDTO,
		),
		httpapi.ItemEndpoint(
			op(huma.Operation{
				OperationID: "update-task",
				Method:      http.MethodPut,
				Path:        "/v1/tasks/{id}",
				Summary:     "Replace an owned task",
			}),
			updateTask,
			toDTO,
		),
		httpapi.NoContentEndpoint(
			op(huma.Operation{
				OperationID: "delete-task",
				Method:      http.MethodDelete,
				Path:        "/v1/tasks/{id}",
				Summary:     "Delete an owned task",
			}),
			deleteTask,
		),
	}
}

func createTask(service *Service, ctx context.Context, input *TaskCreateInput) (Record, error) {
	return service.Create(ctx, identity.Subject(ctx), Details{
		Title:       input.Body.Title,
		Description: input.Body.Description,
		Status:      input.Body.Status,
	})
}

func getTask(service *Service, ctx context.Context, input *TaskIDInput) (Record, error) {
	return service.Get(ctx, identity.Subject(ctx), input.ID)
}

func listTasks(service *Service, ctx context.Context, input *TaskListInput) (pagination.Result[Record], error) {
	return service.List(ctx, identity.Subject(ctx), input.Status, input.PageQuery.Params())
}

func updateTask(service *Service, ctx context.Context, input *TaskUpdateInput) (Record, error) {
	return service.Update(ctx, identity.Subject(ctx), input.ID, Details{
		Title:       input.Body.Title,
		Description: input.Body.Description,
		Status:      input.Body.Status,
	})
}

func deleteTask(service *Service, ctx context.Context, input *TaskIDInput) error {
	return service.Delete(ctx, identity.Subject(ctx), input.ID)
}

func taskLocation(item Task) string { return "/v1/tasks/" + item.ID.String() }
