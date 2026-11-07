package logger

import (
	"github.com/rs/zerolog"
)

type Config struct {
	Level        string `yaml:"level" env:"LEVEL" env-default:"info"`
	Stacktrace   bool   `json:"stacktrace" env:"STACKTRACE" env-default:"false"`
	PrettyOutput bool   `json:"prettyoutput" env:"PRETTYOUTPUT" env-default:"false"`
	Masking      bool   `json:"masking" env:"MASKING" env-default:"true"`
}

func (c *Config) GetZerologLevel() zerolog.Level {
	level, err := zerolog.ParseLevel(c.Level)
	if err != nil {
		return zerolog.InfoLevel
	}

	return level
}
