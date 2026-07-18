package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/config/domain"
)

func TestNewVendedorAsignacion_EstadoPorSlotsLlenos(t *testing.T) {
	t.Parallel()

	slot := func(id int) *domain.VendedorSlot {
		return &domain.VendedorSlot{ListaID: id, Nombre: "Juan Pérez"}
	}

	cases := []struct {
		name       string
		v1, v2, v3 *domain.VendedorSlot
		want       string
	}{
		{"sin asignar", nil, nil, nil, "sin asignar"},
		{"1/3 primer slot", slot(1), nil, nil, "1/3"},
		{"1/3 tercer slot", nil, nil, slot(3), "1/3"},
		{"2/3", slot(1), slot(2), nil, "2/3"},
		{"3/3", slot(1), slot(2), slot(3), "3/3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			usuarioID := uuid.New()
			a := domain.NewVendedorAsignacion(usuarioID, "Rosa Martínez", "rosa.martinez@muebleriamsp.mx", tc.v1, tc.v2, tc.v3)

			assert.Equal(t, tc.want, a.Estado)
			assert.Equal(t, usuarioID, a.UsuarioID)
			assert.Equal(t, "Rosa Martínez", a.Nombre)
			assert.Equal(t, tc.v1, a.V1)
			assert.Equal(t, tc.v2, a.V2)
			assert.Equal(t, tc.v3, a.V3)
		})
	}
}
