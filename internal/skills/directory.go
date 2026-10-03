package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
)

// DirectorySkill is a result from the same public search used by skills.sh's CLI.
type DirectorySkill struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	SkillID  string `json:"skillId"`
	Installs int    `json:"installs"`
}

// Directory uses bounded HTTP requests; installing never executes downloaded code.
type Directory struct {
	Client     *http.Client
	BaseURL    string
	ArchiveURL string
}

func NewDirectory() *Directory {
	return &Directory{Client: &http.Client{Timeout: 60 * time.Second}, BaseURL: "https://skills.sh", ArchiveURL: "https://codeload.github.com"}
}

func (d *Directory) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Crush skills manager")
	rsp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skill download: HTTP %d", rsp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(rsp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("skill download exceeds size limit")
	}
	return data, nil
}

func (d *Directory) Search(ctx context.Context, query string) ([]DirectorySkill, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, fmt.Errorf("type at least two characters to search skills.sh")
	}
	data, err := d.get(ctx, d.BaseURL+"/api/search?"+url.Values{"q": {query}, "limit": {"50"}}.Encode(), 2<<20)
	if err != nil {
		return nil, err
	}
	var result struct {
		Skills []DirectorySkill `json:"skills"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("skills.sh search: %w", err)
	}
	return result.Skills, nil
}

// Install copies only the selected skill and its supporting files. Existing
// installations (including symlinks) are never replaced.
func (d *Directory) Install(ctx context.Context, selected DirectorySkill, root string) (*Skill, error) {
	source := strings.Split(selected.Source, "/")
	name := selected.SkillID
	if name == "" {
		name = path.Base(selected.ID)
	}
	if !namePattern.MatchString(name) || len(name) > MaxNameLength {
		return nil, fmt.Errorf("invalid skill name")
	}
	if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
		return nil, fmt.Errorf("%s is already installed", name)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if len(source) == 1 && repositoryPart(source[0]) && strings.Contains(source[0], ".") {
		return d.installWellKnown(ctx, selected.Source, name, root)
	}
	if len(source) != 2 || !repositoryPart(source[0]) || !repositoryPart(source[1]) {
		return nil, fmt.Errorf("unsupported skills.sh source")
	}
	data, err := d.get(ctx, d.ArchiveURL+"/"+selected.Source+"/zip/HEAD", 32<<20)
	if err != nil {
		return nil, err
	}
	return installArchive(ctx, data, name, root)
}

func repositoryPart(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func installArchive(ctx context.Context, data []byte, name, root string) (*Skill, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("read skill archive: %w", err)
	}
	var candidates []string
	var manifestBytes uint64
	manifestCount := 0
	if len(archive.File) > 20000 {
		return nil, fmt.Errorf("skill repository contains too many files")
	}
	for _, file := range archive.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if path.Base(file.Name) != SkillFileName || file.Mode()&os.ModeSymlink != 0 {
			continue
		}
		manifestCount++
		manifestBytes += file.UncompressedSize64
		if manifestCount > 1000 || manifestBytes > 16<<20 {
			return nil, fmt.Errorf("skill repository exceeds manifest discovery limit")
		}
		content, err := readArchiveFile(file, 1<<20)
		if err != nil {
			return nil, err
		}
		skill, err := ParseContent(content)
		if err == nil && skill.Name == name {
			prefix := path.Dir(file.Name) + "/"
			if prefix == "./" {
				prefix = ""
			}
			candidates = append(candidates, prefix)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%s was not found in the repository", name)
	}
	slices.Sort(candidates)
	// Repositories can ship multiple agent-specific variants. Prefer the
	// shared standard directory, then the conventional skills directory.
	prefix := candidates[0]
	for _, suffix := range []string{"/skills/" + name + "/", "/.agents/skills/" + name + "/"} {
		for _, candidate := range candidates {
			if strings.HasSuffix(candidate, suffix) {
				prefix = candidate
				break
			}
		}
	}
	files := make(map[string][]byte)
	modes := make(map[string]os.FileMode)
	var total int64
	for _, file := range archive.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(file.Name, prefix) || file.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(file.Name, prefix)
		if !fsSafePath(rel) || !file.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe skill file: %s", rel)
		}
		if _, exists := files[rel]; exists {
			return nil, fmt.Errorf("duplicate skill file: %s", rel)
		}
		if file.UncompressedSize64 > 16<<20 || uint64(total)+file.UncompressedSize64 > 64<<20 || len(files) >= 2000 {
			return nil, fmt.Errorf("skill exceeds installation size limit")
		}
		content, err := readArchiveFile(file, 16<<20)
		if err != nil {
			return nil, err
		}
		total += int64(len(content))
		if total > 64<<20 || len(files) >= 2000 {
			return nil, fmt.Errorf("skill exceeds installation size limit")
		}
		files[rel] = content
		modes[rel] = 0o644 | file.Mode().Perm()&0o111
	}
	skill, err := ParseContent(files[SkillFileName])
	if err != nil {
		return nil, err
	}
	skill.Path = filepath.Join(root, name)
	skill.SkillFilePath = filepath.Join(skill.Path, SkillFileName)
	if err := skill.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	release, err := lock.File(ctx, filepath.Join(filepath.Dir(root), ".crush-skills.lock"))
	if err != nil {
		return nil, err
	}
	defer release()
	if _, err := os.Lstat(skill.Path); err == nil {
		return nil, fmt.Errorf("%s is already installed", name)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Stage outside the discovery root so no partial skill can be loaded.
	stage, err := os.MkdirTemp(filepath.Dir(root), ".crush-skill-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	for rel, content := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dest := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, content, modes[rel]); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := publishSkillDir(stage, skill.Path); err != nil {
		return nil, err
	}
	return skill, nil
}

func fsSafePath(rel string) bool {
	if rel == "" || strings.Contains(rel, "\\") || strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") || path.Clean(rel) != rel {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || part == ".git" || strings.Contains(part, ":") {
			return false
		}
	}
	return true
}

func readArchiveFile(file *zip.File, limit int64) ([]byte, error) {
	if file.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("skill file exceeds size limit: %s", file.Name)
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	content, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("skill file exceeds size limit: %s", file.Name)
	}
	return content, err
}
