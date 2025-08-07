package repository

import (
	"context"
	"errors"
	"sync"
	"microservice-template/internal/domain"
)

type userRepository struct {
	users  map[int]*domain.User
	lastID int
	mu     sync.RWMutex
}

func NewUserRepository() domain.UserRepository {
	return &userRepository{
		users: make(map[int]*domain.User),
	}
}

func (r *userRepository) GetByID(ctx context.Context, id int) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, exists := r.users[id]
	if !exists {
		return nil, errors.New("user not found")
	}

	// Return copy to prevent external modifications
	userCopy := *user
	return &userCopy, nil
}

func (r *userRepository) Create(ctx context.Context, user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastID++
	user.ID = r.lastID

	// Store copy to prevent external modifications
	userCopy := *user
	r.users[user.ID] = &userCopy

	return nil
}

func (r *userRepository) Update(ctx context.Context, user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[user.ID]; !exists {
		return errors.New("user not found")
	}

	// Store copy to prevent external modifications
	userCopy := *user
	r.users[user.ID] = &userCopy

	return nil
}

func (r *userRepository) Delete(ctx context.Context, id int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[id]; !exists {
		return errors.New("user not found")
	}

	delete(r.users, id)
	return nil
}

func (r *userRepository) List(ctx context.Context) ([]*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	users := make([]*domain.User, 0, len(r.users))
	for _, user := range r.users {
		// Return copies to prevent external modifications
		userCopy := *user
		users = append(users, &userCopy)
	}

	return users, nil
}
