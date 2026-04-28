package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDynamicRoutesAndAdminEndpoints(t *testing.T) {
	mockServer := newTestServer(t, `{
		"endpoints": [
			{"path": "/call_metrics", "method": "GET", "status": 200, "response": {"ok": true}},
			{"path": "/upload", "method": "POST", "status": 201, "response": {"message": "Success"}}
		]
	}`)
	testServer := httptest.NewServer(mockServer.Handler())
	defer testServer.Close()

	status, body := requestJSON(t, http.MethodGet, testServer.URL+"/call_metrics")
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("expected /call_metrics 200 ok, got status=%d body=%#v", status, body)
	}

	status, body = requestJSON(t, http.MethodPost, testServer.URL+"/upload")
	if status != http.StatusCreated || body["message"] != "Success" {
		t.Fatalf("expected /upload 201 success, got status=%d body=%#v", status, body)
	}

	status, body = requestJSON(t, http.MethodGet, testServer.URL+"/__mock/health")
	if status != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("expected admin health ok, got status=%d body=%#v", status, body)
	}
}

func TestConcurrentDelaysOverlap(t *testing.T) {
	mockServer := newTestServer(t, `{
		"endpoints": [
			{"path": "/delay/fast", "method": "GET", "status": 200, "response": {"n": "fast"}, "delay": 0.02},
			{"path": "/delay/slow", "method": "GET", "status": 200, "response": {"n": "slow"}, "delay": 0.35}
		]
	}`)
	testServer := httptest.NewServer(mockServer.Handler())
	defer testServer.Close()

	startedAt := time.Now()
	fastDone := make(chan int, 1)
	slowDone := make(chan int, 1)

	go func() {
		fastDone <- requestStatus(testServer.URL + "/delay/fast")
	}()
	go func() {
		slowDone <- requestStatus(testServer.URL + "/delay/slow")
	}()

	if status := <-fastDone; status != http.StatusOK {
		t.Fatalf("fast request status = %d", status)
	}
	if status := <-slowDone; status != http.StatusOK {
		t.Fatalf("slow request status = %d", status)
	}

	if elapsed := time.Since(startedAt); elapsed >= 450*time.Millisecond {
		t.Fatalf("expected delay overlap, got %s", elapsed)
	}
}

func TestReloadSwapsActiveRoutes(t *testing.T) {
	routesPath := writeRoutes(t, `{
		"endpoints": [
			{"path": "/a", "method": "GET", "status": 200, "response": {"x": 1}}
		]
	}`)

	mockServer, err := New(routesPath, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	testServer := httptest.NewServer(mockServer.Handler())
	defer testServer.Close()

	status, body := requestJSON(t, http.MethodGet, testServer.URL+"/a")
	if status != http.StatusOK || body["x"] != float64(1) {
		t.Fatalf("expected initial /a route, got status=%d body=%#v", status, body)
	}

	if err := os.WriteFile(routesPath, []byte(`{
		"endpoints": [
			{"path": "/b", "method": "GET", "status": 200, "response": {"y": 2}}
		]
	}`), 0o600); err != nil {
		t.Fatalf("write replacement routes: %v", err)
	}

	status, body = requestJSON(t, http.MethodPost, testServer.URL+"/__mock/reload")
	if status != http.StatusOK || body["reloaded"] != routesPath {
		t.Fatalf("expected reload response, got status=%d body=%#v", status, body)
	}

	status, body = requestJSON(t, http.MethodGet, testServer.URL+"/b")
	if status != http.StatusOK || body["y"] != float64(2) {
		t.Fatalf("expected replacement /b route, got status=%d body=%#v", status, body)
	}

	response, err := http.Get(testServer.URL + "/a")
	if err != nil {
		t.Fatalf("GET /a after reload: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected /a to be removed after reload, got %d", response.StatusCode)
	}
}

func TestOpenAPIUsesActiveManifest(t *testing.T) {
	mockServer := newTestServer(t, `{
		"endpoints": [
			{"path": "/visible", "method": "GET", "status": 200, "response": {}, "summary": "Visible route"},
			{"path": "/hidden", "method": "GET", "status": 200, "response": {}, "include_in_schema": false}
		]
	}`)
	testServer := httptest.NewServer(mockServer.Handler())
	defer testServer.Close()

	status, body := requestJSON(t, http.MethodGet, testServer.URL+"/openapi.json")
	if status != http.StatusOK {
		t.Fatalf("expected openapi status 200, got %d", status)
	}

	paths, ok := body["paths"].(map[string]any)
	if !ok {
		t.Fatalf("expected paths object, got %#v", body["paths"])
	}
	if _, ok := paths["/visible"]; !ok {
		t.Fatalf("expected /visible in OpenAPI paths: %#v", paths)
	}
	if _, ok := paths["/hidden"]; ok {
		t.Fatalf("expected /hidden to be excluded from OpenAPI paths: %#v", paths)
	}
}

func TestRequestLoggingIncludesStatusAndDuration(t *testing.T) {
	var logs bytes.Buffer
	mockServer := newTestServerWithLogger(t, `{
		"endpoints": [
			{"path": "/created", "method": "POST", "status": 201, "response": {"ok": true}}
		]
	}`, log.New(&logs, "", 0))
	testServer := httptest.NewServer(mockServer.Handler())
	defer testServer.Close()

	status, body := requestJSON(t, http.MethodPost, testServer.URL+"/created?source=test")
	if status != http.StatusCreated || body["ok"] != true {
		t.Fatalf("expected created response, got status=%d body=%#v", status, body)
	}

	assertLogContains(t, logs.String(),
		"request method=POST",
		"path=/created?source=test",
		"status=201",
		"duration=",
		"remote=",
		"request_id=",
	)
}

func TestRequestLoggingIncludesNotFoundStatus(t *testing.T) {
	var logs bytes.Buffer
	mockServer := newTestServerWithLogger(t, `{"endpoints": []}`, log.New(&logs, "", 0))
	testServer := httptest.NewServer(mockServer.Handler())
	defer testServer.Close()

	response, err := http.Get(testServer.URL + "/missing")
	if err != nil {
		t.Fatalf("GET /missing: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected missing route status 404, got %d", response.StatusCode)
	}

	assertLogContains(t, logs.String(),
		"request method=GET",
		"path=/missing",
		"status=404",
		"duration=",
	)
}

func TestIsTruthyEnv(t *testing.T) {
	for _, value := range []string{"1", " true ", "YES", "On"} {
		if !IsTruthyEnv(value) {
			t.Fatalf("expected %q to be truthy", value)
		}
	}
	if IsTruthyEnv("0") {
		t.Fatal("expected 0 to be false")
	}
}

func newTestServer(t *testing.T, content string) *Server {
	t.Helper()

	return newTestServerWithLogger(t, content, nil)
}

func newTestServerWithLogger(t *testing.T, content string, logger *log.Logger) *Server {
	t.Helper()

	mockServer, err := New(writeRoutes(t, content), logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return mockServer
}

func assertLogContains(t *testing.T, output string, expectedParts ...string) {
	t.Helper()

	for _, expectedPart := range expectedParts {
		if !strings.Contains(output, expectedPart) {
			t.Fatalf("expected log to contain %q, got %q", expectedPart, output)
		}
	}
}

func writeRoutes(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "routes.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write routes: %v", err)
	}
	return path
}

func requestJSON(t *testing.T, method string, url string) (int, map[string]any) {
	t.Helper()

	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer response.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return response.StatusCode, body
}

func requestStatus(url string) int {
	response, err := http.Get(url)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	return response.StatusCode
}
