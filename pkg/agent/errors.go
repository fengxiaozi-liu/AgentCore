package agent

import "errors"

var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
	ErrToolNotFound     = errors.New("tool not found")
	ErrToolAmbiguous    = errors.New("ambiguous tool name")
)
