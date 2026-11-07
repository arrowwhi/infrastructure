package masking_test

import (
	"bytes"
	"encoding/json"
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
// BenchmarkMaskJSON
// BenchmarkMaskJSON/2KiB
// BenchmarkMaskJSON/2KiB-14                  14100             84887 ns/op          32.62 MB/s       32401 B/op       1378 allocs/op
// BenchmarkMaskJSON/4KiB
// BenchmarkMaskJSON/4KiB-14                   7989            145421 ns/op          32.61 MB/s       52317 B/op       2261 allocs/op
// BenchmarkMaskJSON/8KiB
// BenchmarkMaskJSON/8KiB-14                   4509            270843 ns/op          32.72 MB/s       91261 B/op       3962 allocs/op
// BenchmarkMaskJSON/16KiB
// BenchmarkMaskJSON/16KiB-14                  2406            511579 ns/op          32.48 MB/s      170329 B/op       7494 allocs/op
// BenchmarkMaskJSON/32KiB
// BenchmarkMaskJSON/32KiB-14                  1240            958637 ns/op          32.97 MB/s      321255 B/op      13806 allocs/op
// BenchmarkMaskJSON/64KiB
// BenchmarkMaskJSON/64KiB-14                   598           1939270 ns/op          32.37 MB/s      622342 B/op      26942 allocs/op
// BenchmarkMaskJSON/128KiB
// BenchmarkMaskJSON/128KiB-14                  316           3765082 ns/op          32.91 MB/s     1204331 B/op      52181 allocs/op
// BenchmarkMaskJSON/256KiB
// BenchmarkMaskJSON/256KiB-14                  160           7458711 ns/op          33.02 MB/s     2403657 B/op     105524 allocs/op
// BenchmarkMaskJSON/512KiB
// BenchmarkMaskJSON/512KiB-14                   76          15114005 ns/op          32.59 MB/s     4850958 B/op     212569 allocs/op
// BenchmarkMaskJSON/1MiB
// BenchmarkMaskJSON/1MiB-14                     39          30154035 ns/op          32.61 MB/s     9702275 B/op     425607 allocs/op
// BenchmarkMaskJSON/2MiB
// BenchmarkMaskJSON/2MiB-14                     19          59567553 ns/op          33.02 MB/s    19187887 B/op     846835 allocs/op
// BenchmarkMaskJSON/4MiB
// BenchmarkMaskJSON/4MiB-14                      9         118896745 ns/op          33.08 MB/s    38085550 B/op    1690464 allocs/op
// BenchmarkMaskJSON/8MiB
// BenchmarkMaskJSON/8MiB-14                      5         238245492 ns/op          33.01 MB/s    76110537 B/op    3382736 allocs/op
// BenchmarkMaskJSON/16MiB
// BenchmarkMaskJSON/16MiB-14                     3         475882569 ns/op          33.05 MB/s    152258405 B/op   6782741 allocs/op
// BenchmarkMaskJSON/32MiB
// BenchmarkMaskJSON/32MiB-14                     2         948897021 ns/op          33.15 MB/s    304634804 B/op  13578019 allocs/op
// BenchmarkMaskJSON/64MiB
// BenchmarkMaskJSON/64MiB-14                     1        1893566583 ns/op          33.23 MB/s    608929040 B/op  27151619 allocs/op
// BenchmarkMaskJSON/128MiB
// BenchmarkMaskJSON/128MiB-14                    1        3826407750 ns/op          32.88 MB/s    1217669064 B/op 54287127 allocs/op

func BenchmarkMaskJSON(b *testing.B) {
	sizes := makeSizes(b)

	cfg := masking.Config{
		Enabled:      true,
		IsJSONMasked: true,
		KeyRules: []masking.RuleConfig{
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
			data := genJSON(size, r)

			if *jsonSanity && !json.Valid(data) {
				b.Fatalf("generated JSON is not valid for size %d", size)
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

// ---- Генератор JSON ----

func genJSON(targetBytes int, r *rand.Rand) []byte {
	if targetBytes < 128 {
		targetBytes = 128
	}
	buf := bytes.NewBuffer(make([]byte, 0, targetBytes+targetBytes/8))
	now := time.Now().Unix()

	buf.WriteString(`{"batch":"`)
	buf.WriteString(randStr(r, 12))
	buf.WriteString(`","items":[`)

	first := true
	for buf.Len() < targetBytes-(targetBytes/16) {
		item := makeItem(r)
		j, _ := json.Marshal(item)
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.Write(j)
	}

	buf.WriteString(`],"meta":{"ts":`)
	buf.WriteString(strconv.FormatInt(now, 10))
	buf.WriteString(`,"tags":[`)
	for i := 0; i < 6; i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteByte('"')
		buf.WriteString(randStr(r, 8+r.Intn(8)))
		buf.WriteByte('"')
	}
	buf.WriteString(`],"attrs":{"note":"`)
	buf.WriteString(randStr(r, 32))
	buf.WriteString(`"}}}`)

	return buf.Bytes()
}

func makeItem(r *rand.Rand) map[string]any {
	item := map[string]any{
		"id":         r.Int63(),
		"full_name":  fullName(r),
		"phone":      phone(r),
		"inn":        digits(r, 10),
		"cardNumber": digits(r, 16+r.Intn(4)),
		"passport":   passport(r),
		"mobiles":    []string{phone(r), phone(r)},
		"note":       "order #" + digits(r, 5) + " for client " + digits(r, 9),
		"account":    "ACC-" + digits(r, 2) + "-" + digits(r, 4) + "-" + digits(r, 2),
		"tags":       []string{randStr(r, 6), randStr(r, 10), randStr(r, 14)},
		"nested": map[string]any{
			"contact": map[string]any{
				"mobile":   phone(r),
				"passport": passport(r),
			},
			"members": []any{
				map[string]any{
					"full_name": fullName(r),
					"mobiles":   []string{phone(r)},
					"meta":      map[string]any{"inn": digits(r, 12)},
				},
				map[string]any{
					"full_name": fullName(r),
					"mobiles":   []string{phone(r), phone(r)},
				},
			},
		},
		"blob": randStr(r, 64+r.Intn(512)),
	}
	if r.Intn(3) == 0 {
		item["children"] = []any{
			map[string]any{
				"full_name": fullName(r),
				"phone":     phone(r),
				"blob":      randStr(r, 32+r.Intn(256)),
			},
		}
	}
	return item
}
