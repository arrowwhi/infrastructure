package masking_test

import (
	"bytes"
	"encoding/xml"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

// goos: darwin
// goarch: arm64
// pkg: git.dip.pics/dip/go/masking.git
// cpu: Apple M4 Pro
// BenchmarkMaskXML
// BenchmarkMaskXML/2KiB
// BenchmarkMaskXML/2KiB-14                    8100            132655 ns/op          22.43 MB/s       60962 B/op       1343 allocs/op
// BenchmarkMaskXML/4KiB
// BenchmarkMaskXML/4KiB-14                    6610            181851 ns/op          22.57 MB/s       83910 B/op       1726 allocs/op
// BenchmarkMaskXML/8KiB
// BenchmarkMaskXML/8KiB-14                    3591            335490 ns/op          23.69 MB/s      137968 B/op       3194 allocs/op
// BenchmarkMaskXML/16KiB
// BenchmarkMaskXML/16KiB-14                   1753            697705 ns/op          23.39 MB/s      307345 B/op       6574 allocs/op
// BenchmarkMaskXML/32KiB
// BenchmarkMaskXML/32KiB-14                    898           1351879 ns/op          23.53 MB/s      531400 B/op      12879 allocs/op
// BenchmarkMaskXML/64KiB
// BenchmarkMaskXML/64KiB-14                    454           2634145 ns/op          23.49 MB/s     1018590 B/op      24848 allocs/op
// BenchmarkMaskXML/128KiB
// BenchmarkMaskXML/128KiB-14                   225           5287624 ns/op          23.29 MB/s     2031878 B/op      49772 allocs/op
// BenchmarkMaskXML/256KiB
// BenchmarkMaskXML/256KiB-14                   100          10616583 ns/op          23.20 MB/s     4063942 B/op      99494 allocs/op
// BenchmarkMaskXML/512KiB
// BenchmarkMaskXML/512KiB-14                    54          21687922 ns/op          22.69 MB/s     8084144 B/op     198542 allocs/op
// BenchmarkMaskXML/1MiB
// BenchmarkMaskXML/1MiB-14                      27          43291060 ns/op          22.73 MB/s    16020768 B/op     392787 allocs/op
// BenchmarkMaskXML/2MiB
// BenchmarkMaskXML/2MiB-14                      13          85109311 ns/op          23.11 MB/s    31794722 B/op     781733 allocs/op
// BenchmarkMaskXML/4MiB
// BenchmarkMaskXML/4MiB-14                       6         169334979 ns/op          23.22 MB/s    63469128 B/op    1558421 allocs/op
// BenchmarkMaskXML/8MiB
// BenchmarkMaskXML/8MiB-14                       3         336967236 ns/op          23.34 MB/s    127120005 B/op   3123516 allocs/op
// BenchmarkMaskXML/16MiB
// BenchmarkMaskXML/16MiB-14                      2         671714020 ns/op          23.42 MB/s    254007244 B/op   6236172 allocs/op
// BenchmarkMaskXML/32MiB
// BenchmarkMaskXML/32MiB-14                      1        1345654542 ns/op          23.38 MB/s    507976728 B/op  12462556 allocs/op
// BenchmarkMaskXML/64MiB
// BenchmarkMaskXML/64MiB-14                      1        2674096666 ns/op          23.53 MB/s    1015555328 B/op 24900828 allocs/op
// BenchmarkMaskXML/128MiB
// BenchmarkMaskXML/128MiB-14                     1        5383614666 ns/op          23.37 MB/s    2031674608 B/op 49807084 allocs/op

func BenchmarkMaskXML(b *testing.B) {
	sizes := makeSizes(b)

	cfg := masking.Config{
		Enabled:     true,
		IsXMLMasked: true,
		KeyRules: []masking.RuleConfig{
			{Type: masking.Const, FromPatterns: []string{`(?i)token|secret`}, MaskVal: "*****"},
			{Type: masking.Phone, FromPatterns: []string{`(?i)phone|mobile`}},
			{Type: masking.Card, FromPatterns: []string{`(?i)card(number)?`}},
			{Type: masking.INN, FromPatterns: []string{`(?i)inn`}},
			{Type: masking.Passport, FromPatterns: []string{`(?i)passport`}},
			{Type: masking.Fullname, FromPatterns: []string{`(?i)full_?name`}},
		},
		ValRules: []masking.RuleConfig{
			{Type: masking.Card, FromPatterns: []string{`(?m)(?:\D|^)(\d{16,})(?:\D|$)`}},
		},
	}
	m, err := masking.NewMaskerBuilder(cfg).Build()
	if err != nil {
		b.Fatalf("build masker: %v", err)
	}

	for _, sz := range sizes {
		size := sz
		b.Run(humanSize(size), func(b *testing.B) {
			r := rand.New(rand.NewSource(*benchSeed))
			data := genXML(size, r)

			if *xmlSanity {
				if !isWellFormedXML(data) {
					b.Fatalf("generated XML is not well-formed for size %d", size)
				}
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for b.Loop() {
				out, err := m.Mask(data)
				if err != nil {
					b.Fatalf("mask: %v", err)
				}
				sinkBytes += len(out)
			}
		})
	}
}

func genXML(targetBytes int, r *rand.Rand) []byte {
	if targetBytes < 256 {
		targetBytes = 256
	}
	buf := bytes.NewBuffer(make([]byte, 0, targetBytes+targetBytes/8))
	now := time.Now().Unix()

	buf.WriteString(`<doc token="`)
	buf.WriteString(randStr(r, 8))
	buf.WriteString(`" secret="`)
	buf.WriteString(randStr(r, 6))
	buf.WriteString(`"><items>`)

	for buf.Len() < targetBytes-(targetBytes/16) {
		buf.Write(makeItemXML(r))
	}

	buf.WriteString(`</items><meta ts="`)
	buf.WriteString(strconv.FormatInt(now, 10))
	buf.WriteString(`"><tags>`)
	for i := 0; i < 6; i++ {
		buf.WriteString(`<t>`)
		buf.WriteString(randStr(r, 8+r.Intn(8)))
		buf.WriteString(`</t>`)
	}
	buf.WriteString(`</tags><attrs note="`)
	buf.WriteString(randStr(r, 32))
	buf.WriteString(`"/></meta></doc>`)
	return buf.Bytes()
}

func makeItemXML(r *rand.Rand) []byte {
	var b bytes.Buffer
	b.WriteString(`<item id="`)
	b.WriteString(strconv64(r.Int63()))
	b.WriteString(`">`)

	b.WriteString(`<full_name>`)
	b.WriteString(fullName(r))
	b.WriteString(`</full_name>`)

	ph := phone(r)
	b.WriteString(`<phone value="`)
	b.WriteString(ph)
	b.WriteString(`">`)
	b.WriteString(ph)
	b.WriteString(`</phone>`)

	b.WriteString(`<inn>`)
	b.WriteString(digits(r, 10))
	b.WriteString(`</inn>`)

	b.WriteString(`<cardNumber>`)
	b.WriteString(digits(r, 16+r.Intn(4)))
	b.WriteString(`</cardNumber>`)

	b.WriteString(`<passport>`)
	if r.Intn(2) == 0 {
		b.WriteString(digits(r, 4))
		b.WriteByte(' ')
		b.WriteString(digits(r, 6))
	} else {
		b.WriteString(digits(r, 10))
	}
	b.WriteString(`</passport>`)

	b.WriteString(`<note>order #`)
	b.WriteString(digits(r, 5))
	b.WriteString(` for client `)
	b.WriteString(digits(r, 9))
	b.WriteString(`</note>`)

	b.WriteString(`<account>ACC-`)
	b.WriteString(digits(r, 2))
	b.WriteByte('-')
	b.WriteString(digits(r, 4))
	b.WriteByte('-')
	b.WriteString(digits(r, 2))
	b.WriteString(`</account>`)

	b.WriteString(`<tags><t>`)
	b.WriteString(randStr(r, 6))
	b.WriteString(`</t><t>`)
	b.WriteString(randStr(r, 10))
	b.WriteString(`</t><t>`)
	b.WriteString(randStr(r, 14))
	b.WriteString(`</t></tags>`)

	b.WriteString(`<nested mobile="`)
	b.WriteString(phone(r))
	b.WriteString(`">data</nested>`)

	b.WriteString(`<members>`)
	for i := 0; i < 2+r.Intn(2); i++ {
		b.WriteString(`<member full_name="`)
		b.WriteString(fullName(r))
		b.WriteString(`" mobile="`)
		b.WriteString(phone(r))
		b.WriteString(`"/>`)
	}
	b.WriteString(`</members>`)

	b.WriteString(`<blob>`)
	b.WriteString(randStr(r, 64+r.Intn(512)))
	b.WriteString(`</blob>`)

	b.WriteString(`</item>`)
	return b.Bytes()
}

func isWellFormedXML(bts []byte) bool {
	var t struct{}
	return xml.Unmarshal(bts, &t) == nil
}
