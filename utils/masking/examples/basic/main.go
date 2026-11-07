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
		IsXMLMasked:  true,
		KeyRules: []masking.RuleConfig{
			// Маскируем значения по ключам
			{Type: masking.Phone, FromPatterns: []string{`(?i)phone|mobile`}},
			{Type: masking.Card, FromPatterns: []string{`(?i)card(number)?`}},
			{Type: masking.INN, FromPatterns: []string{`(?i)inn`}},
			{Type: masking.Passport, FromPatterns: []string{`(?i)passport`}},
			{Type: masking.Fullname, FromPatterns: []string{`(?i)full_?name`}},
		},
		ValRules: nil,
	}

	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		log.Fatalf("build masker: %v", err)
	}

	jsonInput := []byte(`{
  "full_name": "Иванов Иван Иванович",
  "phone": "+7 (999) 123-45-67",
  "inn": "7712345678",
  "cardNumber": "4111111111111111",
  "passport": "1234 567890",
  "nested": {
    "mobile": "89991234567"
  }
}`)

	maskedJSON, err := m.Mask(jsonInput)
	if err != nil {
		log.Fatalf("mask json: %v", err)
	}
	fmt.Println("Masked JSON:")
	fmt.Println(string(maskedJSON))

	xmlInput := []byte(`
<client>
  <full_name>Иванов Иван Иванович</full_name>
  <phone>+7 (999) 123-45-67</phone>
  <inn>7712345678</inn>
  <cardNumber>4111111111111111</cardNumber>
  <passport>1234567890</passport>
  <nested mobile="89991234567">data</nested>
</client>`)

	maskedXML, err := m.Mask(xmlInput)
	if err != nil {
		log.Fatalf("mask xml: %v", err)
	}
	fmt.Println("\nMasked XML:")
	fmt.Println(string(maskedXML))

	// Result:
	// Masked JSON:
	// {
	//   "full_name": "****** **** ********",
	//   "phone": "+* (***) ***-45-67",
	//   "inn": "77****5678",
	//   "cardNumber": "41**********1111",
	//   "passport": "1234 ****90",
	//   "nested": {
	//     "mobile": "*******4567"
	//   }
	// }

	// Masked XML:

	// <client>
	//
	//	<full_name>****** **** ********</full_name>
	//	<phone>+* (***) ***-45-67</phone>
	//	<inn>77****5678</inn>
	//	<cardNumber>41**********1111</cardNumber>
	//	<passport>1234****90</passport>
	//	<nested mobile="*******4567">data</nested>
	//
	// </client>
}
