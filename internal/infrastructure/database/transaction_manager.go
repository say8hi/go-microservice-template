package database

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"api-template/internal/domain/repository"
	"api-template/pkg/logger"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

const (
	// txKey is the key for storing transaction in context
	txKey contextKey = "transaction"
	// gormTxKey is the key for storing GORM transaction in context
	gormTxKey contextKey = "gorm_transaction"
)

// GormTransactionManager implements TransactionManager using GORM
type GormTransactionManager struct {
	db     *gorm.DB
	logger logger.Logger
}

// NewGormTransactionManager creates a new GORM transaction manager
func NewGormTransactionManager(db *gorm.DB, logger logger.Logger) repository.TransactionManager {
	return &GormTransactionManager{
		db:     db,
		logger: logger,
	}
}

// WithTransaction executes a function within a database transaction
func (tm *GormTransactionManager) WithTransaction(
	ctx context.Context,
	fn func(ctx context.Context) error,
) error {
	// Start transaction
	tx := tm.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	// Add transaction to context
	txCtx := context.WithValue(ctx, gormTxKey, tx)

	// Setup panic recovery
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r) // Re-panic after rollback
		}
	}()

	// Execute function
	if err := fn(txCtx); err != nil {
		if rbErr := tx.Rollback(); rbErr.Error != nil {
			return fmt.Errorf(
				"failed to rollback transaction: %w (original error: %v)",
				rbErr.Error,
				err,
			)
		}
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err.Error != nil {
		return fmt.Errorf("failed to commit transaction: %w", err.Error)
	}

	return nil
}
