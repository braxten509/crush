package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func skillArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, content := range files {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return b.Bytes()
}

func TestDirectorySearchAndInstallSelectedSkillWithResources(t *testing.T) {
	archive := skillArchive(t, map[string]string{
		"repo-main/skills/example/SKILL.md":       "---\nname: example\ndescription: Test skill\n---\nUse this.",
		"repo-main/skills/example/scripts/run.py": "print('ok')",
		"repo-main/skills/other/SKILL.md":         "---\nname: other\ndescription: Another\n---\nOther",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			require.Equal(t, "react native & SQL", r.URL.Query().Get("q"))
			fmt.Fprint(w, `{"skills":[{"id":"owner/repo/example","name":"example","skillId":"example","source":"owner/repo","installs":12}]}`)
		case "/owner/repo/zip/HEAD":
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := &Directory{Client: server.Client(), BaseURL: server.URL, ArchiveURL: server.URL}
	results, err := d.Search(t.Context(), "react native & SQL")
	require.NoError(t, err)
	require.Len(t, results, 1)
	root := filepath.Join(t.TempDir(), "skills")
	skill, err := d.Install(t.Context(), results[0], root)
	require.NoError(t, err)
	require.Equal(t, "example", skill.Name)
	content, err := os.ReadFile(filepath.Join(root, "example", "scripts", "run.py"))
	require.NoError(t, err)
	require.Equal(t, "print('ok')", string(content))
	_, err = os.Stat(filepath.Join(root, "other"))
	require.True(t, os.IsNotExist(err))
	_, err = d.Install(t.Context(), results[0], root)
	require.ErrorContains(t, err, "already installed")
}

func TestSkillInstallRejectsTraversalAndLeavesExistingSkillsAlone(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	for _, unsafe := range []string{"../outside", "/outside", "scripts/../outside", ".git/config", `scripts\evil`, "C:evil"} {
		archive := skillArchive(t, map[string]string{
			"repo/skills/example/SKILL.md":  "---\nname: example\ndescription: Test\n---\nBody",
			"repo/skills/example/" + unsafe: "bad",
		})
		_, err := installArchive(t.Context(), archive, "example", root)
		require.Error(t, err, unsafe)
		_, err = os.Stat(filepath.Join(root, "example"))
		require.True(t, os.IsNotExist(err))
	}
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "example")))
	archive := skillArchive(t, map[string]string{"repo/example/SKILL.md": "---\nname: example\ndescription: Test\n---\nBody"})
	_, err := installArchive(t.Context(), archive, "example", root)
	require.ErrorContains(t, err, "already installed")
}

func TestDirectorySearchErrorsAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	d := &Directory{Client: server.Client(), BaseURL: server.URL}
	_, err := d.Search(t.Context(), "sql")
	require.ErrorContains(t, err, "429")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = d.Search(ctx, "sql")
	require.ErrorIs(t, err, context.Canceled)
}

func TestDirectoryLiveSearchAndInstall(t *testing.T) {
	if os.Getenv("CRUSH_SKILLS_LIVE") != "1" {
		t.Skip("set CRUSH_SKILLS_LIVE=1 for a live skills.sh installation check")
	}
	d := NewDirectory()
	results, err := d.Search(t.Context(), "find-skills")
	require.NoError(t, err)
	var selected DirectorySkill
	for _, result := range results {
		if result.Source == "vercel-labs/skills" && result.SkillID == "find-skills" {
			selected = result
			break
		}
	}
	require.NotEmpty(t, selected.ID)
	skill, err := d.Install(t.Context(), selected, filepath.Join(t.TempDir(), "skills"))
	require.NoError(t, err)
	require.Equal(t, "find-skills", skill.Name)
	require.FileExists(t, skill.SkillFilePath)
}

func TestArchiveDiscoveryIsBoundedAndCancellable(t *testing.T) {
	files := make(map[string]string)
	for i := 0; i < 1001; i++ {
		files[fmt.Sprintf("repo/skill-%d/SKILL.md", i)] = "invalid"
	}
	data := skillArchive(t, files)
	_, err := installArchive(t.Context(), data, "example", t.TempDir())
	require.ErrorContains(t, err, "manifest discovery limit")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = installArchive(ctx, data, "example", t.TempDir())
	require.ErrorIs(t, err, context.Canceled)
}
