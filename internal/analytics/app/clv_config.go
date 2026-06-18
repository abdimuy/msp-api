// Package app — clv_config.go implements the CLV policy parameters loader.
// The policy params (margin, horizon, discount rate, band thresholds, LGD) live
// in clv_params.json (embedded at compile time). This file owns only the policy
// block; the Gamma-Gamma and BG/BB parameters in the same JSON are consumed by
// btyd.go — two consumers of the same embedded file is intentional.
//
//nolint:misspell // Spanish field names per project convention.
package app

import (
	"encoding/json"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// clvPolicyJSON is the subset of clv_params.json this loader reads.
// Only the fields owned by the CLV policy layer; btyd.go reads other fields.
type clvPolicyJSON struct {
	Margin          float64       `json:"margin"`
	HorizonMonths   int           `json:"horizon_months"`
	MonthlyDiscount float64       `json:"monthly_discount"`
	BandsPesos      clvBandsJSON  `json:"bands_pesos"`
	Riesgo          clvRiesgoJSON `json:"riesgo"`
}

type clvBandsJSON struct {
	AltoMin  float64 `json:"alto_min"`
	MedioMin float64 `json:"medio_min"`
}

type clvRiesgoJSON struct {
	LGD float64 `json:"lgd"`
}

// CLVParams is an immutable value object holding the parsed CLV policy
// parameters. Construct it once at startup via LoadCLVParams (uses the embedded
// JSON) or ParseCLVParams (accepts raw bytes, useful in tests).
type CLVParams struct {
	margin          float64
	horizonMonths   int
	monthlyDiscount float64
	altoMinPesos    float64
	medioMinPesos   float64
	lgd             float64
	loaded          bool
}

// LoadCLVParams constructs CLVParams from the compile-time-embedded clv_params.json.
// Returns domain.ErrCLVParamsInvalido if the embedded blob fails validation.
func LoadCLVParams() (CLVParams, error) {
	return ParseCLVParams(embeddedCLVParamsJSON)
}

// ParseCLVParams constructs CLVParams from raw JSON bytes.
// Validates all fields. Returns domain.ErrCLVParamsInvalido on any structural error.
func ParseCLVParams(data []byte) (CLVParams, error) {
	var raw clvPolicyJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return CLVParams{}, domain.ErrCLVParamsInvalido
	}
	p := CLVParams{
		margin:          raw.Margin,
		horizonMonths:   raw.HorizonMonths,
		monthlyDiscount: raw.MonthlyDiscount,
		altoMinPesos:    raw.BandsPesos.AltoMin,
		medioMinPesos:   raw.BandsPesos.MedioMin,
		lgd:             raw.Riesgo.LGD,
	}
	if err := p.validate(); err != nil {
		return CLVParams{}, err
	}
	p.loaded = true
	return p, nil
}

// validate checks structural constraints on the parsed CLV params.
func (p CLVParams) validate() error {
	if p.margin <= 0 || p.margin > 1 {
		return domain.ErrCLVParamsInvalido
	}
	if p.horizonMonths <= 0 {
		return domain.ErrCLVParamsInvalido
	}
	if p.monthlyDiscount < 0 {
		return domain.ErrCLVParamsInvalido
	}
	if p.medioMinPesos < 0 || p.altoMinPesos <= p.medioMinPesos {
		return domain.ErrCLVParamsInvalido
	}
	if p.lgd < 0 || p.lgd > 1 {
		return domain.ErrCLVParamsInvalido
	}
	return nil
}

// Loaded reports whether the params were successfully parsed and are ready to use.
// A zero-value CLVParams{} returns false.
func (p CLVParams) Loaded() bool { return p.loaded }

// Margin returns the gross margin fraction (0, 1].
func (p CLVParams) Margin() float64 { return p.margin }

// HorizonMonths returns the CLV projection horizon in months.
func (p CLVParams) HorizonMonths() int { return p.horizonMonths }

// MonthlyDiscount returns the monthly discount rate.
func (p CLVParams) MonthlyDiscount() float64 { return p.monthlyDiscount }

// AltoMinPesos returns the minimum pesos for the ALTO band.
func (p CLVParams) AltoMinPesos() float64 { return p.altoMinPesos }

// MedioMinPesos returns the minimum pesos for the MEDIO band.
func (p CLVParams) MedioMinPesos() float64 { return p.medioMinPesos }

// LGD returns the loss-given-default fraction [0, 1].
func (p CLVParams) LGD() float64 { return p.lgd }
