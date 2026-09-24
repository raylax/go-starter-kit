package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type HealthResponse struct {
	Status string `json:"status"`
}

type HealthOutput = ItemOutput[HealthResponse]

func HealthRoutes() []Route[func(context.Context) error] {
	return []Route[func(context.Context) error]{
		Endpoint(
			NewOperation(Public, huma.Operation{
				OperationID: "liveness",
				Method:      http.MethodGet,
				Path:        "/health/live",
				Tags:        []string{"Health"},
			}),
			liveness,
		),
		Endpoint(
			NewOperation(Public, huma.Operation{
				OperationID: "readiness",
				Method:      http.MethodGet,
				Path:        "/health/ready",
				Tags:        []string{"Health"},
				Errors:      []int{http.StatusServiceUnavailable},
			}),
			readiness,
		),
	}
}

func liveness(_ func(context.Context) error, _ context.Context, _ *struct{}) (*HealthOutput, error) {
	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}

func readiness(ready func(context.Context) error, ctx context.Context, _ *struct{}) (*HealthOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ready(ctx); err != nil {
		return nil, huma.Error503ServiceUnavailable("service not ready")
	}
	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}
