package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

func TestConsultas_Bandeja_SinUsuarioNoTocaRepos(t *testing.T) {
	t.Parallel()

	identity := &fakeConsultaIdentity{
		usuarioErr: domain.ErrUsuarioNoAutenticado,
	}
	bandeja := &fakeConsultaBandejaRepo{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Bandeja:  bandeja,
		Identity: identity,
	})

	_, err := consultas.Bandeja(context.Background(), app.BandejaQuery{})

	require.ErrorIs(t, err, domain.ErrUsuarioNoAutenticado)
	require.Equal(t, 1, identity.usuarioCalls)
	require.Zero(t, identity.permisoCalls)
	require.Zero(t, bandeja.calls)
}

func TestConsultas_Bandeja_SinPermisoNoTocaRepos(t *testing.T) {
	t.Parallel()

	identity := &fakeConsultaIdentity{
		usuario:   outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permitido: false,
	}
	bandeja := &fakeConsultaBandejaRepo{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Bandeja:  bandeja,
		Identity: identity,
	})

	_, err := consultas.Bandeja(context.Background(), app.BandejaQuery{})

	require.ErrorIs(t, err, domain.ErrPermisoDenegado)
	require.Equal(t, 1, identity.usuarioCalls)
	require.Equal(t, 1, identity.permisoCalls)
	require.Equal(t, domain.PermisoLeer, identity.ultimoPermiso)
	require.Zero(t, bandeja.calls)
}

func TestConsultas_Bandeja_ErrorDePermisoSePropaga(t *testing.T) {
	t.Parallel()

	errPermiso := errors.New("fallo identidad")
	identity := &fakeConsultaIdentity{
		usuario:    outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permisoErr: errPermiso,
	}
	bandeja := &fakeConsultaBandejaRepo{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Bandeja:  bandeja,
		Identity: identity,
	})

	_, err := consultas.Bandeja(context.Background(), app.BandejaQuery{})

	require.ErrorIs(t, err, errPermiso)
	require.Zero(t, bandeja.calls)
}

func TestConsultas_Bandeja_CaminoFeliz(t *testing.T) {
	t.Parallel()

	garantia := &domain.Garantia{}
	identity := &fakeConsultaIdentity{
		usuario:   outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permitido: true,
	}
	bandeja := &fakeConsultaBandejaRepo{
		pagina: outbound.Pagina[*domain.Garantia]{
			Items:           []*domain.Garantia{garantia},
			SiguienteCursor: "cursor-siguiente",
		},
	}

	var estado domain.EstadoFolio
	var origen domain.OrigenFolio
	var etapa domain.Etapa
	var ubicacion domain.Ubicacion

	clienteID := 25
	desde := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	hasta := time.Date(2026, time.October, 14, 0, 0, 0, 0, time.UTC)

	query := app.BandejaQuery{
		Estado:    &estado,
		Origen:    &origen,
		ClienteID: &clienteID,
		Etapa:     &etapa,
		Ubicacion: &ubicacion,
		Desde:     &desde,
		Hasta:     &hasta,
		Cursor:    "cursor-actual",
		Limite:    20,
	}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Bandeja:  bandeja,
		Identity: identity,
	})

	pagina, err := consultas.Bandeja(context.Background(), query)

	require.NoError(t, err)
	require.Equal(t, []*domain.Garantia{garantia}, pagina.Items)
	require.Equal(t, "cursor-siguiente", pagina.SiguienteCursor)
	require.Equal(t, 1, bandeja.calls)

	require.Equal(t, query.Estado, bandeja.filtros.Estado)
	require.Equal(t, query.Origen, bandeja.filtros.Origen)
	require.Equal(t, query.ClienteID, bandeja.filtros.ClienteID)
	require.Equal(t, query.Etapa, bandeja.filtros.Etapa)
	require.Equal(t, query.Ubicacion, bandeja.filtros.Ubicacion)
	require.Equal(t, query.Desde, bandeja.filtros.Desde)
	require.Equal(t, query.Hasta, bandeja.filtros.Hasta)
	require.Equal(t, query.Cursor, bandeja.paginacion.Cursor)
	require.Equal(t, query.Limite, bandeja.paginacion.Limite)
	require.Equal(t, domain.PermisoLeer, identity.ultimoPermiso)
}

func TestConsultas_Bandeja_ErrorRepositorioSePropaga(t *testing.T) {
	t.Parallel()

	errRepo := errors.New("fallo bandeja")
	identity := &fakeConsultaIdentity{
		usuario:   outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permitido: true,
	}
	bandeja := &fakeConsultaBandejaRepo{
		err: errRepo,
	}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Bandeja:  bandeja,
		Identity: identity,
	})

	_, err := consultas.Bandeja(context.Background(), app.BandejaQuery{})

	require.ErrorIs(t, err, errRepo)
	require.Equal(t, 1, bandeja.calls)
}

