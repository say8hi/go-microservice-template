package entity

import (
	"errors"
	"time"
)

var (
	ErrInvalidEmail = errors.New("invalid email address")
	ErrInvalidName  = errors.New("name must be between 2 and 50 characters")
	ErrInvalidAge   = errors.New("age must be between 13 and 150")
	ErrEmailExists  = errors.New("email already exists")
	ErrUserNotFound = errors.New("user not found")
)

type UserID string

type User struct {
	id        UserID
	name      string
	email     string
	age       int
	createdAt time.Time
	updatedAt time.Time
}

func NewUser(name, email string, age int) (*User, error) {
	user := &User{
		name:      name,
		email:     email,
		age:       age,
		createdAt: time.Now(),
		updatedAt: time.Now(),
	}

	if err := user.validate(); err != nil {
		return nil, err
	}

	return user, nil
}

func RestoreUser(id UserID, name, email string, age int, createdAt, updatedAt time.Time) *User {
	return &User{
		id:        id,
		name:      name,
		email:     email,
		age:       age,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}
}

// Getters
func (u *User) ID() UserID           { return u.id }
func (u *User) Name() string         { return u.name }
func (u *User) Email() string        { return u.email }
func (u *User) Age() int             { return u.age }
func (u *User) CreatedAt() time.Time { return u.createdAt }
func (u *User) UpdatedAt() time.Time { return u.updatedAt }

// Buisness methods
func (u *User) ChangeName(newName string) error {
	if len(newName) < 2 || len(newName) > 50 {
		return ErrInvalidName
	}
	u.name = newName
	u.updatedAt = time.Now()
	return nil
}

func (u *User) ChangeEmail(newEmail string) error {
	if !isValidEmail(newEmail) {
		return ErrInvalidEmail
	}
	u.email = newEmail
	u.updatedAt = time.Now()
	return nil
}

func (u *User) ChangeAge(newAge int) error {
	if newAge < 13 || newAge > 150 {
		return ErrInvalidAge
	}
	u.age = newAge
	u.updatedAt = time.Now()
	return nil
}

func (u *User) Update(name, email string, age int) error {
	if name != "" {
		if err := u.ChangeName(name); err != nil {
			return err
		}
	}

	if email != "" {
		if err := u.ChangeEmail(email); err != nil {
			return err
		}
	}

	if age > 0 {
		if err := u.ChangeAge(age); err != nil {
			return err
		}
	}

	return nil
}

func (u *User) validate() error {
	if len(u.name) < 2 || len(u.name) > 50 {
		return ErrInvalidName
	}

	if !isValidEmail(u.email) {
		return ErrInvalidEmail
	}

	if u.age < 13 || u.age > 150 {
		return ErrInvalidAge
	}

	return nil
}

func (u *User) SetID(id UserID) {
	u.id = id
}

// email validation
func isValidEmail(email string) bool {
	return len(email) > 0 &&
		len(email) <= 255 &&
		containsAt(email) &&
		containsDot(email)
}

func containsAt(s string) bool {
	for _, r := range s {
		if r == '@' {
			return true
		}
	}
	return false
}

func containsDot(s string) bool {
	for _, r := range s {
		if r == '.' {
			return true
		}
	}
	return false
}
