package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/config"
)

const projectsFileName = "projects.json"

// Project represents a tracked project directory.
type Project struct {
	Path         string    `json:"path"`
	DataDir      string    `json:"data_dir"`
	LastAccessed time.Time `json:"last_accessed"`
	Saved        bool      `json:"saved,omitempty"`
}

// ProjectList holds the list of tracked projects.
type ProjectList struct {
	Projects []Project `json:"projects"`
}

var mu sync.Mutex

// projectsFilePath returns the path to the projects.json file.
func projectsFilePath() string {
	return filepath.Join(filepath.Dir(config.GlobalConfigData()), projectsFileName)
}

// Load reads the projects list from disk.
func Load() (*ProjectList, error) {
	mu.Lock()
	defer mu.Unlock()

	path := projectsFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectList{Projects: []Project{}}, nil
		}
		return nil, err
	}

	var list ProjectList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}

	return &list, nil
}

// Save writes the projects list to disk.
func Save(list *ProjectList) error {
	mu.Lock()
	defer mu.Unlock()

	path := projectsFilePath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// Register adds or updates a project in the list.
func Register(workingDir, dataDir string) error {
	list, err := Load()
	if err != nil {
		return err
	}

	project := Project{Path: workingDir}
	if index := slices.IndexFunc(list.Projects, func(p Project) bool { return p.Path == workingDir }); index >= 0 {
		project = list.Projects[index]
		list.Projects = slices.Delete(list.Projects, index, index+1)
	}
	project.DataDir = dataDir
	project.LastAccessed = time.Now().UTC()
	// Move the existing entry to the front so tied timestamps cannot reorder
	// projects, while retaining the user's saved-project flag.
	list.Projects = slices.Insert(list.Projects, 0, project)

	return Save(list)
}

// List returns all tracked projects, most recently registered first.
func List() ([]Project, error) {
	list, err := Load()
	if err != nil {
		return nil, err
	}
	return list.Projects, nil
}

// MarkSaved flags workingDir as a user-saved project, adding it if needed.
func MarkSaved(workingDir string) error {
	list, err := Load()
	if err != nil {
		return err
	}
	for i, p := range list.Projects {
		if p.Path == workingDir {
			list.Projects[i].Saved = true
			return Save(list)
		}
	}
	list.Projects = append(list.Projects, Project{Path: workingDir, LastAccessed: time.Now().UTC(), Saved: true})
	return Save(list)
}

// Unsave removes workingDir from the saved projects.
func Unsave(workingDir string) error {
	list, err := Load()
	if err != nil {
		return err
	}
	for i, p := range list.Projects {
		if p.Path == workingDir {
			list.Projects[i].Saved = false
			return Save(list)
		}
	}
	return nil
}

// SavedList returns user-saved projects sorted by last accessed.
func SavedList() ([]Project, error) {
	all, err := List()
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(p Project) bool { return !p.Saved }), nil
}
