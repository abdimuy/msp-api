package domain

// diaMesMin is the smallest valid day-of-month value.
const diaMesMin = 1

// diaMesMax is the largest valid day-of-month value.
const diaMesMax = 31

// DiaCobranza is a discriminated union: exactly one of semana/mes is set.
// Use the constructors to obtain a valid instance.
type DiaCobranza struct {
	semana *DiaSemana
	mes    *int
}

// NewDiaCobranzaSemana builds a DiaCobranza scheduled by weekday.
func NewDiaCobranzaSemana(d DiaSemana) (DiaCobranza, error) {
	if !d.IsValid() {
		return DiaCobranza{}, ErrDiaSemanaInvalido
	}
	dd := d
	return DiaCobranza{semana: &dd}, nil
}

// NewDiaCobranzaMes builds a DiaCobranza scheduled by day-of-month (1..31).
func NewDiaCobranzaMes(day int) (DiaCobranza, error) {
	if day < diaMesMin || day > diaMesMax {
		return DiaCobranza{}, ErrDiaMesInvalido
	}
	d := day
	return DiaCobranza{mes: &d}, nil
}

// HydrateDiaCobranza rebuilds a DiaCobranza from persistence. Exactly one of
// semana/mes is expected to be non-nil; the function does not validate that
// invariant (repositories must guarantee it).
func HydrateDiaCobranza(semana *DiaSemana, mes *int) DiaCobranza {
	return DiaCobranza{semana: semana, mes: mes}
}

// IsSemana reports whether the DiaCobranza is scheduled by weekday.
func (d DiaCobranza) IsSemana() bool { return d.semana != nil }

// IsMes reports whether the DiaCobranza is scheduled by day-of-month.
func (d DiaCobranza) IsMes() bool { return d.mes != nil }

// Semana returns the weekday pointer (nil when scheduled by month).
func (d DiaCobranza) Semana() *DiaSemana { return d.semana }

// Mes returns the day-of-month pointer (nil when scheduled by weekday).
func (d DiaCobranza) Mes() *int { return d.mes }

// Equals reports whether two DiaCobranza values are identical.
func (d DiaCobranza) Equals(other DiaCobranza) bool {
	if (d.semana == nil) != (other.semana == nil) {
		return false
	}
	if (d.mes == nil) != (other.mes == nil) {
		return false
	}
	if d.semana != nil && *d.semana != *other.semana {
		return false
	}
	if d.mes != nil && *d.mes != *other.mes {
		return false
	}
	return true
}
