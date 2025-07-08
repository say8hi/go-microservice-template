package error

import (
	"errors"
	"fmt"
)

// Common domain errors
var (
	// User errors
	ErrUserNotFound       = errors.New("user not found")
	ErrUserAlreadyExists  = errors.New("user already exists")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrInvalidEmail       = errors.New("invalid email format")
	ErrInvalidAge         = errors.New("invalid age")
	ErrInvalidName        = errors.New("invalid name")

	// Generic errors
	ErrInvalidInput   = errors.New("invalid input")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrForbidden      = errors.New("forbidden")
	ErrInternalError  = errors.New("internal error")
	ErrNotImplemented = errors.New("not implemented")
)

// DomainError represents the base interface for all domain errors
type DomainError interface {
	error
	Code() string
	Details() map[string]interface{}
}

// ValidationError represents validation errors
type ValidationError struct {
	Message string
	Field   string
	Value   interface{}
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

func (e *ValidationError) Code() string {
	return "VALIDATION_ERROR"
}

func (e *ValidationError) Details() map[string]interface{} {
	details := map[string]interface{}{
		"message": e.Message,
	}
	if e.Field != "" {
		details["field"] = e.Field
	}
	if e.Value != nil {
		details["value"] = e.Value
	}
	return details
}

// NotFoundError represents resource not found errors
type NotFoundError struct {
	Message  string
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	if e.Resource != "" && e.ID != "" {
		return fmt.Sprintf("%s with ID '%s' not found", e.Resource, e.ID)
	}
	if e.Message != "" {
		return e.Message
	}
	return "resource not found"
}

func (e *NotFoundError) Code() string {
	return "NOT_FOUND"
}

func (e *NotFoundError) Details() map[string]interface{} {
	details := map[string]interface{}{
		"message": e.Error(),
	}
	if e.Resource != "" {
		details["resource"] = e.Resource
	}
	if e.ID != "" {
		details["id"] = e.ID
	}
	return details
}

// ConflictError represents resource conflict errors
type ConflictError struct {
	Message  string
	Resource string
	Field    string
	Value    interface{}
}

func (e *ConflictError) Error() string {
	if e.Resource != "" && e.Field != "" {
		return fmt.Sprintf("%s with %s '%v' already exists", e.Resource, e.Field, e.Value)
	}
	if e.Message != "" {
		return e.Message
	}
	return "resource conflict"
}

func (e *ConflictError) Code() string {
	return "CONFLICT"
}

func (e *ConflictError) Details() map[string]interface{} {
	details := map[string]interface{}{
		"message": e.Error(),
	}
	if e.Resource != "" {
		details["resource"] = e.Resource
	}
	if e.Field != "" {
		details["field"] = e.Field
	}
	if e.Value != nil {
		details["value"] = e.Value
	}
	return details
}

// UnauthorizedError represents authentication errors
type UnauthorizedError struct {
	Message string
	Reason  string
}

func (e *UnauthorizedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "unauthorized access"
}

func (e *UnauthorizedError) Code() string {
	return "UNAUTHORIZED"
}

func (e *UnauthorizedError) Details() map[string]interface{} {
	details := map[string]interface{}{
		"message": e.Error(),
	}
	if e.Reason != "" {
		details["reason"] = e.Reason
	}
	return details
}

// ForbiddenError represents authorization errors
type ForbiddenError struct {
	Message  string
	Resource string
	Action   string
	UserRole string
	Required string
}

func (e *ForbiddenError) Error() string {
	if e.Resource != "" && e.Action != "" {
		return fmt.Sprintf("forbidden: cannot %s %s", e.Action, e.Resource)
	}
	if e.Message != "" {
		return e.Message
	}
	return "forbidden access"
}

func (e *ForbiddenError) Code() string {
	return "FORBIDDEN"
}

func (e *ForbiddenError) Details() map[string]interface{} {
	details := map[string]interface{}{
		"message": e.Error(),
	}
	if e.Resource != "" {
		details["resource"] = e.Resource
	}
	if e.Action != "" {
		details["action"] = e.Action
	}
	if e.UserRole != "" {
		details["user_role"] = e.UserRole
	}
	if e.Required != "" {
		details["required_role"] = e.Required
	}
	return details
}

// InternalError represents internal server errors
type InternalError struct {
	Message string
	Cause   error
}

func (e *InternalError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "internal server error"
}

func (e *InternalError) Code() string {
	return "INTERNAL_ERROR"
}

func (e *InternalError) Details() map[string]interface{} {
	details := map[string]interface{}{
		"message": e.Error(),
	}
	if e.Cause != nil {
		details["cause"] = e.Cause.Error()
	}
	return details
}

func (e *InternalError) Unwrap() error {
	return e.Cause
}

// BusinessError represents business logic errors
type BusinessError struct {
	Message    string
	StatusCode string
	Rule       string
}

func (e *BusinessError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "business rule violation"
}

func (e *BusinessError) Code() string {
	if e.StatusCode != "" {
		return e.StatusCode
	}
	return "BUSINESS_ERROR"
}

func (e *BusinessError) Details() map[string]interface{} {
	details := map[string]interface{}{
		"message": e.Error(),
	}
	if e.Rule != "" {
		details["rule"] = e.Rule
	}
	return details
}

// RateLimitError represents rate limiting errors
type RateLimitError struct {
	Message   string
	Limit     int
	Window    string
	ResetTime int64
}

func (e *RateLimitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "rate limit exceeded"
}

func (e *RateLimitError) Code() string {
	return "RATE_LIMIT_EXCEEDED"
}

func (e *RateLimitError) Details() map[string]interface{} {
	details := map[string]interface{}{
		"message": e.Error(),
	}
	if e.Limit > 0 {
		details["limit"] = e.Limit
	}
	if e.Window != "" {
		details["window"] = e.Window
	}
	if e.ResetTime > 0 {
		details["reset_time"] = e.ResetTime
	}
	return details
}

// Helper functions for creating common errors

// NewValidationError creates a new validation error
func NewValidationError(field, message string, value interface{}) *ValidationError {
	return &ValidationError{
		Message: message,
		Field:   field,
		Value:   value,
	}
}

// NewNotFoundError creates a new not found error
func NewNotFoundError(resource, id string) *NotFoundError {
	return &NotFoundError{
		Resource: resource,
		ID:       id,
	}
}

// NewConflictError creates a new conflict error
func NewConflictError(resource, field string, value interface{}) *ConflictError {
	return &ConflictError{
		Resource: resource,
		Field:    field,
		Value:    value,
	}
}

// NewUnauthorizedError creates a new unauthorized error
func NewUnauthorizedError(reason string) *UnauthorizedError {
	return &UnauthorizedError{
		Reason: reason,
	}
}

// NewForbiddenError creates a new forbidden error
func NewForbiddenError(resource, action string) *ForbiddenError {
	return &ForbiddenError{
		Resource: resource,
		Action:   action,
	}
}

// NewInternalError creates a new internal error
func NewInternalError(message string, cause error) *InternalError {
	return &InternalError{
		Message: message,
		Cause:   cause,
	}
}

// NewBusinessError creates a new business error
func NewBusinessError(code, message, rule string) *BusinessError {
	return &BusinessError{
		StatusCode: code,
		Message:    message,
		Rule:       rule,
	}
}

// NewRateLimitError creates a new rate limit error
func NewRateLimitError(limit int, window string, resetTime int64) *RateLimitError {
	return &RateLimitError{
		Limit:     limit,
		Window:    window,
		ResetTime: resetTime,
	}
}

// Error checking helper functions

// IsValidationError checks if error is a validation error
func IsValidationError(err error) bool {
	var validationErr *ValidationError
	return errors.As(err, &validationErr)
}

// IsNotFoundError checks if error is a not found error
func IsNotFoundError(err error) bool {
	var notFoundErr *NotFoundError
	return errors.As(err, &notFoundErr)
}

// IsConflictError checks if error is a conflict error
func IsConflictError(err error) bool {
	var conflictErr *ConflictError
	return errors.As(err, &conflictErr)
}

// IsUnauthorizedError checks if error is an unauthorized error
func IsUnauthorizedError(err error) bool {
	var unauthorizedErr *UnauthorizedError
	return errors.As(err, &unauthorizedErr)
}

// IsForbiddenError checks if error is a forbidden error
func IsForbiddenError(err error) bool {
	var forbiddenErr *ForbiddenError
	return errors.As(err, &forbiddenErr)
}

// IsInternalError checks if error is an internal error
func IsInternalError(err error) bool {
	var internalErr *InternalError
	return errors.As(err, &internalErr)
}

// IsBusinessError checks if error is a business error
func IsBusinessError(err error) bool {
	var businessErr *BusinessError
	return errors.As(err, &businessErr)
}

// IsRateLimitError checks if error is a rate limit error
func IsRateLimitError(err error) bool {
	var rateLimitErr *RateLimitError
	return errors.As(err, &rateLimitErr)
}

// GetErrorCode extracts error code from domain error
func GetErrorCode(err error) string {
	if domainErr, ok := err.(DomainError); ok {
		return domainErr.Code()
	}
	return "UNKNOWN_ERROR"
}

// GetErrorDetails extracts error details from domain error
func GetErrorDetails(err error) map[string]interface{} {
	if domainErr, ok := err.(DomainError); ok {
		return domainErr.Details()
	}
	return map[string]interface{}{
		"message": err.Error(),
	}
}
