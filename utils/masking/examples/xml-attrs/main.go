package main

import (
	"fmt"
	"log"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

func main() {
	cfg := masking.Config{
		Enabled:     true,
		IsXMLMasked: true,
		KeyRules: []masking.RuleConfig{
			{Type: masking.Const, FromPatterns: []string{`(?i)token|secret`}, MaskVal: "*****"},
			{Type: masking.Phone, FromPatterns: []string{`(?i)phone`}},
		},
	}

	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	in := []byte(`
<user token="abcd-1234" secret="shh">
  <phone value="+7 (999) 123-45-67">+7 (999) 123-45-67</phone>
  <profile email="u@example.com">ok</profile>
</user>`)

	out, err := m.Mask(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}
	fmt.Println(string(out))

	// Result:
	// <user token="*****" secret="*****">
	//   <phone value="+7 (999) 123-45-67">+* (***) ***-45-67</phone>
	//   <profile email="u@example.com">ok</profile>
	// </user>
}
