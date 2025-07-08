package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"api-template/internal/application/usecase"
	"api-template/internal/config"
	"api-template/internal/domain/entity"
	domainError "api-template/internal/domain/error"
	"api-template/internal/domain/repository"
	"api-template/internal/infrastructure/http/dto"
	"api-template/pkg/logger"
)

// UserHandler handles user-related HTTP requests
type UserHandler struct {
	*BaseHandler
	userUseCase *usecase.UserUseCase
}

// NewUserHandler creates a new UserHandler instance
func NewUserHandler(
	logger logger.Logger,
	config *config.Config,
	userUseCase *usecase.UserUseCase,
) *UserHandler {
	return &UserHandler{
		BaseHandler: NewBaseHandler(logger, config),
		userUseCase: userUseCase,
	}
}

// CreateUser handles user creation
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest

	// Validate request
	if err := h.validateRequest(c, &req); err != nil {
		h.fail(c, err, "Invalid user creation request")
		return
	}

	// Create user through use case
	user, err := h.userUseCase.CreateUser(c.Request.Context(), req.Name, req.Email, req.Age)
	if err != nil {
		h.fail(c, err, "Failed to create user")
		return
	}

	// Return success response
	response := dto.NewUserResponse(user)
	h.created(c, response, "User created successfully")
}

// GetUser handles user retrieval by ID
func (h *UserHandler) GetUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		h.fail(c, &domainError.ValidationError{Message: "User ID is required"}, "Invalid user ID")
		return
	}

	// Get user through use case
	user, err := h.userUseCase.GetUser(c.Request.Context(), entity.UserID(userID))
	if err != nil {
		h.fail(c, err, "Failed to retrieve user")
		return
	}

	// Return success response
	response := dto.NewUserResponse(user)
	h.success(c, response, "User retrieved successfully")
}

// GetUsers handles user list retrieval with pagination
func (h *UserHandler) GetUsers(c *gin.Context) {
	// Parse pagination parameters
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	// Get users through use case
	users, total, err := h.userUseCase.GetUsers(
		c.Request.Context(),
		&repository.UserFilter{Page: page, PageSize: limit},
	)
	if err != nil {
		h.fail(c, err, "Failed to retrieve users")
		return
	}

	// Set pagination headers
	h.setPagination(c, int64(page), int64(limit), total)

	// Convert to dto
	userResponses := make([]*dto.UserResponse, 0, len(users))
	for _, u := range users {
		userResponses = append(userResponses, dto.NewUserResponse(u))
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))

	response := dto.GetUsersResponse{
		Users:      userResponses,
		Total:      total,
		Page:       page,
		PageSize:   limit,
		TotalPages: totalPages,
	}

	h.success(c, response, "Users retrieved successfully")
}

// UpdateUser handles user update
func (h *UserHandler) UpdateUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		h.fail(c, &domainError.ValidationError{Message: "User ID is required"}, "Invalid user ID")
		return
	}

	var req dto.UpdateUserRequest
	if err := h.validateRequest(c, &req); err != nil {
		h.fail(c, err, "Invalid user update request")
		return
	}

	// Update user through use case
	err := h.userUseCase.UpdateUser(
		c.Request.Context(),
		entity.UserID(userID),
		req.Name,
		req.Email,
		req.Age,
	)
	if err != nil {
		h.fail(c, err, "Failed to update user")
		return
	}

	// Return success response
	h.success(c, "{}", "User updated successfully")
}

// DeleteUser handles user deletion
func (h *UserHandler) DeleteUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		h.fail(c, &domainError.ValidationError{Message: "User ID is required"}, "Invalid user ID")
		return
	}

	// Delete user through use case
	err := h.userUseCase.DeleteUser(c.Request.Context(), entity.UserID(userID))
	if err != nil {
		h.fail(c, err, "Failed to delete user")
		return
	}

	// Return success response
	h.success(c, nil, "User deleted successfully")
}

// GetCurrentUser handles current authenticated user retrieval
func (h *UserHandler) GetCurrentUser(c *gin.Context) {
	// Extract user ID from token
	userID, err := h.getUserID(c)
	if err != nil {
		h.fail(c, err, "Failed to get current user")
		return
	}

	// Get user through use case
	user, err := h.userUseCase.GetUser(c.Request.Context(), entity.UserID(userID))
	if err != nil {
		h.fail(c, err, "Failed to retrieve current user")
		return
	}

	// Return success response
	response := dto.NewUserResponse(user)
	h.success(c, response, "Current user retrieved successfully")
}
