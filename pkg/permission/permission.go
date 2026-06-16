package permission

import (
	"slices"
	"sync"

	"ferryman-agent/pkg/pubsub"
	"github.com/google/uuid"
)

type PermissionRequest struct {
	ID          string `json:"id,omitempty"`
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	ResourceURI string `json:"resource_uri"`
}

type Service interface {
	pubsub.Subscriber[PermissionRequest]
	GrantPersistant(permission PermissionRequest)
	Grant(permission PermissionRequest)
	Deny(permission PermissionRequest)
	Request(permission PermissionRequest) bool
	AutoApproveSession(sessionID string)
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]
	sessionPermissions  []PermissionRequest
	pendingRequests     sync.Map
	autoApproveSessions []string
}

func NewService() Service {
	return &permissionService{
		Broker:             pubsub.NewBroker[PermissionRequest](),
		sessionPermissions: make([]PermissionRequest, 0),
	}
}

func (s *permissionService) GrantPersistant(permission PermissionRequest) {
	if respCh, ok := s.pendingRequests.Load(permission.ID); ok {
		respCh.(chan bool) <- true
	}
	s.sessionPermissions = append(s.sessionPermissions, permission)
}

func (s *permissionService) Grant(permission PermissionRequest) {
	if respCh, ok := s.pendingRequests.Load(permission.ID); ok {
		respCh.(chan bool) <- true
	}
}

func (s *permissionService) Deny(permission PermissionRequest) {
	if respCh, ok := s.pendingRequests.Load(permission.ID); ok {
		respCh.(chan bool) <- false
	}
}

func (s *permissionService) Request(permission PermissionRequest) bool {
	if slices.Contains(s.autoApproveSessions, permission.SessionID) {
		return true
	}
	if permission.ID == "" {
		permission.ID = uuid.New().String()
	}
	for _, p := range s.sessionPermissions {
		if p.ToolName == permission.ToolName &&
			p.Action == permission.Action &&
			p.SessionID == permission.SessionID &&
			p.ResourceURI == permission.ResourceURI {
			return true
		}
	}
	respCh := make(chan bool, 1)
	s.pendingRequests.Store(permission.ID, respCh)
	defer s.pendingRequests.Delete(permission.ID)
	s.Publish(pubsub.CreatedEvent, permission)
	return <-respCh
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.autoApproveSessions = append(s.autoApproveSessions, sessionID)
}
