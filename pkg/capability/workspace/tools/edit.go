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

type EditParams struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

type EditPermissionsParams struct {
	FilePath string `json:"file_path"`
	Diff     string `json:"diff"`
}

type EditResponseMetadata struct {
	Diff      string `json:"diff"`
	Additions int    `json:"additions"`
	Removals  int    `json:"removals"`
}

type EditTool struct {
	workspace   workspace.Workspace
	fileVersion version.FileVersionService
}

func NewEditTool(opts ...workspace.WorkspaceOption) (*toolcore.Tool, error) {
	options := workspace.ApplyOptions(opts)
	if strings.TrimSpace(options.RootDir) == "" {
		return nil, fmt.Errorf("workspace root dir is required")
	}
	ws := workspace.NewWorkspace(options.RootDir)
	e := &EditTool{workspace: ws, fileVersion: options.FileVersion}
	return toolcore.NewTool(e.Info(), e.Run, toolcore.WithMiddleware(options.Middlewares...)), nil
}

func (e *EditTool) Info() *toolcore.ToolInfo {
	return &toolcore.ToolInfo{
		Name:        EditToolName,
		Description: editDescription,
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to modify",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The text to replace",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The text to replace it with",
			},
		},
		Required: []string{"file_path", "old_string", "new_string"},
	}
}

func (e *EditTool) Run(ctx context.Context, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	var params EditParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return toolcore.NewTextErrorResponse("invalid parameters"), nil
	}

	if params.FilePath == "" {
		return toolcore.NewTextErrorResponse("file_path is required"), nil
	}

	filePath, err := e.workspace.Resolve(params.FilePath)
	if err != nil {
		return toolcore.NewTextErrorResponse(err.Error()), nil
	}
	params.FilePath = filePath

	var response toolcore.ToolResponse

	switch {
	case params.OldString == "":
		response, err = e.createNewFile(ctx, call.ID, params.FilePath, params.NewString)
	case params.NewString == "":
		response, err = e.deleteContent(ctx, call.ID, params.FilePath, params.OldString)
	default:
		response, err = e.replaceContent(ctx, call.ID, params.FilePath, params.OldString, params.NewString)
	}
	if err != nil {
		return response, err
	}
	if response.IsError {
		// Return early if there was an error during content replacement
		// This prevents unnecessary LSP diagnostics processing
		return response, nil
	}

	text := fmt.Sprintf("<result>\n%s\n</result>\n", response.Content)
	response.Content = text
	return response, nil
}

func (e *EditTool) createNewFile(ctx context.Context, toolCallID, filePath, content string) (toolcore.ToolResponse, error) {
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		if fileInfo.IsDir() {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
		}
		return toolcore.NewTextErrorResponse(fmt.Sprintf("file already exists: %s", filePath)), nil
	} else if !os.IsNotExist(err) {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to access file: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	sessionID, messageID := toolcore.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return toolcore.ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}

	diff, additions, removals := diff.GenerateDiff(
		"",
		content,
		filePath,
		e.workspace.Root,
	)
	err = os.WriteFile(filePath, []byte(content), 0o644)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to write file: %w", err)
	}

	fileutil.RecordFileWrite(filePath)
	fileutil.RecordFileRead(filePath)

	response := toolcore.WithResponseMetadata(
		toolcore.NewTextResponse("File created: "+filePath),
		EditResponseMetadata{
			Diff:      diff,
			Additions: additions,
			Removals:  removals,
		},
	)
	change := workspace.FileChange{
		ToolName:   EditToolName,
		ToolCallID: toolCallID,
		SessionID:  sessionID,
		MessageID:  messageID,
		Path:       filePath,
		OldContent: "",
		NewContent: content,
		Diff:       diff,
		Additions:  additions,
		Removals:   removals,
		Action:     "write",
	}
	changes := []workspace.FileChange{change}
	workspace.RecordFileVersions(ctx, e.fileVersion, changes)
	response = toolcore.WithResponseExtra(response, workspace.ExtraKeyFileChanges, changes)
	return response, nil
}

