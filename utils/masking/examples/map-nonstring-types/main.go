package main

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

type payload struct{ X int }

func pretty(m map[string]any) string {
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

func main() {
	cfg := masking.Config{
		Enabled: true,
		KeyRules: []masking.RuleConfig{
			{Type: masking.Const, FromPatterns: []string{`(?i)^amount$`}, ToPattern: `\d`, MaskVal: "*"},
			{Type: masking.Misc, FromPatterns: []string{`(?i)^active$|^payload$`}},
		},
	}

	m, err := masking.NewMapMasker(cfg)
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	in := map[string]any{
		"amount":  12345.67,
		"active":  true,
		"payload": payload{X: 42},
		"note":    "ok",
	}

	got, err := m.MaskMap(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}

	want := map[string]any{
		"amount":  "*****.******",
		"active":  "****",
		"payload": "********",
		"note":    "ok",
	}

	if !reflect.DeepEqual(got, want) {
		log.Fatalf("mismatch\nGot:\n%s\n\nWant:\n%s", pretty(got), pretty(want))
	}
	fmt.Println(pretty(got))

	// Result:
	//	{
	//	  "active": "****",
	//	  "amount": "*****.******",
	//	  "note": "ok",
	//	  "payload": "********"
	//	}
}