func TestConsultas_Detalle_SinUsuarioNoTocaRepos(t *testing.T) {
	t.Parallel()

	identity := &fakeConsultaIdentity{
		usuarioErr: domain.ErrUsuarioNoAutenticado,
	}
	garantias := &fakeConsultaGarantiaRepo{}
	eventos := &fakeConsultaEventoRepo{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Eventos:   eventos,
		Identity:  identity,
	})

	_, err := consultas.Detalle(context.Background(), uuid.New())

	require.ErrorIs(t, err, domain.ErrUsuarioNoAutenticado)
	require.Zero(t, identity.permisoCalls)
	require.Zero(t, garantias.detalleCalls)
	require.Zero(t, eventos.calls)
}

func TestConsultas_Detalle_SinPermisoNoTocaRepos(t *testing.T) {
	t.Parallel()

	identity := &fakeConsultaIdentity{
		usuario:   outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permitido: false,
	}
	garantias := &fakeConsultaGarantiaRepo{}
	eventos := &fakeConsultaEventoRepo{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Eventos:   eventos,
		Identity:  identity,
	})

	_, err := consultas.Detalle(context.Background(), uuid.New())

	require.ErrorIs(t, err, domain.ErrPermisoDenegado)
	require.Zero(t, garantias.detalleCalls)
	require.Zero(t, eventos.calls)
}

func TestConsultas_Detalle_CaminoFeliz(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	garantia := &domain.Garantia{}
	eventoUno := &domain.Evento{}
	eventoDos := &domain.Evento{}

	identity := &fakeConsultaIdentity{
		usuario:   outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permitido: true,
	}
	garantias := &fakeConsultaGarantiaRepo{
		garantia: garantia,
		eventos:  []*domain.Evento{eventoUno, eventoDos},
	}
	eventos := &fakeConsultaEventoRepo{
		eventos: []*domain.Evento{eventoUno, eventoDos},
	}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Eventos:   eventos,
		Identity:  identity,
	})

	detalle, err := consultas.Detalle(context.Background(), id)

	require.NoError(t, err)
	require.Same(t, garantia, detalle.Garantia)
	require.Equal(t, []*domain.Evento{eventoUno, eventoDos}, detalle.Eventos)
	require.Equal(t, 1, garantias.detalleCalls)
	require.Equal(t, id, garantias.ultimoID)
	require.Zero(t, eventos.calls)
	require.Equal(t, domain.PermisoLeer, identity.ultimoPermiso)
}

func TestConsultas_Detalle_GarantiaNoEncontrada(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	identity := &fakeConsultaIdentity{
		usuario:   outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permitido: true,
	}
	garantias := &fakeConsultaGarantiaRepo{
		detalleErr: domain.ErrGarantiaNoEncontrada,
	}
	eventos := &fakeConsultaEventoRepo{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Eventos:   eventos,
		Identity:  identity,
	})

	_, err := consultas.Detalle(context.Background(), id)

	require.ErrorIs(t, err, domain.ErrGarantiaNoEncontrada)
	require.Equal(t, 1, garantias.detalleCalls)
	require.Zero(t, eventos.calls)
}

func TestConsultas_Detalle_ErrorLecturaSePropaga(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	errEventos := errors.New("fallo eventos")

	identity := &fakeConsultaIdentity{
		usuario:   outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permitido: true,
	}
	garantias := &fakeConsultaGarantiaRepo{
		garantia:   &domain.Garantia{},
		detalleErr: errEventos,
	}
	eventos := &fakeConsultaEventoRepo{
		err: errEventos,
	}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Eventos:   eventos,
		Identity:  identity,
	})

	_, err := consultas.Detalle(context.Background(), id)

	require.ErrorIs(t, err, errEventos)
	require.Equal(t, 1, garantias.detalleCalls)
	require.Zero(t, eventos.calls)
}

func TestConsultas_ValidarAutenticacion(t *testing.T) {
	t.Parallel()

	t.Run("sin_usuario", func(t *testing.T) {
		t.Parallel()

		identity := &fakeConsultaIdentity{
			usuarioErr: domain.ErrUsuarioNoAutenticado,
		}

		consultas := app.NewConsultas(app.ConsultasDeps{
			Identity: identity,
		})

		err := consultas.ValidarAutenticacion(context.Background())

		require.ErrorIs(t, err, domain.ErrUsuarioNoAutenticado)
		require.Equal(t, 1, identity.usuarioCalls)
		require.Zero(t, identity.permisoCalls)
	})

	t.Run("con_usuario", func(t *testing.T) {
		t.Parallel()

		identity := &fakeConsultaIdentity{
			usuario: outbound.Usuario{ID: "1", Nombre: "Ruben"},
		}

		consultas := app.NewConsultas(app.ConsultasDeps{
			Identity: identity,
		})

		err := consultas.ValidarAutenticacion(context.Background())

		require.NoError(t, err)
		require.Equal(t, 1, identity.usuarioCalls)
		require.Zero(t, identity.permisoCalls)
	})
}
