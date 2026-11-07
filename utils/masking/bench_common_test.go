package masking_test

import (
	"flag"
	"fmt"
	"math/rand"
	"strconv"
	"testing"
)

const (
	KiB = 1024
	MiB = 1024 * KiB
)

var (
	benchMaxMiB  = flag.Int("mask_max_mib", 128, "max MiB size for benchmarks (2..128)")
	skipAboveMiB = flag.Int("mask_skip_above_mib_in_short", 8, "when -test.short, skip sizes above this MiB")
	benchSeed    = flag.Int64("mask_seed", 1, "random seed for deterministic generators")

	jsonSanity = flag.Bool("mask_json_sanity", false, "validate generated JSON before benchmarking")
	xmlSanity  = flag.Bool("mask_xml_sanity", false, "validate generated XML before benchmarking")
)

var sinkBytes int

func makeSizes(_ *testing.B) []int {
	maxBytes := *benchMaxMiB * MiB
	if maxBytes < 2*KiB {
		maxBytes = 2 * KiB
	}
	var sizes []int
	for sz := 2 * KiB; sz <= maxBytes; sz <<= 1 {
		if testing.Short() && sz > *skipAboveMiB*MiB {
			break
		}
		sizes = append(sizes, sz)
	}
	return sizes
}

func humanSize(n int) string {
	if n%MiB == 0 {
		return fmt.Sprintf("%dMiB", n/MiB)
	}
	if n%KiB == 0 {
		return fmt.Sprintf("%dKiB", n/KiB)
	}
	return fmt.Sprintf("%dB", n)
}

func randStr(r *rand.Rand, n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-. "
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.Intn(len(alphabet))]
	}
	return string(b)
}

func digits(r *rand.Rand, n int) string {
	const d = "0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = d[r.Intn(10)]
	}
	return string(b)
}

func phone(r *rand.Rand) string {
	if r.Intn(2) == 0 {
		return "+7 (" + digits(r, 3) + ") " + digits(r, 3) + "-" + digits(r, 2) + "-" + digits(r, 2)
	}
	return "8" + digits(r, 10)
}

func passport(r *rand.Rand) string {
	if r.Intn(2) == 0 {
		return digits(r, 4) + " " + digits(r, 6)
	}
	return digits(r, 10)
}

func fullName(r *rand.Rand) string {
	first := []string{"Ivan", "Petr", "Sidor", "Olga", "Anna", "John", "Jane"}
	last := []string{"Ivanov", "Petrov", "Sidorov", "Smith", "Doe", "Brown"}
	mid := []string{"Ivanovich", "Petrovich", "Sidorovich", "A.", "B.", "C."}
	return last[r.Intn(len(last))] + " " + first[r.Intn(len(first))] + " " + mid[r.Intn(len(mid))]
}

func strconv64(v int64) string { return strconv.FormatInt(v, 10) }
