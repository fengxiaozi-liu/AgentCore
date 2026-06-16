package message

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	datadb "ferryman-agent/pkg/data/db"
	"ferryman-agent/pkg/memory/session"
	"ferryman-agent/pkg/pubsub"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("message not found")

type CreateMessageParams struct {
	Kind  MessageKind
	Role  MessageRole
	Parts []ContentPart
	Model string
}

type ListCondition struct {
	Kinds          []MessageKind
	AfterMessageID string
}

type Service interface {
	pubsub.Subscriber[MessageRecord]
	Create(ctx context.Context, sessionID string, params CreateMessageParams) (MessageRecord, error)
	Update(ctx context.Context, message MessageRecord) error
	Get(ctx context.Context, id string) (MessageRecord, error)
	List(ctx context.Context, sessionID string, conditions ...ListCondition) ([]MessageRecord, error)
	Delete(ctx context.Context, id string) error
	DeleteSessionMessages(ctx context.Context, sessionID string) error
}

type service struct {
	*pubsub.Broker[MessageRecord]
	client *datadb.DbClient
}

func NewService(options ...Option) Service {
	opts := optionsConfig{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	if opts.client == nil {
		panic("message db client is required")
	}
	if err := opts.client.AutoMigrate(&MessageRecord{}); err != nil {
		panic(err)
	}
	return &service{
		Broker: pubsub.NewBroker[MessageRecord](),
		client: opts.client,
	}
}

func (s *service) Delete(ctx context.Context, id string) error {
	message, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.client.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&MessageRecord{}, "id = ?", message.ID).Error; err != nil {
			return err
		}
		return decrementMessageCount(tx, message.SessionID)
	}); err != nil {
		return err
	}
	s.Publish(pubsub.DeletedEvent, message)
	return nil
}

func (s *service) Create(ctx context.Context, sessionID string, params CreateMessageParams) (MessageRecord, error) {
	if params.Kind == "" {
		params.Kind = MessageKindConversation
	}
	if params.Role != Assistant {
		params.Parts = append(params.Parts, Finish{Reason: "stop"})
	}
	dbMessage := MessageRecord{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Kind:      params.Kind,
		Role:      params.Role,
		Parts:     ContentParts(params.Parts),
		Model:     string(params.Model),
	}
	err := s.client.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&dbMessage).Error; err != nil {
			return err
		}
		return tx.Model(&session.SessionRecord{}).Where("id = ?", dbMessage.SessionID).
			Update("message_count", gorm.Expr("message_count + ?", 1)).Error
	})
	if err != nil {
		return MessageRecord{}, err
	}
	message := normalizeRecord(dbMessage)
	s.Publish(pubsub.CreatedEvent, message)
	return message, nil
}

