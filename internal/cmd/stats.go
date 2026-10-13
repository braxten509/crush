package cmd

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/crush/internal/stats"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

//go:embed stats/index.html
var statsTemplate string

//go:embed stats/index.css
var statsCSS string

//go:embed stats/index.js
var statsJS string

//go:embed stats/header.svg
var headerSVG string

//go:embed stats/heartbit.svg
var heartbitSVG string

//go:embed stats/footer.svg
var footerSVG string

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show usage statistics",
	Long:  "Generate and display usage statistics including token usage, costs, and activity patterns",
	RunE:  runStats,
}

func init() {
	statsCmd.Flags().String("crawl-dir", "", "Crawl a directory recursively for all crush projects and aggregate stats")
	statsCmd.Flags().Bool("all", false, "Aggregate stats from all known projects (from projects.json)")
}

func runStats(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	dataDir, _ := cmd.Flags().GetString("data-dir")
	crawlDir, _ := cmd.Flags().GetString("crawl-dir")
	useAll, _ := cmd.Flags().GetBool("all")

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return err
	}

	var projectStats []stats.ProjectStats

	switch {
	case crawlDir != "":
		projectStats, err = stats.Crawl(ctx, crawlDir)
		if err != nil {
			return fmt.Errorf("failed to crawl for stats: %w", err)
		}
	case useAll:
		projectStats, err = stats.FromProjects(ctx)
		if err != nil {
			return fmt.Errorf("failed to gather stats from projects: %w", err)
		}
	default:
		cfg, err := config.Init(cwd, dataDir, false)
		if err != nil {
			return fmt.Errorf("failed to initialize config: %w", err)
		}
		if dataDir == "" {
			dataDir = cfg.Config().Options.DataDirectory
		}
		if shouldEnableMetrics(cfg.Config()) {
			event.Init()
		}

		event.StatsViewed()

		conn, err := db.Connect(ctx, dataDir)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer conn.Close()

		gathered, err := stats.Gather(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to gather stats: %w", err)
		}

		projectStats = []stats.ProjectStats{{ProjectPath: "", Stats: gathered}}
	}

	if len(projectStats) == 0 {
		return fmt.Errorf("no data available: no projects found")
	}

	// Merge stats from all projects.
	mergedStats := stats.Merge(projectStats)

	if mergedStats.Total.TotalSessions == 0 {
		return fmt.Errorf("no data available: no sessions found in database")
	}

	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}
	username := currentUser.Username

	var projName string
	switch {
	case crawlDir != "":
		projName = crawlDir
	case useAll:
		projName = "all projects"
	default:
		project, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		projName = strings.Replace(project, currentUser.HomeDir, "~", 1)
	}

	outputDataDir := dataDir
	if outputDataDir == "" {
		cfg, err := config.Init(cwd, "", false)
		if err == nil {
			outputDataDir = cfg.Config().Options.DataDirectory
		}
	}
	if outputDataDir == "" {
		outputDataDir = ".crush"
	}

	htmlPath := filepath.Join(outputDataDir, "stats/index.html")
	if err := generateHTML(mergedStats, projectStats, projName, username, htmlPath); err != nil {
		return fmt.Errorf("failed to generate HTML: %w", err)
	}

	fmt.Printf("Stats generated: %s\n", htmlPath)

	if err := browser.OpenFile(htmlPath); err != nil {
		fmt.Printf("Could not open browser: %v\n", err)
		fmt.Println("Please open the file manually.")
	}

	return nil
}

func generateHTML(merged *stats.Stats, projectStats []stats.ProjectStats, projName, username, path string) error {
	statsJSON, err := json.Marshal(merged)
	if err != nil {
		return err
	}

	projectStatsJSON, err := json.Marshal(projectStats)
	if err != nil {
		return err
	}

	tmpl, err := template.New("stats").Parse(statsTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	data := struct {
		StatsJSON        template.JS
		ProjectStatsJSON template.JS
		CSS              template.CSS
		JS               template.JS
		Header           template.HTML
		Heartbit         template.HTML
		Footer           template.HTML
		Favicon          template.URL
		GeneratedAt      string
		ProjectName      string
		Username         string
	}{
		StatsJSON:        template.JS(statsJSON),
		ProjectStatsJSON: template.JS(projectStatsJSON),
		CSS:              template.CSS(statsCSS),
		JS:               template.JS(statsJS),
		Header:           template.HTML(headerSVG),
		Heartbit:         template.HTML(heartbitSVG),
		Footer:           template.HTML(footerSVG),
		Favicon:          template.URL("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(heartbitSVG))),
		GeneratedAt:      merged.GeneratedAt.Format("2006-01-02"),
		ProjectName:      projName,
		Username:         username,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	return os.WriteFile(path, buf.Bytes(), 0o644)
}
