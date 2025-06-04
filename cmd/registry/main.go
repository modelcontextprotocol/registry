package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/registry/internal/api"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/service"
)

func main() {
	// Parse command line flags
	showVersion := flag.Bool("version", false, "Display version information")
	flag.Parse()

	// Show version information if requested
	if *showVersion {
		log.Printf("MCP Registry v%s\n", Version)
		log.Printf("Git commit: %s\n", GitCommit)
		log.Printf("Build time: %s\n", BuildTime)
		return
	}

	log.Printf("Starting MCP Registry Application v%s (commit: %s)", Version, GitCommit)

	// Initialize configuration
	cfg := config.NewConfig()

	// Initialize services based on environment
	var registryService service.RegistryService

	switch cfg.DatabaseType {
	case config.DatabaseTypeMemory:
		memoryDB := database.NewMemoryDB(map[string]*model.Server{})
		registryService = service.NewRegistryServiceWithDB(memoryDB)
	case config.DatabaseTypeMongoDB:
		// Use MongoDB for real registry service in production/other environments
		// Create a context with timeout for MongoDB connection
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Connect to MongoDB
		mongoDB, err := database.NewMongoDB(ctx, cfg.DatabaseURL, cfg.DatabaseName, cfg.CollectionName)
		if err != nil {
			log.Printf("Failed to connect to MongoDB: %v", err)
			return
		}

		// Create registry service with MongoDB
		registryService = service.NewRegistryServiceWithDB(mongoDB)
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

		if cfg.SeedImport {
			log.Println("Importing data...")
			if err := database.ImportSeedFile(mongoDB, cfg.SeedFilePath); err != nil {
				log.Printf("Failed to import seed file: %v", err)
			} else {
				log.Println("Data import completed successfully")
			}
		}
	default:
		log.Printf("Invalid database type: %s; supported types: %s, %s", cfg.DatabaseType, config.DatabaseTypeMemory, config.DatabaseTypeMongoDB)
		return
	}

	// Initialize authentication services
	authService := auth.NewAuthService(cfg)

	// Initialize HTTP server
	server := api.NewServer(cfg, registryService, authService)

	// Start server in a goroutine so it doesn't block signal handling
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Failed to start server: %v", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Create context with timeout for shutdown
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()

	// Gracefully shutdown the server
	if err := server.Shutdown(sctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}
