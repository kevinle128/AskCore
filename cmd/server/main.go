package main

import (
	"errors"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"AskCore/internal/database"
	"AskCore/internal/handlers"
	"AskCore/proto"
)

var logger *zap.Logger

func initLogger() {
	var err error
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "production" {
		logger, err = zap.NewProduction()
	} else {
		logger, err = zap.NewDevelopment()
	}
	if err != nil {
		panic(err)
	}
}

func main() {
	// Load environment variables
	godotenv.Load()

	// Initialize logger
	initLogger()
	defer logger.Sync()

	logger.Info("Starting AskCore server")

	// Initialize database
	db, err := database.InitDB()
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	logger.Info("Database connected successfully")
	_ = db // Use db in your handlers

	// Get host from environment
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	// Get HTTP port from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := host + ":" + port

	// Start the gRPC server in the background; the HTTP server runs in the main goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	grpcServer := grpc.NewServer()

	// Start gRPC server
	go func() {
		defer wg.Done()
		grpcPort := os.Getenv("GRPC_PORT")
		if grpcPort == "" {
			grpcPort = "50051"
		}
		grpcAddr := host + ":" + grpcPort
		logger.Info("Starting gRPC server", zap.String("address", grpcAddr))

		lis, err := net.Listen("tcp", grpcAddr)
		if err != nil {
			logger.Fatal("Failed to listen for gRPC", zap.Error(err))
		}

		proto.RegisterGreeterServer(grpcServer, &proto.GreeterService{})
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			logger.Fatal("Failed to serve gRPC", zap.Error(err))
		}
	}()

	logger.Info("Starting HTTP server", zap.String("address", addr))

	// CORS: pinned to CORS_ORIGIN when set, permissive otherwise.
	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "*"
	}

	// Create Echo instance
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{corsOrigin},
	}))

	// Health check endpoint
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status":  "ok",
			"message": "Server is running",
		})
	})

	// Root endpoint
	e.GET("/", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"message": "Welcome to AskCore!",
		})
	})

	e.GET("/api/users", handlers.GetUsers)
	e.POST("/api/users", handlers.CreateUser)
	e.GET("/api/users/:id", handlers.GetUser)
	e.PUT("/api/users/:id", handlers.UpdateUser)
	e.DELETE("/api/users/:id", handlers.DeleteUser)
	e.GET("/api/posts", handlers.GetPosts)
	e.POST("/api/posts", handlers.CreatePost)
	e.GET("/api/posts/:id", handlers.GetPost)
	e.PUT("/api/posts/:id", handlers.UpdatePost)
	e.DELETE("/api/posts/:id", handlers.DeletePost)

	// Start server
	if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
		logger.Fatal("Failed to start server", zap.Error(err))
	}


	wg.Wait()
}
