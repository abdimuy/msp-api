package app

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

const fixtureTol = 1e-6

// ─── fixture types ──────────────────────────────────────────────────────────────

type btydFixtures struct {
	BGBBParams     bgbbJSON          `json:"bgbb_params"`
	HorizonPeriods int               `json:"horizon_periods"`
	Cases          []btydFixtureCase `json:"cases"`
}

type btydFixtureCase struct {
	X      int     `json:"x"`
	TX     int     `json:"t_x"`
	N      int     `json:"n"`
	PAlive float64 `json:"p_alive"`
	Exp12m float64 `json:"exp_12m"`
}

type ggFixtures struct {
	GammaGamma gammaGammaJSON  `json:"gamma_gamma"`
	Cases      []ggFixtureCase `json:"cases"`
}

type ggFixtureCase struct {
	Frequency int     `json:"frequency"`
	Monetary  float64 `json:"monetary"`
	EM        float64 `json:"e_m"`
}

func loadBTYDForTest(t *testing.T) BTYD {
	t.Helper()
	b, err := LoadBTYD()
	if err != nil {
		t.Fatalf("LoadBTYD: %v", err)
	}
	if !b.Loaded() {
		t.Fatal("LoadBTYD returned an unloaded engine")
	}
	return b
}

func loadBTYDFixtures(t *testing.T) btydFixtures {
	t.Helper()
	data, err := os.ReadFile("btyd_fixtures.json")
	if err != nil {
		t.Fatalf("read btyd_fixtures.json: %v", err)
	}
	var fx btydFixtures
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("unmarshal btyd_fixtures.json: %v", err)
	}
	if len(fx.Cases) == 0 {
		t.Fatal("btyd_fixtures.json has no cases")
	}
	return fx
}

func loadGGFixtures(t *testing.T) ggFixtures {
	t.Helper()
	data, err := os.ReadFile("gg_fixtures.json")
	if err != nil {
		t.Fatalf("read gg_fixtures.json: %v", err)
	}
	var fx ggFixtures
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("unmarshal gg_fixtures.json: %v", err)
	}
	if len(fx.Cases) == 0 {
		t.Fatal("gg_fixtures.json has no cases")
	}
	return fx
}

// ─── fixture parity (the key tests) ─────────────────────────────────────────────

func TestBTYDFixtureParity(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	fx := loadBTYDFixtures(t)

	var maxPAlive, maxExp float64
	for _, c := range fx.Cases {
		gotPAlive := b.PAlive(c.X, c.TX, c.N)
		gotExp := b.ExpectedPurchases(fx.HorizonPeriods, c.X, c.TX, c.N)

		dPAlive := math.Abs(gotPAlive - c.PAlive)
		dExp := math.Abs(gotExp - c.Exp12m)
		maxPAlive = math.Max(maxPAlive, dPAlive)
		maxExp = math.Max(maxExp, dExp)

		if dPAlive > fixtureTol {
			t.Errorf("PAlive(x=%d,tx=%d,n=%d) = %.10f, want %.10f (Δ=%.2e)",
				c.X, c.TX, c.N, gotPAlive, c.PAlive, dPAlive)
		}
		if dExp > fixtureTol {
			t.Errorf("ExpectedPurchases(12,x=%d,tx=%d,n=%d) = %.10f, want %.10f (Δ=%.2e)",
				c.X, c.TX, c.N, gotExp, c.Exp12m, dExp)
		}
	}
	t.Logf("BG/BB max abs error: p_alive=%.3e exp_12m=%.3e over %d cases", maxPAlive, maxExp, len(fx.Cases))
}

// ggRelTol bounds the Gamma-Gamma parity check by RELATIVE error rather than
// absolute. The committed Gamma-Gamma params (clv_params.json) are rounded to 8
// decimals, but the harness generated gg_fixtures.json from the full-precision
// fitted params (analysis/clv/clv_harness.py rounds gg_params only at write time,
// after computing e_m). The Go port is bit-identical to the lifetimes formula —
// reproducing the harness with the rounded params in pure Python yields the same
// ~1.3e-5 absolute gap, concentrated in the monetary=100000 case where the result
// itself is ~3.9e4 pesos. As a fraction of the result that gap is ~3e-10, so a
// relative tolerance of 1e-6 verifies the math exactly given the committed params.
const ggRelTol = 1e-6

