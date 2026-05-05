package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/platform/domain"
)

func TestNewMoney_HappyPath(t *testing.T) {
	t.Parallel()
	m, err := domain.NewMoney(decimal.NewFromFloat(123.45), "mxn")
	require.NoError(t, err)
	assert.Equal(t, "MXN", m.Currency(), "currency must be uppercased")
	assert.True(t, m.Amount().Equal(decimal.NewFromFloat(123.45)))
}

func TestNewMoney_RejectsInvalidCurrency(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		currency string
	}{
		{"empty", ""},
		{"only spaces", "   "},
		{"two letters", "MX"},
		{"four letters", "MXNX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewMoney(decimal.NewFromInt(100), tc.currency)
			require.Error(t, err)
		})
	}
}

func TestNewMoneyMXN_FixedCurrency(t *testing.T) {
	t.Parallel()
	m := domain.NewMoneyMXN(decimal.NewFromInt(50))
	assert.Equal(t, "MXN", m.Currency())
}

func TestHydrateMoney_NoValidation(t *testing.T) {
	t.Parallel()
	// Hydrate must accept anything, even invalid currencies.
	m := domain.HydrateMoney(decimal.NewFromInt(0), "garbage")
	assert.Equal(t, "garbage", m.Currency())
}

func TestMoney_String(t *testing.T) {
	t.Parallel()
	m := domain.NewMoneyMXN(decimal.NewFromFloat(1234.5))
	assert.Equal(t, "1234.50 MXN", m.String())
}

func TestMoney_Equals(t *testing.T) {
	t.Parallel()
	a := domain.NewMoneyMXN(decimal.NewFromInt(10))
	b := domain.NewMoneyMXN(decimal.NewFromInt(10))
	c := domain.NewMoneyMXN(decimal.NewFromInt(20))
	usd, _ := domain.NewMoney(decimal.NewFromInt(10), "USD")

	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))
	assert.False(t, a.Equals(usd))
}

func TestMoney_SignChecks(t *testing.T) {
	t.Parallel()
	zero := domain.NewMoneyMXN(decimal.Zero)
	pos := domain.NewMoneyMXN(decimal.NewFromInt(1))
	neg := domain.NewMoneyMXN(decimal.NewFromInt(-1))

	assert.True(t, zero.IsZero())
	assert.False(t, zero.IsPositive())
	assert.False(t, zero.IsNegative())

	assert.True(t, pos.IsPositive())
	assert.False(t, pos.IsZero())
	assert.False(t, pos.IsNegative())

	assert.True(t, neg.IsNegative())
	assert.False(t, neg.IsZero())
	assert.False(t, neg.IsPositive())
}

func TestMoney_Add_SameCurrency(t *testing.T) {
	t.Parallel()
	a := domain.NewMoneyMXN(decimal.NewFromInt(10))
	b := domain.NewMoneyMXN(decimal.NewFromInt(7))
	sum, err := a.Add(b)
	require.NoError(t, err)
	assert.True(t, sum.Amount().Equal(decimal.NewFromInt(17)))
}

func TestMoney_Add_DifferentCurrencyFails(t *testing.T) {
	t.Parallel()
	mxn := domain.NewMoneyMXN(decimal.NewFromInt(10))
	usd, err := domain.NewMoney(decimal.NewFromInt(5), "USD")
	require.NoError(t, err)
	_, err = mxn.Add(usd)
	assert.Error(t, err)
}

func TestMoney_Sub_SameCurrency(t *testing.T) {
	t.Parallel()
	a := domain.NewMoneyMXN(decimal.NewFromInt(10))
	b := domain.NewMoneyMXN(decimal.NewFromInt(3))
	diff, err := a.Sub(b)
	require.NoError(t, err)
	assert.True(t, diff.Amount().Equal(decimal.NewFromInt(7)))
}

func TestMoney_Sub_DifferentCurrencyFails(t *testing.T) {
	t.Parallel()
	mxn := domain.NewMoneyMXN(decimal.NewFromInt(10))
	usd, _ := domain.NewMoney(decimal.NewFromInt(5), "USD")
	_, err := mxn.Sub(usd)
	assert.Error(t, err)
}

func TestMoney_Mul(t *testing.T) {
	t.Parallel()
	m := domain.NewMoneyMXN(decimal.NewFromInt(10))
	got := m.Mul(decimal.NewFromFloat(1.5))
	assert.True(t, got.Amount().Equal(decimal.NewFromInt(15)))
}

// Property-based: addition is commutative for same currency.
func TestMoney_Add_Commutative(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		x := rapid.Int64Range(-1_000_000, 1_000_000).Draw(rt, "x")
		y := rapid.Int64Range(-1_000_000, 1_000_000).Draw(rt, "y")
		a := domain.NewMoneyMXN(decimal.NewFromInt(x))
		b := domain.NewMoneyMXN(decimal.NewFromInt(y))

		s1, err := a.Add(b)
		require.NoError(rt, err)
		s2, err := b.Add(a)
		require.NoError(rt, err)
		assert.True(rt, s1.Equals(s2))
	})
}

// Property-based: a.Add(b).Sub(b) == a for same currency.
func TestMoney_AddThenSubIsIdentity(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		x := rapid.Int64Range(-1_000_000, 1_000_000).Draw(rt, "x")
		y := rapid.Int64Range(-1_000_000, 1_000_000).Draw(rt, "y")
		a := domain.NewMoneyMXN(decimal.NewFromInt(x))
		b := domain.NewMoneyMXN(decimal.NewFromInt(y))

		sum, err := a.Add(b)
		require.NoError(rt, err)
		back, err := sum.Sub(b)
		require.NoError(rt, err)
		assert.True(rt, back.Equals(a))
	})
}
