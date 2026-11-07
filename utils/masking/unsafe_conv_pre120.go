//go:build !go1.20

package masking

import (
	"reflect"
	"unsafe"
)

func b2s(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	return *(*string)(unsafe.Pointer(&b))
}

func s2bRO(s string) []byte {
	if s == "" {
		return nil
	}
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := reflect.SliceHeader{Data: sh.Data, Len: sh.Len, Cap: sh.Len}
	return *(*[]byte)(unsafe.Pointer(&bh))
}
