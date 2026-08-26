package attribution

import (
	"testing"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

func TestEnvironmentCoherenceDetectsTemperatureSwing(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/attr.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk, _ := st.Clocks.Create("cs-attr", "cesium", 9192631770.0, false, 0)
	base := time.Now().UTC()
	for i, temp := range []float64{25.0, 26.2} {
		if err := st.Envs.Insert(&model.EnvReading{
			ClockID: clk.ID, TakenAt: base.Add(time.Duration(i) * time.Minute),
			TempC: temp, HumidityPct: 40, PressureHPa: 1013, Vibration: 0.05,
		}); err != nil {
			t.Fatalf("insert env: %v", err)
		}
	}
	sc := NewScorer(st.Envs, st.Links, st.Jumps)
	score, _ := sc.environmentCoherence(clk.ID, base, base.Add(2*time.Minute))
	if score <= 0 {
		t.Fatalf("temperature swing should produce environment coherence score, got %v", score)
	}
}

func TestLinkCoherenceUsesSuspectLinks(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/link.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ref, _ := st.Clocks.Create("ref", "reference", 10000000, true, 0)
	clk, _ := st.Clocks.Create("cs-link", "cesium", 9192631770.0, false, ref.ID)
	lk, _ := st.Links.Upsert(clk.ID, ref.ID, 0.1, 5)
	if _, err := st.Links.SetStatus(lk.ID, model.LinkSuspect); err != nil {
		t.Fatalf("set suspect: %v", err)
	}
	sc := NewScorer(st.Envs, st.Links, st.Jumps)
	score, _, err := sc.linkCoherence(clk.ID, time.Now().UTC(), time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("link coherence: %v", err)
	}
	if score <= 0 {
		t.Fatalf("suspect link should raise link coherence score")
	}
}
