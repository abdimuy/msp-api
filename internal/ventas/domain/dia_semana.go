package domain

// DiaSemana enumerates the days of the week for weekly cobranza scheduling.
// The string forms match MSP_VENTAS.DIA_COBRANZA_SEMANA (Spanish, no accents).
type DiaSemana string

// DiaSemana enum values.
const (
	// DiaSemanaLunes is Monday.
	DiaSemanaLunes DiaSemana = "LUNES"
	// DiaSemanaMartes is Tuesday.
	DiaSemanaMartes DiaSemana = "MARTES"
	// DiaSemanaMiercoles is Wednesday.
	DiaSemanaMiercoles DiaSemana = "MIERCOLES"
	// DiaSemanaJueves is Thursday.
	DiaSemanaJueves DiaSemana = "JUEVES"
	// DiaSemanaViernes is Friday.
	DiaSemanaViernes DiaSemana = "VIERNES"
	// DiaSemanaSabado is Saturday.
	DiaSemanaSabado DiaSemana = "SABADO"
	// DiaSemanaDomingo is Sunday.
	DiaSemanaDomingo DiaSemana = "DOMINGO"
)

// ParseDiaSemana parses a string into a DiaSemana or returns
// ErrDiaSemanaInvalido.
func ParseDiaSemana(s string) (DiaSemana, error) {
	d := DiaSemana(s)
	if !d.IsValid() {
		return "", ErrDiaSemanaInvalido
	}
	return d, nil
}

// IsValid reports whether d is a recognized DiaSemana.
func (d DiaSemana) IsValid() bool {
	switch d {
	case DiaSemanaLunes, DiaSemanaMartes, DiaSemanaMiercoles,
		DiaSemanaJueves, DiaSemanaViernes, DiaSemanaSabado, DiaSemanaDomingo:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (d DiaSemana) String() string { return string(d) }
