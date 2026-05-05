// Package domain holds value objects shared by every module.
package domain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Sentinel errors for Money operations.
var (
	errMoneyCurrencyRequired = errors.New("money: currency is required")
	errMoneyCurrencyInvalid  = errors.New("money: currency must be ISO 4217 (3 letters)")
	errMoneyCurrencyMismatch = errors.New("money: currency mismatch")
)

// Money is an immutable monetary amount with an ISO 4217 currency code.
//
// Stored as decimal.Decimal so we never lose precision. Currency is normalized
// to upper case. Two Money values can only be combined when their currencies
// match.
type Money struct {
	amount   decimal.Decimal
	currency string
}

// NewMoney builds a Money. Currency must be a 3-letter ISO 4217 code.
func NewMoney(amount decimal.Decimal, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return Money{}, errMoneyCurrencyRequired
	}
	if len(currency) != 3 {
		return Money{}, fmt.Errorf("%w: got %q", errMoneyCurrencyInvalid, currency)
	}
	return Money{amount: amount, currency: currency}, nil
}

// NewMoneyMXN is the most common constructor for this codebase.
func NewMoneyMXN(amount decimal.Decimal) Money {
	return Money{amount: amount, currency: "MXN"}
}

// HydrateMoney rebuilds a Money from persistence without validation.
func HydrateMoney(amount decimal.Decimal, currency string) Money {
	return Money{amount: amount, currency: currency}
}

// Amount returns the monetary amount.
func (m Money) Amount() decimal.Decimal { return m.amount }

// Currency returns the ISO 4217 code.
func (m Money) Currency() string { return m.currency }

// String renders the money as "<amount> <currency>".
func (m Money) String() string {
	return fmt.Sprintf("%s %s", m.amount.StringFixed(2), m.currency)
}

// Equals reports whether the two amounts and currencies match exactly.
func (m Money) Equals(other Money) bool {
	return m.amount.Equal(other.amount) && m.currency == other.currency
}

// IsZero reports whether the amount equals zero.
func (m Money) IsZero() bool { return m.amount.IsZero() }

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool { return m.amount.IsNegative() }

// IsPositive reports whether the amount is strictly above zero.
func (m Money) IsPositive() bool { return m.amount.IsPositive() }

// Add returns m+other. Errors if currencies differ.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", errMoneyCurrencyMismatch, m.currency, other.currency)
	}
	return Money{amount: m.amount.Add(other.amount), currency: m.currency}, nil
}

// Sub returns m-other. Errors if currencies differ.
func (m Money) Sub(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", errMoneyCurrencyMismatch, m.currency, other.currency)
	}
	return Money{amount: m.amount.Sub(other.amount), currency: m.currency}, nil
}

// Mul returns m multiplied by a scalar.
func (m Money) Mul(factor decimal.Decimal) Money {
	return Money{amount: m.amount.Mul(factor), currency: m.currency}
}
