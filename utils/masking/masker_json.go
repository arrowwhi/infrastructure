package masking

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

const (
	bufferGrowFactor = 16

	trueString  = "true"
	falseString = "false"

	nullString = "null"

	maxFloatPrecision = 6

	openCurlyBracket   = '{'
	closeCurlyBracket  = '}'
	openSquareBracket  = '['
	closeSquareBracket = ']'
)

func (m *Masker) maskJSON(data []byte) ([]byte, error) {
	var out bytes.Buffer
	out.Grow(len(data) + len(data)/bufferGrowFactor)

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	if err := m.encodeValue(dec, &out, nil); err != nil {
		return nil, fmt.Errorf("json mask: encode root value: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("json mask: unexpected trailing tokens")
		}
		return nil, fmt.Errorf("json mask: read trailing token: %w", err)
	}
	return out.Bytes(), nil
}

//nolint:gocognit,gocyclo,funlen,nestif
func (m *Masker) encodeValue(dec *json.Decoder, out *bytes.Buffer, keyMask maskFn) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("decode token: %w", err)
	}

	if keyMask != nil {
		switch v := tok.(type) {
		case string:
			if err := writeJSONString(out, b2s(keyMask(s2bRO(v)))); err != nil {
				return fmt.Errorf("write masked string (keyMask): %w", err)
			}
			return nil
		case json.Number:
			if err := writeJSONString(out, b2s(keyMask(s2bRO(v.String())))); err != nil {
				return fmt.Errorf("write masked number (keyMask): %w", err)
			}
			return nil
		case bool:
			if v {
				if err := writeJSONString(out, b2s(keyMask(s2bRO(trueString)))); err != nil {
					return fmt.Errorf("write masked bool=true (keyMask): %w", err)
				}
				return nil
			}
			if err := writeJSONString(out, b2s(keyMask(s2bRO(falseString)))); err != nil {
				return fmt.Errorf("write masked bool=false (keyMask): %w", err)
			}
			return nil
		case nil:
			if err := writeJSONString(out, b2s(keyMask(nil))); err != nil {
				return fmt.Errorf("write masked null (keyMask): %w", err)
			}
			return nil
		case json.Delim:
			switch v {
			case openCurlyBracket:
				if err := m.encodeObject(dec, out); err != nil {
					return fmt.Errorf("encode object (keyMask): %w", err)
				}
				return nil
			case openSquareBracket:
				if err := m.encodeArray(dec, out, keyMask); err != nil {
					return fmt.Errorf("encode array (keyMask): %w", err)
				}
				return nil
			default:
				return fmt.Errorf("unexpected delim (keyMask): %q", v)
			}
		case float64:
			if err := writeJSONString(out, b2s(keyMask(s2bRO(json.Number(strconv.FormatFloat(v, 'f', maxFloatPrecision, 64)).String())))); err != nil {
				return fmt.Errorf("write masked float (keyMask): %w", err)
			}
			return nil
		default:
			return fmt.Errorf("unexpected token with keyMask: %T", v)
		}
	}

	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case openCurlyBracket:
			if err := m.encodeObject(dec, out); err != nil {
				return fmt.Errorf("encode object: %w", err)
			}
			return nil
		case openSquareBracket:
			if err := m.encodeArray(dec, out, nil); err != nil {
				return fmt.Errorf("encode array: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("unexpected delim: %q", v)
		}

	case string:
		if r := m.findMatchingRule(v, m.valRules); r != nil {
			if err := writeJSONString(out, b2s(r.maskFn(s2bRO(v)))); err != nil {
				return fmt.Errorf("write masked string (val-rule): %w", err)
			}
			return nil
		}
		if err := writeJSONString(out, v); err != nil {
			return fmt.Errorf("write raw string: %w", err)
		}
		return nil

	case json.Number:
		out.WriteString(v.String())
		return nil

	case bool:
		if v {
			out.WriteString(trueString)
		} else {
			out.WriteString(falseString)
		}
		return nil

	case nil:
		out.WriteString(nullString)
		return nil

	case float64:
		out.WriteString(strconv.FormatFloat(v, 'f', maxFloatPrecision, 64))
		return nil

	default:
		return fmt.Errorf("unexpected token type: %T", v)
	}
}

