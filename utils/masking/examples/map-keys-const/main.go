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
			{Type: masking.Const, FromPatterns: []string{`(?i)password|token|secret`}, ToPattern: `(?s).*`, MaskVal: "*****"},
		},
	}

	m, err := masking.NewMapMasker(cfg)
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	in := map[string]any{
		"user":     "john",
		"password": "P@ssw0rd!",
		"token":    "abcd-efgh-1234",
		"secret":   nil,
	}

	got, err := m.MaskMap(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}

	want := map[string]any{
		"user":     "john",
		"password": "*****",
		"token":    "*****",
		"secret":   "*****",
	}

	if !reflect.DeepEqual(got, want) {
		log.Fatalf("mismatch\nGot:\n%s\n\nWant:\n%s", pretty(got), pretty(want))
	}
	fmt.Println(pretty(got))

	// Result:
	//	{
	//	  "password": "*****",
	//	  "secret": "*****",
	//	  "token": "*****",
	//	  "user": "john"
	//	}
}
