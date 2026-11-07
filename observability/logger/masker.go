package logger

import (
	"io"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

var maskCfg = masking.Config{
	Enabled:      true,
	IsJSONMasked: true,
	KeyRules: []masking.RuleConfig{
		{Type: masking.Phone, FromPatterns: []string{`(?i)phone`}},
		{Type: masking.Card, FromPatterns: []string{`(?i)card(number)?`}},
		{Type: masking.Fullname, FromPatterns: []string{`(?i)full_?name`}},
		{Type: masking.Const, FromPatterns: []string{`(?i)password|token|secret`}, MaskVal: "*****"},
		{Type: masking.Email, FromPatterns: []string{`(?i)^email$|^e[-_]?mail$`}},
	},
	ValRules: []masking.RuleConfig{
		// PAN в тексте
		{Type: masking.Card, FromPatterns: []string{`(?m)(?:\D|^)(\d{16,})(?:\D|$)`}},
		// E-mail как целая строка
		{Type: masking.Email, FromPatterns: []string{`(?i)^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`}},
	},
}

type Masker interface {
	Mask(p []byte) ([]byte, error)
}

type maskingWriter struct {
	next   io.Writer
	masker Masker
}

func newMaskingWriter(next io.Writer) (io.Writer, error) {
	if next == nil {
		return nil, nil
	}

	masker, err := masking.NewMaskerBuilder(maskCfg).Build()
	if err != nil {
		return nil, err
	}

	return &maskingWriter{next: next, masker: masker}, nil
}

func (w *maskingWriter) Write(p []byte) (int, error) {
	masked, err := w.masker.Mask(p)
	if err != nil {
		return 0, err
	}
	if len(masked) == 0 {
		return 0, nil
	}
	if masked[len(masked)-1] == '\n' {
		return w.next.Write(masked)
	}
	buf := make([]byte, len(masked)+1)
	copy(buf, masked)
	buf[len(buf)-1] = '\n'
	n, err := w.next.Write(buf)

	return n, err
}
