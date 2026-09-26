package account

import (
	"github.com/example/go-starter-kit/internal/httpapi"
)

type UserStatusUpdateRequest struct {
	Status UserStatus `json:"status" enum:"active,disabled"`
}

type StatusInput struct {
	ID   string `path:"id" minLength:"1" maxLength:"255"`
	Body UserStatusUpdateRequest
}

type UserListResponse httpapi.Page[User]
