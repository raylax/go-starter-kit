package task

import (
	"time"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/internal/httpapi"
)

// Task 是独立于数据库类型的接口表示。
type Task struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      Status    `json:"status" enum:"todo,in_progress,done"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type TaskCreateRequest struct {
	Title       string `json:"title" minLength:"1" maxLength:"200" example:"Write API documentation"`
	Description string `json:"description,omitempty" maxLength:"2000"`
	Status      Status `json:"status,omitempty" default:"todo" enum:"todo,in_progress,done"`
}

type TaskCreateInput struct{ Body TaskCreateRequest }
type TaskIDInput struct {
	ID uuid.UUID `path:"id" format:"uuid"`
}
type TaskUpdateRequest struct {
	Title       string `json:"title" minLength:"1" maxLength:"200"`
	Description string `json:"description,omitempty" maxLength:"2000"`
	Status      Status `json:"status" enum:"todo,in_progress,done"`
}

type TaskUpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid"`
	Body TaskUpdateRequest
}
type TaskListInput struct {
	Status Status `query:"status" enum:"todo,in_progress,done" required:"false"`
	httpapi.PageQuery
}
type TaskOutput = httpapi.ItemOutput[Task]
type TaskCreatedOutput = httpapi.CreatedOutput[Task]

// 具名分页 DTO 为公开 schema 提供稳定的资源名称。
type TaskListResponse httpapi.Page[Task]

type TaskListOutput = httpapi.ItemOutput[TaskListResponse]

// toDTO 保持业务模型与 DTO 独立，结构不再匹配时会在编译期报错。
func toDTO(item Record) Task {
	return Task(item)
}
