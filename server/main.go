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

	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func main() {
	port := flag.Int("port", 8090, "HTTP server port")
	serverURL := flag.String("server-url", "wss://game.spacemolt.com/ws", "Game server WebSocket URL")
	credsBackend := flag.String("creds-backend", "file", "Credentials backend: file, sqlite, env")
	credsPath := flag.String("creds-path", "", "Path to credentials file/directory/database")
	dbPath := flag.String("db-path", "data/spacemolt-knowledge.db", "Path to knowledge database")
	staticDir := flag.String("static-dir", "", "Directory to serve static frontend files from")
	flag.Parse()

	logger := log.New(os.Stdout, "[observer] ", log.LstdFlags|log.Lmsgprefix)

	creds, err := initCredentialsProvider(*credsBackend, *credsPath)
	if err != nil {
		logger.Fatalf("failed to initialize credentials: %v", err)
	}

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{
		DBPath:       *dbPath,
		WAL:          true,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	})
	if err != nil {
		logger.Fatalf("failed to open knowledge database: %v", err)
	}
	defer func() { _ = kb.Close() }()

	server := NewObserverServer(creds, kb, *serverURL, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", server.HandleBrowserWS)
	mux.HandleFunc("GET /api/agents", server.HandleAPIAgents)
	mux.HandleFunc("POST /api/agents", server.HandleAPIAgents)
	mux.HandleFunc("DELETE /api/agents/{username}", server.HandleAPIAgents)
	mux.HandleFunc("GET /api/systems", server.HandleGetSystems)
	mux.HandleFunc("GET /api/systems/{id}", server.HandleGetSystem)
	mux.HandleFunc("GET /api/systems/{id}/pois", server.HandleGetSystemPOIs)

	if *staticDir != "" {
		if info, err := os.Stat(*staticDir); err == nil && info.IsDir() {
			logger.Printf("serving static files from %s", *staticDir)
			mux.Handle("/", newSPAFileServer(*staticDir))
		} else {
			logger.Printf("static directory %q not found, skipping", *staticDir)
		}
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Printf("starting observer server on :%d", *port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Println("shutting down...")

	server.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("shutdown error: %v", err)
	}
	logger.Println("server stopped")
}

func initCredentialsProvider(backend, path string) (credentials.Provider, error) {
	switch backend {
	case "file":
		if path == "" {
			path = "agents"
		}
		return credentials.NewFileProvider(path), nil
	case "sqlite":
		if path == "" {
			path = "credentials.db"
		}
		enc, err := credentials.LoadOrGenerateKey()
		if err != nil {
			return nil, fmt.Errorf("loading encryption key: %w", err)
		}
		return credentials.NewSQLiteProvider(path, enc)
	case "env":
		return credentials.NewEnvProvider("SPACEMOLT"), nil
	default:
		return nil, fmt.Errorf("unknown credentials backend: %q", backend)
	}
}
