package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"mock-server/internal/manifest"
)

const (
	WatchEnv             = "MOCK_SERVER_WATCH"
	defaultWatchInterval = time.Second
)

type Server struct {
	routesPath string
	logger     *log.Logger
	state      atomic.Value
}

type routeTable struct {
	manifest manifest.Manifest
	routes   map[string]manifest.Endpoint
}

func New(routesPath string, logger *log.Logger) (*Server, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	}

	server := &Server{
		routesPath: routesPath,
		logger:     logger,
	}

	if err := server.Reload(); err != nil {
		return nil, err
	}

	return server, nil
}

func (s *Server) Handler() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(s.logRequests)
	router.Use(middleware.Recoverer)

	router.Get("/__mock/health", s.handleHealth)
	router.Post("/__mock/reload", s.handleReload)
	router.Get("/openapi.json", s.handleOpenAPI)
	router.NotFound(s.handleDynamic)

	return router
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(recorder, r)

		requestID := middleware.GetReqID(r.Context())
		if requestID == "" {
			requestID = "-"
		}

		s.logger.Printf(
			"request method=%s path=%s status=%d duration=%s remote=%s request_id=%s",
			r.Method,
			r.URL.RequestURI(),
			responseStatus(recorder.Status()),
			time.Since(startedAt),
			r.RemoteAddr,
			requestID,
		)
	})
}

func responseStatus(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}

func (s *Server) Reload() error {
	loaded, err := manifest.LoadFromPath(s.routesPath)
	if err != nil {
		return err
	}

	table := routeTable{
		manifest: loaded,
		routes:   make(map[string]manifest.Endpoint, len(loaded.Endpoints)),
	}
	for _, endpoint := range loaded.Endpoints {
		table.routes[routeKey(endpoint.Method, endpoint.Path)] = endpoint
	}

	s.state.Store(table)
	return nil
}

func (s *Server) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultWatchInterval
	}

	lastModTime := fileModTime(s.routesPath)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentModTime := fileModTime(s.routesPath)
			if currentModTime.IsZero() || !currentModTime.After(lastModTime) {
				continue
			}

			if err := s.Reload(); err != nil {
				s.logger.Printf("invalid manifest after file change: %v", err)
				lastModTime = currentModTime
				continue
			}

			lastModTime = currentModTime
			s.logger.Printf("mock routes reloaded from %s", s.routesPath)
		}
	}
}

func IsTruthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Server) table() routeTable {
	value := s.state.Load()
	if value == nil {
		return routeTable{routes: map[string]manifest.Endpoint{}}
	}
	return value.(routeTable)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReload(w http.ResponseWriter, _ *http.Request) {
	if err := s.Reload(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"reloaded": s.routesPath})
}

func (s *Server) handleDynamic(w http.ResponseWriter, r *http.Request) {
	endpoint, ok := s.table().routes[routeKey(r.Method, r.URL.Path)]
	if !ok {
		http.NotFound(w, r)
		return
	}

	if endpoint.Delay > 0 {
		timer := time.NewTimer(time.Duration(endpoint.Delay * float64(time.Second)))
		defer timer.Stop()

		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
	}

	writeJSON(w, endpoint.Status, endpoint.Response)
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildOpenAPI(s.table().manifest))
}

func routeKey(method string, path string) string {
	return method + "\x00" + path
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func buildOpenAPI(routes manifest.Manifest) map[string]any {
	paths := make(map[string]any)
	for _, endpoint := range routes.Endpoints {
		if !endpoint.IncludeInSchema {
			continue
		}

		pathItem, _ := paths[endpoint.Path].(map[string]any)
		if pathItem == nil {
			pathItem = make(map[string]any)
			paths[endpoint.Path] = pathItem
		}

		method := lowerHTTPMethod(endpoint.Method)
		summary := fmt.Sprintf("Mock %s %s", endpoint.Method, endpoint.Path)
		if endpoint.Summary != nil && *endpoint.Summary != "" {
			summary = *endpoint.Summary
		}

		pathItem[method] = map[string]any{
			"summary": summary,
			"responses": map[string]any{
				fmt.Sprintf("%d", endpoint.Status): map[string]any{
					"description": responseDescription(endpoint.Status, endpoint.Summary),
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type":        "object",
								"title":       "ManifestResponse",
								"description": "Arbitrary JSON from the `response` field in routes.json",
							},
						},
					},
				},
			},
		}
	}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Mock Gateway",
			"description": "Configuration-driven mock HTTP API (see `routes.json`, key `endpoints`).",
			"version":     "0.1.0",
		},
		"paths": paths,
	}
}

func responseDescription(status int, summary *string) string {
	if summary != nil && *summary != "" {
		return *summary
	}
	return fmt.Sprintf("Mock response with HTTP %d from manifest", status)
}

func lowerHTTPMethod(method string) string {
	switch method {
	case http.MethodGet:
		return "get"
	case http.MethodHead:
		return "head"
	case http.MethodPost:
		return "post"
	case http.MethodPut:
		return "put"
	case http.MethodPatch:
		return "patch"
	case http.MethodDelete:
		return "delete"
	case http.MethodConnect:
		return "connect"
	case http.MethodOptions:
		return "options"
	case http.MethodTrace:
		return "trace"
	default:
		return method
	}
}
