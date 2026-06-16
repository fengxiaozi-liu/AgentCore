package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"ferryman-agent/pkg/data/logging"
	"ferryman-agent/pkg/version"
)

type Workspace struct {
	Root string
}

type FileChange struct {
	ToolName   string
	ToolCallID string
	SessionID  string
	MessageID  string
	Path       string
	OldContent string
	NewContent string
	Diff       string
	Additions  int
	Removals   int
	Action     string
}

const ExtraKeyFileChanges = "workspace.file_changes"

func NewWorkspace(root string) Workspace {
	return Workspace{Root: root}
}

func (w Workspace) Resolve(path string) (string, error) {
	root, err := filepath.Abs(w.Root)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("workspace root is required")
	}

	target := path
	if target == "" {
		target = root
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes workspace %q", path, root)
	}
	return target, nil
}

func RecordFileVersions(ctx context.Context, files version.FileVersionService, changes []FileChange) {
	if files == nil {
		return
	}
	for _, change := range changes {
		if change.SessionID == "" || change.Path == "" {
			continue
		}
		file, getErr := files.GetByPathAndSession(ctx, change.Path, change.SessionID)
		if getErr != nil && change.Action != "add" && change.OldContent != "" {
			if _, createErr := files.Create(ctx, change.SessionID, change.Path, change.OldContent); createErr != nil {
				logging.Debug("Error creating file version", "error", createErr)
			}
		}
		if getErr == nil && file.Content != change.OldContent {
			if _, versionErr := files.CreateVersion(ctx, change.SessionID, change.Path, change.OldContent); versionErr != nil {
				logging.Debug("Error creating file version", "error", versionErr)
			}
		}
		if _, versionErr := files.CreateVersion(ctx, change.SessionID, change.Path, change.NewContent); versionErr != nil {
			logging.Debug("Error creating file version", "error", versionErr)
		}
	}
}
