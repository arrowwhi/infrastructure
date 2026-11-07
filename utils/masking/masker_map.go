package masking

import (
	"fmt"
	"strconv"

	"github.com/hashicorp/go-multierror"
)

// MapMasker маскирует данные в структурах вида map[string]any с рекурсивной
// обработкой вложенных карт и срезов ([]any).
//
// Поведение:
//   - KeyRules: при совпадении ключа применяется маскирование к его значению;
//     для массивов маска наследуется на элементы; для вложенных map маска
//     по верхнему ключу НЕ наследуется внутрь — внутри вычисляется заново.
//   - ValRules: применяются к текстовым представлениям значений (string, []byte).
//   - Типы: если было применено маскирование к нестроковому значению, результат — string.
//   - Конкурентный доступ: безопасен, так как MapMasker — иммутабельный после сборки.
type MapMasker struct {
	keyRules  []rule
	valRules  []rule
	isEnabled bool
}

// NewMapMasker создает и валидирует маскер для in-memory структур на основе Config.
//
// Валидация:
//   - Должно быть определено хотя бы одно правило (KeyRules или ValRules);
//   - Все правила компилируются и нормализуются (см. validateRulesConfigs);
//   - При наличии нескольких ошибок валидации они агрегируются через multierror.
//
// Особенности:
//   - Флаги IsJSONMasked/IsXMLMasked игнорируются: MapMasker — отдельный режим,
//     не требующий сериализации/десериализации.
//
// Возвращает:
//   - *MapMasker при успешной валидации;
//   - ошибку вида "map masker configuration validation error: ..." при наличии проблем.
func NewMapMasker(cfg Config) (*MapMasker, error) {
	var (
		merr *multierror.Error
		err  error
	)

	if len(cfg.KeyRules) == 0 && len(cfg.ValRules) == 0 {
		merr = multierror.Append(merr, fmt.Errorf("rules validation error: no masking rules defined"))
	}

	cfg.KeyRules, err = validateRulesConfigs(cfg.KeyRules)
	if err != nil {
		merr = multierror.Append(merr, fmt.Errorf("key rules validation error: %w", err))
	}
	cfg.ValRules, err = validateRulesConfigs(cfg.ValRules)
	if err != nil {
		merr = multierror.Append(merr, fmt.Errorf("value rules validation error: %w", err))
	}
	if merr.ErrorOrNil() != nil {
		return nil, fmt.Errorf("map masker configuration validation error: %w", merr.ErrorOrNil())
	}

	return &MapMasker{
		keyRules:  fromRuleConfigToRule(cfg.KeyRules),
		valRules:  fromRuleConfigToRule(cfg.ValRules),
		isEnabled: cfg.Enabled,
	}, nil
}

// MaskMap возвращает новую карту с замаскированными значениями согласно набору правил.
//
// Вход:
//   - in: исходная карта (может содержать вложенные map[string]any и []any).
//
// Правила применения:
//   - Для каждого ключа определяется подходящее key-rule; если оно найдено,
//     маска применяется к значению по этому ключу;
//   - Для []any keyMask наследуется на элементы;
//   - Для вложенных map keyMask не наследуется — внутри заново определяется по ключам;
//   - ValRules применяются к строкам и []byte;
//   - При маскировании нестроковых значений результат становится string.
//
// Возвращает:
//   - Новую карту с примененными масками;
//   - nil при входе nil, если маскирование включено;
//   - Исходную карту без изменений, если маскирование выключено (isEnabled=false).
func (m *MapMasker) MaskMap(in map[string]any) (map[string]any, error) {
	if !m.isEnabled {
		return in, nil
	}
	if in == nil {
		return nil, nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		var km maskFn
		if r := findMatchingRule(k, m.keyRules); r != nil {
			km = r.maskFn
		}
		out[k] = m.maskAny(v, km)
	}
	return out, nil
}

