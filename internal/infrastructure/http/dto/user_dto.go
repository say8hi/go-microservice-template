package dto

import (
	"time"

	"api-template/internal/domain/entity"
)

type CreateUserRequest struct {
	Name  string `json:"name"  binding:"required,min=2,max=50"`
	Email string `json:"email" binding:"required,email"`
	Age   int    `json:"age"   binding:"required,min=13,max=150"`
}

type UpdateUserRequest struct {
	Name  string `json:"name,omitempty"  binding:"omitempty,min=2,max=50"`
	Email string `json:"email,omitempty" binding:"omitempty,email"`
	Age   int    `json:"age,omitempty"   binding:"omitempty,min=13,max=150"`
}

type UserResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Age       int       `json:"age"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewUserResponse(user *entity.User) *UserResponse {
	return &UserResponse{
		ID:        string(user.ID()),
		Name:      user.Name(),
		Email:     user.Email(),
		Age:       user.Age(),
		CreatedAt: user.CreatedAt(),
		UpdatedAt: user.UpdatedAt(),
	}
}

type GetUsersResponse struct {
	Users      []*UserResponse `json:"users"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalPages int             `json:"total_pages"`
}
