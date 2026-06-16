package db

type DatabaseType string

const (
	DatabaseSQLite DatabaseType = "sqlite"
	DatabaseMySQL  DatabaseType = "mysql"
)
