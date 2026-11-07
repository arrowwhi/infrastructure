package masking

import "testing"

func TestMaskXML(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     Config
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "disabled_returns_input_xml",
			cfg: Config{
				Enabled:     false,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   `<root><phone>+7 999 1112233</phone></root>`,
			want: `<root><phone>+7 999 1112233</phone></root>`,
		},
		{
			name: "key_rule_element_phone",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   `<root><phone>+7 999 1112233</phone></root>`,
			want: `<root><phone>+* *** ***2233</phone></root>`,
		},
		{
			name: "key_rule_attribute_phone",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   `<root><contact phone="+7 999 1112233"/></root>`,
			want: `<root><contact phone="+* *** ***2233"></contact></root>`,
		},
		{
			name: "value_rule_card_in_text_element_digits_only",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				ValRules:    []RuleConfig{{Type: "card", FromPatterns: []string{`\b\d{16}\b`}}},
			},
			in:   `<root><text>4111111111111111</text></root>`,
			want: `<root><text>41**********1111</text></root>`,
		},
		{
			name: "fullname_cyrillic_spaces_preserved",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "fullname", FromPatterns: []string{`(?i)^fullname$`}}},
			},
			in:   `<root><fullname>Иван Петрович</fullname></root>`,
			want: `<root><fullname>**** ********</fullname></root>`,
		},
		{
			name: "passport_not_10_digits_mask_all",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "passport", FromPatterns: []string{`(?i)^passport$`}}},
			},
			in:   `<root><passport>1234567</passport></root>`,
			want: `<root><passport>*******</passport></root>`,
		},
		{
			name: "deep_nested_phone_element",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   `<root><a><b><c><phone>+7 999 1112233</phone></c></b></a></root>`,
			want: `<root><a><b><c><phone>+* *** ***2233</phone></c></b></a></root>`,
		},
		{
			name: "unsupported_format_json_error",
			cfg: Config{
				Enabled:      true,
				IsXMLMasked:  true,
				IsJSONMasked: false,
				KeyRules:     []RuleConfig{{Type: "misc", FromPatterns: []string{`^__never_match__$`}}},
			},
			in:      `{"x":1}`,
			wantErr: true,
		},
		{
			name: "multiple_sibling_phone_elements",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^phone$`}}},
			},
			in:   `<root><phone>+7 999 1112233</phone><phone>12345</phone></root>`,
			want: `<root><phone>+* *** ***2233</phone><phone>*****</phone></root>`,
		},
		{
			name: "value_rule_card_on_attribute_value",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				ValRules:    []RuleConfig{{Type: "card", FromPatterns: []string{`\b\d{16}\b`}}},
			},
			in:   `<root><card number="4111111111111111"></card></root>`,
			want: `<root><card number="41**********1111"></card></root>`,
		},
		{
			name: "const_rule_precedes_phone_rule_on_same_key",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules: []RuleConfig{
					{Type: "const", MaskVal: "H", ToPattern: `(?s).*`, FromPatterns: []string{`^phone$`}},
					{Type: "phone", FromPatterns: []string{`^phone$`}},
				},
			},
			in:   `<root><phone>+7 999 1112233</phone></root>`,
			want: `<root><phone>H</phone></root>`,
		},
		{
			name: "namespaced_element_phone",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "phone", FromPatterns: []string{`(?i)^(?:.+:)?phone$`}}},
			},
			in:   `<root><ns:phone xmlns:ns="x">+7 999 1112233</ns:phone></root>`,
			want: `<root><ns:phone xmlns:ns="x">+* *** ***2233</ns:phone></root>`,
		},
		{
			name: "self_closing_becomes_canonical_end_tags",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "phone", FromPatterns: []string{`^phone$`}}},
			},
			in:   `<root><phone/></root>`,
			want: `<root><phone></phone></root>`,
		},
		{
			name: "malformed_xml_returns_error",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "misc", FromPatterns: []string{`^__never_match__$`}}},
			},
			in:      `<root><phone></root>`,
			wantErr: true,
		},
		{
			name: "mask_attribute_with_hyphen_name",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "const", MaskVal: "XXX", ToPattern: `(?s).*`, FromPatterns: []string{`^secret-code$`}}},
			},
			in:   `<root><item secret-code="123456"/></root>`,
			want: `<root><item secret-code="XXX"></item></root>`,
		},
		{
			name: "value_rule_card_in_element_with_prefix_digits",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				ValRules:    []RuleConfig{{Type: "card", FromPatterns: []string{`\b\d{16}\b`}}},
			},
			in:   `<root><text>my card 4111111111111111</text></root>`,
			want: `<root><text>my card 41**********1111</text></root>`,
		},
		{
			name: "key_rule_overrides_value_rule_on_same_element",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules:    []RuleConfig{{Type: "misc", FromPatterns: []string{`^id$`}}},
				ValRules:    []RuleConfig{{Type: "card", FromPatterns: []string{`\b\d{16}\b`}}},
			},
			in:   `<root><id>4111111111111111</id></root>`,
			want: `<root><id>****************</id></root>`,
		},
		{
			name: "mixed_content_child_bold_has_card",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				ValRules:    []RuleConfig{{Type: "card", FromPatterns: []string{`\b\d{16}\b`}}},
			},
			in:   `<root><p>my card <b>4111111111111111</b></p></root>`,
			want: `<root><p>my card <b>41**********1111</b></p></root>`,
		},
		{
			name: "attributes_and_elements_masked_together",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				KeyRules: []RuleConfig{
					{Type: "phone", FromPatterns: []string{`^phone$`}},
					{Type: "inn", FromPatterns: []string{`^inn$`}},
				},
			},
			in:   `<root><user phone="+7 999 1112233" inn="7712345678"><phone>+7 999 1112233</phone><inn>7712345678</inn></user></root>`,
			want: `<root><user phone="+* *** ***2233" inn="77****5678"><phone>+* *** ***2233</phone><inn>77****5678</inn></user></root>`,
		},
		{
			name: "value_rule_card_in_attribute_with_text_prefix",
			cfg: Config{
				Enabled:     true,
				IsXMLMasked: true,
				ValRules:    []RuleConfig{{Type: "card", FromPatterns: []string{`\b\d{16}\b`}}},
			},
			in:   `<root><card number="4111111111111111"></card></root>`,
			want: `<root><card number="41**********1111"></card></root>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := buildXMLMaskerT(t, tc.cfg)
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

func buildXMLMaskerT(t *testing.T, cfg Config) *Masker {
	t.Helper()
	m, err := NewMaskerBuilder(cfg).Build()
	if err != nil {
		t.Fatalf("build masker: %v", err)
	}
	return m
}
