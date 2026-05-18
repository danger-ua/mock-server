package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
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

// wholeResponseParam matches a string consisting of exactly `{paramName}` for response interpolation (typed substitution).
var wholeResponseParamRE = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

type Server struct {
	routesPath string
	logger     *log.Logger
	state      atomic.Value
}

type routeTable struct {
	manifest manifest.Manifest
	byMethod map[string][]manifest.Endpoint
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
		byMethod: make(map[string][]manifest.Endpoint),
	}
	for _, endpoint := range loaded.Endpoints {
		table.byMethod[endpoint.Method] = append(table.byMethod[endpoint.Method], endpoint)
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
		return routeTable{byMethod: map[string][]manifest.Endpoint{}}
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
	endpoints := s.table().byMethod[r.Method]
	if len(endpoints) == 0 {
		http.NotFound(w, r)
		return
	}

	reqSegments := splitRequestPath(r.URL.Path)
	var matched manifest.Endpoint
	var params map[string]string
	var found bool
	for _, endpoint := range endpoints {
		got, ok := matchSegments(endpoint.Segments, reqSegments)
		if ok {
			matched = endpoint
			params = got
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	if matched.Delay > 0 {
		timer := time.NewTimer(time.Duration(matched.Delay * float64(time.Second)))
		defer timer.Stop()

		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
	}

	writeJSON(w, matched.Status, interpolateResponse(matched.Response, params))
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildOpenAPI(s.table().manifest))
}

func splitRequestPath(path string) []string {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return nil
	}
	var out []string
	for _, segment := range strings.Split(trimmed, "/") {
		if segment != "" {
			out = append(out, segment)
		}
	}
	return out
}

func matchSegments(pattern []manifest.PathSegment, requestSegments []string) (map[string]string, bool) {
	if len(pattern) != len(requestSegments) {
		return nil, false
	}
	params := make(map[string]string)
	for index, segment := range pattern {
		value := requestSegments[index]
		if segment.IsParam {
			if value == "" {
				return nil, false
			}
			params[segment.Param] = value
			continue
		}
		if segment.Literal != value {
			return nil, false
		}
	}
	return params, true
}

func interpolateResponse(value any, params map[string]string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, vv := range v {
			out[key] = interpolateResponse(vv, params)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for index, vv := range v {
			out[index] = interpolateResponse(vv, params)
		}
		return out
	case string:
		return interpolateString(v, params)
	default:
		return value
	}
}

func interpolateString(s string, params map[string]string) any {
	match := wholeResponseParamRE.FindStringSubmatch(s)
	if len(match) == 2 {
		name := match[1]
		rawValue, ok := params[name]
		if ok {
			var typed any
			if err := json.Unmarshal([]byte(rawValue), &typed); err == nil {
				return typed
			}
			return rawValue
		}
	}
	out := s
	for paramName, rawValue := range params {
		out = strings.ReplaceAll(out, "{"+paramName+"}", rawValue)
	}
	return out
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
