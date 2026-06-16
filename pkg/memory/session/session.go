package session

import (
	"context"
	"errors"

	datadb "ferryman-agent/pkg/data/db"
	"ferryman-agent/pkg/pubsub"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("session not found")

type SessionRecord struct {
	ID               string  `gorm:"column:id;primaryKey" json:"id"`
	ParentSessionID  string  `gorm:"column:parent_session_id;index" json:"parentSessionId,omitempty"`
	Title            string  `gorm:"column:title" json:"title"`
	MessageCount     int64   `gorm:"column:message_count" json:"messageCount"`
	PromptTokens     int64   `gorm:"column:prompt_tokens" json:"promptTokens"`
	CompletionTokens int64   `gorm:"column:completion_tokens" json:"completionTokens"`
	SummaryMessageID string  `gorm:"column:summary_message_id" json:"summaryMessageId,omitempty"`
	Cost             float64 `gorm:"column:cost" json:"cost"`
	CreatedAt        int64   `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt        int64   `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (SessionRecord) TableName() string {
	return "sessions"
}

type Service interface {
	pubsub.Subscriber[SessionRecord]
	Create(ctx context.Context, title string) (SessionRecord, error)
	CreateTitleSession(ctx context.Context, parentSessionID string) (SessionRecord, error)
	CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (SessionRecord, error)
	Get(ctx context.Context, id string) (SessionRecord, error)
	List(ctx context.Context) ([]SessionRecord, error)
	Save(ctx context.Context, session SessionRecord) (SessionRecord, error)
	Delete(ctx context.Context, id string) error
}

type service struct {
	*pubsub.Broker[SessionRecord]
	client *datadb.DbClient
}

func NewSessionService(options ...Option) Service {
	opts := optionsConfig{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	if opts.client == nil {
		panic("session db client is required")
	}
	if err := opts.client.AutoMigrate(&SessionRecord{}); err != nil {
		panic(err)
	}
	return &service{Broker: pubsub.NewBroker[SessionRecord](), client: opts.client}
}

func (s *service) Create(ctx context.Context, title string) (SessionRecord, error) {
	dbSession := SessionRecord{
		ID: uuid.New().String(), Title: title,
	}
	err := s.client.DB.WithContext(ctx).Create(&dbSession).Error
	if err != nil {
		return SessionRecord{}, err
	}
	s.Publish(pubsub.CreatedEvent, dbSession)
	return dbSession, nil
}

func (s *service) CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (SessionRecord, error) {
	dbSession := SessionRecord{
		ID:              toolCallID,
		ParentSessionID: parentSessionID,
		Title:           title,
	}
	err := s.client.DB.WithContext(ctx).Create(&dbSession).Error
	if err != nil {
		return SessionRecord{}, err
	}
	s.Publish(pubsub.CreatedEvent, dbSession)
	return dbSession, nil
}

func (s *service) CreateTitleSession(ctx context.Context, parentSessionID string) (SessionRecord, error) {
	dbSession := SessionRecord{
		ID:              "title-" + parentSessionID,
		ParentSessionID: parentSessionID,
		Title:           "Generate a title",
	}
	err := s.client.DB.WithContext(ctx).Create(&dbSession).Error
	if err != nil {
		return SessionRecord{}, err
	}
	s.Publish(pubsub.CreatedEvent, dbSession)
	return dbSession, nil
}

func (s *service) Delete(ctx context.Context, id string) error {
	session, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	result := s.client.DB.WithContext(ctx).Delete(&SessionRecord{}, "id = ?", session.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	s.Publish(pubsub.DeletedEvent, session)
	return nil
}

func (s *service) Get(ctx context.Context, id string) (SessionRecord, error) {
	var item SessionRecord
	if err := s.client.DB.WithContext(ctx).First(&item, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionRecord{}, ErrNotFound
		}
		return SessionRecord{}, err
	}
	return item, nil
}

func (s *service) Save(ctx context.Context, session SessionRecord) (SessionRecord, error) {
	var item SessionRecord
	if err := s.client.DB.WithContext(ctx).First(&item, "id = ?", session.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionRecord{}, ErrNotFound
		}
		return SessionRecord{}, err
	}
	item.Title = session.Title
	item.PromptTokens = session.PromptTokens
	item.CompletionTokens = session.CompletionTokens
	item.SummaryMessageID = session.SummaryMessageID
	item.Cost = session.Cost
	if err := s.client.DB.WithContext(ctx).Save(&item).Error; err != nil {
		return SessionRecord{}, err
	}
	s.Publish(pubsub.UpdatedEvent, item)
	return item, nil
}

func (s *service) List(ctx context.Context) ([]SessionRecord, error) {
	var rows []SessionRecord
	if err := s.client.DB.WithContext(ctx).Where("parent_session_id = ?", "").Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
