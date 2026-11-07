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
			{Type: masking.Phone, FromPatterns: []string{`(?i)phones|mobiles`}},
			{Type: masking.Fullname, FromPatterns: []string{`(?i)full_?name`}},
			{Type: masking.Misc, FromPatterns: []string{`(?i)^client$`}},
		},
	}

	m, err := masking.NewMapMasker(cfg)
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	in := map[string]any{
		"full_name": "Иванов Иван Иванович",
		"phones":    []any{"+7 (999) 123-45-67", "89991234567"},
		"members": []any{
			map[string]any{
				"full_name": "Петров Петр Петрович",
				"mobiles":   []any{"+7 800 555 35 35"},
				"age":       30,
			},
			map[string]any{
				"full_name": "Сидоров Сидор",
				"mobiles":   []any{},
			},
		},
		"client": map[string]any{
			"id":        1,
			"full_name": "Вася Пупкин",
		},
	}

	got, err := m.MaskMap(in)
	if err != nil {
		log.Fatalf("mask: %v", err)
	}

	want := map[string]any{
		"full_name": "****** **** ********",
		"phones":    []any{"+* (***) ***-45-67", "*******4567"},
		"members": []any{
			map[string]any{
				"full_name": "****** **** ********",
				"mobiles":   []any{"+* *** *** 35 35"},
				"age":       30,
			},
			map[string]any{
				"full_name": "******* *****",
				"mobiles":   []any{},
			},
		},
		"client": map[string]any{
			"id":        1,
			"full_name": "**** ******",
		},
	}

	if !reflect.DeepEqual(got, want) {
		log.Fatalf("mismatch\nGot:\n%s\n\nWant:\n%s", pretty(got), pretty(want))
	}
	fmt.Println(pretty(got))

	// Result:
	// {
	//   "client": {
	//     "full_name": "**** ******",
	//     "id": 1
	//   },
	//   "full_name": "****** **** ********",
	//   "members": [
	//     {
	//       "age": 30,
	//       "full_name": "****** **** ********",
	//       "mobiles": [
	//         "+* *** *** 35 35"
	//       ]
	//     },
	//     {
	//       "full_name": "******* *****",
	//       "mobiles": []
	//     }
	//   ],
	//   "phones": [
	//     "+* (***) ***-45-67",
	//     "*******4567"
	//   ]
	// }
}
