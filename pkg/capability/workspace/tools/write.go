package tools

import (
	"context"
	"encoding/json"
	"ferryman-agent/pkg/capability/workspace"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	toolcore "ferryman-agent/pkg/tools"
	"ferryman-agent/pkg/utils/diff"
	"ferryman-agent/pkg/utils/fileutil"
	"ferryman-agent/pkg/version"
)

type WriteParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type WritePermissionsParams struct {
	FilePath string `json:"file_path"`
	Diff     string `json:"diff"`
}

type WriteTool struct {
	workspace   workspace.Workspace
	fileVersion version.FileVersionService
}

type WriteResponseMetadata struct {
	Diff      string `json:"diff"`
	Additions int    `json:"additions"`
	Removals  int    `json:"removals"`
}

func NewWriteTool(opts ...workspace.WorkspaceOption) (*toolcore.Tool, error) {
	options := workspace.ApplyOptions(opts)
	if strings.TrimSpace(options.RootDir) == "" {
		return nil, fmt.Errorf("workspace root dir is required")
	}
	ws := workspace.NewWorkspace(options.RootDir)
	w := &WriteTool{workspace: ws, fileVersion: options.FileVersion}
	return toolcore.NewTool(w.Info(), w.Run, toolcore.WithMiddleware(options.Middlewares...)), nil
}

func (w *WriteTool) Info() *toolcore.ToolInfo {
	return &toolcore.ToolInfo{
		Name:        WriteToolName,
		Description: writeDescription,
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file to write",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		Required: []string{"file_path", "content"},
	}
}

func (w *WriteTool) Run(ctx context.Context, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	var params WriteParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.FilePath == "" {
		return toolcore.NewTextErrorResponse("file_path is required"), nil
	}

	if params.Content == "" {
		return toolcore.NewTextErrorResponse("content is required"), nil
	}

	filePath, err := w.workspace.Resolve(params.FilePath)
	if err != nil {
		return toolcore.NewTextErrorResponse(err.Error()), nil
	}

	fileInfo, err := os.Stat(filePath)
	if err == nil {
		if fileInfo.IsDir() {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("Path is a directory, not a file: %s", filePath)), nil
		}

		modTime := fileInfo.ModTime()
		lastRead := fileutil.GetLastReadTime(filePath)
		if modTime.After(lastRead) {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("File %s has been modified since it was last read.\nLast modification: %s\nLast read: %s\n\nPlease read the file again before modifying it.",
				filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339))), nil
		}

		oldContent, readErr := os.ReadFile(filePath)
		if readErr == nil && string(oldContent) == params.Content {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("File %s already contains the exact content. No changes made.", filePath)), nil
		}
	} else if !os.IsNotExist(err) {
		return toolcore.ToolResponse{}, fmt.Errorf("error checking file: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("error creating directory: %w", err)
	}

	oldContent := ""
	if fileInfo != nil && !fileInfo.IsDir() {
		oldBytes, readErr := os.ReadFile(filePath)
		if readErr == nil {
			oldContent = string(oldBytes)
		}
	}

	sessionID, messageID := toolcore.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return toolcore.ToolResponse{}, fmt.Errorf("session_id and message_id are required")
	}

	diff, additions, removals := diff.GenerateDiff(
		oldContent,
		params.Content,
		filePath,
		w.workspace.Root,
	)

	err = os.WriteFile(filePath, []byte(params.Content), 0o644)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("error writing file: %w", err)
	}

	fileutil.RecordFileWrite(filePath)
	fileutil.RecordFileRead(filePath)

	result := fmt.Sprintf("File successfully written: %s", filePath)
	result = fmt.Sprintf("<result>\n%s\n</result>", result)
	response := toolcore.WithResponseMetadata(toolcore.NewTextResponse(result),
		WriteResponseMetadata{
			Diff:      diff,
			Additions: additions,
			Removals:  removals,
		},
	)
	change := workspace.FileChange{
		ToolName:   WriteToolName,
		ToolCallID: call.ID,
		SessionID:  sessionID,
		MessageID:  messageID,
		Path:       filePath,
		OldContent: oldContent,
		NewContent: params.Content,
		Diff:       diff,
		Additions:  additions,
		Removals:   removals,
		Action:     "write",
	}
	changes := []workspace.FileChange{change}
	workspace.RecordFileVersions(ctx, w.fileVersion, changes)
	response = toolcore.WithResponseExtra(response, workspace.ExtraKeyFileChanges, changes)
	return response, nil
}
