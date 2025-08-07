package main

import (
	"context"
	"microservice-template/config"
	"microservice-template/internal/handler"
	"microservice-template/internal/middleware"
	"microservice-template/internal/repository"
	"microservice-template/internal/usecase"
	"microservice-template/pkg/logger"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize dependencies
	cfg := config.Load()
	log := logger.NewLogger(cfg.LogLevel, true, "logs/")

	// Initialize layers
	userRepo := repository.NewUserRepository()
	userService := usecase.NewUserService(userRepo, log)
	userHandler := handler.NewUserHandler(userService, log)

	// Setup Gin
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(middleware.CORS())

	// Health check
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Setup routes
	setupRoutes(router, userHandler)

	// Start server
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// Graceful start
	go func() {
		log.Info("Starting server on port %s", logger.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start server: %s", logger.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown: %s", logger.Error(err))
	}

	log.Info("Server exited")
}

func setupRoutes(router *gin.Engine, userHandler *handler.UserHandler) {
	api := router.Group("/api/v1")
	{
		users := api.Group("/users")
		{
			users.GET("", userHandler.List)
			users.POST("", userHandler.Create)
			users.GET("/:id", userHandler.GetByID)
			users.PUT("/:id", userHandler.Update)
			users.DELETE("/:id", userHandler.Delete)
		}
	}
}
