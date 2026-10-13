package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWellKnownIndexErrors covers the index entries that are refused before
// anything is installed, and the fallback from agent-skills to skills.
func TestWellKnownIndexErrors(t *testing.T) {
	t.Parallel()

	artifact := []byte("---\nname: example\ndescription: A domain skill\n---\nInstructions")
	sum := sha256.Sum256(artifact)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	tests := []struct {
		name    string
		index   map[string]string
		wantErr string
	}{
		{
			name:    "unsupported schema",
			index:   map[string]string{"agent-skills": `{"$schema":"https://example.com/other.json","skills":[{"name":"example","files":["SKILL.md"]}]}`},
			wantErr: "unsupported skill discovery schema",
		},
		{
			name:    "unsupported artifact type",
			index:   map[string]string{"agent-skills": `{"$schema":"` + wellKnownSchemaV2 + `","skills":[{"name":"example","type":"binary","url":"/artifact","digest":"` + digest + `"}]}`},
			wantErr: "unsupported skill artifact type",
		},
		{
			name:    "plain http artifact",
			index:   map[string]string{"agent-skills": `{"$schema":"` + wellKnownSchemaV2 + `","skills":[{"name":"example","type":"skill-md","url":"http://example.com/a","digest":"` + digest + `"}]}`},
			wantErr: "invalid skill artifact URL",
		},
		{
			name:    "empty file list",
			index:   map[string]string{"agent-skills": `{"skills":[{"name":"example","files":[]}]}`},
			wantErr: "invalid skill file list",
		},
		{
			name:    "unsafe file",
			index:   map[string]string{"agent-skills": `{"skills":[{"name":"example","files":["../SKILL.md"]}]}`},
			wantErr: "unsafe skill file: ../SKILL.md",
		},
		{
			name:    "duplicate file",
			index:   map[string]string{"agent-skills": `{"skills":[{"name":"example","files":["SKILL.md","SKILL.md"]}]}`},
			wantErr: "duplicate skill file: SKILL.md",
		},
		{
			name:    "not published",
			index:   map[string]string{"agent-skills": `not json`, "skills": `{"skills":[{"name":"other","files":["SKILL.md"]}]}`},
			wantErr: "example was not found in",
		},
		{
			name:  "falls back to skills directory",
			index: map[string]string{"skills": `{"$schema":"` + wellKnownSchemaV2 + `","skills":[{"name":"example","type":"skill-md","url":"/artifact","digest":"` + digest + `"}]}`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for directory, index := range tt.index {
					if r.URL.Path == "/.well-known/"+directory+"/index.json" {
						fmt.Fprint(w, index)
						return
					}
				}
				if r.URL.Path == "/artifact" || strings.HasSuffix(r.URL.Path, "/SKILL.md") {
					w.Write(artifact)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			d := &Directory{Client: server.Client()}
			root := filepath.Join(t.TempDir(), "skills")
			skill, err := d.installWellKnown(t.Context(), strings.TrimPrefix(server.URL, "https://"), "example", root)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.NoDirExists(t, root)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "example", skill.Name)
			require.FileExists(t, filepath.Join(root, "example", SkillFileName))
		})
	}
}
