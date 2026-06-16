package version

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	datadb "ferryman-agent/pkg/data/db"
	"ferryman-agent/pkg/pubsub"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("file version not found")

type FileVersionRecord struct {
	ID        string `gorm:"column:id;primaryKey" json:"id"`
	SessionID string `gorm:"column:session_id;index" json:"sessionId"`
	Path      string `gorm:"column:path;index" json:"path"`
	Content   string `gorm:"column:content" json:"content"`
	Version   string `gorm:"column:version" json:"version"`
	CreatedAt int64  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt int64  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (FileVersionRecord) TableName() string {
	return "history"
}

type FileVersionService interface {
	pubsub.Subscriber[FileVersionRecord]
	Create(ctx context.Context, sessionID, path, content string) (FileVersionRecord, error)
	CreateVersion(ctx context.Context, sessionID, path, content string) (FileVersionRecord, error)
	Get(ctx context.Context, id string) (FileVersionRecord, error)
	GetByPathAndSession(ctx context.Context, path, sessionID string) (FileVersionRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]FileVersionRecord, error)
	ListLatestSessionFiles(ctx context.Context, sessionID string) ([]FileVersionRecord, error)
	Update(ctx context.Context, file FileVersionRecord) (FileVersionRecord, error)
	Delete(ctx context.Context, id string) error
	DeleteSessionFiles(ctx context.Context, sessionID string) error
}

type fileVersionService struct {
	*pubsub.Broker[FileVersionRecord]
	client *datadb.DbClient
}

func NewFileVersionService(options ...Option) FileVersionService {
	opts := optionsConfig{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	if opts.client == nil {
		panic("version db client is required")
	}
	if err := opts.client.AutoMigrate(&FileVersionRecord{}); err != nil {
		panic(err)
	}
	return &fileVersionService{Broker: pubsub.NewBroker[FileVersionRecord](), client: opts.client}
}

func (s *fileVersionService) Create(ctx context.Context, sessionID, path, content string) (FileVersionRecord, error) {
	return s.createWithVersion(ctx, sessionID, path, content, InitialVersion)
}

func (s *fileVersionService) CreateVersion(ctx context.Context, sessionID, path, content string) (FileVersionRecord, error) {
	var files []FileVersionRecord
	if err := s.client.DB.WithContext(ctx).Where("path = ?", path).Order("created_at desc, id desc").Find(&files).Error; err != nil {
		return FileVersionRecord{}, err
	}
	if len(files) == 0 {
		return s.Create(ctx, sessionID, path, content)
	}

	latestFile := files[0]
	latestVersion := latestFile.Version
	var nextVersion string
	if latestVersion == InitialVersion {
		nextVersion = "v1"
	} else if strings.HasPrefix(latestVersion, "v") {
		versionNum, err := strconv.Atoi(latestVersion[1:])
		if err != nil {
			nextVersion = fmt.Sprintf("v%d", latestFile.CreatedAt)
		} else {
			nextVersion = fmt.Sprintf("v%d", versionNum+1)
		}
	} else {
		nextVersion = fmt.Sprintf("v%d", latestFile.CreatedAt)
	}
	return s.createWithVersionAfter(ctx, sessionID, path, content, nextVersion, latestFile.CreatedAt)
}

func (s *fileVersionService) createWithVersion(ctx context.Context, sessionID, path, content, version string) (FileVersionRecord, error) {
	return s.createWithVersionAfter(ctx, sessionID, path, content, version, 0)
}

func (s *fileVersionService) createWithVersionAfter(ctx context.Context, sessionID, path, content, version string, after int64) (FileVersionRecord, error) {
	createdAt := int64(0)
	if after > 0 {
		createdAt = after + 1
	}
	dbFile := FileVersionRecord{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Path:      path,
		Content:   content,
		Version:   version,
		CreatedAt: createdAt,
	}
	if err := s.client.DB.WithContext(ctx).Create(&dbFile).Error; err != nil {
		return FileVersionRecord{}, err
	}
	s.Publish(pubsub.CreatedEvent, dbFile)
	return dbFile, nil
}

func (s *fileVersionService) Get(ctx context.Context, id string) (FileVersionRecord, error) {
	var dbFile FileVersionRecord
	if err := s.client.DB.WithContext(ctx).First(&dbFile, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FileVersionRecord{}, ErrNotFound
		}
		return FileVersionRecord{}, err
	}
	return dbFile, nil
}

func (s *fileVersionService) GetByPathAndSession(ctx context.Context, path, sessionID string) (FileVersionRecord, error) {
	var dbFile FileVersionRecord
	if err := s.client.DB.WithContext(ctx).
		Where("path = ? AND session_id = ?", path, sessionID).
		Order("created_at desc, id desc").
		First(&dbFile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FileVersionRecord{}, ErrNotFound
		}
		return FileVersionRecord{}, err
	}
	return dbFile, nil
}

func (s *fileVersionService) ListBySession(ctx context.Context, sessionID string) ([]FileVersionRecord, error) {
	var dbFiles []FileVersionRecord
	if err := s.client.DB.WithContext(ctx).Where("session_id = ?", sessionID).Order("created_at asc").Find(&dbFiles).Error; err != nil {
		return nil, err
	}
	return dbFiles, nil
}

func (s *fileVersionService) ListLatestSessionFiles(ctx context.Context, sessionID string) ([]FileVersionRecord, error) {
	var dbFiles []FileVersionRecord
	if err := s.client.DB.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("path asc, created_at desc, id desc").
		Find(&dbFiles).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	files := make([]FileVersionRecord, 0, len(dbFiles))
	for _, dbFile := range dbFiles {
		if seen[dbFile.Path] {
			continue
		}
		seen[dbFile.Path] = true
		files = append(files, dbFile)
	}
	return files, nil
}

func (s *fileVersionService) Update(ctx context.Context, file FileVersionRecord) (FileVersionRecord, error) {
	var dbFile FileVersionRecord
	if err := s.client.DB.WithContext(ctx).First(&dbFile, "id = ?", file.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FileVersionRecord{}, ErrNotFound
		}
		return FileVersionRecord{}, err
	}
	dbFile.Content = file.Content
	dbFile.Version = file.Version
	if err := s.client.DB.WithContext(ctx).Save(&dbFile).Error; err != nil {
		return FileVersionRecord{}, err
	}
	s.Publish(pubsub.UpdatedEvent, dbFile)
	return dbFile, nil
}

func (s *fileVersionService) Delete(ctx context.Context, id string) error {
	file, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	result := s.client.DB.WithContext(ctx).Delete(&FileVersionRecord{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	s.Publish(pubsub.DeletedEvent, file)
	return nil
}

func (s *fileVersionService) DeleteSessionFiles(ctx context.Context, sessionID string) error {
	files, err := s.ListBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := s.Delete(ctx, file.ID); err != nil {
			return err
		}
	}
	return nil
}
