package tools

import (
	"context"
	"encoding/json"
	"ferryman-agent/pkg/capability/workspace"
	toolcore "ferryman-agent/pkg/tools"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
)

type FetchParams struct {
	URL     string `json:"url"`
	Format  string `json:"format"`
	Timeout int    `json:"timeout,omitempty"`
}

type FetchTool struct {
	workspace workspace.Workspace
	client    *http.Client
}

func NewFetchTool(opts ...workspace.WorkspaceOption) (*toolcore.Tool, error) {
	options := workspace.ApplyOptions(opts)
	if strings.TrimSpace(options.RootDir) == "" {
		return nil, fmt.Errorf("workspace root dir is required")
	}
	ws := workspace.NewWorkspace(options.RootDir)
	t := &FetchTool{
		workspace: ws,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	return toolcore.NewTool(t.Info(), t.Run, toolcore.WithMiddleware(options.Middlewares...)), nil
}

func (t *FetchTool) Info() *toolcore.ToolInfo {
	return &toolcore.ToolInfo{
		Name:        FetchToolName,
		Description: fetchToolDescription,
		Parameters: map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch content from",
			},
			"format": map[string]any{
				"type":        "string",
				"description": "The format to return the content in (text, markdown, or html)",
				"enum":        []string{"text", "markdown", "html"},
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Optional timeout in seconds (max 120)",
			},
		},
		Required: []string{"url", "format"},
	}
}

func (t *FetchTool) Run(ctx context.Context, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	var params FetchParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return toolcore.NewTextErrorResponse("Failed to parse fetch parameters: " + err.Error()), nil
	}

	if params.URL == "" {
		return toolcore.NewTextErrorResponse("URL parameter is required"), nil
	}

	format := strings.ToLower(params.Format)
	if format != "text" && format != "markdown" && format != "html" {
		return toolcore.NewTextErrorResponse("Format must be one of: text, markdown, html"), nil
	}

	if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
		return toolcore.NewTextErrorResponse("URL must start with http:// or https://"), nil
	}

	client := t.client
	if params.Timeout > 0 {
		maxTimeout := 120 // 2 minutes
		if params.Timeout > maxTimeout {
			params.Timeout = maxTimeout
		}
		client = &http.Client{
			Timeout: time.Duration(params.Timeout) * time.Second,
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "ferryer/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("Request failed with status code: %d", resp.StatusCode)), nil
	}

	maxSize := int64(5 * 1024 * 1024) // 5MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return toolcore.NewTextErrorResponse("Failed to read response body: " + err.Error()), nil
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")

	switch format {
	case "text":
		if strings.Contains(contentType, "text/html") {
			text, err := extractTextFromHTML(content)
			if err != nil {
				return toolcore.NewTextErrorResponse("Failed to extract text from HTML: " + err.Error()), nil
			}
			return toolcore.NewTextResponse(text), nil
		}
		return toolcore.NewTextResponse(content), nil

	case "markdown":
		if strings.Contains(contentType, "text/html") {
			markdown, err := convertHTMLToMarkdown(content)
			if err != nil {
				return toolcore.NewTextErrorResponse("Failed to convert HTML to Markdown: " + err.Error()), nil
			}
			return toolcore.NewTextResponse(markdown), nil
		}

		return toolcore.NewTextResponse("```\n" + content + "\n```"), nil

	case "html":
		return toolcore.NewTextResponse(content), nil

	default:
		return toolcore.NewTextResponse(content), nil
	}
}

func extractTextFromHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	text := doc.Text()
	text = strings.Join(strings.Fields(text), " ")

	return text, nil
}

func convertHTMLToMarkdown(html string) (string, error) {
	converter := md.NewConverter("", true, nil)

	markdown, err := converter.ConvertString(html)
	if err != nil {
		return "", err
	}

	return markdown, nil
}
