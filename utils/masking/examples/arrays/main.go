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
			{Type: masking.Phone, FromPatterns: []string{`(?i)phones|mobiles`}},
			{Type: masking.Fullname, FromPatterns: []string{`(?i)full_?name`}},
		},
	}

	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	in := []byte(`{
  "full_name": "Иванов Иван Иванович",
  "phones": ["+7 (999) 123-45-67", "89991234567"],
  "members": [
    {"full_name": "Петров Петр Петрович", "mobiles": ["+7 800 555 35 35"]},
    {"full_name": "Сидоров Сидор", "mobiles": []}
  ]
}`)
	out, err := m.Mask(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}
	fmt.Println(string(out))

	// Result:
	// {
	//   "full_name": "****** **** ********",
	//   "phones": ["+* (***) ***-45-67", "*******4567"],
	//   "members": [
	//     {"full_name": "****** **** ********", "mobiles": ["+* *** *** 35 35"]},
	//     {"full_name": "******* *****", "mobiles": []}
	//   ]
	// }
}
