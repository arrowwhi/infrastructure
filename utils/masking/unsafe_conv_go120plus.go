//go:build go1.20

package masking

import "unsafe"

// b2s — zero-copy []byte -> string. ВАЖНО: b должен оставаться живым, пока используется результат.
func b2s(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}

// s2bRO — zero-copy string -> []byte (READ-ONLY!). Нельзя модифицировать результат.
func s2bRO(s string) []byte {
	if s == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
