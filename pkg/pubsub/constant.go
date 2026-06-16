package pubsub

type EventType string

const (
	CreatedEvent EventType = "created"
	UpdatedEvent EventType = "updated"
	DeletedEvent EventType = "deleted"
	bufferSize             = 64
)
