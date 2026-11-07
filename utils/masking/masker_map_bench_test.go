package masking_test

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/arrowwhi/infrastructure/utils/masking"
)

// goos: darwin
// goarch: arm64
// pkg: git.dip.pics/dip/go/masking.git
// cpu: Apple M4 Pro
// BenchmarkMaskMap
// BenchmarkMaskMap/2KiB
// BenchmarkMaskMap/2KiB-14                   33849             33652 ns/op          62.85 MB/s        7888 B/op         137 allocs/op
// BenchmarkMaskMap/4KiB
// BenchmarkMaskMap/4KiB-14                   16308             73555 ns/op          63.42 MB/s       18267 B/op        333 allocs/op
// BenchmarkMaskMap/8KiB
// BenchmarkMaskMap/8KiB-14                    8400            143872 ns/op          61.85 MB/s       31762 B/op        587 allocs/op
// BenchmarkMaskMap/16KiB
// BenchmarkMaskMap/16KiB-14                   4422            274283 ns/op          59.96 MB/s       55606 B/op       1037 allocs/op
// BenchmarkMaskMap/32KiB
// BenchmarkMaskMap/32KiB-14                   2348            518667 ns/op          60.25 MB/s      103489 B/op       1929 allocs/op
// BenchmarkMaskMap/64KiB
// BenchmarkMaskMap/64KiB-14                   1170           1018254 ns/op          61.30 MB/s      202432 B/op       3790 allocs/op
// BenchmarkMaskMap/128KiB
// BenchmarkMaskMap/128KiB-14                   579           2032921 ns/op          60.53 MB/s      412616 B/op       7679 allocs/op
// BenchmarkMaskMap/256KiB
// BenchmarkMaskMap/256KiB-14                   285           4086490 ns/op          60.35 MB/s      827197 B/op      15386 allocs/op
// BenchmarkMaskMap/512KiB
// BenchmarkMaskMap/512KiB-14                   146           8126767 ns/op          60.48 MB/s     1640609 B/op      30670 allocs/op
// BenchmarkMaskMap/1MiB
// BenchmarkMaskMap/1MiB-14                      72          16431642 ns/op          59.86 MB/s     3254557 B/op      61200 allocs/op
// BenchmarkMaskMap/2MiB
// BenchmarkMaskMap/2MiB-14                      36          32603586 ns/op          60.32 MB/s     6436283 B/op     121490 allocs/op
// BenchmarkMaskMap/4MiB
// BenchmarkMaskMap/4MiB-14                      16          70504378 ns/op          55.77 MB/s    12831792 B/op     242384 allocs/op
// BenchmarkMaskMap/8MiB
// BenchmarkMaskMap/8MiB-14                       7         143061304 ns/op          54.97 MB/s    25652004 B/op     484690 allocs/op

var sinkMap map[string]any

func BenchmarkMaskMap(b *testing.B) {
	sizes := makeSizes(b)

	cfg := masking.Config{
		Enabled: true,
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
	mm, err := masking.NewMapMasker(cfg)
	if err != nil {
		b.Fatalf("build map masker: %v", err)
	}

	for _, sz := range sizes {
		b.Run(humanSize(sz), func(b *testing.B) {
			r := rand.New(rand.NewSource(*benchSeed))
			root := genMap(sz, r)

			jb, err := json.Marshal(root)
			if err != nil {
				b.Fatalf("marshal for size estimation: %v", err)
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(jb)))
			b.ResetTimer()

			for b.Loop() {
				out, err := mm.MaskMap(root)
				if err != nil {
					b.Fatalf("mask map: %v", err)
				}
				sinkMap = out
			}
		})
	}
}

func genMap(targetBytes int, r *rand.Rand) map[string]any {
	if targetBytes < 128 {
		targetBytes = 128
	}
	root := map[string]any{
		"batch": randStr(r, 12),
		"items": []any{},
		"meta": map[string]any{
			"ts":   time.Now().Unix(),
			"tags": []string{},
			"attrs": map[string]any{
				"note": randStr(r, 32),
			},
		},
	}

	tags := make([]string, 6)
	for i := range tags {
		tags[i] = randStr(r, 8+r.Intn(8))
	}
	root["meta"].(map[string]any)["tags"] = tags

	items := root["items"].([]any)
	for {
		items = append(items, makeItem(r))
		root["items"] = items

		jb, _ := json.Marshal(root)
		if len(jb) >= targetBytes-(targetBytes/16) {
			break
		}
	}
	return root
}
