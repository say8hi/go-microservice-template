package persistence

import (
	"time"

	"gorm.io/gorm"

	"api-template/internal/domain/entity"
)

// GORM model
type UserModel struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	Name      string `gorm:"not null;size:50"`
	Email     string `gorm:"uniqueIndex;not null;size:255"`
	Age       int    `gorm:"not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UserModel) TableName() string {
	return "users"
}

// From domain entity to GORM model
func (m *UserModel) FromDomain(user *entity.User) {
	m.ID = string(user.ID())
	m.Name = user.Name()
	m.Email = user.Email()
	m.Age = user.Age()
	m.CreatedAt = user.CreatedAt()
	m.UpdatedAt = user.UpdatedAt()
}

// From GORM model to domain entity
func (m *UserModel) ToDomain() *entity.User {
	return entity.RestoreUser(
		entity.UserID(m.ID),
		m.Name,
		m.Email,
		m.Age,
		m.CreatedAt,
		m.UpdatedAt,
	)
}
