package main

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

func pretty(m map[string]any) string {
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

func main() {
	cfg := masking.Config{
		Enabled: true,
		KeyRules: []masking.RuleConfig{
			{Type: masking.Phone, FromPatterns: []string{`(?i)^phones$`}},
			{Type: masking.Misc, FromPatterns: []string{`(?i)^client$`}},
		},
	}

	m, err := masking.NewMapMasker(cfg)
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	in := map[string]any{
		"client": map[string]any{
			"id":     1,
			"phones": []any{"1234567"},
			"name":   "John",
		},
	}

	got, err := m.MaskMap(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}

	want := map[string]any{
		"client": map[string]any{
			"id":     1,
			"phones": []any{"***4567"},
			"name":   "John",
		},
	}

	if !reflect.DeepEqual(got, want) {
		log.Fatalf("mismatch\nGot:\n%s\n\nWant:\n%s", pretty(got), pretty(want))
	}
	fmt.Println(pretty(got))
}
