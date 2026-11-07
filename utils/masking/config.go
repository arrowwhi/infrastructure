package masking

import "regexp"

// Config задаёт настройки для маскировщика.
type Config struct {
	// Список правил маскирования для ключей объектов.
	KeyRules []RuleConfig `yaml:"keyRules" mapstructure:"keyRules"`

	// Список правил маскирования для значений объектов.
	ValRules []RuleConfig `yaml:"valRules" mapstructure:"valRules"`

	// Флаг, указывающий, включено ли маскирование.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Флаг, определяющий, следует ли применять маскирование к JSON-данным.
	IsJSONMasked bool `yaml:"isJSONMasked" mapstructure:"isJSONMasked"`

	// Флаг, определяющий, следует ли применять маскирование к XML-данным.
	IsXMLMasked bool `yaml:"isXMLMasked" mapstructure:"isXMLMasked"`
}

// RuleConfig описывает отдельное правило маскирования.
// Правило определяет метод обработки данных с помощью регулярных выражений для поиска и замены.
type RuleConfig struct {
	// Компилированное регулярное выражение для подстановки значения.
	// Если строка с паттерном замены (ToPattern) пустая, используется шаблон по умолчанию (MaskVal подставляется полностью)
	to *regexp.Regexp

	// Тип маскирования, определяющий, какая функция будет применена (например, для номеров телефонов, карт, ИНН и т.д.).
	Type string `yaml:"type" mapstructure:"type"`

	// Строковое представление регулярного выражения для замены найденных значений.
	ToPattern string `yaml:"toPattern" mapstructure:"toPattern"`

	// Значение, которым будет заменяться исходное, если не используется функция маскирования.
	MaskVal string `yaml:"maskVal" mapstructure:"maskVal"`

	// Список строковых регулярных выражений для сопоставления исходных данных.
	FromPatterns []string `yaml:"fromPatterns" mapstructure:"fromPatterns"`

	// Срез компилированных регулярных выражений, полученных из FromPatterns.
	froms []*regexp.Regexp
}
