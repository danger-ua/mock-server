package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromPathNormalizesEndpoint(t *testing.T) {
	path := writeManifest(t, `{
		"endpoints": [{
			"path": "/upload",
			"method": " post ",
			"status": 201,
			"response": {"message": "Success"}
		}]
	}`)

	loaded, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}

	endpoint := loaded.Endpoints[0]
	if endpoint.Method != "POST" {
		t.Fatalf("expected method POST, got %q", endpoint.Method)
	}
	if !endpoint.IncludeInSchema {
		t.Fatal("expected include_in_schema to default true")
	}
}

func TestLoadFromPathRejectsDuplicateRoutes(t *testing.T) {
	path := writeManifest(t, `{
		"endpoints": [
			{"path": "/a", "method": "GET", "status": 200, "response": {}},
			{"path": "/a", "method": "get", "status": 404, "response": {}}
		]
	}`)

	_, err := LoadFromPath(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate mock endpoint") {
		t.Fatalf("expected duplicate route error, got %v", err)
	}
}

func TestLoadFromPathRejectsUnknownFields(t *testing.T) {
	path := writeManifest(t, `{
		"endpoints": [{
			"path": "/a",
			"method": "GET",
			"status": 200,
			"response": {},
			"unexpected": true
		}]
	}`)

	_, err := LoadFromPath(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestLoadFromPathAllowsNullResponse(t *testing.T) {
	path := writeManifest(t, `{
		"endpoints": [{
			"path": "/a",
			"method": "GET",
			"status": 200,
			"response": null
		}]
	}`)

	loaded, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if loaded.Endpoints[0].Response != nil {
		t.Fatalf("expected nil response, got %#v", loaded.Endpoints[0].Response)
	}
}

func TestLoadUsesEnvironmentOverride(t *testing.T) {
	path := writeManifest(t, `{"endpoints": []}`)
	t.Setenv(EnvRoutesPath, path)

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Endpoints) != 0 {
		t.Fatalf("expected no endpoints, got %d", len(loaded.Endpoints))
	}

	resolved, err := ResolveRoutesPath()
	if err != nil {
		t.Fatalf("ResolveRoutesPath() error = %v", err)
	}
	if resolved != path {
		t.Fatalf("expected resolved path %q, got %q", path, resolved)
	}
}

func TestLoadFromPathRejectsDuplicateParameterizedRoutes(t *testing.T) {
	path := writeManifest(t, `{
		"endpoints": [
			{"path": "/x/{a}", "method": "GET", "status": 200, "response": {}},
			{"path": "/x/{b}", "method": "GET", "status": 200, "response": {}}
		]
	}`)

	_, err := LoadFromPath(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate mock endpoint") {
		t.Fatalf("expected duplicate route error, got %v", err)
	}
}

func TestLoadFromPathRejectsInvalidPlaceholderEmptyName(t *testing.T) {
	path := writeManifest(t, `{
		"endpoints": [{
			"path": "/x/{}",
			"method": "GET",
			"status": 200,
			"response": {}
		}]
	}`)

	_, err := LoadFromPath(path)
	if err == nil || !strings.Contains(err.Error(), "invalid path placeholder") {
		t.Fatalf("expected invalid placeholder error, got %v", err)
	}
}

func TestLoadFromPathRejectsInvalidPlaceholderLeadingDigit(t *testing.T) {
	path := writeManifest(t, `{
		"endpoints": [{
			"path": "/x/{1bad}",
			"method": "GET",
			"status": 200,
			"response": {}
		}]
	}`)

	_, err := LoadFromPath(path)
	if err == nil || !strings.Contains(err.Error(), "invalid path placeholder") {
		t.Fatalf("expected invalid placeholder error, got %v", err)
	}
}

func writeManifest(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "routes.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}
