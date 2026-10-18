package market

import (
	"testing"
	"time"
)

// fakeClock returns a settable clock.
func fakeClock() (*time.Time, func() time.Time) {
	t := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	return &t, func() time.Time { return t }
}

func TestSameSeedSamePricesDifferentSeedDifferentPrices(t *testing.T) {
	at, now := fakeClock()
	a, _ := New(7, DefaultSymbols, 100*time.Millisecond, now)
	b, _ := New(7, DefaultSymbols, 100*time.Millisecond, now)
	c, _ := New(8, DefaultSymbols, 100*time.Millisecond, now)
	*at = at.Add(10 * time.Second)
	qa, _ := a.Quote("ACME")
	qb, _ := b.Quote("ACME")
	qc, _ := c.Quote("ACME")
	if qa != qb {
		t.Fatalf("same seed differs: %+v vs %+v", qa, qb)
	}
	if qa.PriceMicros == qc.PriceMicros {
		t.Fatal("different seeds gave the same price after 100 ticks")
	}
	if qa.Tick != 100 {
		t.Fatalf("tick %d, want 100", qa.Tick)
	}
}

func TestLazyCatchUpEqualsStepByStep(t *testing.T) {
	at, now := fakeClock()
	lazy, _ := New(3, DefaultSymbols, time.Second, now)
	steps, _ := New(3, DefaultSymbols, time.Second, now)
	start := *at
	for i := 1; i <= 500; i++ {
		*at = start.Add(time.Duration(i) * time.Second)
		if _, err := steps.Quote("FLUX"); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := lazy.Quote("FLUX")
	b, _ := steps.Quote("FLUX")
	if a != b {
		t.Fatalf("lazy %+v != stepwise %+v", a, b)
	}
}

func TestMovesStayWithinTheStepLimitAndPositive(t *testing.T) {
	at, now := fakeClock()
	m, _ := New(11, map[string]int64{"TINY": 3, "BIG": 9_000_000_000}, time.Millisecond, now)
	start := *at
	for _, sym := range []string{"TINY", "BIG"} {
		prev, _ := m.Quote(sym)
		for i := 1; i <= 2000; i++ {
			*at = start.Add(time.Duration(i) * time.Millisecond)
			q, _ := m.Quote(sym)
			if q.PriceMicros < 1 {
				t.Fatalf("%s price %d below 1", sym, q.PriceMicros)
			}
			if limit := prev.PriceMicros*MaxStepBP/10_000 + 1; abs(q.ChangeMicros) > limit {
				t.Fatalf("%s moved %d, limit %d", sym, q.ChangeMicros, limit)
			}
			if q.ChangeMicros != q.PriceMicros-prev.PriceMicros || q.Tick != prev.Tick+1 {
				t.Fatalf("%s change or tick inconsistent: %+v after %+v", sym, q, prev)
			}
			prev = q
		}
		*at = start
	}
}

// abs returns the absolute value.
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestRejectsBadInput(t *testing.T) {
	_, now := fakeClock()
	if _, err := New(1, DefaultSymbols, 0, now); err == nil {
		t.Error("zero step accepted")
	}
	if _, err := New(1, map[string]int64{"bad sym": 1}, time.Second, now); err == nil {
		t.Error("bad symbol accepted")
	}
	if _, err := New(1, map[string]int64{"OK": 0}, time.Second, now); err == nil {
		t.Error("zero price accepted")
	}
	if _, err := New(1, nil, time.Second, now); err == nil {
		t.Error("empty market accepted")
	}
	m, _ := New(1, DefaultSymbols, time.Second, now)
	if _, err := m.Quote("NOPE"); err == nil {
		t.Error("unknown symbol quoted")
	}
	if got := m.Symbols(); len(got) != 8 || got[0] != "ACME" || got[7] != "HALO" {
		t.Errorf("symbols %v", got)
	}
}
