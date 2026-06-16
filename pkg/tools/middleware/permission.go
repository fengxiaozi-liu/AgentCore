package middleware

import (
	"context"

	"ferryman-agent/pkg/permission"
	toolcore "ferryman-agent/pkg/tools"
)

const ExtraKeyPermission = "permission.requests"

type PermissionResolver interface {
	ResolvePermissionRequests(ctx context.Context, call toolcore.ToolCall) ([]permission.PermissionRequest, error)
}

func PermissionMiddleware(permissions permission.Service, resolver PermissionResolver) toolcore.ToolMiddleware {
	return func(next toolcore.ToolHandler) toolcore.ToolHandler {
		return func(ctx context.Context, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
			if permissions == nil || resolver == nil {
				return next(ctx, call)
			}
			requests, err := resolver.ResolvePermissionRequests(ctx, call)
			if err != nil {
				return toolcore.NewTextErrorResponse(err.Error()), nil
			}
			for _, request := range requests {
				allowed := permissions.Request(request)
				if !allowed {
					return toolcore.ToolResponse{}, permission.ErrorPermissionDenied
				}
			}
			resp, err := next(ctx, call)
			if len(requests) > 0 {
				resp = toolcore.WithResponseExtra(resp, ExtraKeyPermission, requests)
			}
			return resp, err
		}
	}
}
