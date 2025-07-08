package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"api-template/internal/config"
	domainError "api-template/internal/domain/error"
	"api-template/pkg/logger"
)

// BaseHandler provides common functionality for all handlers
type BaseHandler struct {
	logger logger.Logger
	config *config.Config
}

// NewBaseHandler creates a new BaseHandler instance
func NewBaseHandler(logger logger.Logger, config *config.Config) *BaseHandler {
	return &BaseHandler{
		logger: logger,
		config: config,
	}
}

// ErrorResponse represents standard error response format
type ErrorResponse struct {
	Error     string    `json:"error"`
	Message   string    `json:"message,omitempty"`
	Code      string    `json:"code,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// SuccessResponse represents standard success response format
type SuccessResponse struct {
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// fail logs the error and returns standardized HTTP error response
func (h *BaseHandler) fail(c *gin.Context, err error, message string) {
	requestID := h.getRequestID(c)

	// Log the error with context
	h.logger.Error("Handler error occurred",
		logger.String("request_id", requestID),
		logger.String("method", c.Request.Method),
		logger.String("path", c.Request.URL.Path),
		logger.String("user_agent", c.Request.UserAgent()),
		logger.String("remote_addr", c.ClientIP()),
		logger.Error(err),
		logger.String("custom_message", message),
	)

	// Determine HTTP status code and error details
	statusCode, errorCode, publicMessage := h.mapError(err, message)

	response := ErrorResponse{
		Error:     publicMessage,
		Message:   message,
		Code:      errorCode,
		RequestID: requestID,
		Timestamp: time.Now(),
	}

	c.JSON(statusCode, response)
}

// success returns standardized success response
func (h *BaseHandler) success(c *gin.Context, data interface{}, message string) {
	requestID := h.getRequestID(c)

	response := SuccessResponse{
		Data:      data,
		Message:   message,
		RequestID: requestID,
		Timestamp: time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// created returns standardized created response
func (h *BaseHandler) created(c *gin.Context, data interface{}, message string) {
	requestID := h.getRequestID(c)

	response := SuccessResponse{
		Data:      data,
		Message:   message,
		RequestID: requestID,
		Timestamp: time.Now(),
	}

	c.JSON(http.StatusCreated, response)
}

// getRequestID extracts or generates request ID
func (h *BaseHandler) getRequestID(c *gin.Context) string {
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
		c.Header("X-Request-ID", requestID)
	}
	return requestID
}

// mapError maps domain errors to HTTP status codes and public messages
func (h *BaseHandler) mapError(
	err error,
	customMessage string,
) (statusCode int, errorCode string, publicMessage string) {
	switch e := err.(type) {
	case *domainError.ValidationError:
		return http.StatusBadRequest, "VALIDATION_ERROR", e.Message
	case *domainError.NotFoundError:
		return http.StatusNotFound, "NOT_FOUND", e.Message
	case *domainError.ConflictError:
		return http.StatusConflict, "CONFLICT", e.Message
	case *domainError.UnauthorizedError:
		return http.StatusUnauthorized, "UNAUTHORIZED", e.Message
	case *domainError.ForbiddenError:
		return http.StatusForbidden, "FORBIDDEN", e.Message
	case *domainError.InternalError:
		// Don't expose internal error details to client
		return http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error"
	default:
		// Unknown error - treat as internal
		return http.StatusInternalServerError, "UNKNOWN_ERROR", "Internal server error"
	}
}

// validateRequest validates request body using validator
func (h *BaseHandler) validateRequest(c *gin.Context, req interface{}) error {
	if err := c.ShouldBindJSON(req); err != nil {
		return &domainError.ValidationError{Message: "Invalid request format: " + err.Error()}
	}
	return nil
}

// getUserID extracts user ID from JWT token (example)
func (h *BaseHandler) getUserID(c *gin.Context) (string, error) {
	userID, exists := c.Get("user_id")
	if !exists {
		return "", &domainError.UnauthorizedError{Message: "User not authenticated"}
	}

	userIDStr, ok := userID.(string)
	if !ok {
		return "", &domainError.InternalError{Message: "Invalid user ID format"}
	}

	return userIDStr, nil
}

// setPagination sets pagination headers
func (h *BaseHandler) setPagination(c *gin.Context, page, limit, total int64) {
	c.Header("X-Page", fmt.Sprintf("%d", page))
	c.Header("X-Limit", fmt.Sprintf("%d", limit))
	c.Header("X-Total", fmt.Sprintf("%d", total))
	c.Header("X-Total-Pages", fmt.Sprintf("%d", (total+limit-1)/limit))
}
