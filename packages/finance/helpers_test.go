package finance

import (
	"testing"
)

func TestParseDate(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
	}{
		{"2026-07-20", true},
		{"", false},
		{"20/07/2026", false},
		{"2026-13-01", false}, // month 13
		{"hoje", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseDate(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseDate(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if ok && got.Format("2006-01-02") != tc.in {
				t.Fatalf("parseDate(%q) = %v, want the same day back", tc.in, got)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	// Tool args come from LLM output, so an absent, negative or absurd limit
	// must land on a sane bound rather than reaching the store as-is.
	cases := map[int]int{
		0:    20,  // unset
		-5:   20,  // nonsense
		1:    1,   //
		50:   50,  //
		100:  100, // exactly the cap
		101:  100, // over the cap
		9999: 100,
	}
	for in, want := range cases {
		if got := clampLimit(in); got != want {
			t.Fatalf("clampLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestReaisToCentavosRoundsRatherThanTruncating(t *testing.T) {
	cases := map[float64]int64{
		19.99:  1999, // float truncation would give 1998
		0.1:    10,
		0:      0,
		1:      100,
		0.005:  1, // rounds up at the half centavo
		0.004:  0, // and down below it
		-19.99: -1999,
		-0.1:   -10,
	}
	for in, want := range cases {
		if got := reaisToCentavos(in); got != want {
			t.Fatalf("reaisToCentavos(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestCentavosToReaisRoundTrip(t *testing.T) {
	for _, centavos := range []int64{0, 1, 99, 100, 1999, 1234567} {
		if got := reaisToCentavos(centavosToReais(centavos)); got != centavos {
			t.Fatalf("round trip of %d centavos gave %d", centavos, got)
		}
	}
}

func TestKnownCategory(t *testing.T) {
	slugs := categorySlugs()
	if len(slugs) == 0 {
		t.Fatal("expected the domain to define default categories")
	}
	if !knownCategory(slugs[0]) {
		t.Fatalf("knownCategory(%q) = false, want true for a default slug", slugs[0])
	}
	// A hallucinated category must be rejected so it never reaches storage.
	if knownCategory("categoria_inventada_pela_llm") {
		t.Fatal("knownCategory accepted a category that is not defined")
	}
	if knownCategory("") {
		t.Fatal("knownCategory accepted an empty category")
	}
}