func (m *Masker) encodeObject(dec *json.Decoder, out *bytes.Buffer) error {
	out.WriteByte(openCurlyBracket)
	first := true
	for dec.More() {
		if !first {
			out.WriteByte(',')
		}
		first = false

		ktok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("object: read key token: %w", err)
		}
		key, ok := ktok.(string)
		if !ok {
			return fmt.Errorf("object: expected key string, got %T", ktok)
		}
		if err := writeJSONString(out, key); err != nil {
			return fmt.Errorf("object: write key %q: %w", key, err)
		}
		out.WriteByte(':')

		var km maskFn
		if r := m.findMatchingRule(key, m.keyRules); r != nil {
			km = r.maskFn
		}

		if err := m.encodeValue(dec, out, km); err != nil {
			return fmt.Errorf("object: encode value for key %q: %w", key, err)
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("object: read closing '}' token: %w", err)
	}
	out.WriteByte(closeCurlyBracket)
	return nil
}

//nolint:gocognit,gocyclo,funlen
func (m *Masker) encodeArray(dec *json.Decoder, out *bytes.Buffer, keyMask maskFn) error {
	out.WriteByte(openSquareBracket)
	i := 0
	for dec.More() {
		if i > 0 {
			out.WriteByte(',')
		}
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("array[%d]: decode token: %w", i, err)
		}

		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case openCurlyBracket:
				if err := m.encodeObject(dec, out); err != nil {
					return fmt.Errorf("array[%d]: encode object: %w", i, err)
				}
			case openSquareBracket:
				if err := m.encodeArray(dec, out, nil); err != nil {
					return fmt.Errorf("array[%d]: encode nested array: %w", i, err)
				}
			default:
				return fmt.Errorf("array[%d]: unexpected delim: %q", i, v)
			}

		case string:
			switch {
			case keyMask != nil:
				if err := writeJSONString(out, b2s(keyMask(s2bRO(v)))); err != nil {
					return fmt.Errorf("array[%d]: write masked string by keyMask: %w", i, err)
				}
			case m.findMatchingRule(v, m.valRules) != nil:
				r := m.findMatchingRule(v, m.valRules)
				if err := writeJSONString(out, b2s(r.maskFn(s2bRO(v)))); err != nil {
					return fmt.Errorf("array[%d]: write masked string by val-rule: %w", i, err)
				}
			default:
				if err := writeJSONString(out, v); err != nil {
					return fmt.Errorf("array[%d]: write raw string: %w", i, err)
				}
			}

		case json.Number:
			switch {
			case keyMask != nil:
				if err := writeJSONString(out, b2s(keyMask(s2bRO(v.String())))); err != nil {
					return fmt.Errorf("array[%d]: write masked number: %w", i, err)
				}
			default:
				out.WriteString(v.String())
			}

		case bool:
			switch {
			case keyMask != nil && v:
				if err := writeJSONString(out, b2s(keyMask(s2bRO(trueString)))); err != nil {
					return fmt.Errorf("array[%d]: write masked bool(true): %w", i, err)
				}
			case keyMask != nil && !v:
				if err := writeJSONString(out, b2s(keyMask(s2bRO(falseString)))); err != nil {
					return fmt.Errorf("array[%d]: write masked bool(false): %w", i, err)
				}
			case v:
				out.WriteString(trueString)
			default:
				out.WriteString(falseString)
			}

		case nil:
			switch {
			case keyMask != nil:
				if err := writeJSONString(out, b2s(keyMask(nil))); err != nil {
					return fmt.Errorf("array[%d]: write masked null: %w", i, err)
				}
			default:
				out.WriteString(nullString)
			}

		case float64:
			switch {
			case keyMask != nil:
				if err := writeJSONString(out, b2s(keyMask(s2bRO(strconv.FormatFloat(v, 'f', maxFloatPrecision, 64))))); err != nil {
					return fmt.Errorf("array[%d]: write masked float: %w", i, err)
				}
			default:
				out.WriteString(strconv.FormatFloat(v, 'f', maxFloatPrecision, 64))
			}

		default:
			return fmt.Errorf("array[%d]: unexpected token type: %T", i, v)
		}

		i++
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("array: read closing ']' token: %w", err)
	}
	out.WriteByte(closeSquareBracket)
	return nil
}

func writeJSONString(out *bytes.Buffer, s string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal json string: %w", err)
	}
	out.Write(b)
	return nil
}
