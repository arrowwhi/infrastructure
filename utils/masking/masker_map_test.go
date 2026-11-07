package masking

import (
	"reflect"
	"testing"
)

func TestMaskMap(t *testing.T) {
	t.Parallel()

	type M = map[string]any
	type S = []any

	cases := []struct {
		name string
		cfg  Config
		in   M
		want M
	}{
		{
			name: "disabled_returns_input_map",
			cfg: Config{
				Enabled:  false,
				KeyRules: []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   M{"phone": "+7 999 1112233"},
			want: M{"phone": "+7 999 1112233"},
		},
		{
			name: "nil_input_returns_nil",
			cfg: Config{
				Enabled:  true,
				KeyRules: []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   nil,
			want: nil,
		},
		{
			name: "key_rule_primitive_phone",
			cfg: Config{
				Enabled:  true,
				KeyRules: []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   M{"phone": "+7 999 1112233"},
			want: M{"phone": "+* *** ***2233"},
		},
		{
			name: "key_rule_array_of_primitives_phones",
			cfg: Config{
				Enabled:  true,
				KeyRules: []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phones$`}}},
			},
			in:   M{"phones": S{"+7 999 1112233", "12345"}},
			want: M{"phones": S{"+* *** ***2233", "*****"}},
		},
		{
			name: "key_rule_array_mixed_types_recurses",
			cfg: Config{
				Enabled: true,
				KeyRules: []RuleConfig{
					{Type: "phone", FromPatterns: []string{`(?i)^phones$`}},
					{Type: "phone", FromPatterns: []string{`(?i)^phone$`}},
				},
			},
			in: M{
				"phones": S{
					"+7 999 1112233",
					M{"phone": "+7 999 1112233"},
					S{M{"phone": "+7 999 1112233"}},
				},
			},
			want: M{
				"phones": S{
					"+* *** ***2233",
					M{"phone": "+* *** ***2233"},
					S{M{"phone": "+* *** ***2233"}},
				},
			},
		},
		{
			name: "value_rule_card_digits_in_text",
			cfg: Config{
				Enabled: true,
				ValRules: []RuleConfig{
					{Type: "card", FromPatterns: []string{`\b\d{13,19}\b`}},
				},
			},
			in:   M{"text": "4111111111111111"},
			want: M{"text": "41**********1111"},
		},
		{
			name: "key_rule_number_becomes_string",
			cfg: Config{
				Enabled:  true,
				KeyRules: []RuleConfig{{Type: "misc", FromPatterns: []string{`^otp$`}}},
			},
			in:   M{"otp": 123456},
			want: M{"otp": "******"},
		},
		{
			name: "const_rule_full_replace",
			cfg: Config{
				Enabled: true,
				KeyRules: []RuleConfig{{
					Type:         "const",
					MaskVal:      "*****",
					ToPattern:    `(?s).*`,
					FromPatterns: []string{`^secret$`},
				}},
			},
			in:   M{"secret": "abc"},
			want: M{"secret": "*****"},
		},
		{
			name: "const_rule_precedes_phone_rule_on_same_key",
			cfg: Config{
				Enabled: true,
				KeyRules: []RuleConfig{
					{Type: "const", MaskVal: "H", ToPattern: `(?s).*`, FromPatterns: []string{`^phone$`}},
					{Type: "phone", FromPatterns: []string{`^phone$`}},
				},
			},
			in:   M{"phone": "+7 999 1112233"},
			want: M{"phone": "H"},
		},
		{
			name: "large_nested_structure_stable",
			cfg: Config{
				Enabled: true,
				KeyRules: []RuleConfig{
					{Type: "phone", FromPatterns: []string{`(?i)^phone$`}},
					{Type: "phone", FromPatterns: []string{`(?i)^phones$`}},
					{Type: "fullname", FromPatterns: []string{`(?i)^fullname$`}},
					{Type: "passport", FromPatterns: []string{`(?i)^passport$`}},
					{Type: "inn", FromPatterns: []string{`(?i)^inn$`}},
				},
				ValRules: []RuleConfig{
					{Type: "card", FromPatterns: []string{`\b\d{16}\b`}},
				},
			},
			in: M{
				"users": S{
					M{
						"phone": "+7 999 1112233",
						"inn":   "7712345678",
						"profile": M{
							"fullname": "Ivan Ivanov",
							"passport": "1234567890",
						},
						"contacts": M{
							"phones": S{"+7 999 1112233", "12345", "8 800 555 0000"},
						},
						"notes": S{"my card 4111111111111111", "safe text"},
						"deep": M{
							"level1": M{
								"level2": M{
									"level3": M{"phone": "+7 999 1112233"},
								},
							},
						},
						"extra": M{
							"nestedArray": S{
								M{"phone": "+7 999 1112233"},
								S{M{"passport": "1234567890"}, "no-change"},
							},
						},
					},
				},
			},
			want: M{
				"users": S{
					M{
						"phone": "+* *** ***2233",
						"inn":   "77****5678",
						"profile": M{
							"fullname": "**** ******",
							"passport": "1234****90",
						},
						"contacts": M{
							"phones": S{"+* *** ***2233", "*****", "* *** *** 0000"},
						},
						"notes": S{"my card 41**********1111", "safe text"},
						"deep": M{
							"level1": M{
								"level2": M{
									"level3": M{"phone": "+* *** ***2233"},
								},
							},
						},
						"extra": M{
							"nestedArray": S{
								M{"phone": "+* *** ***2233"},
								S{M{"passport": "1234****90"}, "no-change"},
							},
						},
					},
				},
			},
		},
		{
			name: "fullname_cyrillic_spaces_preserved",
			cfg: Config{
				Enabled:  true,
				KeyRules: []RuleConfig{{Type: "fullname", FromPatterns: []string{`(?i)^fullname$`}}},
			},
			in:   M{"fullname": "Иван Петрович"},
			want: M{"fullname": "**** ********"},
		},
		{
			name: "inn_too_short_mask_all_digits",
			cfg: Config{
				Enabled:  true,
				KeyRules: []RuleConfig{{Type: "inn", FromPatterns: []string{`(?i)^inn$`}}},
			},
			in:   M{"inn": "12345"},
			want: M{"inn": "*****"},
		},
		{
			name: "passport_not_10_digits_mask_all",
			cfg: Config{
				Enabled:  true,
				KeyRules: []RuleConfig{{Type: "passport", FromPatterns: []string{`(?i)^passport$`}}},
			},
			in:   M{"passport": "1234567"},
			want: M{"passport": "*******"},
		},
		{
			name: "card_15_digits_mask_all_digits_keep_spaces",
			cfg: Config{
				Enabled: true,
				ValRules: []RuleConfig{
					{Type: "card", FromPatterns: []string{`\b\d{15}\b`}},
				},
			},
			in:   M{"text": "123456789012345"},
			want: M{"text": "***************"},
		},
		{
			name: "card_15_digits_with_spaces_mask_all_digits_keep_separators",
			cfg: Config{
				Enabled: true,
				ValRules: []RuleConfig{
					{Type: "card", FromPatterns: []string{`\d(?:[\d ]{13}\d)`}},
				},
			},
			in:   M{"text": "1234 5678 9012 345"},
			want: M{"text": "**** **** **** ***"},
		},
		{
			name: "value_rule_misc_masks_numeric_string",
			cfg: Config{
				Enabled: true,
				ValRules: []RuleConfig{
					{Type: "misc", FromPatterns: []string{`^\d+$`}},
				},
			},
			in:   M{"text": "123456"},
			want: M{"text": "******"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := buildMapMaskerT(t, tc.cfg)
			out, err := m.MaskMap(tc.in)
			if err != nil {
				t.Fatalf("mask map error: %v", err)
			}
			if !reflect.DeepEqual(out, tc.want) {
				t.Fatalf("mismatch:\nwant: %#v\n got: %#v", tc.want, out)
			}
		})
	}
}

func buildMapMaskerT(t *testing.T, cfg Config) *MapMasker {
	t.Helper()
	m, err := NewMapMasker(cfg)
	if err != nil {
		t.Fatalf("build map masker: %v", err)
	}
	return m
}
