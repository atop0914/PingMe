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
	"PingMe/internal/gateway"
	"PingMe/internal/handler"
	"PingMe/internal/handler/auth"
	"PingMe/internal/handler/userprofile"
	"PingMe/internal/handler/ws"
	"PingMe/internal/logger"
	"PingMe/internal/middleware"
	"PingMe/internal/pkg/database"
	"PingMe/internal/pkg/jwt"
	userrepo "PingMe/internal/repository/user"
	msgrepo "PingMe/internal/repository/message"
	"PingMe/internal/service"
	msghandler "PingMe/internal/handler/message"
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
	msgRepo := msgrepo.NewRepository(db)

	// Initialize user schema
	if err := userRepo.InitSchema(); err != nil {
		logger.Error("Failed to initialize user schema", "error", err)
		os.Exit(1)
	}
	logger.Info("User schema initialized")

	// Initialize message schema
	if err := msgRepo.InitSchema(); err != nil {
		logger.Error("Failed to initialize message schema", "error", err)
		os.Exit(1)
	}
	logger.Info("Message schema initialized")

	// Initialize WebSocket hub first (before initializing services that depend on it)
	hub := gateway.NewHub(cfg)

	// Initialize Redis client for online status
	if err := hub.InitRedis(); err != nil {
		logger.Warn("Failed to initialize Redis client, online status disabled",
			"error", err)
		// Continue without Redis - online status won't be shared across instances
	}

	// Initialize services
	jwtSvc := jwt.NewTokenService(&cfg.JWT)
	userSvc := service.NewService(userRepo, jwtSvc)
	msgSvc := service.NewMessageService(msgRepo, userRepo, hub)

	// 设置消息服务到 Hub（用于通过 WS 发送消息）
	hub.MessageService = msgSvc

	// Initialize handlers
	baseHandler := handler.NewHandler(cfg)
	authHandler := auth.NewHandler(userSvc)
	userHandler := userprofile.NewHandler(userSvc)
	messageHandler := msghandler.NewMessageHandler(msgSvc)

	wsHandler := ws.NewHandler(cfg, hub)

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
		authRoutes := v1.Group("/auth")
		{
			authRoutes.POST("/register", authHandler.Register)
			authRoutes.POST("/login", authHandler.Login)
		}

		// User routes (auth required)
		userRoutes := v1.Group("/user")
		userRoutes.Use(middleware.AuthMiddleware(jwtSvc))
		{
			userRoutes.GET("/profile", userHandler.GetProfile)
			userRoutes.PUT("/profile", userHandler.UpdateProfile)
		}

		// Message routes (auth required)
		messageRoutes := v1.Group("/messages")
		messageRoutes.Use(middleware.AuthMiddleware(jwtSvc))
		{
			messageRoutes.POST("", messageHandler.SendMessage)          // 发送消息
			messageRoutes.GET("/history", messageHandler.GetHistory)    // 获取历史消息
			messageRoutes.GET("/offline", messageHandler.PullOfflineMessages) // 拉取离线消息
		}

		// Conversation routes (auth required)
		conversationRoutes := v1.Group("/conversations")
		conversationRoutes.Use(middleware.AuthMiddleware(jwtSvc))
		{
			conversationRoutes.GET("", messageHandler.GetConversations)       // 获取会话列表
			conversationRoutes.GET("/:id", messageHandler.GetConversation)   // 获取会话详情
		}
	}

	// 404 handler
	r.NoRoute(func(c *gin.Context) {
		c.JSON(404, response.FailWithMessage("not found", 404))
	})

	// Create context for hub
	ctx, cancel := context.WithCancel(context.Background())

	// Start hub with context
	hub.Run(ctx)

	// Start cleanup task for online status
	if hub.RedisClient != nil {
		hub.StartCleanupTask(ctx)
	}

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

	// Stop cleanup task
	hub.StopCleanupTask()

	// Stop message deduplicator
	if hub.Deduplicator != nil {
		hub.Deduplicator.Stop()
	}

	// Cancel hub context
	cancel()

	// Close Redis client
	if hub.RedisClient != nil {
		hub.RedisClient.Close()
	}

	// Send signal to shutdown hub
	hubCtx, hubCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer hubCancel()

	// Wait for hub to shutdown
	select {
	case <-hubCtx.Done():
	}

	logger.Info("Server shutdown complete")
}
