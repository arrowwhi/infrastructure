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
			// Type=const + ToPattern=`\d` → заменяем только цифры символом '*'
			{
				Type:         masking.Const,
				FromPatterns: []string{`(?i)account`},
				ToPattern:    `\d`,
				MaskVal:      "*",
			},
		},
	}

	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		log.Fatalf("build masker: %v", err)
	}

	in := []byte(`{
  "account": "ACC-12-3456-78",
  "owner": "Иванов Иван",
  "note": "номер: 998877"
}`)

	out, err := m.Mask(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}

	fmt.Println(string(out))

	// Result:
	// {
	//   "account": "ACC-**-****-**",
	//   "owner": "Иванов Иван",
	//   "note": "номер: 998877"
	// }
}
