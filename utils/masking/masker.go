package masking

import (
	"errors"
	"fmt"
)

// Masker — основной рантайм-маскировщик для JSON и XML.
// Экземпляр создаётся через MaskerBuilder на основе Config и затем используется
// только для чтения; после сборки не содержит изменяемого состояния и пригоден
// для конкурентного использования из нескольких горутин.
type Masker struct {
	keyRules     []rule
	valRules     []rule
	isJSONMasked bool
	isXMLMasked  bool
	isEnabled    bool
}

// Mask выполняет маскирование входных данных b.
// Поведение:
//   - Если маскирование отключено (isEnabled=false), возвращает исходные данные без изменений.
//   - Если включён режим JSON и b — валидный JSON, применяется JSON-маскирование.
//   - Если включён режим XML и b — корректный XML, применяется XML-маскирование.
//   - В противном случае возвращает ошибку "unsupported data format for masking".
//
// Ошибки из конкретных режимов оборачиваются в "JSON masking error"/"XML masking error".
func (m *Masker) Mask(b []byte) ([]byte, error) {
	if !m.isEnabled {
		return b, nil
	}

	if m.isJSONMasked && isJSON(b) {
		out, err := m.maskJSON(b)
		if err != nil {
			return nil, fmt.Errorf("JSON masking error: %w", err)
		}
		return out, nil
	}

	if m.isXMLMasked && isXML(b) {
		out, err := m.maskXML(b)
		if err != nil {
			return nil, fmt.Errorf("XML masking error: %w", err)
		}
		return out, nil
	}

	return nil, errors.New("unsupported data format for masking")
}

func findMatchingRule(s string, rules []rule) *rule {
	for i := range rules {
		for j := range rules[i].froms {
			if rules[i].froms[j].MatchString(s) {
				return &rules[i]
			}
		}
	}
	return nil
}
