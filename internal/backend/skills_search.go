package backend

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/crush/internal/skills"
)

// ErrSkillsSearch wraps a skills.sh search that failed; its text is the
// terminal's.
var ErrSkillsSearch = errors.New("skills search")

// ErrSkillsQuery is a search too short to run.
var ErrSkillsQuery = errors.New("skills query")

// searchSkills runs the skills.sh search the terminal's Browse tab runs;
// tests and the crushtest seams replace it.
var searchSkills = func(ctx context.Context, query string) ([]skills.DirectorySkill, error) {
	return skills.NewDirectory().Search(ctx, query)
}

// SetSkillsSearchForTest replaces the skills.sh search and returns a
// function that restores it.
func SetSkillsSearchForTest(search func(ctx context.Context, query string) ([]skills.DirectorySkill, error)) func() {
	prev := searchSkills
	searchSkills = search
	return func() { searchSkills = prev }
}

// SearchSkills searches skills.sh as the terminal's skill browser does
// (internal/skills/directory.go; desktop design 5-6, G-14). The results
// are skills.sh's own JSON.
func (b *Backend) SearchSkills(ctx context.Context, workspaceID, query string) ([]skills.DirectorySkill, error) {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
		return nil, err
	}
	if len([]rune(strings.TrimSpace(query))) < 2 {
		return nil, kindErr(ErrSkillsQuery, errors.New("type at least two characters to search skills.sh"))
	}
	found, err := searchSkills(ctx, query)
	if err != nil {
		return nil, kindErr(ErrSkillsSearch, err)
	}
	if found == nil {
		found = []skills.DirectorySkill{}
	}
	return found, nil
}
