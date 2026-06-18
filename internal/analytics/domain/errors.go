// Package domain holds the analytics module's aggregate root entities,
// value objects, and sentinel errors. It depends only on the standard
// library, uuid, decimal, and internal/platform/{audit,apperror}.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the analytics domain. All are produced via apperror.New*
// constructors so they participate in the typed error model (Kind → HTTPStatus)
// and so the err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without a
// trailing period, per project conventions (CLAUDE.md Rule 3).
var (
	// ErrWinbackCandidatoFrecuenciaInvalida is returned when frecuencia < 0.
	ErrWinbackCandidatoFrecuenciaInvalida = apperror.NewValidation(
		"winback_candidato_frecuencia_invalida",
		"la frecuencia del candidato winback no puede ser negativa",
	)

	// ErrWinbackCandidatoMontoInvalido is returned when monetary < 0.
	ErrWinbackCandidatoMontoInvalido = apperror.NewValidation(
		"winback_candidato_monto_invalido",
		"el monto del candidato winback no puede ser negativo",
	)

	// ErrWinbackCandidatoSaldoInvalido is returned when saldo < 0.
	ErrWinbackCandidatoSaldoInvalido = apperror.NewValidation(
		"winback_candidato_saldo_invalido",
		"el saldo del candidato winback no puede ser negativo",
	)

	// ErrWinbackCandidatoCohorteInvalida is returned when cohorteFecha is zero.
	ErrWinbackCandidatoCohorteInvalida = apperror.NewValidation(
		"winback_candidato_cohorte_invalida",
		"la fecha de cohorte del candidato winback es obligatoria",
	)

	// ErrSegmentoInvalido is returned when a string cannot be parsed as a Segmento.
	ErrSegmentoInvalido = apperror.NewValidation(
		"segmento_invalido",
		"el segmento no es válido",
	)

	// ErrScoreWinbackFueraDeRango is returned when a score is not in [0, 100].
	ErrScoreWinbackFueraDeRango = apperror.NewValidation(
		"score_winback_fuera_de_rango",
		"el score winback debe estar entre 0 y 100",
	)

	// ErrEstadoPagoInvalido is returned when a string cannot be parsed as an EstadoPago.
	ErrEstadoPagoInvalido = apperror.NewValidation(
		"estado_pago_invalido",
		"el estado de pago no es válido",
	)

	// ErrRefreshStateNotFound is returned when no MSP_AN_REFRESH_STATE row exists.
	ErrRefreshStateNotFound = apperror.NewNotFound(
		"refresh_state_not_found",
		"no se encontró el estado de refresco",
	)

	// ErrWinbackCandidatoNotFound is returned when no MSP_AN_WINBACK_CANDIDATOS row
	// exists for the requested clienteID (e.g. a client with zero purchase history).
	ErrWinbackCandidatoNotFound = apperror.NewNotFound(
		"winback_candidato_not_found",
		"candidato winback no encontrado",
	)

	// ErrTierRiesgoInvalido is returned when an unrecognized TierRiesgo value is parsed.
	ErrTierRiesgoInvalido = apperror.NewValidation(
		"tier_riesgo_invalido",
		"el tier de riesgo de cobranza no es válido",
	)

	// ErrScoreCreditoFueraDeRango is returned when a credit score is not in [0, 100].
	ErrScoreCreditoFueraDeRango = apperror.NewValidation(
		"score_credito_fuera_de_rango",
		"el score de crédito debe estar entre 0 y 100",
	)

	// ErrBandaCreditoInvalida is returned when an unrecognized BandaCredito value is parsed.
	ErrBandaCreditoInvalida = apperror.NewValidation(
		"banda_credito_invalida",
		"la banda de riesgo de crédito no es válida",
	)

	// ErrScorecardInvalido is returned when the embedded scorecard cannot be parsed or is structurally invalid.
	ErrScorecardInvalido = apperror.NewInternal(
		"scorecard_invalido",
		"el scorecard de crédito embebido no es válido",
	)

	// ErrBTYDParamsInvalido is returned when the embedded BG/BB or Gamma-Gamma
	// parameters cannot be parsed or are structurally invalid (non-finite or
	// non-positive shape parameters).
	ErrBTYDParamsInvalido = apperror.NewInternal(
		"btyd_params_invalido",
		"los parámetros btyd embebidos no son válidos",
	)

	// ErrScoreRecompraFueraDeRango is returned when a repurchase score is not in [0, 100].
	ErrScoreRecompraFueraDeRango = apperror.NewValidation(
		"score_recompra_fuera_de_rango",
		"el score de recompra debe estar entre 0 y 100",
	)

	// ErrBandaRecompraInvalida is returned when an unrecognized BandaRecompra value is parsed.
	ErrBandaRecompraInvalida = apperror.NewValidation(
		"banda_recompra_invalida",
		"la banda de propensión a recompra no es válida",
	)

	// ErrRecompraScorecardInvalido is returned when the embedded recompra scorecard cannot be parsed or is structurally invalid.
	ErrRecompraScorecardInvalido = apperror.NewInternal(
		"recompra_scorecard_invalido",
		"el scorecard de recompra embebido no es válido",
	)

	// ErrMontoCLVNegativo is returned when a CLV monetary amount is negative.
	ErrMontoCLVNegativo = apperror.NewValidation(
		"monto_clv_negativo",
		"el monto de clv no puede ser negativo",
	)

	// ErrBandaCLVInvalida is returned when an unrecognized BandaCLV value is parsed.
	ErrBandaCLVInvalida = apperror.NewValidation(
		"banda_clv_invalida",
		"la banda de clv no es válida",
	)

	// ErrCLVParamsInvalido is returned when the embedded CLV params cannot be parsed or are structurally invalid.
	ErrCLVParamsInvalido = apperror.NewInternal(
		"clv_params_invalido",
		"los parámetros de clv embebidos no son válidos",
	)
)