func TestGammaGammaFixtureParity(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	fx := loadGGFixtures(t)

	var maxAbs, maxRel float64
	for _, c := range fx.Cases {
		got := b.ExpectedAvgProfit(c.Frequency, c.Monetary)
		absErr := math.Abs(got - c.EM)
		relErr := absErr / math.Abs(c.EM)
		maxAbs = math.Max(maxAbs, absErr)
		maxRel = math.Max(maxRel, relErr)
		if relErr > ggRelTol {
			t.Errorf("ExpectedAvgProfit(freq=%d,monetary=%.1f) = %.10f, want %.10f (rel=%.2e)",
				c.Frequency, c.Monetary, got, c.EM, relErr)
		}
	}
	t.Logf("Gamma-Gamma max error over %d cases: abs=%.3e rel=%.3e", len(fx.Cases), maxAbs, maxRel)
}

// ─── property tests ─────────────────────────────────────────────────────────────

func TestBTYDPropertyPAliveInRange(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	for _, n := range []int{0, 1, 6, 12, 24, 60, 120, 240} {
		for x := 0; x <= n; x++ {
			for _, tx := range []int{0, n / 2, n} {
				if tx > n {
					continue
				}
				p := b.PAlive(x, tx, n)
				if !isFinite(p) || p < 0 || p > 1 {
					t.Fatalf("PAlive(x=%d,tx=%d,n=%d) = %v out of [0,1]", x, tx, n, p)
				}
			}
		}
	}
}

func TestBTYDPropertyExpectedPurchasesNonDecreasing(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	cases := []struct{ x, tx, n int }{
		{0, 0, 12}, {4, 8, 12}, {2, 5, 24}, {10, 45, 48}, {0, 0, 60},
	}
	for _, c := range cases {
		prev := b.ExpectedPurchases(0, c.x, c.tx, c.n)
		if prev != 0 {
			t.Errorf("ExpectedPurchases(0,...) = %v, want 0", prev)
		}
		for m := 1; m <= 36; m++ {
			cur := b.ExpectedPurchases(m, c.x, c.tx, c.n)
			if !isFinite(cur) || cur < 0 {
				t.Fatalf("ExpectedPurchases(%d,x=%d,tx=%d,n=%d) = %v not finite/≥0", m, c.x, c.tx, c.n, cur)
			}
			if cur < prev-1e-9 {
				t.Errorf("ExpectedPurchases not non-decreasing: m=%d cur=%v < prev=%v (x=%d,tx=%d,n=%d)",
					m, cur, prev, c.x, c.tx, c.n)
			}
			prev = cur
		}
	}
}

func TestBTYDPropertyDETBounded(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	const horizon = 24
	const discount = 0.00948879
	cases := []struct{ x, tx, n int }{
		{0, 0, 12}, {4, 8, 12}, {2, 5, 24}, {10, 45, 48}, {0, 0, 60}, {3, 198, 201},
	}
	for _, c := range cases {
		det := b.DET(c.x, c.tx, c.n, horizon, discount)
		if !isFinite(det) || det < 0 {
			t.Fatalf("DET(x=%d,tx=%d,n=%d) = %v not finite/≥0", c.x, c.tx, c.n, det)
		}
		// Undiscounted upper bound: E[X(horizon)].
		ub := b.ExpectedPurchases(horizon, c.x, c.tx, c.n)
		if det > ub+1e-9 {
			t.Errorf("DET=%v exceeds undiscounted upper bound %v (x=%d,tx=%d,n=%d)",
				det, ub, c.x, c.tx, c.n)
		}
	}
}

