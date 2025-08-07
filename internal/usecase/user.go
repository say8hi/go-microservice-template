package usecase

import (
	"context"
	"errors"
	"microservice-template/internal/domain"
	"microservice-template/pkg/logger"
	"time"
)

type userService struct {
	repo   domain.UserRepository
	logger logger.Logger
}

func NewUserService(repo domain.UserRepository, logger logger.Logger) domain.UserService {
	return &userService{
		repo:   repo,
		logger: logger,
	}
}

func (s *userService) GetByID(ctx context.Context, id int) (*domain.User, error) {
	s.logger.Info("Getting user with ID: %d", logger.Int("id", id))

	// Business logic: validate ID
	if id <= 0 {
		return nil, errors.New("invalid user ID")
	}

	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		s.logger.Error("Failed to get user: %s", logger.Error(err))
		return nil, err
	}

	return user, nil
}

func (s *userService) Create(
	ctx context.Context,
	req *domain.CreateUserRequest,
) (*domain.User, error) {
	s.logger.Info("Creating user with email: %s", logger.String("email", req.Email))

	// Business logic: check if email already exists
	if err := s.validateUniqueEmail(ctx, req.Email); err != nil {
		return nil, err
	}

	user := &domain.User{
		Name:      req.Name,
		Email:     req.Email,
		CreatedAt: time.Now(),
	}

	if err := s.repo.Create(ctx, user); err != nil {
		s.logger.Error("Failed to create user: %s", logger.Error(err))
		return nil, err
	}

	s.logger.Info("User created successfully with ID: %d", logger.Int("user_id", user.ID))
	return user, nil
}

func (s *userService) Update(
	ctx context.Context,
	id int,
	req *domain.UpdateUserRequest,
) (*domain.User, error) {
	s.logger.Info("Updating user with ID: %d", logger.Int("id", id))

	// Business logic: validate ID
	if id <= 0 {
		return nil, errors.New("invalid user ID")
	}

	// Get existing user
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		s.logger.Error("User not found for update: %s", logger.Error(err))
		return nil, err
	}

	// Business logic: update only provided fields
	if req.Name != "" {
		user.Name = req.Name
	}
	if req.Email != "" {
		// Check email uniqueness if changing
		if user.Email != req.Email {
			if err := s.validateUniqueEmail(ctx, req.Email); err != nil {
				return nil, err
			}
		}
		user.Email = req.Email
	}

	if err := s.repo.Update(ctx, user); err != nil {
		s.logger.Error("Failed to update user: %s", logger.Error(err))
		return nil, err
	}

	s.logger.Info("User updated successfully with ID: %d", logger.Int("user_id", user.ID))
	return user, nil
}

func (s *userService) Delete(ctx context.Context, id int) error {
	s.logger.Info("Deleting user with ID: %d", logger.Int("id", id))

	// Business logic: validate ID
	if id <= 0 {
		return errors.New("invalid user ID")
	}

	// Check if user exists before deletion
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		s.logger.Error("User not found for deletion: %s", logger.Error(err))
		return err
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		s.logger.Error("Failed to delete user: %s", logger.Error(err))
		return err
	}

	s.logger.Info("User deleted successfully with ID: %d", logger.Int("id", id))
	return nil
}

func (s *userService) List(ctx context.Context) ([]*domain.User, error) {
	s.logger.Info("Listing all users")

	users, err := s.repo.List(ctx)
	if err != nil {
		s.logger.Error("Failed to list users: %s", logger.Error(err))
		return nil, err
	}

	s.logger.Info("Found %d users", logger.Int("users amount", len(users)))
	return users, nil
}

// Private helper methods for business logic
func (s *userService) validateUniqueEmail(ctx context.Context, email string) error {
	// This is a simplified check - in real app you'd have a proper method in repository
	users, err := s.repo.List(ctx)
	if err != nil {
		return err
	}

	for _, user := range users {
		if user.Email == email {
			return errors.New("email already exists")
		}
	}
	return nil
}
