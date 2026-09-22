package project

import (
	"time"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/internal/httpapi"
)

// Project 是独立于数据库类型的接口表示。
type Project struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProjectCreateRequest struct {
	Name        string `json:"name" minLength:"1" maxLength:"100" example:"My project"`
	Description string `json:"description,omitempty" maxLength:"2000"`
}

// ProjectUpdateRequest 当前复用创建字段，保留独立的公开请求类型。
type ProjectUpdateRequest ProjectCreateRequest

type CreateInput struct{ Body ProjectCreateRequest }
type IDInput struct {
	ID uuid.UUID `path:"id" format:"uuid"`
}
type UpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid"`
	Body ProjectUpdateRequest
}
type ListInput struct {
	httpapi.PageQuery
}
type ProjectOutput = httpapi.ItemOutput[Project]
type CreatedOutput = httpapi.CreatedOutput[Project]

// 具名分页 DTO 为公开 schema 提供稳定的资源名称。
type ProjectListResponse httpapi.Page[Project]

type ListOutput = httpapi.ItemOutput[ProjectListResponse]

// toDTO 保持业务模型与 DTO 独立，结构不再匹配时会在编译期报错。
func toDTO(item Record) Project {
	return Project(item)
}
