package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jansitarski/mailtagger/internal/config"
)

// Server wraps an HTTP server with a chi router and graceful shutdown.
type Server struct {
	httpServer *http.Server
	router     chi.Router
	logger     *slog.Logger
}

// New creates a new Server from the given HTTPConfig.
func New(cfg config.HTTPConfig, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}

	readTimeout := 10 * time.Second
	writeTimeout := 10 * time.Second

	if cfg.ReadTimeout != "" {
		d, err := time.ParseDuration(cfg.ReadTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid read_timeout %q: %w", cfg.ReadTimeout, err)
		}
		readTimeout = d
	}

	if cfg.WriteTimeout != "" {
		d, err := time.ParseDuration(cfg.WriteTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid write_timeout %q: %w", cfg.WriteTimeout, err)
		}
		writeTimeout = d
	}

	addr := cfg.Addr
	if addr == "" {
		addr = ":8080"
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	return &Server{
		httpServer: srv,
		router:     r,
		logger:     logger,
	}, nil
}

// Router returns the chi router for registering routes.
func (s *Server) Router() chi.Router {
	return s.router
}

// Start begins listening and serving HTTP requests. It blocks until the server
// is shut down or encounters a fatal error.
func (s *Server) Start() error {
	s.logger.Info("http server listening", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server error: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server with the given timeout.
func (s *Server) Shutdown(timeout time.Duration) error {
	s.logger.Info("http server shutting down", "timeout", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}
