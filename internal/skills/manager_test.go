package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestManager_NoGlobalMirrorByDefault(t *testing.T) {
	// Not parallel - touches package-level cache.
	prev := GetLatestStates()
	t.Cleanup(func() { SetLatestStates(prev) })

	SetLatestStates(nil)

	mgrA := NewManager(nil, nil, []*SkillState{{Name: "a", State: StateNormal}})
	mgrB := NewManager(nil, nil, []*SkillState{{Name: "b", State: StateNormal}})

	mgrA.PublishStates(mgrA.States())
	mgrB.PublishStates(mgrB.States())

	// Without WithGlobalMirror, the package-level cache must not be
	// touched by manager construction or PublishStates calls.
	require.Nil(t, GetLatestStates(), "package global must remain untouched")
	require.Equal(t, "a", mgrA.States()[0].Name)
	require.Equal(t, "b", mgrB.States()[0].Name)
}

func TestManager_GlobalMirror(t *testing.T) {
	// Not parallel - touches package-level cache.
	prev := GetLatestStates()
	t.Cleanup(func() { SetLatestStates(prev) })

	SetLatestStates(nil)

	mgr := NewManager(nil, nil, []*SkillState{{Name: "x", State: StateNormal}}, WithGlobalMirror())

	got := GetLatestStates()
	require.Len(t, got, 1)
	require.Equal(t, "x", got[0].Name)

	// PublishStates with mirror enabled forwards to the global cache.
	mgr.SetLatestStates([]*SkillState{{Name: "y", State: StateNormal}})
	got = GetLatestStates()
	require.Len(t, got, 1)
	require.Equal(t, "y", got[0].Name)
}

func TestManager_PublishStatesUpdatesCache(t *testing.T) {
	// Not parallel - exercises WithGlobalMirror, which touches the
	// package-level cache.
	prev := GetLatestStates()
	t.Cleanup(func() { SetLatestStates(prev) })

	SetLatestStates(nil)

	mgr := NewManager(nil, nil, []*SkillState{{Name: "old"}}, WithGlobalMirror())
	t.Cleanup(mgr.Shutdown)

	// PublishStates must update every observable snapshot, not just the
	// SSE subscribers: Manager.States() (used by workspaceToProto on
	// the backend) and skills.GetLatestStates() (read by the TUI on the
	// client process and in local mode) must reflect the new value.
	mgr.PublishStates([]*SkillState{{Name: "new"}})

	got := mgr.States()
	require.Len(t, got, 1)
	require.Equal(t, "new", got[0].Name)

	cached := GetLatestStates()
	require.Len(t, cached, 1)
	require.Equal(t, "new", cached[0].Name)
}

func TestManager_SubscribeReceivesPublishedStates(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil, nil, nil)
	t.Cleanup(mgr.Shutdown)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := mgr.SubscribeEvents(ctx)

	want := []*SkillState{{Name: "k", State: StateNormal}}
	go mgr.PublishStates(want)

	select {
	case ev := <-ch:
		require.Equal(t, "k", ev.Payload.States[0].Name)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for manager event")
	}
}

func TestManager_ConcurrentWorkspacesAreIsolated(t *testing.T) {
	t.Parallel()

	// Two managers without WithGlobalMirror should not see each other's
	// events; this models the multi-workspace backend.
	mgrA := NewManager(nil, nil, nil)
	mgrB := NewManager(nil, nil, nil)
	t.Cleanup(mgrA.Shutdown)
	t.Cleanup(mgrB.Shutdown)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	chA := mgrA.SubscribeEvents(ctx)
	chB := mgrB.SubscribeEvents(ctx)

	go mgrA.PublishStates([]*SkillState{{Name: "from-a"}})

	select {
	case ev := <-chA:
		require.Equal(t, "from-a", ev.Payload.States[0].Name)
	case <-time.After(2 * time.Second):
		t.Fatal("workspace A never received its own event")
	}

	select {
	case ev := <-chB:
		t.Fatalf("workspace B received workspace A's event: %v", ev)
	case <-time.After(100 * time.Millisecond):
		// Expected — B's stream is isolated.
	}
}

// drainEvents counts the events already queued on ch. Publish enqueues
// synchronously, so everything sent before the call is counted.
func drainEvents(ch <-chan pubsub.Event[Event]) int {
	n := 0
	for {
		select {
		case <-ch:
			n++
		case <-time.After(50 * time.Millisecond):
			return n
		}
	}
}

func writeTestSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, SkillFileName),
		[]byte("---\nname: "+name+"\ndescription: "+desc+"\n---\nx\n"), 0o644))
}

// Refresh runs on every skills list read; an unchanged discovery must
// not publish again, or clients that reload on each event loop with
// the engine.
func TestManager_RefreshPublishesOnlyOnChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	writeTestSkill(t, tmp, "steady-skill", "First description.")
	cfg := DiscoveryConfig{SkillsPaths: []string{tmp}}

	mgr := NewManager(nil, nil, nil)
	t.Cleanup(mgr.Shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := mgr.SubscribeEvents(ctx)

	mgr.Refresh(cfg)
	mgr.Refresh(cfg)
	require.Equal(t, 1, drainEvents(ch), "same discovery twice must publish exactly once")

	// Same name, path and state; only the skill itself changed.
	writeTestSkill(t, tmp, "steady-skill", "Second description.")
	mgr.Refresh(cfg)
	require.Equal(t, 1, drainEvents(ch), "a changed skill must publish again")

	writeTestSkill(t, tmp, "added-skill", "New skill.")
	mgr.Refresh(cfg)
	mgr.Refresh(cfg)
	require.Equal(t, 1, drainEvents(ch), "an added skill publishes once")
}

