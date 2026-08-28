package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/flota/domain"
)

// TestNuevaObservacion_ExigeUID verifies the only hard requirement: without
// the Firestore document id there is no identity to hang history on.
func TestNuevaObservacion_ExigeUID(t *testing.T) {
	t.Parallel()

	for _, uid := range []string{"", "   ", "\t\n"} {
		_, err := domain.NuevaObservacion(uid, "a@b.com", "Ana", domain.SinCamioneta())
		require.ErrorIs(t, err, domain.ErrUsuarioUIDRequerido, "uid %q", uid)
	}
}

// TestNuevaObservacion_Normaliza pins the normalisation contract. It matters
// more than it looks: if a trailing space or a capitalised address survived,
// two photographs of an unchanged roster would differ and the module would
// report churn that never happened.
func TestNuevaObservacion_Normaliza(t *testing.T) {
	t.Parallel()

	o, err := domain.NuevaObservacion(
		"  uid-1  ", "  Eliseo.Ramirez@MSP.COM ", "  Eliseo Ramírez  ", domain.SinCamioneta(),
	)
	require.NoError(t, err)

	assert.Equal(t, "uid-1", o.UsuarioUID())
	assert.Equal(t, "eliseo.ramirez@msp.com", o.Email(), "el correo es un identificador: va en minúsculas")
	assert.Equal(t, "Eliseo Ramírez", o.Nombre(),
		"el nombre conserva sus mayúsculas: es cómo el padrón muestra a la persona")
}

// TestNuevaObservacion_CamposVaciosSonLegitimos verifies that a roster entry
// with no email and no name is accepted. Only the uid is structural.
func TestNuevaObservacion_CamposVaciosSonLegitimos(t *testing.T) {
	t.Parallel()

	o, err := domain.NuevaObservacion("uid-1", "", "", domain.SinCamioneta())
	require.NoError(t, err)
	assert.Empty(t, o.Email())
	assert.Empty(t, o.Nombre())
	assert.False(t, o.Camioneta().Asignada())
}
