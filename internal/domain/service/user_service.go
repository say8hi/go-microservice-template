package service

import (
	"context"

	"api-template/internal/domain/entity"
	"api-template/internal/domain/repository"
)

// Domain services for hard buisness logic
type UserDomainService struct {
	userRepo repository.UserRepository
}

func NewUserDomainService(userRepo repository.UserRepository) *UserDomainService {
	return &UserDomainService{
		userRepo: userRepo,
	}
}

func (s *UserDomainService) IsEmailUnique(ctx context.Context, email string) (bool, error) {
	exists, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// Is able to change email
func (s *UserDomainService) CanChangeEmail(
	ctx context.Context,
	userID entity.UserID,
	newEmail string,
) (bool, error) {
	// Check if email is in use
	existingUser, err := s.userRepo.FindByEmail(ctx, newEmail)
	if err != nil && err != entity.ErrUserNotFound {
		return false, err
	}

	if existingUser != nil && existingUser.ID() != entity.UserID(userID) {
		return false, nil
	}

	return true, nil
}
