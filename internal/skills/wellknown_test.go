package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWellKnownSkillIncludesSupportingFiles(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-skills/index.json":
			fmt.Fprint(w, `{"skills":[{"name":"example","files":["SKILL.md","references/guide.md"]}]}`)
		case "/.well-known/agent-skills/example/SKILL.md":
			fmt.Fprint(w, "---\nname: example\ndescription: A domain skill\n---\nInstructions")
		case "/.well-known/agent-skills/example/references/guide.md":
			fmt.Fprint(w, "Guide")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := &Directory{Client: server.Client()}
	root := filepath.Join(t.TempDir(), "skills")
	_, err := d.installWellKnown(t.Context(), strings.TrimPrefix(server.URL, "https://"), "example", root)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, "example", "references", "guide.md"))
	require.NoError(t, err)
	require.Equal(t, "Guide", string(content))
}

func TestTarMetadataReadsAreBoundedInsideNext(t *testing.T) {
	// Consecutive PAX records are consumed inside a single tar.Reader.Next.
	payload := []byte("13 comment=x\n")
	var header [512]byte
	copy(header[:100], "PaxHeaders/entry")
	copy(header[100:108], "0000644\x00")
	copy(header[108:116], "0000000\x00")
	copy(header[116:124], "0000000\x00")
	copy(header[124:136], fmt.Sprintf("%011o\x00", len(payload)))
	copy(header[136:148], "00000000000\x00")
	for i := 148; i < 156; i++ {
		header[i] = ' '
	}
	header[156] = tar.TypeXHeader
	copy(header[257:265], "ustar\x0000")
	sum := 0
	for _, b := range header {
		sum += int(b)
	}
	copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
	var raw bytes.Buffer
	for i := 0; i < 4; i++ {
		raw.Write(header[:])
		raw.Write(payload)
		raw.Write(make([]byte, 512-len(payload)))
	}
	r := tar.NewReader(&archiveStreamReader{ctx: t.Context(), reader: bytes.NewReader(raw.Bytes()), remaining: 1024})
	_, err := r.Next()
	require.ErrorContains(t, err, "size limit")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = io.ReadAll(&archiveStreamReader{ctx: ctx, reader: bytes.NewReader(raw.Bytes()), remaining: 1024})
	require.ErrorIs(t, err, context.Canceled)
}

func TestTarSkillNormalizesPathsAndPreservesExecutableScripts(t *testing.T) {
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	w := tar.NewWriter(gz)
	for _, file := range []struct {
		name, content string
		mode          int64
	}{
		{"./SKILL.md", "---\nname: example\ndescription: Test\n---\nBody", 0o644},
		{"./scripts/run.sh", "#!/bin/sh\necho ok", 0o755},
	} {
		require.NoError(t, w.WriteHeader(&tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.content)), Typeflag: tar.TypeReg}))
		_, err := w.Write([]byte(file.content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, gz.Close())
	archive, err := tarToZip(t.Context(), buffer.Bytes())
	require.NoError(t, err)
	root := filepath.Join(t.TempDir(), "skills")
	_, err = installArchive(t.Context(), archive, "example", root)
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(root, "example", "scripts", "run.sh"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o111), info.Mode().Perm()&0o111)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = tarToZip(ctx, buffer.Bytes())
	require.ErrorIs(t, err, context.Canceled)
}

func TestWellKnownArtifactDigestIsVerified(t *testing.T) {
	artifact := []byte("---\nname: example\ndescription: A domain skill\n---\nInstructions")
	digest := sha256.Sum256(artifact)
	for _, checksum := range []string{strings.Repeat("0", 64), hex.EncodeToString(digest[:])} {
		t.Run(checksum[:4], func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/agent-skills/index.json" {
					fmt.Fprintf(w, `{"$schema":"https://schemas.agentskills.io/discovery/0.2.0/schema.json","skills":[{"name":"example","type":"skill-md","url":"/artifact","digest":"sha256:%s"}]}`, checksum)
				} else {
					w.Write(artifact)
				}
			}))
			defer server.Close()
			d := &Directory{Client: server.Client()}
			root := filepath.Join(t.TempDir(), "skills")
			_, err := d.installWellKnown(t.Context(), strings.TrimPrefix(server.URL, "https://"), "example", root)
			if checksum == strings.Repeat("0", 64) {
				require.ErrorContains(t, err, "digest mismatch")
				require.NoDirExists(t, root)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
