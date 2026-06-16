package db

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DatabaseConfig struct {
	Type                DatabaseType `json:"type"`
	DSN                 string       `json:"dsn,omitempty"`
	Host                string       `json:"host,omitempty"`
	Port                int          `json:"port,omitempty"`
	Username            string       `json:"username,omitempty"`
	Password            string       `json:"password,omitempty"`
	Database            string       `json:"database,omitempty"`
	Charset             string       `json:"charset,omitempty"`
	ParseTime           bool         `json:"parseTime,omitempty"`
	Loc                 string       `json:"loc,omitempty"`
	MaxOpenConns        int          `json:"maxOpenConns,omitempty"`
	MaxIdleConns        int          `json:"maxIdleConns,omitempty"`
	ConnMaxLifetimeSecs int          `json:"connMaxLifetimeSecs,omitempty"`
	LogLevel            string       `json:"logLevel,omitempty"`
}

type DbClient struct {
	DB     *gorm.DB
	Config DatabaseConfig
}

func NewClient(options ...DatabaseOption) (*DbClient, error) {
	cfg := DatabaseConfig{
		Type: DatabaseSQLite,
		DSN:  filepath.Join(".ferryer", "agent.db"),
	}
	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}

	if cfg.Type == "" {
		cfg.Type = DatabaseSQLite
	}
	gormCfg := &gorm.Config{Logger: logger.Default.LogMode(parseLogLevel(cfg.LogLevel))}
	var (
		database *gorm.DB
		err      error
	)
	switch cfg.Type {
	case DatabaseSQLite:
		dsn := cfg.DSN
		if dsn == "" {
			dsn = filepath.Join(".ferryer", "agent.db")
		}
		if dir := filepath.Dir(dsn); dir != "." && dir != "" {
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
				return nil, mkErr
			}
		}
		database, err = gorm.Open(sqlite.Open(dsn), gormCfg)
	case DatabaseMySQL:
		dsn := cfg.DSN
		if dsn == "" {
			dsn = mysqlDSN(cfg)
		}
		database, err = gorm.Open(mysql.Open(dsn), gormCfg)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}
	if err != nil {
		return nil, err
	}

	if sqlDB, err := database.DB(); err == nil {
		if cfg.MaxOpenConns > 0 {
			sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		}
		if cfg.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		}
		if cfg.ConnMaxLifetimeSecs > 0 {
			sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSecs) * time.Second)
		}
	}

	return &DbClient{DB: database, Config: cfg}, nil
}

func (c *DbClient) AutoMigrate(models ...any) error {
	return c.DB.AutoMigrate(models...)
}

func mysqlDSN(cfg DatabaseConfig) string {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Port
	if port == 0 {
		port = 3306
	}
	charset := cfg.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	loc := cfg.Loc
	if loc == "" {
		loc = "Local"
	}
	values := url.Values{}
	values.Set("charset", charset)
	values.Set("parseTime", fmt.Sprintf("%t", cfg.ParseTime))
	values.Set("loc", loc)
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
		cfg.Username,
		cfg.Password,
		host,
		port,
		cfg.Database,
		values.Encode(),
	)
}

func parseLogLevel(level string) logger.LogLevel {
	switch level {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "warn":
		return logger.Warn
	case "info":
		return logger.Info
	default:
		return logger.Warn
	}
}
