package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/arrowwhi/infrastructure/observability/logger"

	http_swagger "github.com/swaggo/http-swagger"
)

type DocsConfig struct {
	URL      string `yaml:"url" env:"DOCS_SWAGGER_URL" env-default:"/openapi.yaml"`
	Path     string `yaml:"path" env:"DOCS_PATH" env-default:"/docs"`
	SpecFile string `yaml:"spec_file" env:"DOCS_SPEC_FILE" env-default:"openapi.yaml"`
}

// RegisterDocsHandler регистрирует Swagger UI handler и отдает OpenAPI спецификацию
// Использует конфигурацию из Server.config
func (s *Server) RegisterDocsHandler() {
	ctx := context.Background()
	if !s.config.IsSwaggerEnabled {
		logger.Info(ctx).Msg("Swagger documentation is disabled")
		return
	}

	cfg := s.config.Docs
	if cfg.Path == "" {
		cfg.Path = "/docs"
	}

	// Регистрируем эндпоинт для OpenAPI спецификации
	if cfg.SpecFile != "" {
		s.router.Get(cfg.URL, func(w http.ResponseWriter, r *http.Request) {
			// Проверяем существование файла
			if _, err := os.Stat(cfg.SpecFile); os.IsNotExist(err) {
				logger.Error(r.Context()).Str("file", cfg.SpecFile).Msg("OpenAPI spec file not found")
				http.Error(w, "OpenAPI specification not found", http.StatusNotFound)
				return
			}

			// Определяем Content-Type по расширению
			ext := filepath.Ext(cfg.SpecFile)
			contentType := "application/json"
			if ext == ".yaml" || ext == ".yml" {
				contentType = "application/x-yaml"
			}

			w.Header().Set("Content-Type", contentType)
			http.ServeFile(w, r, cfg.SpecFile)
		})
	}

	// Swagger UI endpoint
	s.router.Get(cfg.Path+"/*", http_swagger.Handler(
		http_swagger.URL(cfg.URL),
	))

	logger.Info(ctx).
		Str("path", cfg.Path).
		Str("spec_url", cfg.URL).
		Str("spec_file", cfg.SpecFile).
		Msg("Swagger documentation registered")
}
