package main

import (
	"fmt"
	"log"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

func main() {
	cfg := masking.Config{
		Enabled:      true,
		IsJSONMasked: true,
		ValRules: []masking.RuleConfig{
			// Любые 16+ цифр маскируем алгоритмом карты
			{Type: masking.Card, FromPatterns: []string{`(?m)(?:\D|^)(\d{16,})(?:\D|$)`}},
		},
	}

	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	in := []byte(`{
  "note": "мой номер 4111111111111111, держи в секрете",
  "nested": {"text": "PAN: 5500000000000004"}
}`)

	out, err := m.Mask(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}
	fmt.Println(string(out))

	// Result:
	// {
	//   "note": "мой номер 41**********1111, держи в секрете",
	//   "nested": {"text": "PAN: 55**********0004"}
	// }
}
