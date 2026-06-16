package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"ferryman-agent/pkg/permission"
	toolcore "ferryman-agent/pkg/tools"
	toolmiddleware "ferryman-agent/pkg/tools/middleware"
	"ferryman-agent/pkg/utils/diff"
)

type PermissionResolver struct {
	WorkspaceRoot string
}

func Permission(workspaceRoot string, permissions permission.Service) toolcore.ToolMiddleware {
	return toolmiddleware.PermissionMiddleware(permissions, PermissionResolver{WorkspaceRoot: workspaceRoot})
}

func (r PermissionResolver) ResolvePermissionRequests(ctx context.Context, call toolcore.ToolCall) ([]permission.PermissionRequest, error) {
	sessionID, _ := toolcore.GetContextValues(ctx)
	switch call.Name {
	case BashToolName:
		var params BashParams
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			return nil, err
		}
		if isSafeReadOnly(params.Command) {
			return []permission.PermissionRequest{}, nil
		}
		return []permission.PermissionRequest{{
			SessionID:   sessionID,
			ResourceURI: r.WorkspaceRoot,
			ToolName:    BashToolName,
			Action:      "execute",
			Description: fmt.Sprintf("Execute command: %s", params.Command),
			Params:      params,
		}}, nil
	case FetchToolName:
		var params FetchParams
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			return nil, err
		}
		return []permission.PermissionRequest{{
			SessionID:   sessionID,
			ResourceURI: params.URL,
			ToolName:    FetchToolName,
			Action:      "fetch",
			Description: fmt.Sprintf("Fetch content from URL: %s", params.URL),
			Params:      params,
		}}, nil
	case WriteToolName:
		var params WriteParams
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			return nil, err
		}
		return []permission.PermissionRequest{filePermission(sessionID, r.WorkspaceRoot, WriteToolName, "write", params.FilePath, params)}, nil
	case EditToolName:
		var params EditParams
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			return nil, err
		}
		return []permission.PermissionRequest{filePermission(sessionID, r.WorkspaceRoot, EditToolName, "write", params.FilePath, params)}, nil
	case PatchToolName:
		var params PatchParams
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			return nil, err
		}
		files := diff.IdentifyFilesNeeded(params.PatchText)
		files = append(files, diff.IdentifyFilesAdded(params.PatchText)...)
		requests := make([]permission.PermissionRequest, 0, len(files))
		for _, path := range files {
			requests = append(requests, filePermission(sessionID, r.WorkspaceRoot, PatchToolName, "write", path, params))
		}
		return requests, nil
	default:
		return []permission.PermissionRequest{}, nil
	}
}

func filePermission(sessionID, workspaceRoot, toolName, action, filePath string, params any) permission.PermissionRequest {
	permissionPath := filepath.Dir(filePath)
	if !filepath.IsAbs(permissionPath) || strings.HasPrefix(filePath, workspaceRoot) {
		permissionPath = workspaceRoot
	}
	return permission.PermissionRequest{
		SessionID:   sessionID,
		ResourceURI: permissionPath,
		ToolName:    toolName,
		Action:      action,
		Description: fmt.Sprintf("%s %s", action, filePath),
		Params:      params,
	}
}

func isSafeReadOnly(command string) bool {
	cmdLower := strings.ToLower(command)
	for _, safe := range SafeReadOnlyCommands {
		if strings.HasPrefix(cmdLower, strings.ToLower(safe)) {
			if len(cmdLower) == len(safe) || cmdLower[len(safe)] == ' ' || cmdLower[len(safe)] == '-' {
				return true
			}
		}
	}
	return false
}
