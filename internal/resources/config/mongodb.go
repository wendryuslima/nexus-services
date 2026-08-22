package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	mongoDBURIEnvironment              = "MONGODB_URI"
	mongoDBNameEnvironment             = "MONGODB_DATABASE"
	mongoDBConnectTimeoutEnvironment   = "MONGODB_CONNECT_TIMEOUT"
	mongoDBOperationTimeoutEnvironment = "MONGODB_OPERATION_TIMEOUT"
	mongoDBShutdownTimeoutEnvironment  = "MONGODB_SHUTDOWN_TIMEOUT"

	defaultMongoDBConnectTimeout   = 10 * time.Second
	defaultMongoDBOperationTimeout = 5 * time.Second
	defaultMongoDBShutdownTimeout  = 10 * time.Second

	maximumMongoDBTimeout = 5 * time.Minute
)

type MongoDBConfig struct {
	URI              string
	Database         string
	ConnectTimeout   time.Duration
	OperationTimeout time.Duration
	ShutdownTimeout  time.Duration
}

func LoadMongoDBConfig() (MongoDBConfig, error) {
	uri, err := readRequiredEnvironment(mongoDBURIEnvironment)
	if err != nil {
		return MongoDBConfig{}, err
	}

	database, err := readRequiredEnvironment(mongoDBNameEnvironment)
	if err != nil {
		return MongoDBConfig{}, err
	}

	connectTimeout, err := readDurationEnvironment(
		mongoDBConnectTimeoutEnvironment,
		defaultMongoDBConnectTimeout,
	)
	if err != nil {
		return MongoDBConfig{}, err
	}

	operationTimeout, err := readDurationEnvironment(
		mongoDBOperationTimeoutEnvironment,
		defaultMongoDBOperationTimeout,
	)
	if err != nil {
		return MongoDBConfig{}, err
	}

	shutdownTimeout, err := readDurationEnvironment(
		mongoDBShutdownTimeoutEnvironment,
		defaultMongoDBShutdownTimeout,
	)
	if err != nil {
		return MongoDBConfig{}, err
	}

	config := MongoDBConfig{
		URI:              uri,
		Database:         database,
		ConnectTimeout:   connectTimeout,
		OperationTimeout: operationTimeout,
		ShutdownTimeout:  shutdownTimeout,
	}

	if err := config.Validate(); err != nil {
		return MongoDBConfig{}, err
	}

	return config, nil
}

func (config MongoDBConfig) Validate() error {
	if strings.TrimSpace(config.URI) == "" {
		return fmt.Errorf(
			"%w: URI is empty",
			ErrInvalidMongoDBConfig,
		)
	}

	if strings.TrimSpace(config.Database) == "" {
		return fmt.Errorf(
			"%w: database name is empty",
			ErrInvalidMongoDBConfig,
		)
	}

	if err := validateTimeout(
		"connect timeout",
		config.ConnectTimeout,
	); err != nil {
		return err
	}

	if err := validateTimeout(
		"operation timeout",
		config.OperationTimeout,
	); err != nil {
		return err
	}

	if err := validateTimeout(
		"shutdown timeout",
		config.ShutdownTimeout,
	); err != nil {
		return err
	}

	return nil
}

func readRequiredEnvironment(
	name string,
) (string, error) {
	value, exists := os.LookupEnv(name)
	if !exists || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf(
			"%w: %s",
			ErrMissingEnvironmentVariable,
			name,
		)
	}

	return strings.TrimSpace(value), nil
}

func readDurationEnvironment(
	name string,
	defaultValue time.Duration,
) (time.Duration, error) {
	rawValue, exists := os.LookupEnv(name)
	if !exists {
		return defaultValue, nil
	}

	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return 0, fmt.Errorf(
			"%w: %s cannot be empty",
			ErrInvalidEnvironmentVariable,
			name,
		)
	}

	value, err := time.ParseDuration(rawValue)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: %s must be a duration: %v",
			ErrInvalidEnvironmentVariable,
			name,
			err,
		)
	}

	if err := validateTimeout(name, value); err != nil {
		return 0, err
	}

	return value, nil
}

func validateTimeout(
	name string,
	value time.Duration,
) error {
	if value <= 0 || value > maximumMongoDBTimeout {
		return fmt.Errorf(
			"%w: %s must be between zero and %s",
			ErrInvalidMongoDBConfig,
			name,
			maximumMongoDBTimeout,
		)
	}

	return nil
}
