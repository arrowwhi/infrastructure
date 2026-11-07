package masking

import (
	"testing"
)

func TestMaskJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     Config
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "disabled_returns_input",
			cfg: Config{
				Enabled:      false,
				IsJSONMasked: true,
				KeyRules:     []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   `{"phone":"+7 999 1112233"}`,
			want: `{"phone":"+7 999 1112233"}`,
		},
		{
			name: "key_rule_primitive_phone",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules:     []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   `{"phone":"+7 999 1112233"}`,
			want: `{"phone":"+* *** ***2233"}`,
		},
		{
			name: "key_rule_array_of_primitives_phones",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules:     []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phones$`}}},
			},
			in:   `{"phones":["+7 999 1112233","12345"]}`,
			want: `{"phones":["+* *** ***2233","*****"]}`,
		},
		{
			name: "value_rule_card_digits_in_text",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				ValRules:     []RuleConfig{{Type: "card", FromPatterns: []string{`\b\d{13,19}\b`}}},
			},
			in:   `{"text":"4111111111111111"}`,
			want: `{"text":"41**********1111"}`,
		},
		{
			name: "key_rule_number_becomes_string",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules:     []RuleConfig{{Type: "misc", FromPatterns: []string{`^otp$`}}},
			},
			in:   `{"otp":123456}`,
			want: `{"otp":"******"}`,
		},
		{
			name: "const_rule_full_replace",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules: []RuleConfig{{
					Type:         "const",
					MaskVal:      "*****",
					ToPattern:    `(?s).*`,
					FromPatterns: []string{`^secret$`},
				}},
			},
			in:   `{"secret":"abc"}`,
			want: `{"secret":"*****"}`,
		},
		{
			name: "unsupported_format_xml_error",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				IsXMLMasked:  false,
				KeyRules: []RuleConfig{{
					Type:         "misc",
					FromPatterns: []string{`^__never_match__$`},
				}},
			},
			in:      `<xml/>`,
			wantErr: true,
		},
		{
			name: "large_nested_structure_stable",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
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
			in:   `{"users":[{"phone":"+7 999 1112233","inn":"7712345678","profile":{"fullname":"Ivan Ivanov","passport":"1234567890"},"contacts":{"phones":["+7 999 1112233","12345","8 800 555 0000"]},"notes":["my card 4111111111111111","safe text"],"deep":{"level1":{"level2":{"level3":{"phone":"+7 999 1112233"}}}},"extra":{"nestedArray":[{"phone":"+7 999 1112233"},[{"passport":"1234567890"},"no-change"]]}}]}`,
			want: `{"users":[{"phone":"+* *** ***2233","inn":"77****5678","profile":{"fullname":"**** ******","passport":"1234****90"},"contacts":{"phones":["+* *** ***2233","*****","* *** *** 0000"]},"notes":["my card 41**********1111","safe text"],"deep":{"level1":{"level2":{"level3":{"phone":"+* *** ***2233"}}}},"extra":{"nestedArray":[{"phone":"+* *** ***2233"},[{"passport":"1234****90"},"no-change"]]}}]}`,
		},
		{
			name: "key_rule_array_mixed_types_recurses",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules:     []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phones$`}}, {Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   `{"phones":["+7 999 1112233",{"phone":"+7 999 1112233"},[{"phone":"+7 999 1112233"}]]}`,
			want: `{"phones":["+* *** ***2233",{"phone":"+* *** ***2233"},[{"phone":"+* *** ***2233"}]]}`,
		},
		{
			name: "fullname_cyrillic_spaces_preserved",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules:     []RuleConfig{{Type: "fullname", FromPatterns: []string{`(?i)^fullname$`}}},
			},
			in:   `{"fullname":"Иван Петрович"}`,
			want: `{"fullname":"**** ********"}`,
		},
		{
			name: "inn_too_short_mask_all_digits",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules:     []RuleConfig{{Type: "inn", FromPatterns: []string{`(?i)^inn$`}}},
			},
			in:   `{"inn":"12345"}`,
			want: `{"inn":"*****"}`,
		},
		{
			name: "passport_not_10_digits_mask_all",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules:     []RuleConfig{{Type: "passport", FromPatterns: []string{`(?i)^passport$`}}},
			},
			in:   `{"passport":"1234567"}`,
			want: `{"passport":"*******"}`,
		},
		{
			name: "card_15_digits_mask_all_digits_keep_spaces",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				ValRules:     []RuleConfig{{Type: "card", FromPatterns: []string{`\b\d{15}\b`}}},
			},
			in:   `{"text":"123456789012345"}`,
			want: `{"text":"***************"}`,
		},
		{
			name: "card_15_digits_with_spaces_mask_all_digits_keep_separators",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				ValRules:     []RuleConfig{{Type: "card", FromPatterns: []string{`\d(?:[\d ]{13}\d)`}}},
			},
			in:   `{"text":"1234 5678 9012 345"}`,
			want: `{"text":"**** **** **** ***"}`,
		},
		{
			name: "value_rule_misc_masks_numeric_string",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				ValRules:     []RuleConfig{{Type: "misc", FromPatterns: []string{`^\d+$`}}},
			},
			in:   `{"text":"123456"}`,
			want: `{"text":"******"}`,
		},
		{
			name: "unsupported_plain_text_error",
			cfg: Config{
				Enabled:      true,
				IsJSONMasked: true,
				KeyRules: []RuleConfig{{
					Type:         "misc",
					FromPatterns: []string{`^__never__match__$`},
				}},
			},
			in:      `not-json`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := buildMaskerT(t, tc.cfg)
			out, err := m.Mask([]byte(tc.in))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (out=%s)", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("mask error: %v", err)
			}
			if string(out) != tc.want {
				t.Fatalf("want:\n%s\n\ngot:\n%s", tc.want, out)
			}
		})
	}
}

func buildMaskerT(t *testing.T, cfg Config) *Masker {
	t.Helper()

	m, err := NewMaskerBuilder(cfg).Build()
	if err != nil {
		t.Fatalf("build masker: %v", err)
	}

	return m
}
