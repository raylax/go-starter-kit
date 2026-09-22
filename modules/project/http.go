package project

import (
	"context"
	"net/http"

	"github.com/example/go-starter-kit/httpapi"
	"github.com/example/go-starter-kit/identity"
	"github.com/example/go-starter-kit/pagination"
)

func Routes() []httpapi.Route[*Service] {
	op := httpapi.ProtectedOperations("Projects", http.StatusConflict)
	create := op("create-project", http.MethodPost, "/v1/projects", "Create a project")
	return []httpapi.Route[*Service]{
		httpapi.CreatedEndpoint(create, createProject, toDTO, func(item Project) string { return "/v1/projects/" + item.ID.String() }),
		httpapi.ItemEndpoint(op("get-project", http.MethodGet, "/v1/projects/{id}", "Get an owned project"), getProject, toDTO),
		httpapi.PageEndpoint[ProjectListResponse](op("list-projects", http.MethodGet, "/v1/projects", "List owned projects"), listProjects, toDTO),
		httpapi.ItemEndpoint(op("update-project", http.MethodPut, "/v1/projects/{id}", "Replace an owned project"), updateProject, toDTO),
		httpapi.NoContentEndpoint(op("delete-project", http.MethodDelete, "/v1/projects/{id}", "Delete an owned project"), deleteProject),
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
