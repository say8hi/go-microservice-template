package domain

import (
	"context"
	"time"
)

// User entity
type User struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// Request/Response models
type CreateUserRequest struct {
	Name  string `json:"name"  validate:"required,min=2,max=50"`
	Email string `json:"email" validate:"required,email"`
}

type UpdateUserRequest struct {
	Name  string `json:"name,omitempty"  validate:"omitempty,min=2,max=50"`
	Email string `json:"email,omitempty" validate:"omitempty,email"`
}

type UserResponse struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// Service interface (port)
type UserService interface {
	GetByID(ctx context.Context, id int) (*User, error)
	Create(ctx context.Context, req *CreateUserRequest) (*User, error)
	Update(ctx context.Context, id int, req *UpdateUserRequest) (*User, error)
	Delete(ctx context.Context, id int) error
	List(ctx context.Context) ([]*User, error)
}

// Repository interface (port)
type UserRepository interface {
	GetByID(ctx context.Context, id int) (*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context) ([]*User, error)
}
