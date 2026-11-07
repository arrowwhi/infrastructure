package txmanager

import "errors"

var (
	ErrPostgresNotInitialized = errors.New("postgres connection not initialized")
	ErrRedisNotInitialized    = errors.New("redis connection not initialized")
)