func (s *service) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	messages, err := s.List(ctx, sessionID, ListCondition{
		Kinds: []MessageKind{MessageKindConversation, MessageKindSummary},
	})
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.SessionID == sessionID {
			if err := s.Delete(ctx, message.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *service) Update(ctx context.Context, message MessageRecord) error {
	finishedAt := int64(0)
	if f := message.FinishPart(); f != nil {
		finishedAt = f.Time
	}
	var item MessageRecord
	if err := s.client.DB.WithContext(ctx).First(&item, "id = ?", message.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	item.Parts = message.Parts
	item.FinishedAt = finishedAt
	if err := s.client.DB.WithContext(ctx).Save(&item).Error; err != nil {
		return err
	}
	message.UpdatedAt = time.Now().Unix()
	s.Publish(pubsub.UpdatedEvent, message)
	return nil
}

func (s *service) Get(ctx context.Context, id string) (MessageRecord, error) {
	var item MessageRecord
	if err := s.client.DB.WithContext(ctx).First(&item, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return MessageRecord{}, ErrNotFound
		}
		return MessageRecord{}, err
	}
	return normalizeRecord(item), nil
}

func (s *service) List(ctx context.Context, sessionID string, conditions ...ListCondition) ([]MessageRecord, error) {
	condition := ListCondition{}
	if len(conditions) > 0 {
		condition = conditions[0]
	}
	kinds := make([]string, 0, len(condition.Kinds))
	for _, kind := range condition.Kinds {
		kinds = append(kinds, string(kind))
	}
	if len(kinds) == 0 {
		kinds = []string{string(MessageKindConversation)}
	}

	query := s.client.DB.WithContext(ctx).Where("session_id = ?", sessionID)
	if len(kinds) == 1 && kinds[0] == string(MessageKindConversation) {
		query = query.Where("(kind = ? OR kind = '')", string(MessageKindConversation))
	} else {
		query = query.Where("kind IN ?", kinds)
	}
	if condition.AfterMessageID != "" {
		var after MessageRecord
		if err := s.client.DB.WithContext(ctx).First(&after, "id = ?", condition.AfterMessageID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		if after.SessionID != sessionID {
			return nil, ErrNotFound
		}
		query = query.Where("created_at > ?", after.CreatedAt)
	}

	var rows []MessageRecord
	if err := query.Order("created_at asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	messages := make([]MessageRecord, len(rows))
	for i, row := range rows {
		messages[i] = normalizeRecord(row)
	}
	return messages, nil
}

func decrementMessageCount(tx *gorm.DB, sessionID string) error {
	return tx.Model(&session.SessionRecord{}).Where("id = ?", sessionID).
		Update("message_count", gorm.Expr("CASE WHEN message_count - 1 < 0 THEN 0 ELSE message_count - 1 END")).Error
}

func (p ContentParts) Value() (driver.Value, error) {
	parts, err := marshallParts([]ContentPart(p))
	if err != nil {
		return nil, err
	}
	return string(parts), nil
}

func (p *ContentParts) Scan(value any) error {
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	case nil:
		*p = nil
		return nil
	default:
		return fmt.Errorf("unsupported content parts value: %T", value)
	}
	parts, err := unmarshallParts(data)
	if err != nil {
		return err
	}
	*p = ContentParts(parts)
	return nil
}

func normalizeRecord(record MessageRecord) MessageRecord {
	record.Kind = normalizeKind(record.Kind)
	return record
}

func normalizeKind(kind MessageKind) MessageKind {
	if kind == "" {
		return MessageKindConversation
	}
	return kind
}

type partWrapper struct {
	Type partType    `json:"type"`
	Data ContentPart `json:"data"`
}

func marshallParts(parts []ContentPart) ([]byte, error) {
	wrappedParts := make([]partWrapper, len(parts))
	for i, part := range parts {
		var typ partType
		switch part.(type) {
		case ReasoningContent:
			typ = reasoningType
		case TextContent:
			typ = textType
		case ImageURLContent:
			typ = imageURLType
		case BinaryContent:
			typ = binaryType
		case ToolCall:
			typ = toolCallType
		case ToolResult:
			typ = toolResultType
		case Finish:
			typ = finishType
		default:
			return nil, fmt.Errorf("unknown part type: %T", part)
		}
		wrappedParts[i] = partWrapper{Type: typ, Data: part}
	}
	return json.Marshal(wrappedParts)
}

func unmarshallParts(data []byte) ([]ContentPart, error) {
	temp := []json.RawMessage{}
	if err := json.Unmarshal(data, &temp); err != nil {
		return nil, err
	}

	parts := make([]ContentPart, 0)
	for _, rawPart := range temp {
		var wrapper struct {
			Type partType        `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rawPart, &wrapper); err != nil {
			return nil, err
		}
		switch wrapper.Type {
		case reasoningType:
			part := ReasoningContent{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case textType:
			part := TextContent{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case imageURLType:
			part := ImageURLContent{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case binaryType:
			part := BinaryContent{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case toolCallType:
			part := ToolCall{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case toolResultType:
			part := ToolResult{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case finishType:
			part := Finish{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		default:
			return nil, fmt.Errorf("unknown part type: %s", wrapper.Type)
		}
	}
	return parts, nil
}
