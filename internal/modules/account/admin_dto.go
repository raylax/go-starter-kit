package account

import (
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/google/uuid"
)

type UserStatusUpdateRequest struct {
	Status UserStatus `json:"status" enum:"active,disabled"`
}

type StatusInput struct {
	ID   uuid.UUID `path:"id"`
	Body UserStatusUpdateRequest
}

type UserListResponse httpapi.Page[User]
