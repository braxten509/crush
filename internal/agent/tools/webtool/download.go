package webtool

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/permission"
)

//go:embed download.md.tpl
var downloadDescriptionTmpl []byte

var downloadDescriptionTpl = template.Must(
	template.New("downloadDescription").
		Parse(string(downloadDescriptionTmpl)),
)

type downloadDescriptionData struct {
	MaxDownloadTimeout int
}

func downloadDescription() string {
	return tools.RenderTemplate(downloadDescriptionTpl, downloadDescriptionData{
		MaxDownloadTimeout: 600,
	})
}

func NewDownloadTool(permissions permission.Service, workingDir string, client *http.Client) fantasy.AgentTool {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 10
		transport.IdleConnTimeout = 90 * time.Second

		client = &http.Client{
			Timeout:   5 * time.Minute, // Default 5 minute timeout for downloads
			Transport: transport,
		}
	}
	return fantasy.NewParallelAgentTool(
		tools.DownloadToolName,
		downloadDescription(),
		func(ctx context.Context, params tools.DownloadParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.URL == "" {
				return fantasy.NewTextErrorResponse("URL parameter is required"), nil
			}

			if params.FilePath == "" {
				return fantasy.NewTextErrorResponse("file_path parameter is required"), nil
			}

			if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
				return fantasy.NewTextErrorResponse("URL must start with http:// or https://"), nil
			}

			filePath := fsext.SmartJoin(workingDir, params.FilePath)
			relPath, _ := filepath.Rel(workingDir, filePath)
			relPath = filepath.ToSlash(cmp.Or(relPath, filePath))

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for downloading files")
			}

			p, err := permissions.Request(
				ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        filePath,
					ToolName:    tools.DownloadToolName,
					Action:      "download",
					Description: fmt.Sprintf("Download file from URL: %s to %s", params.URL, filePath),
					Params:      tools.DownloadPermissionsParams(params),
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return tools.NewPermissionDeniedResponse(), nil
			}

			// Handle timeout with context
			requestCtx := ctx
			if params.Timeout > 0 {
				maxTimeout := 600 // 10 minutes
				if params.Timeout > maxTimeout {
					params.Timeout = maxTimeout
				}
				var cancel context.CancelFunc
				requestCtx, cancel = context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
				defer cancel()
			}

			req, err := http.NewRequestWithContext(requestCtx, "GET", params.URL, nil)
			if err != nil {
				return fantasy.NewTextErrorResponse("Failed to create request: " + err.Error()), nil
			}

			req.Header.Set("User-Agent", "crush/1.0")

			resp, err := client.Do(req)
			if err != nil {
				return fantasy.NewTextErrorResponse("Failed to download from URL: " + err.Error()), nil
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"Request failed with status code: %d %s",
					resp.StatusCode,
					http.StatusText(resp.StatusCode),
				)), nil
			}

			if err := ensureParentDir(filePath); err != nil {
				return fantasy.NewTextErrorResponse("Download failed: " + err.Error()), nil
			}

			// Create the output file
			outFile, err := os.Create(filePath)
			if err != nil {
				return fantasy.NewTextErrorResponse("Failed to create output file: " + err.Error()), nil
			}
			defer outFile.Close()

			// Copy data without an explicit size limit.
			// The overall download is still constrained by the HTTP client's timeout
			// and any upstream server limits.
			bytesWritten, err := io.Copy(outFile, resp.Body)
			if err != nil {
				return fantasy.NewTextErrorResponse("Failed to write file: " + err.Error()), nil
			}

			contentType := resp.Header.Get("Content-Type")
			responseMsg := fmt.Sprintf("Successfully downloaded %d bytes to %s", bytesWritten, relPath)
			if contentType != "" {
				responseMsg += fmt.Sprintf(" (Content-Type: %s)", contentType)
			}

			return fantasy.NewTextResponse(responseMsg), nil
		},
	)
}

// ensureParentDir creates the parent directories of path. Call it only
// after the user has granted permission to write path. (The tools package
// keeps its own copy for the write tools; this package can't reach it.)
func ensureParentDir(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create parent directories: %w", err)
	}
	return nil
}
