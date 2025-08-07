package handler

import (
	"microservice-template/internal/domain"
	"microservice-template/pkg/logger"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	service domain.UserService
	logger  logger.Logger
}

func NewUserHandler(
	service domain.UserService,
	logger logger.Logger,
) *UserHandler {
	return &UserHandler{
		service: service,
		logger:  logger,
	}
}

func (h *UserHandler) GetByID(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	user, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		h.handleError(c, err)
		return
	}

	response := h.toUserResponse(user)
	c.JSON(http.StatusOK, response)
}

func (h *UserHandler) Create(c *gin.Context) {
	var req domain.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	user, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		h.handleError(c, err)
		return
	}

	response := h.toUserResponse(user)
	c.JSON(http.StatusCreated, response)
}

func (h *UserHandler) Update(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req domain.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	user, err := h.service.Update(c.Request.Context(), id, &req)
	if err != nil {
		h.handleError(c, err)
		return
	}

	response := h.toUserResponse(user)
	c.JSON(http.StatusOK, response)
}

func (h *UserHandler) Delete(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *UserHandler) List(c *gin.Context) {
	users, err := h.service.List(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}

	responses := make([]*domain.UserResponse, len(users))
	for i, user := range users {
		responses[i] = h.toUserResponse(user)
	}

	c.JSON(http.StatusOK, responses)
}

// Helper methods
func (h *UserHandler) toUserResponse(user *domain.User) *domain.UserResponse {
	return &domain.UserResponse{
		ID:        user.ID,
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	}
}

func (h *UserHandler) handleError(c *gin.Context, err error) {
	h.logger.Error("Handler error: %s", logger.Error(err))

	// Simple error handling - in production you'd have more sophisticated error types
	switch err.Error() {
	case "user not found":
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
	case "email already exists":
		c.JSON(http.StatusConflict, gin.H{"error": "Email already exists"})
	case "invalid user ID":
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
	}
}