func (e *EditTool) deleteContent(ctx context.Context, toolCallID, filePath, oldString string) (toolcore.ToolResponse, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("file not found: %s", filePath)), nil
		}
		return toolcore.ToolResponse{}, fmt.Errorf("failed to access file: %w", err)
	}

	if fileInfo.IsDir() {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
	}

	if fileutil.GetLastReadTime(filePath).IsZero() {
		return toolcore.NewTextErrorResponse("you must read the file before editing it. Use the View tool first"), nil
	}

	modTime := fileInfo.ModTime()
	lastRead := fileutil.GetLastReadTime(filePath)
	if modTime.After(lastRead) {
		return toolcore.NewTextErrorResponse(
			fmt.Sprintf("file %s has been modified since it was last read (mod time: %s, last read: %s)",
				filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
			)), nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to read file: %w", err)
	}

	oldContent := string(content)

	index := strings.Index(oldContent, oldString)
	if index == -1 {
		return toolcore.NewTextErrorResponse("old_string not found in file. Make sure it matches exactly, including whitespace and line breaks"), nil
	}

	lastIndex := strings.LastIndex(oldContent, oldString)
	if index != lastIndex {
		return toolcore.NewTextErrorResponse("old_string appears multiple times in the file. Please provide more context to ensure a unique match"), nil
	}

	newContent := oldContent[:index] + oldContent[index+len(oldString):]

	sessionID, messageID := toolcore.GetContextValues(ctx)

	if sessionID == "" || messageID == "" {
		return toolcore.ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}

	diff, additions, removals := diff.GenerateDiff(
		oldContent,
		newContent,
		filePath,
		e.workspace.Root,
	)

	err = os.WriteFile(filePath, []byte(newContent), 0o644)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to write file: %w", err)
	}

	fileutil.RecordFileWrite(filePath)
	fileutil.RecordFileRead(filePath)

	response := toolcore.WithResponseMetadata(
		toolcore.NewTextResponse("Content deleted from file: "+filePath),
		EditResponseMetadata{
			Diff:      diff,
			Additions: additions,
			Removals:  removals,
		},
	)
	change := workspace.FileChange{
		ToolName:   EditToolName,
		ToolCallID: toolCallID,
		SessionID:  sessionID,
		MessageID:  messageID,
		Path:       filePath,
		OldContent: oldContent,
		NewContent: newContent,
		Diff:       diff,
		Additions:  additions,
		Removals:   removals,
		Action:     "write",
	}
	changes := []workspace.FileChange{change}
	workspace.RecordFileVersions(ctx, e.fileVersion, changes)
	response = toolcore.WithResponseExtra(response, workspace.ExtraKeyFileChanges, changes)
	return response, nil
}

func (e *EditTool) replaceContent(ctx context.Context, toolCallID, filePath, oldString, newString string) (toolcore.ToolResponse, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("file not found: %s", filePath)), nil
		}
		return toolcore.ToolResponse{}, fmt.Errorf("failed to access file: %w", err)
	}

	if fileInfo.IsDir() {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
	}

	if fileutil.GetLastReadTime(filePath).IsZero() {
		return toolcore.NewTextErrorResponse("you must read the file before editing it. Use the View tool first"), nil
	}

	modTime := fileInfo.ModTime()
	lastRead := fileutil.GetLastReadTime(filePath)
	if modTime.After(lastRead) {
		return toolcore.NewTextErrorResponse(
			fmt.Sprintf("file %s has been modified since it was last read (mod time: %s, last read: %s)",
				filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
			)), nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to read file: %w", err)
	}

	oldContent := string(content)

	index := strings.Index(oldContent, oldString)
	if index == -1 {
		return toolcore.NewTextErrorResponse("old_string not found in file. Make sure it matches exactly, including whitespace and line breaks"), nil
	}

	lastIndex := strings.LastIndex(oldContent, oldString)
	if index != lastIndex {
		return toolcore.NewTextErrorResponse("old_string appears multiple times in the file. Please provide more context to ensure a unique match"), nil
	}

	newContent := oldContent[:index] + newString + oldContent[index+len(oldString):]

	if oldContent == newContent {
		return toolcore.NewTextErrorResponse("new content is the same as old content. No changes made."), nil
	}
	sessionID, messageID := toolcore.GetContextValues(ctx)

	if sessionID == "" || messageID == "" {
		return toolcore.ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}
	diff, additions, removals := diff.GenerateDiff(
		oldContent,
		newContent,
		filePath,
		e.workspace.Root,
	)
	err = os.WriteFile(filePath, []byte(newContent), 0o644)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("failed to write file: %w", err)
	}

	fileutil.RecordFileWrite(filePath)
	fileutil.RecordFileRead(filePath)

	response := toolcore.WithResponseMetadata(
		toolcore.NewTextResponse("Content replaced in file: "+filePath),
		EditResponseMetadata{
			Diff:      diff,
			Additions: additions,
			Removals:  removals,
		})
	change := workspace.FileChange{
		ToolName:   EditToolName,
		ToolCallID: toolCallID,
		SessionID:  sessionID,
		MessageID:  messageID,
		Path:       filePath,
		OldContent: oldContent,
		NewContent: newContent,
		Diff:       diff,
		Additions:  additions,
		Removals:   removals,
		Action:     "write",
	}
	changes := []workspace.FileChange{change}
	workspace.RecordFileVersions(ctx, e.fileVersion, changes)
	response = toolcore.WithResponseExtra(response, workspace.ExtraKeyFileChanges, changes)
	return response, nil
}

func (e *EditTool) permissionPath(filePath string) (string, error) {
	rootDir, err := e.workspace.Resolve("")
	if err != nil {
		return "", err
	}
	permissionPath := filepath.Dir(filePath)
	if strings.HasPrefix(filePath, rootDir) {
		permissionPath = rootDir
	}
	return permissionPath, nil
}
