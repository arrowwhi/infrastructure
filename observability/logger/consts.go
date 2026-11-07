package logger

import (
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"
)

const (
	// doubleDigitThreshold is the minimum value for a two-digit ANSI color code
	doubleDigitThreshold = 10
	levelTypeLen         = 5
)

var consoleWriter = zerolog.ConsoleWriter{
	Out:        os.Stdout,
	TimeFormat: "02.01.2006 15:04:05",
	FormatLevel: func(i any) string {

		var s string
		switch v := i.(type) {
		case string:
			s = v
		case []byte:
			s = string(v)
		default:
			// редкий случай — приводим к строке
			s = strings.ToLower(fmt.Sprint(v))
		}

		var levelName string
		var color int
		switch s {
		case "trace":
			levelName, color = "TRACE", 36
		case "debug":
			levelName, color = "DEBUG", 32
		case "info":
			levelName, color = "INFO", 34
		case "warn":
			levelName, color = "WARN", 33
		case "error":
			levelName, color = "ERROR", 31
		case "fatal":
			levelName, color = "FATAL", 35
		case "panic":
			levelName, color = "PANIC", 35
		default:
			levelName, color = strings.ToUpper(s), 0
		}

		if l := len(levelName); l < levelTypeLen {
			levelName += strings.Repeat(" ", 5-l)
		}

		if color == 0 {
			return levelName
		}

		b := make([]byte, 0, 12+len(levelName))
		b = append(b, '\x1b', '[')
		if color >= doubleDigitThreshold {
			b = append(b, byte('0'+(color/10)%10))
		}
		b = append(b, byte('0'+color%10))
		b = append(b, 'm')
		b = append(b, levelName...)
		b = append(b, '\x1b', '[', '0', 'm')

		return string(b)
	},
	FormatMessage: func(i any) string {
		var s string
		switch v := i.(type) {
		case string:
			s = v
		case []byte:
			s = string(v)
		default:
			s = fmt.Sprint(v)
		}

		var b strings.Builder
		b.Grow(len(s) + 4)
		_, _ = b.WriteString("| ")
		_, _ = b.WriteString(s)
		_, _ = b.WriteString(" |")

		return b.String()
	},
}
