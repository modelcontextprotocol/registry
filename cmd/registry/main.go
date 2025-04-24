package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/registry/internal/api"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
)

func main() {
	// Parse command line flags
	showVersion := flag.Bool("version", false, "Display version information")
	flag.Parse()

	// Show version information if requested
	if *showVersion {
		fmt.Printf("MCP Registry v%s\n", Version)
		fmt.Printf("Git commit: %s\n", GitCommit)
		fmt.Printf("Build time: %s\n", BuildTime)
		return
	}

	log.Printf("Starting MCP Registry Application v%s (commit: %s)", Version, GitCommit)

	// Initialize configuration
	cfg := config.NewConfig()

	// Initialize services based on environment
	var registryService service.RegistryService

	if cfg.Environment == "test" {
		// Use fake registry service in test environment
		registryService = service.NewFakeRegistryService()
		log.Println("Using fake registry service with dummy data (test environment)")
	} else {
		// Use MongoDB for real registry service in production/other environments
		// Create a context with timeout for MongoDB connection
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Connect to MongoDB
		mongoDB, err := database.NewMongoDB(ctx, cfg.DatabaseURL, cfg.DatabaseName, cfg.CollectionName)
		if err != nil {
			log.Fatalf("Failed to connect to MongoDB: %v", err)
		}

		// Create registry service with MongoDB
		registryService = service.NewRegistryServiceWithDB(mongoDB)
		log.Printf("Using MongoDB-based registry service (%s environment)", cfg.Environment)
		log.Printf("MongoDB database name: %s", cfg.DatabaseName)
		log.Printf("MongoDB collection name: %s", cfg.CollectionName)

		// Store the MongoDB instance for later cleanup
		defer func() {
			if err := mongoDB.Close(); err != nil {
				log.Printf("Error closing MongoDB connection: %v", err)
			} else {
				log.Println("MongoDB connection closed successfully")
			}
		}()
	}

	// Initialize HTTP server
	server := api.NewServer(cfg, registryService)

	// Start server in a goroutine so it doesn't block signal handling
	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Create context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Gracefully shutdown the server
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}
