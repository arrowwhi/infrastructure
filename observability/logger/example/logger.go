package main

import (
	"context"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

func main() {
	ctx := context.Background()
	cfg := logger.Config{
		Level:        "debug",
		Stacktrace:   true,
		PrettyOutput: false,
		Masking:      false,
	}
	logger.Init(&cfg, "example-app", "1.0.0", "development")

	logger.Fatal(ctx).Msg("Fatal")
}
