package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EnvRoutesPath = "MOCK_SERVER_ROUTES_PATH"

type JSONValue = any

type Endpoint struct {
	Comment         *string   `json:"comment,omitempty"`
	Path            string    `json:"path"`
	Method          string    `json:"method"`
	Status          int       `json:"status"`
	Response        JSONValue `json:"response"`
	Delay           float64   `json:"delay"`
	Name            *string   `json:"name,omitempty"`
	Summary         *string   `json:"summary,omitempty"`
	IncludeInSchema bool      `json:"include_in_schema"`
}

type Manifest struct {
	Endpoints []Endpoint `json:"endpoints"`
}

type rawEndpoint struct {
	Comment         *string         `json:"comment,omitempty"`
	Path            string          `json:"path"`
	Method          string          `json:"method"`
	Status          int             `json:"status"`
	Response        json.RawMessage `json:"response"`
	Delay           float64         `json:"delay"`
	Name            *string         `json:"name"`
	Summary         *string         `json:"summary"`
	IncludeInSchema *bool           `json:"include_in_schema"`
}

type rawManifest struct {
	Endpoints *[]rawEndpoint `json:"endpoints"`
}

func DefaultRoutesPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Join(wd, "routes.json"), nil
}

func ResolveRoutesPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvRoutesPath)); override != "" {
		return filepath.Abs(override)
	}
	return DefaultRoutesPath()
}

func Load() (Manifest, error) {
	path, err := ResolveRoutesPath()
	if err != nil {
		return Manifest{}, err
	}
	return LoadFromPath(path)
}

func LoadFromPath(path string) (Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read routes file %q: %w", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()

	var raw rawManifest
	if err := decoder.Decode(&raw); err != nil {
		return Manifest{}, fmt.Errorf("decode routes file %q: %w", path, err)
	}

	if raw.Endpoints == nil {
		return Manifest{}, errors.New("endpoints is required")
	}

	manifest := Manifest{Endpoints: make([]Endpoint, 0, len(*raw.Endpoints))}
	seen := make(map[string]struct{}, len(*raw.Endpoints))

	for index, endpoint := range *raw.Endpoints {
		normalized, err := normalizeEndpoint(endpoint)
		if err != nil {
			return Manifest{}, fmt.Errorf("endpoint %d: %w", index, err)
		}

		key := normalized.Method + " " + normalized.Path
		if _, exists := seen[key]; exists {
			return Manifest{}, fmt.Errorf("duplicate mock endpoint: %s", key)
		}
		seen[key] = struct{}{}
		manifest.Endpoints = append(manifest.Endpoints, normalized)
	}

	return manifest, nil
}

func normalizeEndpoint(raw rawEndpoint) (Endpoint, error) {
	if strings.TrimSpace(raw.Path) == "" {
		return Endpoint{}, errors.New("path must not be empty")
	}

	method := strings.ToUpper(strings.TrimSpace(raw.Method))
	if method == "" {
		return Endpoint{}, errors.New("method must not be empty")
	}

	if raw.Status < 100 || raw.Status > 599 {
		return Endpoint{}, fmt.Errorf("status must be between 100 and 599")
	}

	if raw.Response == nil {
		return Endpoint{}, errors.New("response is required")
	}

	if raw.Delay < 0 {
		return Endpoint{}, errors.New("delay must be greater than or equal to 0")
	}

	includeInSchema := true
	if raw.IncludeInSchema != nil {
		includeInSchema = *raw.IncludeInSchema
	}

	var response JSONValue
	if err := json.Unmarshal(raw.Response, &response); err != nil {
		return Endpoint{}, fmt.Errorf("decode response: %w", err)
	}

	return Endpoint{
		Path:            raw.Path,
		Method:          method,
		Status:          raw.Status,
		Response:        response,
		Delay:           raw.Delay,
		Name:            raw.Name,
		Summary:         raw.Summary,
		IncludeInSchema: includeInSchema,
	}, nil
}
