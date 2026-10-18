// Package market simulates prices for a fixed list of fictional symbols, deterministically per seed.
package market

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"regexp"
	"slices"
	"sync"
	"time"
)

// MaxStepBP is the largest move per tick, in basis points (0.50%).
const MaxStepBP = 50

var symbolRE = regexp.MustCompile(`^[A-Z]{1,5}$`)

// Quote is one simulated price observation.
type Quote struct {
	Symbol       string
	PriceMicros  int64
	ChangeMicros int64
	Tick         int64
	Time         time.Time
}

type walk struct {
	tick  int64
	price int64
	prev  int64
}

// Market advances one random walk per symbol, lazily, in fixed time steps.
type Market struct {
	mu    sync.Mutex
	seed  uint64
	start time.Time
	step  time.Duration
	now   func() time.Time
	walks map[string]*walk
}

// DefaultSymbols are fictional tickers with their opening prices in millionths.
var DefaultSymbols = map[string]int64{
	"ACME": 120_000_000, "BLUE": 45_500_000, "CRNA": 310_250_000, "DYNA": 8_750_000,
	"ECHO": 77_000_000, "FLUX": 1_250_000_000, "GRID": 19_990_000, "HALO": 233_100_000,
}

// New returns a market that starts now and moves every step.
func New(seed uint64, opening map[string]int64, step time.Duration, now func() time.Time) (*Market, error) {
	if step < time.Millisecond {
		return nil, fmt.Errorf("step %s is below 1ms", step)
	}
	if len(opening) == 0 {
		return nil, fmt.Errorf("no symbols")
	}
	m := &Market{seed: seed, start: now(), step: step, now: now, walks: map[string]*walk{}}
	for sym, p := range opening {
		if !symbolRE.MatchString(sym) || p <= 0 {
			return nil, fmt.Errorf("symbol %q or opening price %d invalid", sym, p)
		}
		m.walks[sym] = &walk{price: p, prev: p}
	}
	return m, nil
}

// Symbols returns the listed symbols, sorted.
func (m *Market) Symbols() []string {
	out := make([]string, 0, len(m.walks))
	for s := range m.walks {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// Has reports whether a symbol is listed.
func (m *Market) Has(symbol string) bool {
	_, ok := m.walks[symbol]
	return ok
}

// Quote returns the symbol's price at the current tick.
func (m *Market) Quote(symbol string) (Quote, error) {
	w, ok := m.walks[symbol]
	if !ok {
		return Quote{}, fmt.Errorf("unknown symbol %q", symbol)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	target := int64(m.now().Sub(m.start) / m.step)
	for w.tick < target {
		w.tick++
		w.prev = w.price
		w.price = next(m.seed, symbol, w.tick, w.price)
	}
	return Quote{Symbol: symbol, PriceMicros: w.price, ChangeMicros: w.price - w.prev, Tick: w.tick,
		Time: m.start.Add(time.Duration(w.tick) * m.step)}, nil
}

// next moves a price by a hash-derived amount within MaxStepBP, never below one millionth.
func next(seed uint64, symbol string, tick, price int64) int64 {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], seed)
	binary.BigEndian.PutUint64(buf[8:], uint64(tick))
	h := sha256.Sum256(append(buf[:], symbol...))
	bp := int64(binary.BigEndian.Uint64(h[:8])%(2*MaxStepBP+1)) - MaxStepBP
	moved := price + price*bp/10_000
	return max(moved, 1)
}
