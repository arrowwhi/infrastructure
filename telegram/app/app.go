// Package app — прежний путь запуска сервиса.
//
// Deprecated: пакет оставлен для обратной совместимости, весь код переехал в
// [github.com/arrowwhi/infrastructure/telegram]. Все типы здесь — алиасы. Новые имена:
// Config → AppConfig, New → NewApp.
package app

import (
	"context"

	"github.com/arrowwhi/infrastructure/telegram"
)

type (
	App           = telegram.App
	Config        = telegram.AppConfig
	AdminConfig   = telegram.AdminConfig
	Component     = telegram.Component
	ComponentFunc = telegram.ComponentFunc
)

// New вызывает [telegram.NewApp].
func New(ctx context.Context, cfg Config) (*App, error) { return telegram.NewApp(ctx, cfg) }

// LoadConfig вызывает [telegram.LoadConfig].
func LoadConfig(cfg any) error { return telegram.LoadConfig(cfg) }
