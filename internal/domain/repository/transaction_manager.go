package repository

import (
	"context"
)

// TransactionManager provides transaction management for repository operations
type TransactionManager interface {
	// WithTransaction executes a function within a database transaction
	// If the function returns an error, the transaction is rolled back
	// Otherwise, the transaction is committed
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// Alternative approach with context-based transaction passing
type TransactionContext interface {
	// BeginTransaction starts a new transaction and returns a context with the transaction
	BeginTransaction(ctx context.Context) (context.Context, error)

	// Commit commits the transaction associated with the context
	Commit(ctx context.Context) error

	// Rollback rolls back the transaction associated with the context
	Rollback(ctx context.Context) error
}
