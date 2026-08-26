package baseline

import (
	"math"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name     string
		freq     float64
		nominal  float64
		wantPPB  float64
		wantErr  bool
	}{
		{"exact nominal", 9192631770.0, 9192631770.0, 0, false},
		{"plus 1ppb", 9192631770.0 * (1 + 1e-9), 9192631770.0, 1.0, false},
		{"minus 0.5ppb", 9192631770.0 * (1 - 0.5e-9), 9192631770.0, -0.5, false},
		{"unit mismatch (MHz vs Hz)", 9192.631770, 9192631770.0, 0, true},
		{"zero nominal", 1, 0, 0, true},
		{"negative freq", -5, 9192631770.0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Normalize(c.freq, c.nominal)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(got-c.wantPPB) > 1e-6 {
				t.Fatalf("got %v ppb, want %v ppb", got, c.wantPPB)
			}
		})
	}
}

func TestParseBaseline(t *testing.T) {
	if got, _ := ParseBaseline(""); got != RefNominal {
		t.Fatalf("empty baseline should be nominal, got %s", got)
	}
	if got, _ := ParseBaseline("UTC"); got != RefUTC {
		t.Fatalf("UTC baseline parse failed: %s", got)
	}
	if _, err := ParseBaseline("mars-time"); err == nil {
		t.Fatalf("expected error for unknown baseline")
	}
}

func TestAlignWindow(t *testing.T) {
	// epoch 对齐：t=10 落在 [0,60)
	start, end := AlignWindow(timeFromUnix(10), 60*time.Second)
	if start.Unix() != 0 || end.Unix() != 60 {
		t.Fatalf("align(10s) = [%d,%d), want [0,60)", start.Unix(), end.Unix())
	}
	start, end = AlignWindow(timeFromUnix(125), 60*time.Second)
	if start.Unix() != 120 || end.Unix() != 180 {
		t.Fatalf("align(125s) = [%d,%d), want [120,180)", start.Unix(), end.Unix())
	}
}
