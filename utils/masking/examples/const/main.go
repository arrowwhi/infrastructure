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
		KeyRules: []masking.RuleConfig{
			// Type=const → подстановка MaskVal целиком (если ToPattern пустой)
			{Type: masking.Const, FromPatterns: []string{`(?i)password|token|secret`}, MaskVal: "*****"},
		},
	}

	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		log.Fatalf("build masker: %v", err)
	}

	in := []byte(`{
  "username": "john",
  "password": "P@ssw0rd!",
  "apiToken": "abcd-efgh-1234",
  "secret": "top-secret"
}`)

	out, err := m.Mask(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}

	fmt.Println(string(out))

	// Result:
	// {
	//   "username": "john",
	//   "password": "*****",
	//   "apiToken": "*****",
	//   "secret": "*****"
	// }
}