func TestManager_SetEnabledPublishes(t *testing.T) {
	// Not parallel - isolates config.Init from the host via t.Setenv.
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(hostHome, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(hostHome, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(hostHome, ".cache"))
	t.Setenv("CRUSH_GLOBAL_CONFIG", "")
	t.Setenv("CRUSH_GLOBAL_DATA", "")
	t.Setenv("CRUSH_SKILLS_DIR", t.TempDir())

	wd := t.TempDir()
	writeTestSkill(t, filepath.Join(wd, ".agents", "skills"), "toggle-skill", "Toggled in this test.")
	store, err := config.Init(wd, "", false)
	require.NoError(t, err)

	mgr := NewManager(nil, nil, nil, WithWorkingDir(wd))
	t.Cleanup(mgr.Shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := mgr.SubscribeEvents(ctx)

	mgr.Refresh(ConfigDiscovery(store))
	require.Equal(t, 1, drainEvents(ch))
	active := func() bool {
		return slices.ContainsFunc(mgr.ActiveSkills(), func(s *Skill) bool { return s.Name == "toggle-skill" })
	}
	require.True(t, active())

	require.NoError(t, SetEnabled(store, mgr, "toggle-skill", false))
	require.False(t, active())
	require.Equal(t, 1, drainEvents(ch), "disabling a skill must publish")

	require.NoError(t, SetEnabled(store, mgr, "toggle-skill", true))
	require.True(t, active())
	require.Equal(t, 1, drainEvents(ch), "enabling a skill must publish")
}

func TestDiscoverFromConfig(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "custom-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, SkillFileName),
		[]byte("---\nname: custom-skill\ndescription: A custom skill for tests.\n---\nDo a thing.\n"),
		0o644,
	))

	allSkills, activeSkills, states := DiscoverFromConfig(DiscoveryConfig{
		SkillsPaths:    []string{tmp},
		DisabledSkills: nil,
	})

	// Builtins plus our one custom skill.
	require.NotEmpty(t, allSkills)
	require.NotEmpty(t, activeSkills)
	require.GreaterOrEqual(t, len(allSkills), 2)
	require.GreaterOrEqual(t, len(activeSkills), 2)

	// The custom skill is present with full Instructions populated, so
	// the coordinator can render system prompts without re-walking the
	// filesystem.
	var custom *Skill
	for _, s := range allSkills {
		if s.Name == "custom-skill" {
			custom = s
			break
		}
	}
	require.NotNil(t, custom)
	require.NotEmpty(t, custom.Instructions, "DiscoverFromConfig must return Skill.Instructions")

	// State snapshot includes the custom skill too.
	foundCustom := false
	for _, s := range states {
		if s.Name == "custom-skill" {
			foundCustom = true
			require.Equal(t, StateNormal, s.State)
		}
	}
	require.True(t, foundCustom, "states slice should include the custom skill")
}

func TestDiscoverFromConfig_DisabledFiltered(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "off-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, SkillFileName),
		[]byte("---\nname: off-skill\ndescription: Should be filtered.\n---\nx\n"),
		0o644,
	))

	allSkills, activeSkills, states := DiscoverFromConfig(DiscoveryConfig{
		SkillsPaths:    []string{tmp},
		DisabledSkills: []string{"off-skill"},
	})

	// All discovered: yes; active: no.
	hasInAll := false
	for _, s := range allSkills {
		if s.Name == "off-skill" {
			hasInAll = true
		}
	}
	require.True(t, hasInAll, "DisabledSkills must not be removed from allSkills")

	for _, s := range activeSkills {
		require.NotEqual(t, "off-skill", s.Name, "DisabledSkills must be removed from activeSkills")
	}

	// State snapshot still carries discovered entries (UI re-applies filter).
	hasInStates := false
	for _, s := range states {
		if s.Name == "off-skill" {
			hasInStates = true
		}
	}
	require.True(t, hasInStates)
}

func TestDiscoverFromConfig_Resolver(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "envvar-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, SkillFileName),
		[]byte("---\nname: envvar-skill\ndescription: Env-resolved.\n---\nx\n"),
		0o644,
	))

	allSkills, _, _ := DiscoverFromConfig(DiscoveryConfig{
		SkillsPaths: []string{"$CUSTOM_SKILLS_DIR"},
		Resolver: func(s string) (string, error) {
			if s == "$CUSTOM_SKILLS_DIR" {
				return tmp, nil
			}
			return s, errors.New("unknown")
		},
	})

	found := false
	for _, s := range allSkills {
		if s.Name == "envvar-skill" {
			found = true
		}
	}
	require.True(t, found, "DiscoverFromConfig must expand $VAR via Resolver")
}
