package stats

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/projects"
)

// dbSource is a project's database to gather stats from.
type dbSource struct {
	dbPath     string
	projectDir string
}

// Crawl crawls a directory recursively looking for .crush/crush.db files.
func Crawl(ctx context.Context, rootDir string) ([]ProjectStats, error) {
	var dbPaths []dbSource

	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors (permission denied, etc.)
		}

		// Skip common ignored directories early
		if d.IsDir() && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}

		// Look for .crush/crush.db pattern
		if !d.IsDir() && d.Name() == "crush.db" {
			dir := filepath.Dir(path)
			if filepath.Base(dir) == ".crush" {
				projectDir := filepath.Dir(dir)
				dbPaths = append(dbPaths, dbSource{dbPath: path, projectDir: projectDir})
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return gatherStatsFromDBPaths(ctx, dbPaths)
}

// shouldSkipDir returns true for directories that should be skipped during crawling.
func shouldSkipDir(name string) bool {
	skipDirs := map[string]bool{
		".git":             true,
		".svn":             true,
		".hg":              true,
		"node_modules":     true,
		"vendor":           true,
		"dist":             true,
		"build":            true,
		"target":           true,
		".idea":            true,
		".vscode":          true,
		"__pycache__":      true,
		"bin":              true,
		"obj":              true,
		"out":              true,
		"coverage":         true,
		"logs":             true,
		"generated":        true,
		"bower_components": true,
		"jspm_packages":    true,
		".cache":           true,
		".npm":             true,
		".cargo":           true,
		"Library":          true,
		"Applications":     true,
		"System":           true,
	}
	return skipDirs[name]
}

// FromProjects gathers stats from all known projects in projects.json.
func FromProjects(ctx context.Context) ([]ProjectStats, error) {
	projectList, err := projects.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load projects: %w", err)
	}

	var dbPaths []dbSource

	for _, p := range projectList.Projects {
		dbPath := filepath.Join(p.DataDir, "crush.db")
		if _, err := os.Stat(dbPath); err == nil {
			dbPaths = append(dbPaths, dbSource{dbPath: dbPath, projectDir: p.Path})
		}
	}

	return gatherStatsFromDBPaths(ctx, dbPaths)
}

// gatherStatsFromDBPaths gathers stats from a list of database paths in parallel.
func gatherStatsFromDBPaths(ctx context.Context, dbPaths []dbSource) ([]ProjectStats, error) {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []ProjectStats
	)

	sem := make(chan struct{}, 10) // Limit concurrent DB connections

	for _, item := range dbPaths {
		wg.Add(1)
		go func(dbPath, projectDir string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			conn, err := db.ConnectReadOnly(ctx, dbPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: skipping %s: %v\n", dbPath, err)
				return
			}
			defer conn.Close()

			stats, err := Gather(ctx, conn)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: failed to gather stats from %s: %v\n", dbPath, err)
				return
			}

			mu.Lock()
			results = append(results, ProjectStats{
				ProjectPath: projectDir,
				Stats:       stats,
			})
			mu.Unlock()
		}(item.dbPath, item.projectDir)
	}

	wg.Wait()

	return results, nil
}
