package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"api-template/internal/application/usecase"
	"api-template/internal/config"
	"api-template/internal/infrastructure/database"
	"api-template/internal/infrastructure/http/handler"
	"api-template/internal/infrastructure/persistence"
	"api-template/pkg/logger"
)

func main() {
	cfg := config.MustLoad()

	log := logger.NewLogger(cfg.Logger.Level, false, ".logs/")

	// Init database
	db, err := database.InitDatabase(cfg.Database.DSN)
	if err != nil {
		log.Fatal("Failed to connect to database: %v", logger.Error(err))
	}

	transactionManager := database.NewGormTransactionManager(db, log)

	// Init layers
	userRepo := persistence.NewUserRepository(db)
	userService := usecase.NewUserUseCase(userRepo, transactionManager)
	userHandler := handler.NewUserHandler(log, cfg, userService)

	router := setupRouter(userHandler)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	// Start server
	go func() {
		log.Info("Server starting on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed to start:", logger.Error(err))
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
		log.Fatal("Server forced to shutdown: %v", logger.Error(err))
	}

	log.Info("Server exited")
}

func setupRouter(userHandler *handler.UserHandler) *gin.Engine {
	router := gin.Default()

	// Middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// Health check
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	// API routes
	api := router.Group("/api/v1")
	{
		users := api.Group("/users")
		{
			users.POST("", userHandler.CreateUser)
			users.GET("", userHandler.GetUsers)
			users.GET("/:id", userHandler.GetUser)
			users.PUT("/:id", userHandler.UpdateUser)
			users.DELETE("/:id", userHandler.DeleteUser)
		}
	}

	return router
}
