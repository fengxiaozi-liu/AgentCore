package db

type DatabaseOption func(*DatabaseConfig)

func WithType(databaseType DatabaseType) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Type = databaseType
	}
}

func WithSQLite(path string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Type = DatabaseSQLite
		config.DSN = path
	}
}

func WithMySQL(dsn string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Type = DatabaseMySQL
		config.DSN = dsn
	}
}

func WithDatabaseConfig(next DatabaseConfig) DatabaseOption {
	return func(config *DatabaseConfig) {
		*config = next
	}
}

func WithDSN(dsn string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.DSN = dsn
	}
}

func WithHost(host string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Host = host
	}
}

func WithPort(port int) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Port = port
	}
}

func WithUsername(username string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Username = username
	}
}

func WithPassword(password string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Password = password
	}
}

func WithDatabase(database string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Database = database
	}
}

func WithCharset(charset string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Charset = charset
	}
}

func WithParseTime(parseTime bool) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.ParseTime = parseTime
	}
}

func WithLoc(loc string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.Loc = loc
	}
}

func WithMaxOpenConns(maxOpenConns int) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.MaxOpenConns = maxOpenConns
	}
}

func WithMaxIdleConns(maxIdleConns int) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.MaxIdleConns = maxIdleConns
	}
}

func WithConnMaxLifetimeSecs(connMaxLifetimeSecs int) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.ConnMaxLifetimeSecs = connMaxLifetimeSecs
	}
}

func WithLogLevel(level string) DatabaseOption {
	return func(config *DatabaseConfig) {
		config.LogLevel = level
	}
}
