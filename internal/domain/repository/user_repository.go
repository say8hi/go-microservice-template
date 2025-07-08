package repository

import (
	"context"

	"api-template/internal/domain/entity"
)

type UserRepository interface {
	Save(ctx context.Context, user *entity.User) error
	FindByID(ctx context.Context, id entity.UserID) (*entity.User, error)
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	FindAll(ctx context.Context, filter *UserFilter) ([]*entity.User, int64, error)
	Delete(ctx context.Context, id entity.UserID) error
	ExistsByEmail(ctx context.Context, email string) (bool, error)
}

type UserFilter struct {
	Name     string
	Email    string
	MinAge   int
	MaxAge   int
	Page     int
	PageSize int
}
