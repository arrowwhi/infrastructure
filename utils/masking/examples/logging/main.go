package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

// MaskedLogger - обертка для логирования с маскированием данных
type MaskedLogger struct {
	masker *masking.Masker
}

// NewMaskedLogger создает новый логгер с маскированием
func NewMaskedLogger(cfg masking.Config) (*MaskedLogger, error) {
	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create masker: %w", err)
	}
	return &MaskedLogger{masker: m}, nil
}

// Info логирует данные уровня INFO с маскированием
func (ml *MaskedLogger) Info(message string, data interface{}) {
	maskedData := ml.maskData(data)
	log.Printf("[INFO] %s: %s", message, maskedData)
}

// Infof логирует форматированное сообщение с маскированием аргументов
func (ml *MaskedLogger) Infof(format string, args ...interface{}) {
	maskedArgs := make([]interface{}, len(args))
	for i, arg := range args {
		maskedArgs[i] = ml.maskData(arg)
	}
	log.Printf("[INFO] "+format, maskedArgs...)
}

// InfoJSON логирует JSON данные с маскированием
func (ml *MaskedLogger) InfoJSON(message string, jsonData []byte) {
	masked, err := ml.masker.Mask(jsonData)
	if err != nil {
		log.Printf("[INFO] %s: <error masking data: %v>", message, err)
		return
	}
	log.Printf("[INFO] %s: %s", message, string(masked))
}

// maskData маскирует данные (конвертирует в JSON и маскирует)
func (ml *MaskedLogger) maskData(data interface{}) string {
	// Если данные уже строка, возвращаем как есть
	if str, ok := data.(string); ok {
		return str
	}

	// Конвертируем в JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprintf("<error marshaling: %v>", err)
	}

	// Маскируем
	masked, err := ml.masker.Mask(jsonData)
	if err != nil {
		return fmt.Sprintf("<error masking: %v>", err)
	}

	return string(masked)
}

func main() {
	// Настройка маскирования
	cfg := masking.Config{
		Enabled:      true,
		IsJSONMasked: true,
		KeyRules: []masking.RuleConfig{
			{Type: masking.Phone, FromPatterns: []string{`(?i)phone|mobile`}},
			{Type: masking.Email, FromPatterns: []string{`(?i)email`}},
			{Type: masking.Fullname, FromPatterns: []string{`(?i)name|full_?name`}},
			{Type: masking.Misc, FromPatterns: []string{`(?i)password|pwd`}},
			{Type: masking.Card, FromPatterns: []string{`(?i)card|card_?number`}},
		},
	}

	// Создаем логгер с маскированием
	logger, err := NewMaskedLogger(cfg)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	// Пример 1: Логирование структуры данных
	user := map[string]interface{}{
		"name":     "Иванов Иван Иванович",
		"email":    "ivan@example.com",
		"phone":    "+7 (999) 123-45-67",
		"password": "SuperSecret123",
		"card":     "4111 1111 1111 1111",
		"age":      30,
	}
	logger.Info("User data", user)

	// Пример 2: Логирование с форматированием
	logger.Infof("Processing user: %v", user)

	// Пример 3: Логирование готового JSON
	jsonData := []byte(`{
		"name": "Петров Петр",
		"email": "petr@example.com",
		"phone": "+7 800 555 35 35",
		"order": {
			"id": 12345,
			"card_number": "5500 0000 0000 0004"
		}
	}`)
	logger.InfoJSON("Order received", jsonData)

	// Пример 4: Логирование массива данных
	users := []map[string]interface{}{
		{
			"fullname": "Сидоров Сидор",
			"mobile":   "89991234567",
			"email":    "sidor@example.com",
		},
		{
			"fullname": "Козлов Козел",
			"mobile":   "+7 (111) 222-33-44",
			"email":    "kozel@example.com",
		},
	}
	logger.Info("Multiple users", users)

	// Пример 5: Обычные строки логируются без изменений
	logger.Info("Simple message", "Operation completed successfully")
}
