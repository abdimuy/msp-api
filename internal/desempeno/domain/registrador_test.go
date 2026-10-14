package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestIndiceUsuarios_Completar(t *testing.T) {
	t.Parallel()
	juan := domain.UsuarioCobranza{UID: "uid-juan", Nombre: "Juan Carlos Méndez", CobradorID: 11502}
	rosa := domain.UsuarioCobranza{UID: "uid-rosa", Nombre: "Rosa Elena Pérez", CobradorID: 11600}
	lupe := domain.UsuarioCobranza{UID: "uid-lupe", Nombre: "Guadalupe Torres", CobradorID: 11600}
	ix := domain.NuevoIndiceUsuarios([]domain.UsuarioCobranza{juan, rosa, lupe})

	con := func(rp domain.RegistradoPor) domain.RegistradoPor {
		return ix.Completar(domain.Actividad{RegistradoPor: rp}).RegistradoPor
	}

	assert.Equal(t, domain.RegistradoPor{CobradorID: 11502, Nombre: "Oficina", Origen: domain.OrigenOficina},
		con(domain.RegistradoPor{UsuarioUID: "x", CobradorID: 11502, Nombre: "JUAN", Origen: domain.OrigenOficina}))

	assert.Equal(t, domain.RegistradoPor{UsuarioUID: "uid-juan", CobradorID: 11502, Nombre: "Juan Carlos Méndez", Origen: domain.OrigenApp},
		con(domain.RegistradoPor{UsuarioUID: "uid-juan", Nombre: "JUAN CARLOS", Origen: domain.OrigenApp}),
		"uid resuelto por CREATED_BY toma nombre y cobrador de Firestore")

	assert.Equal(t, domain.RegistradoPor{UsuarioUID: "uid-juan", CobradorID: 11502, Nombre: "Juan Carlos Méndez", Origen: domain.OrigenApp},
		con(domain.RegistradoPor{CobradorID: 11502, Nombre: "JUAN CARLOS", Origen: domain.OrigenApp}),
		"sin uid pero COBRADOR_ID de un solo usuario")

	assert.Equal(t, domain.RegistradoPor{CobradorID: 11600, Nombre: "ROSA", Origen: domain.OrigenApp},
		con(domain.RegistradoPor{CobradorID: 11600, Nombre: "ROSA", Origen: domain.OrigenApp}),
		"COBRADOR_ID compartido: no se adivina")

	assert.Equal(t, domain.RegistradoPor{UsuarioUID: "uid-baja", Nombre: "PEDRO", Origen: domain.OrigenApp},
		con(domain.RegistradoPor{UsuarioUID: "uid-baja", Nombre: "PEDRO", Origen: domain.OrigenApp}),
		"uid que ya no está en Firestore conserva el nombre crudo")
}

func TestRegistradaPor(t *testing.T) {
	t.Parallel()
	rosa := domain.UsuarioCobranza{UID: "uid-rosa", CobradorID: 11600}
	lupe := domain.UsuarioCobranza{UID: "uid-lupe", CobradorID: 11600}
	porUID := domain.Actividad{RegistradoPor: domain.RegistradoPor{UsuarioUID: "uid-rosa", CobradorID: 11600, Origen: domain.OrigenApp}}
	compartida := domain.Actividad{RegistradoPor: domain.RegistradoPor{CobradorID: 11600, Origen: domain.OrigenApp}}
	oficina := domain.Actividad{RegistradoPor: domain.RegistradoPor{CobradorID: 11600, Origen: domain.OrigenOficina}}

	assert.True(t, domain.RegistradaPor(porUID, rosa))
	assert.False(t, domain.RegistradaPor(porUID, lupe), "con uid manda el uid aunque compartan cobrador")
	assert.True(t, domain.RegistradaPor(compartida, rosa))
	assert.True(t, domain.RegistradaPor(compartida, lupe))
	assert.False(t, domain.RegistradaPor(oficina, rosa), "lo de oficina no es de nadie")
	assert.False(t, domain.RegistradaPor(compartida, domain.UsuarioCobranza{UID: "x"}), "cobrador 0 no empata")
}
