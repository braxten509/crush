package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/home"
)

type InstalledSkill struct {
	CatalogEntry
	Enabled bool `json:"enabled"`
}

// Management is implemented by both local and server-backed workspaces.
type Management interface {
	ListInstalledSkills(context.Context) ([]InstalledSkill, error)
	SetSkillEnabled(context.Context, string, bool) error
	InstallSkill(context.Context, DirectorySkill) error
}

func ConfigDiscovery(store *config.ConfigStore) DiscoveryConfig {
	opts := store.Config().Options
	cfg := DiscoveryConfig{WorkingDir: store.WorkingDir()}
	if opts != nil {
		cfg.SkillsPaths = opts.SkillsPaths
		cfg.DisabledSkills = opts.DisabledSkills
	}
	if store.Resolver() != nil {
		cfg.Resolver = store.Resolver().ResolveValue
	}
	return cfg
}

// Refresh replaces the discovery snapshot before publishing it.
func (m *Manager) Refresh(cfg DiscoveryConfig) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	all, active, states := DiscoverFromConfig(cfg)
	m.mu.Lock()
	m.allSkills, m.activeSkills = all, active
	m.resolvedPaths, m.workingDir = cfg.ResolvePaths(), cfg.WorkingDir
	m.mu.Unlock()
	m.PublishStates(states)
}

func Installed(store *config.ConfigStore, mgr *Manager) []InstalledSkill {
	cfg := ConfigDiscovery(store)
	mgr.Refresh(cfg)
	entries := Catalog(mgr.AllSkills(), mgr.ResolvedPaths(), mgr.WorkingDir())
	result := make([]InstalledSkill, 0, len(entries))
	for _, entry := range entries {
		result = append(result, InstalledSkill{CatalogEntry: entry, Enabled: !slices.Contains(cfg.DisabledSkills, entry.Name)})
	}
	slices.SortFunc(result, func(a, b InstalledSkill) int { return strings.Compare(a.Name, b.Name) })
	return result
}

func SetEnabled(store *config.ConfigStore, mgr *Manager, name string, enabled bool) error {
	if !slices.ContainsFunc(Installed(store, mgr), func(s InstalledSkill) bool { return s.Name == name }) {
		return fmt.Errorf("skill %q is not installed", name)
	}
	if err := store.SetSkillEnabled(name, enabled); err != nil {
		return err
	}
	mgr.Refresh(ConfigDiscovery(store))
	return nil
}

func Install(ctx context.Context, store *config.ConfigStore, mgr *Manager, selected DirectorySkill) error {
	name := selected.SkillID
	if name == "" {
		name = filepath.Base(selected.ID)
	}
	if slices.ContainsFunc(Installed(store, mgr), func(s InstalledSkill) bool { return s.Name == name }) {
		return fmt.Errorf("%s is already installed", name)
	}
	root, err := installationRoot(ConfigDiscovery(store), filepath.Join(home.Dir(), ".agents", "skills"), os.Getenv("CRUSH_SKILLS_DIR"))
	if err != nil {
		return err
	}
	if _, err := NewDirectory().Install(ctx, selected, root); err != nil {
		return err
	}
	// This root is already discovered by the default shared-skills policy.
	mgr.Refresh(ConfigDiscovery(store))
	return nil
}

func installationRoot(cfg DiscoveryConfig, sharedRoot, override string) (string, error) {
	paths := cfg.ResolvePaths()
	for _, candidate := range []string{home.Long(override), sharedRoot} {
		if candidate == "" {
			continue
		}
		if slices.ContainsFunc(paths, func(p string) bool { return filepath.Clean(p) == filepath.Clean(candidate) }) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no shared skill installation directory is enabled for this workspace")
}
