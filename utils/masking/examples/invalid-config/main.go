package main

import (
	"fmt"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

func main() {
	cfg := masking.Config{
		Enabled:      true,
		IsJSONMasked: false,
		IsXMLMasked:  false,
		KeyRules:     nil,
		ValRules:     nil,
	}
	_, err := masking.NewMaskerBuilder(cfg).Build()
	fmt.Println("expected error:")
	fmt.Println(err) // выводит агрегированную ошибку валидации
}
