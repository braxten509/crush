// Package providers fetches and caches the provider catalogs Crush offers
// (Catwalk's catalog and the Charm Hyper provider), and talks to provider
// endpoints on behalf of package config.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/charmbracelet/crush/internal/config/internal/fileutil"
	"github.com/charmbracelet/x/etag"
)

// syncer fetches a catalog once and memoizes the result.
type syncer[T any] interface {
	Get(context.Context) (T, error)
}

// Cache stores a catalog as JSON in a file shared by Crush instances.
type Cache[T any] struct {
	path string
}

// NewCache returns a Cache backed by the file at path.
func NewCache[T any](path string) Cache[T] {
	return Cache[T]{path: path}
}

// Get reads the cached catalog and its etag.
func (c Cache[T]) Get() (T, string, error) {
	var v T
	data, err := fileutil.ReadFile(c.path)
	if err != nil {
		return v, "", fmt.Errorf("failed to read provider cache file: %w", err)
	}

	if err := json.Unmarshal(data, &v); err != nil {
		return v, "", fmt.Errorf("failed to unmarshal provider data from cache: %w", err)
	}

	return v, etag.Of(data), nil
}

// Store writes the catalog to the cache file atomically.
func (c Cache[T]) Store(v T) error {
	slog.Info("Saving provider data to disk", "path", c.path)
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for provider cache: %w", err)
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal provider data: %w", err)
	}

	// Written through a temporary file and renamed into place. Several Crush
	// instances start independently and race to refresh this cache, and a
	// truncating write would let one of them read a half-written catalog and
	// silently fall back to the bundled copy.
	if err := fileutil.AtomicWriteFile(c.path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write provider data to cache: %w", err)
	}
	return nil
}
