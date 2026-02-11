package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"PingMe/internal/config"
	"PingMe/internal/handler"
	"PingMe/internal/handler/auth"
	"PingMe/internal/handler/userprofile"
	"PingMe/internal/handler/ws"
	"PingMe/internal/logger"
	"PingMe/internal/middleware"
	"PingMe/internal/pkg/database"
	"PingMe/internal/pkg/jwt"
	userrepo "PingMe/internal/repository/user"
	"PingMe/internal/service"
	"PingMe/pkg/response"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	configPath := "config/local.yml"
	if envPath := os.Getenv("APP_CONF"); envPath != "" {
		configPath = envPath
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger.Init(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.Output)
	logger.Info("Starting PingMe IM server", "env", cfg.App.Env)

	// Initialize database
	db, err := database.Init(&cfg.Database)
	if err != nil {
		logger.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	logger.Info("Database connected successfully")

	// Initialize repositories
	userRepo := userrepo.NewRepository(db)

	// Initialize user schema
	if err := userRepo.InitSchema(); err != nil {
		logger.Error("Failed to initialize user schema", "error", err)
		os.Exit(1)
	}
	logger.Info("User schema initialized")

	// Initialize services
	jwtSvc := jwt.NewTokenService(&cfg.JWT)
	userSvc := service.NewService(userRepo, jwtSvc)

	// Initialize handlers
	baseHandler := handler.NewHandler(cfg)
	authHandler := auth.NewHandler(userSvc)
	userHandler := userprofile.NewHandler(userSvc)
	wsHandler := ws.NewHandler(cfg)

	// Set Gin mode based on environment
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize Gin router
	r := gin.New()

	// Apply global middleware
	r.Use(middleware.RecoveryMiddleware())
	r.Use(middleware.RequestIDMiddleware())
	r.Use(middleware.CORSMiddleware())

	// Health and version endpoints (no auth required)
	r.GET("/health", baseHandler.HealthCheck)
	r.GET("/version", baseHandler.VersionInfo)

	// WebSocket endpoint
	r.GET("/ws", wsHandler.HandleWebSocket)
	r.GET("/ws/stats", wsHandler.GetConnectionStats)

	// API v1 group
	v1 := r.Group("/api/v1")
	{
		// Auth routes (no auth required)
		auth := v1.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
		}

		// User routes (auth required)
		userRoutes := v1.Group("/user")
		userRoutes.Use(middleware.AuthMiddleware(jwtSvc))
		{
			userRoutes.GET("/profile", userHandler.GetProfile)
			userRoutes.PUT("/profile", userHandler.UpdateProfile)
		}
	}

	// 404 handler
	r.NoRoute(func(c *gin.Context) {
		c.JSON(404, response.FailWithMessage("not found", 404))
	})

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		addr := fmt.Sprintf("%s:%d", cfg.App.Host, cfg.App.Port)
		logger.Info("Server listening", "address", addr)
		if err := r.Run(addr); err != nil {
			logger.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	logger.Info("Shutting down server...")

	// Shutdown WebSocket hub
	hubCtx, hubCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer hubCancel()
	
	// Send signal to shutdown hub
	select {
	case wsHandler.Hub.Unregister <- nil:
	case <-hubCtx.Done():
	}
}
