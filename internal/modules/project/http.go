package project

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
)

func Routes() []httpapi.Route[*Service] {
	op := httpapi.ProtectedOperations("Projects", http.StatusConflict)
	return []httpapi.Route[*Service]{
		httpapi.CreatedEndpoint(
			op(huma.Operation{
				OperationID: "create-project",
				Method:      http.MethodPost,
				Path:        "/v1/projects",
				Summary:     "Create a project",
			}),
			createProject,
			toDTO,
			projectLocation,
		),
		httpapi.ItemEndpoint(
			op(huma.Operation{
				OperationID: "get-project",
				Method:      http.MethodGet,
				Path:        "/v1/projects/{id}",
				Summary:     "Get an owned project",
			}),
			getProject,
			toDTO,
		),
		httpapi.PageEndpoint[ProjectListResponse](
			op(huma.Operation{
				OperationID: "list-projects",
				Method:      http.MethodGet,
				Path:        "/v1/projects",
				Summary:     "List owned projects",
			}),
			listProjects,
			toDTO,
		),
		httpapi.ItemEndpoint(
			op(huma.Operation{
				OperationID: "update-project",
				Method:      http.MethodPut,
				Path:        "/v1/projects/{id}",
				Summary:     "Replace an owned project",
			}),
			updateProject,
			toDTO,
		),
		httpapi.NoContentEndpoint(
			op(huma.Operation{
				OperationID: "delete-project",
				Method:      http.MethodDelete,
				Path:        "/v1/projects/{id}",
				Summary:     "Delete an owned project",
			}),
			deleteProject,
		),
	}
}

func createProject(service *Service, ctx context.Context, input *CreateInput) (Record, error) {
	return service.Create(ctx, identity.Subject(ctx), input.Body.Name, input.Body.Description)
}

func getProject(service *Service, ctx context.Context, input *IDInput) (Record, error) {
	return service.Get(ctx, identity.Subject(ctx), input.ID)
}

func listProjects(service *Service, ctx context.Context, input *ListInput) (pagination.Result[Record], error) {
	return service.List(ctx, identity.Subject(ctx), input.PageQuery.Params())
}

func updateProject(service *Service, ctx context.Context, input *UpdateInput) (Record, error) {
	return service.Update(ctx, identity.Subject(ctx), input.ID, input.Body.Name, input.Body.Description)
}

func deleteProject(service *Service, ctx context.Context, input *IDInput) error {
	return service.Delete(ctx, identity.Subject(ctx), input.ID)
}

func projectLocation(item Project) string { return "/v1/projects/" + item.ID.String() }
