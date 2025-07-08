package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"api-template/internal/domain/entity"
	"api-template/internal/domain/repository"
)

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) repository.UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Save(ctx context.Context, user *entity.User) error {
	model := &UserModel{}
	model.FromDomain(user)

	var err error
	if user.ID() == "" {
		// Create
		err = r.db.WithContext(ctx).Create(model).Error
		if err == nil {
			user.SetID(entity.UserID(model.ID))
		}
	} else {
		// Update
		err = r.db.WithContext(ctx).Save(model).Error
	}

	if err != nil {
		if isDuplicateError(err) {
			return entity.ErrEmailExists
		}
		return fmt.Errorf("failed to save user: %w", err)
	}

	return nil
}

func (r *userRepository) FindByID(ctx context.Context, id entity.UserID) (*entity.User, error) {
	var model UserModel
	err := r.db.WithContext(ctx).First(&model, string(id)).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, entity.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to find user by ID: %w", err)
	}

	return model.ToDomain(), nil
}

func (r *userRepository) FindByEmail(ctx context.Context, email string) (*entity.User, error) {
	var model UserModel
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, entity.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to find user by email: %w", err)
	}

	return model.ToDomain(), nil
}

func (r *userRepository) FindAll(
	ctx context.Context,
	filter *repository.UserFilter,
) ([]*entity.User, int64, error) {
	var models []UserModel
	var total int64

	query := r.db.WithContext(ctx).Model(&UserModel{})

	// Apply filters
	if filter.Name != "" {
		query = query.Where("name ILIKE ?", "%"+filter.Name+"%")
	}
	if filter.Email != "" {
		query = query.Where("email ILIKE ?", "%"+filter.Email+"%")
	}
	if filter.MinAge > 0 {
		query = query.Where("age >= ?", filter.MinAge)
	}
	if filter.MaxAge > 0 {
		query = query.Where("age <= ?", filter.MaxAge)
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	// Pagination
	offset := (filter.Page - 1) * filter.PageSize
	err := query.Offset(offset).Limit(filter.PageSize).Order("created_at DESC").Find(&models).Error
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find users: %w", err)
	}

	// Make domain entity
	users := make([]*entity.User, len(models))
	for i, model := range models {
		users[i] = model.ToDomain()
	}

	return users, total, nil
}

func (r *userRepository) Delete(ctx context.Context, id entity.UserID) error {
	result := r.db.WithContext(ctx).Delete(&UserModel{}, string(id))
	if result.Error != nil {
		return fmt.Errorf("failed to delete user: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return entity.ErrUserNotFound
	}
	return nil
}

func (r *userRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&UserModel{}).Where("email = ?", email).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check email existence: %w", err)
	}
	return count > 0, nil
}

func isDuplicateError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "duplicate key value") ||
		strings.Contains(err.Error(), "users_email_key") ||
		strings.Contains(err.Error(), "Duplicate entry") ||
		strings.Contains(err.Error(), "UNIQUE constraint failed"))
}
