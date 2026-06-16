package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"ferryman-agent/pkg/capability/workspace"
	toolcore "ferryman-agent/pkg/tools"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"ferryman-agent/pkg/data/logging"
	"ferryman-agent/pkg/utils/fileutil"
)

type GlobParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

type GlobResponseMetadata struct {
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

type GlobTool struct {
	workspace workspace.Workspace
}

func NewGlobTool(opts ...workspace.WorkspaceOption) (*toolcore.Tool, error) {
	options := workspace.ApplyOptions(opts)
	if strings.TrimSpace(options.RootDir) == "" {
		return nil, fmt.Errorf("workspace root dir is required")
	}
	ws := workspace.NewWorkspace(options.RootDir)
	g := &GlobTool{workspace: ws}
	return toolcore.NewTool(g.Info(), g.Run, toolcore.WithMiddleware(options.Middlewares...)), nil
}

func (g *GlobTool) Info() *toolcore.ToolInfo {
	return &toolcore.ToolInfo{
		Name:        GlobToolName,
		Description: globDescription,
		Parameters: map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "The glob pattern to match files against",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "The directory to search in. Defaults to the current working directory.",
			},
		},
		Required: []string{"pattern"},
	}
}

func (g *GlobTool) Run(ctx context.Context, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	var params GlobParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Pattern == "" {
		return toolcore.NewTextErrorResponse("pattern is required"), nil
	}

	searchPath, err := g.workspace.Resolve(params.Path)
	if err != nil {
		return toolcore.NewTextErrorResponse(err.Error()), nil
	}

	files, truncated, err := globFiles(params.Pattern, searchPath, 100)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("error finding files: %w", err)
	}

	var output string
	if len(files) == 0 {
		output = "No files found"
	} else {
		output = strings.Join(files, "\n")
		if truncated {
			output += "\n\n(Results are truncated. Consider using a more specific path or pattern.)"
		}
	}

	return toolcore.WithResponseMetadata(
		toolcore.NewTextResponse(output),
		GlobResponseMetadata{
			NumberOfFiles: len(files),
			Truncated:     truncated,
		},
	), nil
}

func globFiles(pattern, searchPath string, limit int) ([]string, bool, error) {
	cmdRg := fileutil.GetRgCmd(pattern)
	if cmdRg != nil {
		cmdRg.Dir = searchPath
		matches, err := runRipgrep(cmdRg, searchPath, limit)
		if err == nil {
			return matches, len(matches) >= limit && limit > 0, nil
		}
		logging.Warn(fmt.Sprintf("Ripgrep execution failed: %v. Falling back to doublestar.", err))
	}

	return fileutil.GlobWithDoublestar(pattern, searchPath, limit)
}

func runRipgrep(cmd *exec.Cmd, searchRoot string, limit int) ([]string, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("ripgrep: %w\n%s", err, out)
	}

	var matches []string
	for _, p := range bytes.Split(out, []byte{0}) {
		if len(p) == 0 {
			continue
		}
		absPath := string(p)
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(searchRoot, absPath)
		}
		if fileutil.SkipHidden(absPath) {
			continue
		}
		matches = append(matches, absPath)
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return len(matches[i]) < len(matches[j])
	})

	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}