func TestBTYDPropertyExpectedAvgProfitBetween(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	popMean := b.v * b.p / (b.q - 1)
	cases := []struct {
		freq int
		mon  float64
	}{
		{0, 5000}, {1, 100}, {1, 100000}, {5, 8000}, {30, 50000},
	}
	for _, c := range cases {
		got := b.ExpectedAvgProfit(c.freq, c.mon)
		lo := math.Min(c.mon, popMean)
		hi := math.Max(c.mon, popMean)
		if got < lo-1e-6 || got > hi+1e-6 {
			t.Errorf("ExpectedAvgProfit(freq=%d,mon=%.0f) = %v not in [%v,%v]",
				c.freq, c.mon, got, lo, hi)
		}
	}
	// freq=0 → population mean.
	if got := b.ExpectedAvgProfit(0, 9999); math.Abs(got-popMean) > 1e-6 {
		t.Errorf("ExpectedAvgProfit(0,...) = %v, want popMean %v", got, popMean)
	}
}

// ─── Load / Parse error handling ────────────────────────────────────────────────

func TestParseBTYDErrors(t *testing.T) {
	t.Parallel()

	validBGBB := `{"bgbb":{"alpha":0.19,"beta":2.39,"gamma":0.045,"delta":0.6}}`
	validCLV := `{"gamma_gamma":{"p":7.3,"q":14.7,"v":11712.0},"horizon_months":24,"monthly_discount":0.009}`

	tests := []struct {
		name    string
		bgbb    string
		clv     string
		wantErr bool
	}{
		{"valid", validBGBB, validCLV, false},
		{"malformed bgbb", `{not json`, validCLV, true},
		{"malformed clv", validBGBB, `{not json`, true},
		{"zero alpha", `{"bgbb":{"alpha":0,"beta":2.39,"gamma":0.045,"delta":0.6}}`, validCLV, true},
		{"negative beta", `{"bgbb":{"alpha":0.19,"beta":-1,"gamma":0.045,"delta":0.6}}`, validCLV, true},
		{"missing gamma", `{"bgbb":{"alpha":0.19,"beta":2.39,"delta":0.6}}`, validCLV, true},
		{"zero p", validBGBB, `{"gamma_gamma":{"p":0,"q":14.7,"v":11712.0}}`, true},
		{"negative v", validBGBB, `{"gamma_gamma":{"p":7.3,"q":14.7,"v":-1}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b, err := ParseBTYD([]byte(tt.bgbb), []byte(tt.clv))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, domain.ErrBTYDParamsInvalido) {
					t.Errorf("expected ErrBTYDParamsInvalido, got %v", err)
				}
				if b.Loaded() {
					t.Error("engine should not be loaded on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !b.Loaded() {
				t.Error("engine should be loaded")
			}
		})
	}
}

func TestBTYDZeroValueNotLoaded(t *testing.T) {
	t.Parallel()

	var b BTYD
	if b.Loaded() {
		t.Error("zero-value BTYD should not report loaded")
	}
}

func TestBTYDAccessors(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	if b.HorizonMonths() != 24 {
		t.Errorf("HorizonMonths = %d, want 24", b.HorizonMonths())
	}
	if math.Abs(b.MonthlyDiscount()-0.00948879) > 1e-9 {
		t.Errorf("MonthlyDiscount = %v, want 0.00948879", b.MonthlyDiscount())
	}
}

func TestBTYDDETZeroHorizon(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	if got := b.DET(1, 1, 12, 0, 0.01); got != 0 {
		t.Errorf("DET with zero horizon = %v, want 0", got)
	}
}

func TestExpectedPurchasesNonPositiveM(t *testing.T) {
	t.Parallel()

	b := loadBTYDForTest(t)
	if got := b.ExpectedPurchases(-1, 1, 1, 12); got != 0 {
		t.Errorf("ExpectedPurchases(-1,...) = %v, want 0", got)
	}
}

// ─── helper edge cases ──────────────────────────────────────────────────────────

func TestLogAddExpBothNegInf(t *testing.T) {
	t.Parallel()

	if got := logAddExp(math.Inf(-1), math.Inf(-1)); !math.IsInf(got, -1) {
		t.Errorf("logAddExp(-Inf,-Inf) = %v, want -Inf", got)
	}
}

func TestClamp01Finite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want float64
	}{
		{0.5, 0.5},
		{0, 0},
		{1, 1},
		{1.5, 1},  // above range → clamped to 1
		{-0.5, 0}, // below range → clamped to 0
		{math.NaN(), 0},
		{math.Inf(1), 0},
		{math.Inf(-1), 0},
	}
	for _, tt := range tests {
		if got := clamp01Finite(tt.in); got != tt.want {
			t.Errorf("clamp01Finite(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestExpectedPurchasesGammaOneGuard(t *testing.T) {
	t.Parallel()

	// gamma == 1 hits the defensive divide-by-zero guard → 0. Construct an engine
	// with gamma = 1 directly (ParseBTYD accepts it: finite & positive).
	bgbb := `{"bgbb":{"alpha":0.2,"beta":2.4,"gamma":1,"delta":0.6}}`
	clv := `{"gamma_gamma":{"p":7.3,"q":14.7,"v":11712.0},"horizon_months":24,"monthly_discount":0.009}`
	b, err := ParseBTYD([]byte(bgbb), []byte(clv))
	if err != nil {
		t.Fatalf("ParseBTYD: %v", err)
	}
	if got := b.ExpectedPurchases(12, 1, 1, 12); got != 0 {
		t.Errorf("ExpectedPurchases with gamma=1 = %v, want 0", got)
	}
}

func TestExpectedAvgProfitQOneGuard(t *testing.T) {
	t.Parallel()

	// q == 1 makes (q-1) == 0 → population_mean non-finite → guarded to 0.
	bgbb := `{"bgbb":{"alpha":0.2,"beta":2.4,"gamma":0.045,"delta":0.6}}`
	clv := `{"gamma_gamma":{"p":7.3,"q":1,"v":11712.0},"horizon_months":24,"monthly_discount":0.009}`
	b, err := ParseBTYD([]byte(bgbb), []byte(clv))
	if err != nil {
		t.Fatalf("ParseBTYD: %v", err)
	}
	if got := b.ExpectedAvgProfit(0, 5000); got != 0 {
		t.Errorf("ExpectedAvgProfit with q=1 = %v, want 0 (guarded)", got)
	}
}

// ─── fuzz ───────────────────────────────────────────────────────────────────────

func FuzzBTYD(f *testing.F) {
	b, err := LoadBTYD()
	if err != nil {
		f.Fatalf("LoadBTYD: %v", err)
	}
	seeds := []struct {
		x, tx, n, m int
		mon         float64
	}{
		{0, 0, 1, 12, 1000}, {12, 24, 24, 36, 5000}, {3, 198, 201, 1, 100000},
	}
	for _, s := range seeds {
		f.Add(s.x, s.tx, s.n, s.m, s.mon)
	}
	f.Fuzz(func(t *testing.T, x, tx, n, m int, mon float64) {
		// Constrain to the valid domain: 0 <= x <= n <= 240, 0 <= tx <= n.
		if n < 0 {
			n = -n
		}
		n %= 241 // 0..240
		if x < 0 {
			x = -x
		}
		if n > 0 {
			x %= n + 1
		} else {
			x = 0
		}
		if tx < 0 {
			tx = -tx
		}
		if n > 0 {
			tx %= n + 1
		} else {
			tx = 0
		}
		if m < 0 {
			m = -m
		}
		m %= 37 // 0..36

		p := b.PAlive(x, tx, n)
		if !isFinite(p) || p < 0 || p > 1 {
			t.Fatalf("PAlive(%d,%d,%d) = %v out of [0,1]", x, tx, n, p)
		}
		ep := b.ExpectedPurchases(m, x, tx, n)
		if !isFinite(ep) || ep < 0 {
			t.Fatalf("ExpectedPurchases(%d,%d,%d,%d) = %v", m, x, tx, n, ep)
		}
		det := b.DET(x, tx, n, m, 0.01)
		if !isFinite(det) || det < 0 {
			t.Fatalf("DET(%d,%d,%d,%d) = %v", x, tx, n, m, det)
		}
		if math.IsNaN(mon) || math.IsInf(mon, 0) {
			mon = 0
		}
		if mon < 0 {
			mon = -mon
		}
		eap := b.ExpectedAvgProfit(x, mon)
		if !isFinite(eap) || eap < 0 {
			t.Fatalf("ExpectedAvgProfit(%d,%v) = %v", x, mon, eap)
		}
	})
}
