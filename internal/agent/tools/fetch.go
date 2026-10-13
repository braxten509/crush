package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/fantasy"
	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/crush/internal/permission"
)

const (
	FetchToolName = "fetch"
	MaxFetchSize  = 100 * 1024 // 100KB
)

//go:embed fetch.md.tpl
var fetchDescriptionTmpl []byte

var fetchDescriptionTpl = template.Must(
	template.New("fetchDescription").
		Parse(string(fetchDescriptionTmpl)),
)

type fetchDescriptionData struct {
	GhAvailable    bool
	MaxFetchSizeKB int
}

func fetchDescription() string {
	return RenderTemplate(fetchDescriptionTpl, fetchDescriptionData{
		GhAvailable:    GHAvailable,
		MaxFetchSizeKB: MaxFetchSize / 1024,
	})
}

func NewFetchTool(permissions permission.Service, workingDir string, client *http.Client) fantasy.AgentTool {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 10
		transport.IdleConnTimeout = 90 * time.Second

		client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		}
	}

	t := &fetchTool{permissions: permissions, workingDir: workingDir, client: client}
	return fantasy.NewParallelAgentTool(
		FetchToolName,
		fetchDescription(),
		t.run,
	)
}

// fetchTool holds what the fetch tool needs to fetch a URL.
type fetchTool struct {
	permissions permission.Service
	workingDir  string
	client      *http.Client
}

func (t *fetchTool) run(ctx context.Context, params FetchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if params.URL == "" {
		return fantasy.NewTextErrorResponse("URL parameter is required"), nil
	}

	format := strings.ToLower(params.Format)
	if format != "text" && format != "markdown" && format != "html" {
		return fantasy.NewTextErrorResponse("Format must be one of: text, markdown, html"), nil
	}

	if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
		return fantasy.NewTextErrorResponse("URL must start with http:// or https://"), nil
	}

	sessionID := GetSessionFromContext(ctx)
	if sessionID == "" {
		return fantasy.ToolResponse{}, errors.New("session ID is required for fetching a URL")
	}

	p, err := t.permissions.Request(
		ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        t.workingDir,
			ToolCallID:  call.ID,
			ToolName:    FetchToolName,
			Action:      "fetch",
			Description: fmt.Sprintf("Fetch content from URL: %s", params.URL),
			Params:      FetchPermissionsParams(params),
		},
	)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !p {
		return NewPermissionDeniedResponse(), nil
	}
	return t.fetch(ctx, params, format)
}

// fetch requests the URL and returns its content in the given format.
func (t *fetchTool) fetch(ctx context.Context, params FetchParams, format string) (fantasy.ToolResponse, error) {
	// maxFetchTimeoutSeconds is the maximum allowed timeout for fetch requests (2 minutes)
	const maxFetchTimeoutSeconds = 120

	// Handle timeout with context
	requestCtx := ctx
	if params.Timeout > 0 {
		if params.Timeout > maxFetchTimeoutSeconds {
			params.Timeout = maxFetchTimeoutSeconds
		}
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(requestCtx, "GET", params.URL, nil)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "crush/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Request failed with status code: %d", resp.StatusCode)), nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetchSize))
	if err != nil {
		return fantasy.NewTextErrorResponse("Failed to read response body: " + err.Error()), nil
	}

	content := string(body)

	validUTF8 := utf8.ValidString(content)
	if !validUTF8 {
		return fantasy.NewTextErrorResponse("Response content is not valid UTF-8"), nil
	}
	contentType := resp.Header.Get("Content-Type")

	content, errMsg := formatFetched(content, contentType, format)
	if errMsg != "" {
		return fantasy.NewTextErrorResponse(errMsg), nil
	}
	// truncate content if it exceeds max read size
	if int64(len(content)) >= MaxFetchSize {
		content = content[:MaxFetchSize]
		content += fmt.Sprintf("\n\n[Content truncated to %d bytes]", MaxFetchSize)
	}

	return fantasy.NewTextResponse(content), nil
}

// formatFetched converts fetched content to the requested format. A
// non-empty error message means the conversion failed.
func formatFetched(content, contentType, format string) (string, string) {
	switch format {
	case "text":
		if strings.Contains(contentType, "text/html") {
			text, err := extractTextFromHTML(content)
			if err != nil {
				return "", "Failed to extract text from HTML: " + err.Error()
			}
			content = text
		}

	case "markdown":
		if strings.Contains(contentType, "text/html") {
			markdown, err := ConvertHTMLToMarkdown(content)
			if err != nil {
				return "", "Failed to convert HTML to Markdown: " + err.Error()
			}
			content = markdown
		}

		content = "```\n" + content + "\n```"

	case "html":
		// return only the body of the HTML document
		if strings.Contains(contentType, "text/html") {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
			if err != nil {
				return "", "Failed to parse HTML: " + err.Error()
			}
			body, err := doc.Find("body").Html()
			if err != nil {
				return "", "Failed to extract body from HTML: " + err.Error()
			}
			if body == "" {
				return "", "No body content found in HTML"
			}
			content = "<html>\n<body>\n" + body + "\n</body>\n</html>"
		}
	}
	return content, ""
}

func extractTextFromHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	text := doc.Find("body").Text()
	text = strings.Join(strings.Fields(text), " ")

	return text, nil
}