// maskAny применяет keyMask/valRules к значению произвольного типа.
// Внутренняя функция: не экспортируется; описывает точную семантику приведения типов.
//
// Правила:
//   - nil: при наличии keyMask → возвращается замаскированная строка; иначе nil;
//   - string/[]byte: valRules → строка; при keyMask → строка;
//   - []any: keyMask наследуется на элементы (как в JSON-маскере массивов);
//   - map[string]any: keyMask не наследуется внутрь, правила вычисляются заново;
//   - bool/числа: без keyMask остаются как есть; с keyMask → строковое представление.
//
//nolint:gocyclo
func (m *MapMasker) maskAny(v any, keyMask maskFn) any {
	switch x := v.(type) {
	case nil:
		if keyMask != nil {
			return string(keyMask(nil))
		}
		return nil

	case string:
		if keyMask != nil {
			return string(keyMask(s2bRO(x)))
		}
		if r := findMatchingRule(x, m.valRules); r != nil {
			return string(r.maskFn(s2bRO(x)))
		}
		return x

	case []byte:
		s := b2s(x)
		if keyMask != nil {
			return string(keyMask(x))
		}
		if r := findMatchingRule(s, m.valRules); r != nil {
			return string(r.maskFn(x))
		}
		return s

	case []any:
		return m.maskSlice(x, keyMask)

	case map[string]any:
		return m.maskMapNoKeyMask(x)

	case bool:
		if keyMask == nil {
			return x
		}
		if x {
			return string(keyMask(s2bRO(trueString)))
		}
		return string(keyMask(s2bRO(falseString)))

	case float32:
		if keyMask == nil {
			return x
		}
		return string(keyMask(s2bRO(strconv.FormatFloat(float64(x), 'f', maxFloatPrecision, 32))))
	case float64:
		if keyMask == nil {
			return x
		}
		return string(keyMask(s2bRO(strconv.FormatFloat(x, 'f', maxFloatPrecision, 64))))

	case int, int8, int16, int32, int64:
		if keyMask == nil {
			return x
		}
		return string(keyMask(s2bRO(fmtInt(x))))

	case uint, uint8, uint16, uint32, uint64:
		if keyMask == nil {
			return x
		}
		return string(keyMask(s2bRO(fmtUint(x))))

	default:
		if keyMask == nil {
			return x
		}
		s := fmtAny(x)
		return string(keyMask(s2bRO(s)))
	}
}

// maskSlice маскирует элементы среза []any.
// Наследует keyMask на каждый элемент (как массивы в JSON-маскере).
func (m *MapMasker) maskSlice(a []any, keyMask maskFn) []any {
	if a == nil {
		return nil
	}
	out := make([]any, len(a))
	for i := range a {
		out[i] = m.maskAny(a[i], keyMask)
	}
	return out
}

// maskMapNoKeyMask маскирует вложенную map[string]any без наследования полученного
// извне keyMask. Для каждого вложенного ключа вычисляется собственный keyMask.
func (m *MapMasker) maskMapNoKeyMask(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		var km maskFn
		if r := findMatchingRule(k, m.keyRules); r != nil {
			km = r.maskFn
		}
		out[k] = m.maskAny(v, km)
	}
	return out
}

// fmtInt форматирует знаковое целое значение в десятичную строку без рефлексии.
// Используется при применении keyMask к нестроковым типам.
func fmtInt(v any) string {
	switch n := v.(type) {
	case int:
		return strconv.FormatInt(int64(n), 10)
	case int8:
		return strconv.FormatInt(int64(n), 10)
	case int16:
		return strconv.FormatInt(int64(n), 10)
	case int32:
		return strconv.FormatInt(int64(n), 10)
	case int64:
		return strconv.FormatInt(n, 10)
	default:
		return "0"
	}
}

// fmtUint форматирует беззнаковое целое значение в десятичную строку без рефлексии.
// Используется при применении keyMask к нестроковым типам.
func fmtUint(v any) string {
	switch n := v.(type) {
	case uint:
		return strconv.FormatUint(uint64(n), 10)
	case uint8:
		return strconv.FormatUint(uint64(n), 10)
	case uint16:
		return strconv.FormatUint(uint64(n), 10)
	case uint32:
		return strconv.FormatUint(uint64(n), 10)
	case uint64:
		return strconv.FormatUint(n, 10)
	default:
		return "0"
	}
}

// fmtAny формирует компактное строковое представление значения для fallback-маскирования
// (когда применен keyMask к неизвестному типу). Избегает fmt.Sprintf для снижения аллокаций.
func fmtAny(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return b2s(t)
	case bool:
		if t {
			return trueString
		}
		return falseString
	case nil:
		return nullString
	case float32:
		return strconv.FormatFloat(float64(t), 'f', maxFloatPrecision, 32)
	case float64:
		return strconv.FormatFloat(t, 'f', maxFloatPrecision, 64)
	case int, int8, int16, int32, int64:
		return fmtInt(t)
	case uint, uint8, uint16, uint32, uint64:
		return fmtUint(t)
	default:
		return "<masked>"
	}
}
