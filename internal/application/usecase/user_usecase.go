package usecase

import (
	"context"
	"fmt"

	"api-template/internal/domain/entity"
	domainError "api-template/internal/domain/error"
	"api-template/internal/domain/repository"
	domainService "api-template/internal/domain/service"
)

// UserUseCase handles user-related business operations
type UserUseCase struct {
	userRepo           repository.UserRepository
	userDomainService  *domainService.UserDomainService
	transactionManager repository.TransactionManager
}

// NewUserUseCase creates a new UserUseCase instance
func NewUserUseCase(
	userRepo repository.UserRepository,
	transactionManager repository.TransactionManager,
) *UserUseCase {
	return &UserUseCase{
		userRepo:           userRepo,
		userDomainService:  domainService.NewUserDomainService(userRepo),
		transactionManager: transactionManager,
	}
}

// CreateUser creates a new user with validation
func (uc *UserUseCase) CreateUser(
	ctx context.Context,
	name, email string,
	age int,
) (*entity.User, error) {
	// Check if email is unique
	unique, err := uc.userDomainService.IsEmailUnique(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to validate email uniqueness: %w", err)
	}

	if !unique {
		return nil, &domainError.ConflictError{
			Message: "User with this email already exists",
		}
	}

	// Create user entity
	user, err := entity.NewUser(name, email, age)
	if err != nil {
		return nil, fmt.Errorf("failed to create user entity: %w", err)
	}

	// Save user
	if err := uc.userRepo.Save(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to save user: %w", err)
	}

	return user, nil
}

// GetUser retrieves a user by ID
func (uc *UserUseCase) GetUser(ctx context.Context, id entity.UserID) (*entity.User, error) {
	user, err := uc.userRepo.FindByID(ctx, id)
	if err != nil {
		if domainError.IsNotFoundError(err) {
			return nil, &domainError.NotFoundError{
				Message: "User not found",
			}
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
}

// GetUsers retrieves users with pagination and filtering
func (uc *UserUseCase) GetUsers(
	ctx context.Context,
	filter *repository.UserFilter,
) ([]*entity.User, int64, error) {
	// Validate pagination parameters
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 10
	}

	users, total, err := uc.userRepo.FindAll(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get users: %w", err)
	}

	return users, total, nil
}

// UpdateUser updates an existing user
func (uc *UserUseCase) UpdateUser(
	ctx context.Context,
	id entity.UserID,
	name, email string,
	age int,
) error {
	uc.transactionManager.WithTransaction(
		ctx,
		func(txCtx context.Context) error {
			// Get existing user
			user, err := uc.userRepo.FindByID(txCtx, id)
			if err != nil {
				return uc.handleRepositoryError(err, "User not found")
			}

			// Check if email changed and validate uniqueness
			if email != "" && email != user.Email() {
				canChange, err := uc.userDomainService.CanChangeEmail(
					txCtx,
					id,
					email,
				)
				if err != nil {
					return fmt.Errorf("failed to validate email change: %w", err)
				}

				if !canChange {
					return &domainError.ConflictError{
						Message: "Email already exists",
					}
				}
			}

			// Update user entity
			if err := user.Update(name, email, age); err != nil {
				return fmt.Errorf("failed to update user entity: %w", err)
			}

			// Save updated user
			if err := uc.userRepo.Save(txCtx, user); err != nil {
				return fmt.Errorf("failed to save updated user: %w", err)
			}

			return nil
		},
	)
	return nil
}

// DeleteUser deletes a user by ID
func (uc *UserUseCase) DeleteUser(ctx context.Context, id entity.UserID) error {
	// Check if user exists
	_, err := uc.userRepo.FindByID(ctx, id)
	if err != nil {
		if domainError.IsNotFoundError(err) {
			return &domainError.NotFoundError{
				Message: "User not found",
			}
		}
		return fmt.Errorf("failed to get user for deletion: %w", err)
	}

	// Delete user
	if err := uc.userRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// GetUserByEmail retrieves a user by email
func (uc *UserUseCase) GetUserByEmail(ctx context.Context, email string) (*entity.User, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		if domainError.IsNotFoundError(err) {
			return nil, &domainError.NotFoundError{
				Message: "User not found",
			}
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return user, nil
}

func (uc *UserUseCase) handleRepositoryError(err error, notFoundMessage string) error {
	if domainError.IsNotFoundError(err) {
		return &domainError.NotFoundError{
			Message: notFoundMessage,
		}
	}
	return fmt.Errorf("repository error: %w", err)
}

// normalizeUserFilter validates and normalizes filter parameters
func (uc *UserUseCase) normalizeUserFilter(filter *repository.UserFilter) *repository.UserFilter {
	if filter == nil {
		filter = &repository.UserFilter{}
	}

	// Validate pagination parameters
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 10
	}

	return filter
}

// validateEmailsBatch validates that all emails are unique
func (uc *UserUseCase) validateEmailsBatch(ctx context.Context, emails []string) error {
	// Check for duplicates within the batch
	emailSet := make(map[string]bool)
	for _, email := range emails {
		if emailSet[email] {
			return &domainError.ConflictError{
				Message: fmt.Sprintf("Duplicate email in batch: %s", email),
			}
		}
		emailSet[email] = true
	}

	// Check each email against the database
	for _, email := range emails {
		unique, err := uc.userDomainService.IsEmailUnique(ctx, email)
		if err != nil {
			return fmt.Errorf("failed to validate email uniqueness for %s: %w", email, err)
		}
		if !unique {
			return &domainError.ConflictError{
				Message: fmt.Sprintf("User with email %s already exists", email),
			}
		}
	}

	return nil
}
