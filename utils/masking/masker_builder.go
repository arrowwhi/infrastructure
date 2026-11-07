package masking

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/go-multierror"
)

const (
	defaultMaskVal = "*****"
)

var setAll = regexp.MustCompile(`(?s).+`)

// MaskerBuilder — билдер конфигурации для создания экземпляра Masker.
// Отвечает за валидацию Config, компиляцию регулярных выражений в правилах
// и подготовку внутренних структур маскировщика. Экземпляр билдера
// не содержит изменяемого состояния и может свободно переиспользоваться.
type MaskerBuilder struct {
	config Config
}

// NewMaskerBuilder создаёт новый билдер маскировщика на основе переданного Config.
// На этом этапе валидация не выполняется; ошибки (если есть) возвращаются при вызове Build.
func NewMaskerBuilder(config Config) MaskerBuilder {
	return MaskerBuilder{
		config: config,
	}
}

// Build валидирует конфигурацию и создаёт готовый к использованию Masker.
// Выполняется:
//   - Проверка режима (Enabled в связке с флагами IsJSONMasked/IsXMLMasked).
//   - Проверка наличия хотя бы одного правила (KeyRules/ValRules).
//   - Валидация и компиляция всех правил (регулярных выражений).
//
// В случае нескольких ошибок возвращается агрегированная ошибка (go-multierror).
func (b MaskerBuilder) Build() (*Masker, error) {
	if b.config.Enabled && !b.config.IsJSONMasked && !b.config.IsXMLMasked {
		return nil, fmt.Errorf(
			"invalid masker mode: enabled: %t, isJSONMasked: %t, isXMLMasked: %t",
			b.config.Enabled,
			b.config.IsJSONMasked,
			b.config.IsXMLMasked,
		)
	}

	var (
		merr *multierror.Error
		err  error
	)

	if len(b.config.ValRules) == 0 && len(b.config.KeyRules) == 0 {
		merr = multierror.Append(merr, errors.New("rules validation error: no masking rules defined"))
	}

	b.config.KeyRules, err = validateRulesConfigs(b.config.KeyRules)
	if err != nil {
		merr = multierror.Append(merr, fmt.Errorf("key rules validation error: %w", err))
	}

	b.config.ValRules, err = validateRulesConfigs(b.config.ValRules)
	if err != nil {
		merr = multierror.Append(merr, fmt.Errorf("value rules validation error: %w", err))
	}

	if merr.ErrorOrNil() != nil {
		return nil, fmt.Errorf("masker configuration validation error: %w", merr.ErrorOrNil())
	}

	return &Masker{
		keyRules:     fromRuleConfigToRule(b.config.KeyRules),
		valRules:     fromRuleConfigToRule(b.config.ValRules),
		isJSONMasked: b.config.IsJSONMasked,
		isXMLMasked:  b.config.IsXMLMasked,
		isEnabled:    b.config.Enabled,
	}, nil
}

// validateRulesConfigs проходит по всем правилам маскирования и валидирует каждое из них.
// Возвращает слайс валидных правил или ошибку, содержащую все найденные проблемы.
func validateRulesConfigs(rules []RuleConfig) ([]RuleConfig, error) {
	var merr *multierror.Error
	var err error

	for i := range rules {
		rules[i], err = validateRuleConfig(rules[i])
		if err != nil {
			merr = multierror.Append(merr, fmt.Errorf("rule validation error: %w", err))
			continue
		}
	}

	if merr.ErrorOrNil() != nil {
		return nil, merr.ErrorOrNil()
	}

	return rules, nil
}

// validateRuleConfig проверяет корректность отдельного правила маскирования.
func validateRuleConfig(r RuleConfig) (RuleConfig, error) {
	err := validateType(r.Type)
	if err != nil {
		return RuleConfig{}, fmt.Errorf("rule type validation error: %w", err)
	}

	if r.Type == Const && r.MaskVal == "" {
		r.MaskVal = defaultMaskVal
	}

	r.to = setAll
	if r.ToPattern != "" {
		r.to, err = regexp.Compile(r.ToPattern)
		if err != nil {
			return RuleConfig{}, fmt.Errorf("failed to compile replacement regex for pattern %q: %w", r.ToPattern, err)
		}
	}

	if r.Type != Const && len(r.FromPatterns) == 0 {
		r.FromPatterns = append(r.FromPatterns, r.Type)
	}

	r.FromPatterns = filterUniqueFromPatterns(r.FromPatterns)

	r.froms = make([]*regexp.Regexp, len(r.FromPatterns))
	var merr *multierror.Error
	for i := range r.FromPatterns {
		r.froms[i], err = regexp.Compile(r.FromPatterns[i])
		if err != nil {
			merr = multierror.Append(merr, fmt.Errorf("failed to compile matching pattern: %q", r.FromPatterns[i]))
		}
	}

	if merr.ErrorOrNil() != nil {
		return RuleConfig{}, merr.ErrorOrNil()
	}

	return r, nil
}

// validateType проверяет, что тип правила не пуст и соответствует известным типам маскирования.
func validateType(t string) error {
	if t == "" {
		return errors.New("empty type")
	}

	_, ok := maskFns[strings.ToLower(t)]
	if !ok {
		return fmt.Errorf("unknown masking type: %s", t)
	}

	return nil
}

// filterUniqueFromPatterns возвращает слайс уникальных строковых паттернов из исходного списка.
func filterUniqueFromPatterns(patterns []string) []string {
	ufp := make(map[string]struct{}, len(patterns))

	for i := range patterns {
		ufp[patterns[i]] = struct{}{}
	}

	s := make([]string, 0, len(ufp))
	for p := range ufp {
		s = append(s, p)
	}

	return s
}

// fromRuleConfigToRule преобразует RuleConfig в внутреннее представление rule.
// Компилирует функции маскирования по типу (maskFns) либо формирует функцию
// константной подстановки по шаблону r.to, если maskFn отсутствует (тип Const).
func fromRuleConfigToRule(rulesConfig []RuleConfig) []rule {
	rules := make([]rule, len(rulesConfig))
	for i := range rulesConfig {
		rules[i] = rule{
			froms:  rulesConfig[i].froms,
			to:     rulesConfig[i].to,
			maskFn: maskFns[rulesConfig[i].Type],
			mv:     []byte(rulesConfig[i].MaskVal),
		}

		if rules[i].maskFn == nil {
			rules[i].maskFn = rules[i].getDefaultMaskFn()
		}
	}
	return rules
}
