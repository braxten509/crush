package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// Domain publishers in skills.sh use the same well-known discovery formats
// as its CLI, including supporting files and digest-checked v2 artifacts.
func (d *Directory) installWellKnown(ctx context.Context, host, name, root string) (*Skill, error) {
	for _, directory := range []string{"agent-skills", "skills"} {
		base := "https://" + host + "/.well-known/" + directory + "/"
		data, err := d.get(ctx, base+"index.json", 2<<20)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		var index struct {
			Schema string `json:"$schema"`
			Skills []struct {
				Name   string   `json:"name"`
				Files  []string `json:"files"`
				Type   string   `json:"type"`
				URL    string   `json:"url"`
				Digest string   `json:"digest"`
			} `json:"skills"`
		}
		if err := json.Unmarshal(data, &index); err != nil {
			continue
		}
		for _, entry := range index.Skills {
			if entry.Name != name {
				continue
			}
			if index.Schema == "https://schemas.agentskills.io/discovery/0.2.0/schema.json" {
				indexURL, _ := url.Parse(base + "index.json")
				artifactURL, err := indexURL.Parse(entry.URL)
				if err != nil || artifactURL.Scheme != "https" || artifactURL.User != nil {
					return nil, fmt.Errorf("invalid skill artifact URL")
				}
				artifact, err := d.get(ctx, artifactURL.String(), 32<<20)
				if err != nil {
					return nil, err
				}
				digest := sha256.Sum256(artifact)
				if entry.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
					return nil, fmt.Errorf("skill artifact digest mismatch")
				}
				if entry.Type == "skill-md" {
					archive, err := filesArchive(map[string][]byte{SkillFileName: artifact})
					if err != nil {
						return nil, err
					}
					return installArchive(ctx, archive, name, root)
				}
				if entry.Type == "archive" {
					if len(artifact) >= 2 && artifact[0] == 0x1f && artifact[1] == 0x8b {
						artifact, err = tarToZip(ctx, artifact)
						if err != nil {
							return nil, err
						}
					}
					return installArchive(ctx, artifact, name, root)
				}
				return nil, fmt.Errorf("unsupported skill artifact type")
			}
			if index.Schema != "" {
				return nil, fmt.Errorf("unsupported skill discovery schema")
			}
			if len(entry.Files) == 0 || len(entry.Files) > 2000 {
				return nil, fmt.Errorf("invalid skill file list")
			}
			files := make(map[string][]byte)
			var total int
			for _, rel := range entry.Files {
				if !fsSafePath(rel) {
					return nil, fmt.Errorf("unsafe skill file: %s", rel)
				}
				if _, exists := files[rel]; exists {
					return nil, fmt.Errorf("duplicate skill file: %s", rel)
				}
				parts := strings.Split(rel, "/")
				for i := range parts {
					parts[i] = url.PathEscape(parts[i])
				}
				content, err := d.get(ctx, base+url.PathEscape(name)+"/"+strings.Join(parts, "/"), 16<<20)
				if err != nil {
					return nil, err
				}
				total += len(content)
				if total > 64<<20 {
					return nil, fmt.Errorf("skill exceeds installation size limit")
				}
				files[rel] = content
			}
			archive, err := filesArchive(files)
			if err != nil {
				return nil, err
			}
			return installArchive(ctx, archive, name, root)
		}
	}
	return nil, fmt.Errorf("%s was not found in %s's published skills", name, host)
}

func filesArchive(files map[string][]byte) ([]byte, error) {
	return filesArchiveWithModes(files, nil)
}

func filesArchiveWithModes(files map[string][]byte, modes map[string]os.FileMode) ([]byte, error) {
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for name, content := range files {
		header := &zip.FileHeader{Name: "skill/" + name, Method: zip.Deflate}
		header.SetMode(0o644 | modes[name]&0o111)
		file, err := w.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := file.Write(content); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func tarToZip(ctx context.Context, data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	r := tar.NewReader(&archiveStreamReader{ctx: ctx, reader: gz, remaining: 72 << 20})
	files := make(map[string][]byte)
	modes := make(map[string]os.FileMode)
	var total int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		entries++
		if entries > 10000 {
			return nil, fmt.Errorf("skill archive contains too many entries")
		}
		for strings.HasPrefix(h.Name, "./") {
			h.Name = strings.TrimPrefix(h.Name, "./")
		}
		if h.Typeflag == tar.TypeDir {
			if h.Size != 0 {
				return nil, fmt.Errorf("unsafe skill archive directory")
			}
			continue
		}
		if h.Typeflag != tar.TypeReg || !fsSafePath(h.Name) || h.Size < 0 || h.Size > 16<<20 {
			return nil, fmt.Errorf("unsafe skill archive file")
		}
		total += h.Size
		if total > 64<<20 || len(files) >= 2000 {
			return nil, fmt.Errorf("skill exceeds installation size limit")
		}
		if _, exists := files[h.Name]; exists {
			return nil, fmt.Errorf("duplicate skill archive file")
		}
		content, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		files[h.Name] = content
		modes[h.Name] = os.FileMode(h.Mode)
	}
	return filesArchiveWithModes(files, modes)
}

// Bounds everything tar.Next consumes, including invisible PAX/GNU metadata.
type archiveStreamReader struct {
	ctx       context.Context
	reader    io.Reader
	remaining int64
}

func (r *archiveStreamReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.remaining <= 0 {
		return 0, fmt.Errorf("skill archive expands beyond the size limit")
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}
