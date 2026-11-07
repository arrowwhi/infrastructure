package masking

import "testing"

func TestMaskEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple", in: "john.doe@example.com", want: "********@example.com"},
		{name: "one_char_local", in: "j@example.com", want: "*****@example.com"},
		{name: "two_chars_local", in: "jo@example.com", want: "*****@example.com"},
		{name: "with_plus_tag", in: "john+tag@example.com", want: "********@example.com"},
		{name: "double_plus_tag", in: "joe+tag+more@example.com", want: "************@example.com"},
		{name: "separators_all_masked", in: "a_b-c.d@example.com", want: "*******@example.com"},
		{name: "only_sep_and_one_visible", in: "..a..@ex.com", want: "*****@ex.com"},
		{name: "whitespace_trimmed", in: "  j@example.com\t", want: "*****@example.com"},
		{name: "unicode_local_bytes_count", in: "иван@example.ru", want: "********@example.ru"},
		{name: "subdomain_domain_kept", in: "user@sub.domain.co.uk", want: "****@sub.domain.co.uk"},
		{name: "single_char_domain", in: "a@b", want: "*****@b"},
		{name: "multi_at_signs", in: "u@sub@domain.com", want: "*****@sub@domain.com"},
		{name: "not_an_email_fallback", in: "not-an-email", want: "************"},
		{name: "at_at_start_invalid", in: "@example.com", want: "************"},
		{name: "at_at_end_invalid", in: "user@", want: "*****"},
		{name: "empty", in: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := string(maskEmail([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("maskEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
