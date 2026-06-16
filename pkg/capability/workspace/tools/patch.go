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

type PatchParams struct {
	PatchText string `json:"patch_text"`
}

type PatchResponseMetadata struct {
	FilesChanged []string `json:"files_changed"`
	Additions    int      `json:"additions"`
	Removals     int      `json:"removals"`
}

type PatchTool struct {
	workspace   workspace.Workspace
	fileVersion version.FileVersionService
}

func NewPatchTool(opts ...workspace.WorkspaceOption) (*toolcore.Tool, error) {
	options := workspace.ApplyOptions(opts)
	if strings.TrimSpace(options.RootDir) == "" {
		return nil, fmt.Errorf("workspace root dir is required")
	}
	ws := workspace.NewWorkspace(options.RootDir)
	p := &PatchTool{workspace: ws, fileVersion: options.FileVersion}
	return toolcore.NewTool(p.Info(), p.Run, toolcore.WithMiddleware(options.Middlewares...)), nil
}

func (p *PatchTool) Info() *toolcore.ToolInfo {
	return &toolcore.ToolInfo{
		Name:        PatchToolName,
		Description: patchDescription,
		Parameters: map[string]any{
			"patch_text": map[string]any{
				"type":        "string",
				"description": "The full patch text that describes all changes to be made",
			},
		},
		Required: []string{"patch_text"},
	}
}

func (p *PatchTool) Run(ctx context.Context, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	var params PatchParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return toolcore.NewTextErrorResponse("invalid parameters"), nil
	}

	if params.PatchText == "" {
		return toolcore.NewTextErrorResponse("patch_text is required"), nil
	}

	// Identify all files needed for the patch and verify they've been read
	filesToRead := diff.IdentifyFilesNeeded(params.PatchText)
	for _, filePath := range filesToRead {
		absPath, err := p.workspace.Resolve(filePath)
		if err != nil {
			return toolcore.NewTextErrorResponse(err.Error()), nil
		}

		if fileutil.GetLastReadTime(absPath).IsZero() {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("you must read the file %s before patching it. Use the FileRead tool first", filePath)), nil
		}

		fileInfo, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return toolcore.NewTextErrorResponse(fmt.Sprintf("file not found: %s", absPath)), nil
			}
			return toolcore.ToolResponse{}, fmt.Errorf("failed to access file: %w", err)
		}

		if fileInfo.IsDir() {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("path is a directory, not a file: %s", absPath)), nil
		}

		modTime := fileInfo.ModTime()
		lastRead := fileutil.GetLastReadTime(absPath)
		if modTime.After(lastRead) {
			return toolcore.NewTextErrorResponse(
				fmt.Sprintf("file %s has been modified since it was last read (mod time: %s, last read: %s)",
					absPath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
				)), nil
		}
	}

	// Check for new files to ensure they don't already exist
	filesToAdd := diff.IdentifyFilesAdded(params.PatchText)
	for _, filePath := range filesToAdd {
		absPath, err := p.workspace.Resolve(filePath)
		if err != nil {
			return toolcore.NewTextErrorResponse(err.Error()), nil
		}

		_, err = os.Stat(absPath)
		if err == nil {
			return toolcore.NewTextErrorResponse(fmt.Sprintf("file already exists and cannot be added: %s", absPath)), nil
		} else if !os.IsNotExist(err) {
			return toolcore.ToolResponse{}, fmt.Errorf("failed to check file: %w", err)
		}
	}

	// Load all required files
	currentFiles := make(map[string]string)
	for _, filePath := range filesToRead {
		absPath, err := p.workspace.Resolve(filePath)
		if err != nil {
			return toolcore.NewTextErrorResponse(err.Error()), nil
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			return toolcore.ToolResponse{}, fmt.Errorf("failed to read file %s: %w", absPath, err)
		}
		currentFiles[filePath] = string(content)
	}

	// Process the patch
	patch, fuzz, err := diff.TextToPatch(params.PatchText, currentFiles)
	if err != nil {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("failed to parse patch: %s", err)), nil
	}

	if fuzz > 3 {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("patch contains fuzzy matches (fuzz level: %d). Please make your context lines more precise", fuzz)), nil
	}

	// Convert patch to commit
	commit, err := diff.PatchToCommit(patch, currentFiles)
	if err != nil {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("failed to create commit from patch: %s", err)), nil
	}

	// Get session ID and message ID
	sessionID, messageID := toolcore.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return toolcore.ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a patch")
	}

	// Apply the changes to the filesystem
	err = diff.ApplyCommit(commit, func(path string, content string) error {
		absPath, err := p.workspace.Resolve(path)
		if err != nil {
			return err
		}

		// Create parent directories if needed
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create parent directories for %s: %w", absPath, err)
		}

		return os.WriteFile(absPath, []byte(content), 0o644)
	}, func(path string) error {
		absPath, err := p.workspace.Resolve(path)
		if err != nil {
			return err
		}
		return os.Remove(absPath)
	})
	if err != nil {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("failed to apply patch: %s", err)), nil
	}

	changedFiles := []string{}
	totalAdditions := 0
	totalRemovals := 0
	changes := []workspace.FileChange{}

	for path, change := range commit.Changes {
		absPath, err := p.workspace.Resolve(path)
		if err != nil {
			return toolcore.NewTextErrorResponse(err.Error()), nil
		}
		changedFiles = append(changedFiles, absPath)

		oldContent := ""
		if change.OldContent != nil {
			oldContent = *change.OldContent
		}

		newContent := ""
		if change.NewContent != nil {
			newContent = *change.NewContent
		}

		// Calculate diff statistics
		patchDiff, additions, removals := diff.GenerateDiff(oldContent, newContent, path, p.workspace.Root)
		totalAdditions += additions
		totalRemovals += removals

		fileutil.RecordFileWrite(absPath)
		fileutil.RecordFileRead(absPath)

		changes = append(changes, workspace.FileChange{
			ToolName:   PatchToolName,
			ToolCallID: call.ID,
			SessionID:  sessionID,
			MessageID:  messageID,
			Path:       absPath,
			OldContent: oldContent,
			NewContent: newContent,
			Diff:       patchDiff,
			Additions:  additions,
			Removals:   removals,
			Action:     string(change.Type),
		})
	}

	result := fmt.Sprintf("Patch applied successfully. %d files changed, %d additions, %d removals",
		len(changedFiles), totalAdditions, totalRemovals)

	response := toolcore.WithResponseMetadata(
		toolcore.NewTextResponse(result),
		PatchResponseMetadata{
			FilesChanged: changedFiles,
			Additions:    totalAdditions,
			Removals:     totalRemovals,
		})
	response = toolcore.WithResponseExtra(response, workspace.ExtraKeyFileChanges, changes)
	workspace.RecordFileVersions(ctx, p.fileVersion, changes)
	return response, nil
}
