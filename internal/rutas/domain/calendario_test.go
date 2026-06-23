//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// d parses a date string "YYYY-MM-DD" to midnight UTC.
func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic("d: invalid date: " + s)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// dh parses a datetime string in RFC3339 format to UTC.
func dh(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic("dh: invalid datetime: " + s)
	}
	return t.UTC()
}

func TestVencimientosVencidos(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id          string
		frec        rutasdomain.Frecuencia
		fechaCargo  time.Time
		fechaInicio time.Time
		grace       int
		want        int
	}{
		// SEMANAL (grace 0 — ignored)
		{"V-S1", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-01"), 0, 0},
		{"V-S2", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-08"), 0, 1},
		{"V-S3", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-07"), 0, 0},
		{"V-S4", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-15"), 0, 2},
		{"V-S5", rutasdomain.Semanal, d("2026-01-01"), d("2026-02-01"), 0, 4},                       // 31 días → floor(31/7)=4
		{"V-S6", rutasdomain.Semanal, d("2026-01-10"), d("2026-01-05"), 0, 0},                       // inicio antes de cargo → piso 0
		{"V-S7", rutasdomain.Semanal, dh("2026-01-01T23:30:00Z"), dh("2026-01-08T00:10:00Z"), 0, 1}, // D1: hora ignorada
		{"V-S8", "DIARIO", d("2026-01-01"), d("2026-01-22"), 0, 3},                                  // frecuencia desconocida → semanal, 21/7

		// MENSUAL (grace 2)
		{"V-M1", rutasdomain.Mensual, d("2026-01-01"), d("2026-01-15"), 2, 0}, // día-1 2026-01-01 == cargo, excluido; 02-01 futuro
		{"V-M2", rutasdomain.Mensual, d("2025-12-15"), d("2026-01-15"), 2, 1}, // 01-01, +2=01-03 < 01-15
		{"V-M3", rutasdomain.Mensual, d("2025-11-20"), d("2026-02-10"), 2, 3}, // 12-01, 01-01, 02-01; cada +2 < 02-10
		{"V-M4", rutasdomain.Mensual, d("2025-12-15"), d("2026-01-03"), 2, 0}, // 01-01+2=01-03, NO < 01-03 estricto
		{"V-M5", rutasdomain.Mensual, d("2025-12-15"), d("2026-01-04"), 2, 1}, // 01-01+2=01-03 < 01-04
		{"V-M6", rutasdomain.Mensual, d("2025-06-10"), d("2026-01-15"), 2, 7}, // 07-01..2026-01-01 = 7 días-1

		// QUINCENAL (grace 2)
		{"V-Q1", rutasdomain.Quincenal, d("2026-01-01"), d("2026-01-20"), 2, 1}, // 01-15+2=01-17<01-20 sí; 01-31+2=02-02 no
		{"V-Q2", rutasdomain.Quincenal, d("2026-01-01"), d("2026-02-05"), 2, 2}, // 01-15, 01-31; 02-15 futuro
		{"V-Q3", rutasdomain.Quincenal, d("2026-02-01"), d("2026-03-10"), 2, 2}, // 02-15, 02-28 (feb no bisiesto)
		{"V-Q5", rutasdomain.Quincenal, d("2026-04-01"), d("2026-05-05"), 2, 2}, // 04-15, 04-30 (abril 30 días)
		{"V-Q6", rutasdomain.Quincenal, d("2026-03-01"), d("2026-04-05"), 2, 2}, // 03-15, 03-31 (marzo 31)
		{"V-Q7", rutasdomain.Quincenal, d("2026-01-01"), d("2026-02-02"), 2, 1}, // 01-15 sí; 01-31+2=02-02 NO<02-02 estricto
		{"V-Q8", rutasdomain.Quincenal, d("2026-01-01"), d("2026-02-03"), 2, 2}, // 01-15, 01-31+2=02-02<02-03
		{"V-Q9", rutasdomain.Quincenal, d("2025-12-10"), d("2026-01-20"), 2, 3}, // 12-15, 12-31, 01-15; cruce de año

		// CERO / fresh
		{"V-Z1", rutasdomain.Mensual, d("2026-01-05"), d("2026-01-10"), 2, 0},
		{"V-Z2", rutasdomain.Quincenal, d("2026-01-16"), d("2026-01-20"), 2, 0}, // primer candidato>cargo=01-31, +2 no<01-20
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := rutasdomain.VencimientosVencidos(tc.frec, tc.fechaCargo, tc.fechaInicio, tc.grace)
			assert.Equal(
				t, tc.want, got,
				"VencimientosVencidos(%s, cargo=%s, inicio=%s, grace=%d)",
				tc.frec, tc.fechaCargo.Format(time.RFC3339), tc.fechaInicio.Format(time.RFC3339), tc.grace,
			)
		})
	}
}

func TestAplicaEnVentana(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id         string
		frec       rutasdomain.Frecuencia
		fechaCargo time.Time
		desde      time.Time
		hasta      time.Time
		want       bool
	}{
		// SEMANAL
		{"A-S1", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-08"), d("2026-01-14"), true},                                  // k=1 → 01-08
		{"A-S2", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-02"), d("2026-01-07"), false},                                 // D6: semana no cumplida
		{"A-S3", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-15"), d("2026-01-21"), true},                                  // k=2 → 01-15
		{"A-S4", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-09"), d("2026-01-13"), false},                                 // 01-08 antes, 01-15 después
		{"A-S5", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-08"), d("2026-01-08"), true},                                  // borde desde=hasta inclusivo
		{"A-S6", rutasdomain.Semanal, d("2026-01-01"), d("2026-01-03"), d("2026-01-08"), true},                                  // 01-08==hasta inclusivo
		{"A-S7", rutasdomain.Semanal, dh("2026-01-01T18:00:00Z"), dh("2026-01-08T01:00:00Z"), dh("2026-01-08T23:00:00Z"), true}, // D1
		{"A-S8", "X", d("2026-01-01"), d("2026-01-08"), d("2026-01-14"), true},                                                  // frecuencia desconocida → semanal

		// MENSUAL
		{"A-M1", rutasdomain.Mensual, d("2025-12-20"), d("2026-01-01"), d("2026-01-07"), true},  // 01-01 en ventana, >cargo
		{"A-M2", rutasdomain.Mensual, d("2025-12-20"), d("2026-01-02"), d("2026-01-10"), false}, // 01-01 antes de desde
		{"A-M3", rutasdomain.Mensual, d("2025-12-20"), d("2026-01-01"), d("2026-01-01"), true},  // borde
		{"A-M4", rutasdomain.Mensual, d("2026-01-01"), d("2026-01-01"), d("2026-01-10"), false}, // 01-01==cargo no es >cargo; 02-01 fuera
		{"A-M5", rutasdomain.Mensual, d("2026-01-01"), d("2026-01-28"), d("2026-02-03"), true},  // 02-01 en ventana

		// QUINCENAL
		{"A-Q1", rutasdomain.Quincenal, d("2026-01-01"), d("2026-01-15"), d("2026-01-15"), true},   // 15 borde
		{"A-Q2", rutasdomain.Quincenal, d("2026-01-01"), d("2026-01-16"), d("2026-01-30"), false},  // 15 antes, 31 después
		{"A-Q3", rutasdomain.Quincenal, d("2026-01-01"), d("2026-01-31"), d("2026-02-02"), true},   // último-día 31
		{"A-Q4", rutasdomain.Quincenal, d("2026-02-01"), d("2026-02-28"), d("2026-02-28"), true},   // feb NO bisiesto: último=28
		{"A-Q5", rutasdomain.Quincenal, d("2026-02-01"), d("2026-02-16"), d("2026-02-27"), false},  // 15 antes, 28 después
		{"A-Q6", rutasdomain.Quincenal, d("2024-02-01"), d("2024-02-29"), d("2024-02-29"), true},   // feb bisiesto: último=29
		{"A-Q7", rutasdomain.Quincenal, d("2024-02-01"), d("2024-02-28"), d("2024-02-28"), false},  // bisiesto: 28 NO es vencimiento, último=29
		{"A-Q8", rutasdomain.Quincenal, d("2026-04-01"), d("2026-04-30"), d("2026-04-30"), true},   // abril último=30
		{"A-Q9", rutasdomain.Quincenal, d("2026-04-01"), d("2026-04-16"), d("2026-04-29"), false},  // 15 antes, 30 después
		{"A-Q10", rutasdomain.Quincenal, d("2025-12-01"), d("2025-12-31"), d("2026-01-01"), true},  // 12-31 cruce de año
		{"A-Q11", rutasdomain.Quincenal, d("2026-01-15"), d("2026-01-15"), d("2026-01-20"), false}, // 15==cargo no >cargo; 31 fuera
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := rutasdomain.AplicaEnVentana(tc.frec, tc.fechaCargo, tc.desde, tc.hasta)
			assert.Equal(
				t, tc.want, got,
				"AplicaEnVentana(%s, cargo=%s, desde=%s, hasta=%s)",
				tc.frec, tc.fechaCargo.Format(time.RFC3339), tc.desde.Format(time.RFC3339), tc.hasta.Format(time.RFC3339),
			)
		})
	}
}

// TestCalendario_TerminaConRangoInvertido is a regression test for an infinite
// loop: when the start month is AFTER the end month, the month-iterating loops
// must still terminate (return 0 / false), not spin forever. This happens in
// real data when a venta's FechaCargo lands in a later month than the
// cobrador's fechaInicio (QUINCENAL vencidos), or when the [desde, hasta]
// window is degenerate. Each call runs in a goroutine guarded by a timeout so
// the bug surfaces as a test failure instead of hanging the whole suite.
func TestCalendario_TerminaConRangoInvertido(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id   string
		run  func() int // returns count, or 1/0 for the boolean calls
		want int
	}{
		{
			id: "vencidos_quincenal_cargo_mes_posterior",
			run: func() int {
				return rutasdomain.VencimientosVencidos(rutasdomain.Quincenal, d("2026-02-10"), d("2026-01-05"), 2)
			},
			want: 0,
		},
		{
			id: "vencidos_mensual_cargo_mes_posterior",
			run: func() int {
				return rutasdomain.VencimientosVencidos(rutasdomain.Mensual, d("2026-02-10"), d("2026-01-05"), 2)
			},
			want: 0,
		},
		{
			id: "aplica_quincenal_ventana_invertida",
			run: func() int {
				if rutasdomain.AplicaEnVentana(rutasdomain.Quincenal, d("2026-01-01"), d("2026-03-10"), d("2026-01-05")) {
					return 1
				}
				return 0
			},
			want: 0,
		},
		{
			id: "aplica_mensual_ventana_invertida",
			run: func() int {
				if rutasdomain.AplicaEnVentana(rutasdomain.Mensual, d("2026-01-01"), d("2026-03-10"), d("2026-01-05")) {
					return 1
				}
				return 0
			},
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			done := make(chan int, 1)
			go func() { done <- tc.run() }()
			select {
			case got := <-done:
				assert.Equal(t, tc.want, got, tc.id)
			case <-time.After(2 * time.Second):
				t.Fatalf("%s: no terminó en 2s (loop infinito)", tc.id)
			}
		})
	}
}
