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
			// Маскируем только цифры в значении, остальное оставляем
			{Type: masking.Const, ToPattern: `\d`, MaskVal: "*", FromPatterns: []string{`.+`}},
		},
	}

	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	in := []byte(`{
  "message": "order #12345 for client 900200300",
  "comment": "ref=ABC-987"
}`)
	out, err := m.Mask(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}
	fmt.Println(string(out))

	// Result:
	// {
	//   "message": "order #***** for client *********",
	//   "comment": "ref=ABC-***"
	// }
}
