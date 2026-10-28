//nolint:paralleltest // Firebird integration tests must run serially.
package garfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Las tres pruebas de punta a punta del §8. Ejercitan el servicio de aplicación
// real contra Firebird real — sin dobles — dentro de WithTestTransaction, que
// revierte todo al terminar. El TxRunner envuelve firebird.RunInTx, que es
// re-entrante: al detectar la transacción del test ya inyectada en el contexto
// ejecuta dentro de ella en vez de abrir una segunda (Firebird no soporta
// transacciones anidadas).

// identidadPrueba es una identidad de permisos totales, para las tres
// historias: ninguna de ellas es sobre control de acceso.
type identidadPrueba struct {
	usuario string
}

func (i identidadPrueba) UsuarioActual(context.Context) (outbound.Usuario, error) {
	return outbound.Usuario{ID: i.usuario, Nombre: i.usuario}, nil
}

func (identidadPrueba) TienePermiso(context.Context, domain.Permiso) (bool, error) {
	return true, nil
}

// txRunnerPrueba delega en la transacción re-entrante de firebird.
type txRunnerPrueba struct {
	db *firebird.Pool
}

func (t txRunnerPrueba) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return firebird.RunInTx(ctx, t.db.DB, fn)
}

// relojPrueba avanza un minuto por llamada desde un origen fijo, para que la
// línea de tiempo quede ordenada y determinista.
type relojPrueba struct {
	ahora time.Time
}

func (r *relojPrueba) Now() time.Time {
	r.ahora = r.ahora.Add(time.Minute)
	return r.ahora
}

// lineaDeTiempo lee los eventos del folio desde la base, en el orden que el
// repositorio garantiza.
func lineaDeTiempo(
	ctx context.Context,
	t *testing.T,
	eventos *garfb.EventoRepo,
	id uuid.UUID,
) []*domain.Evento {
	t.Helper()

	evs, err := eventos.ListarPorGarantia(ctx, id)
	require.NoError(t, err)

	return evs
}

// llevaEtapas reports whether an event type records a stage transition and so
// must carry its desde/hasta pair. It is the inverse of the folio-level, manual
// and attachment facts: those never move an article's stage.
func llevaEtapas(tipo domain.TipoEvento) bool {
	//nolint:exhaustive // the listed types are exactly the stage-bearing ones; the rest are the explicit negative.
	switch tipo {
	case domain.TipoEventoEtapaAvanzada,
		domain.TipoEventoDiagnosticoRegistrado,
		domain.TipoEventoDictamenRegistrado,
		domain.TipoEventoCambioAutorizado,
		domain.TipoEventoDesenlaceRegistrado,
		domain.TipoEventoDevolucionFallida:
		return true
	default:
		return false
	}
}

// eventoEsperado es una fila de la línea de tiempo esperada. Las cadenas vacías
// significan "nulo en la base": un evento de folio no lleva artículo ni etapa,
// y un avance que no cambia de ubicación no guarda el par de ubicaciones.
// articulo nombra el artículo ("original", "reemplazo"); cada historia da el
// UUID de cada nombre.
type eventoEsperado struct {
	tipo       domain.TipoEvento
	articulo   string
	etapaDesde domain.Etapa
	etapaHasta domain.Etapa
	ubicDesde  domain.Ubicacion
	ubicHasta  domain.Ubicacion
}

// valor devuelve "" para un puntero nulo y el valor en otro caso, para
// comparar contra eventoEsperado.
func valor[T ~string](p *T) T {
	if p == nil {
		return ""
	}
	return *p
}

// verificarValores compara, evento por evento y en orden, el tipo, el
// artículo y los valores exactos de etapa y ubicación desde/hasta. Que sólo
// sean no nulos no basta: un ETAPA_DESDE = entregado en la entrega pasa una
// revisión de nulos y cuenta una historia falsa.
func verificarValores(
	t *testing.T,
	evs []*domain.Evento,
	articulos map[string]uuid.UUID,
	esperados []eventoEsperado,
) {
	t.Helper()

	nombres := make(map[uuid.UUID]string, len(articulos))
	for nombre, id := range articulos {
		nombres[id] = nombre
	}

	got := make([]eventoEsperado, 0, len(evs))
	for _, e := range evs {
		fila := eventoEsperado{
			tipo:       e.Tipo(),
			etapaDesde: valor(e.EtapaDesde()),
			etapaHasta: valor(e.EtapaHasta()),
			ubicDesde:  valor(e.UbicacionDesde()),
			ubicHasta:  valor(e.UbicacionHasta()),
		}
		if ref := e.ArticuloRef(); ref != nil {
			nombre, ok := nombres[*ref]
			require.True(t, ok, "evento %s con artículo sin nombre: %s", e.Tipo(), *ref)
			fila.articulo = nombre
		}
		got = append(got, fila)
	}

	require.Equal(t, esperados, got, "línea de tiempo: valores exactos por evento, en orden")
}

// verificarLineaDeTiempo comprueba las invariantes que el §8 pide en las tres
// historias de punta a punta, sobre la línea de tiempo leída de la base:
//
//   - está completa, en orden, y arranca con la apertura del folio;
//   - ningún evento queda sin su clave de idempotencia;
//   - cada evento que cambia de etapa lleva su par desde/hasta, y ninguno de
//     los demás lo lleva (un folio_abierto con hasta sería una etapa inventada);
//   - al menos un evento guardó el par de ubicaciones desde/hasta (R3/C5);
//   - cada evento, en orden, tiene exactamente el tipo, el artículo y los
//     valores de etapa y ubicación desde/hasta que la historia espera;
//   - cero artículos sin su evento: cada artículo del folio aparece al menos
//     una vez como ArticuloRef, y ningún ArticuloRef apunta a un artículo que
//     no sea del folio.
//
// Lo que sí cambia entre historias — qué evento cierra la línea de tiempo y
// qué desenlaces quedaron — lo afirma cada historia por su cuenta.
func verificarLineaDeTiempo(
	ctx context.Context,
	t *testing.T,
	eventos *garfb.EventoRepo,
	final *domain.Garantia,
	articulos map[string]uuid.UUID,
	esperados []eventoEsperado,
) []*domain.Evento {
	t.Helper()

	evs := lineaDeTiempo(ctx, t, eventos, final.ID())
	require.NotEmpty(t, evs)
	verificarValores(t, evs, articulos, esperados)

	for _, e := range evs {
		require.NotEmpty(t, e.ClaveIdempotencia(), "evento sin clave: %s", e.Tipo())
	}

	require.Equal(t, domain.TipoEventoFolioAbierto, evs[0].Tipo())

	// El par desde/hasta es obligatorio en los eventos que mueven etapa y
	// prohibido en los que no: las dos mitades del contrato.
	for _, e := range evs {
		if llevaEtapas(e.Tipo()) {
			require.NotNil(t, e.EtapaDesde(), "%s sin desde", e.Tipo())
			require.NotNil(t, e.EtapaHasta(), "%s sin hasta", e.Tipo())
			continue
		}
		require.Nil(t, e.EtapaDesde(), "%s no debe llevar desde", e.Tipo())
		require.Nil(t, e.EtapaHasta(), "%s no debe llevar hasta", e.Tipo())
	}

	// R3: la ubicación viaja con el mueble. Al menos un evento tiene que haber
	// dejado su par desde/hasta (C5: si el INSERT omite UBICACION_HASTA, este
	// evento llega con el hasta en nil).
	var conUbicacionHasta *domain.Evento
	for _, e := range evs {
		if e.UbicacionHasta() != nil {
			conUbicacionHasta = e
		}
	}
	require.NotNil(t, conUbicacionHasta,
		"al menos un evento debe haber guardado su ubicación hasta")

	// Cero artículos sin su evento: cada artículo del folio tiene al menos un
	// evento que lo referencia, y ningún evento referencia un artículo ajeno.
	delFolioArts := final.ArticulosForRepo()
	delFolio := make(map[uuid.UUID]bool, len(delFolioArts))
	for _, a := range delFolioArts {
		delFolio[a.ID()] = false
	}

	for _, e := range evs {
		ref := e.ArticuloRef()
		if ref == nil {
			continue
		}
		_, conocido := delFolio[*ref]
		require.True(t, conocido,
			"evento %s referencia un artículo ajeno al folio: %s", e.Tipo(), *ref)
		delFolio[*ref] = true
	}

	for _, a := range delFolioArts {
		require.True(t, delFolio[a.ID()],
			"el artículo %s quedó sin ningún evento en la línea de tiempo", a.ID())
	}

	return evs
}

// abrirPiso abre un folio de piso con un artículo, la mitad común de las
// historias 1 y — casi — la 3. Devuelve el folio ya guardado.
func abrirPiso(
	ctx context.Context,
	t *testing.T,
	svc *app.Service,
	clk *relojPrueba,
) *domain.Garantia {
	t.Helper()

	g, err := svc.AbrirGarantia(ctx, app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "Sillón de piso con costura abierta",
		ClaveIdempotencia: uuid.NewString(),
		DeviceCreatedAt:   clk.Now(),
	})
	require.NoError(t, err)

	articuloMicrosipID := 981003
	g, err = svc.AgregarArticulo(ctx, app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		ArticuloID:        &articuloMicrosipID,
		Clave:             "PISO-001",
		Description:       "Sillón de exhibición",
		ClaveIdempotencia: uuid.NewString(),
		DeviceCreatedAt:   clk.Now(),
	})
	require.NoError(t, err)

	return g
}

// TestE2E_PisoDeLaRecepcionAlCierre recorre la historia 1 del §8: apertura,
// revisión, taller, reparado, reingresado_inventario, desenlace reparado y
// cierre.
func TestE2E_PisoDeLaRecepcionAlCierre(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	grepo := garfb.NewGarantiaRepo(pool)
	erepo := garfb.NewEventoRepo(pool)
	fgen := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		clk := &relojPrueba{ahora: time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)}
		svc := app.NewService(app.Deps{
			GarantiaRepo:   grepo,
			EventoRepo:     erepo,
			FolioGenerator: fgen,
			TxRunner:       txRunnerPrueba{db: pool},
			Identity:       identidadPrueba{usuario: "ruben"},
			Clock:          clk,
		})

		g := abrirPiso(ctx, t, svc, clk)
		articulo := g.ArticulosForRepo()[0]
		id := g.ID()

		clave := func() string { return uuid.NewString() }

		// De abierto a en_proceso: el folio no admite nada más antes.
		_, err := svc.IniciarProceso(ctx, app.IniciarProcesoCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		// Piso nunca recolecta: registrado -> en_revision directo.
		_, err = svc.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			EtapaDestino:      domain.EtapaEnRevision,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		// Diagnóstico: ruta taller y avance a en_taller en la misma llamada.
		_, err = svc.RegistrarDiagnostico(ctx, app.RegistrarDiagnosticoCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			Ruta:              string(domain.RutaReparacionTaller),
			RolDecisor:        string(domain.RolDecisorTecnica),
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		// Taller -> reparado_taller -> listo_entrega: el folio de piso pasa por
		// el taller antes de volver al inventario. El diagnóstico ya deja el
		// artículo en en_taller.
		for _, etapa := range []domain.Etapa{
			domain.EtapaReparadoTaller,
			domain.EtapaListoEntrega,
		} {
			_, err = svc.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
				GarantiaID:        id,
				ArticuloID:        articulo.ID(),
				EtapaDestino:      etapa,
				ClaveIdempotencia: clave(),
				DeviceCreatedAt:   clk.Now(),
			})
			require.NoError(t, err, "avanzar a %s", etapa)
		}

		// Piso: listo_entrega -> reingresado_inventario, sin Entregar.
		_, err = svc.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			EtapaDestino:      domain.EtapaReingresadoInventario,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		_, err = svc.MarcarListoEntrega(ctx, app.MarcarListoEntregaCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		// Cerrado solo se alcanza pasando por entregado: el folio de piso se
		// entrega en mostrador (con GPS) y hasta entonces cierra. La entrega no
		// mueve al artículo —entregado es etapa de cliente (R1)—, así que el
		// mueble sigue reingresado_inventario en el almacén.
		lat, lon := 19.4326, -99.1332
		_, err = svc.Entregar(ctx, app.EntregarCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
			GPSLat:            &lat,
			GPSLon:            &lon,
		})
		require.NoError(t, err)

		_, err = svc.Cerrar(ctx, app.CerrarCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		cargada, err := grepo.Obtener(ctx, id)
		require.NoError(t, err)
		require.Equal(t, domain.EstadoFolioCerrado, cargada.Estado())
		a := cargada.ArticulosForRepo()[0]
		require.Equal(t, domain.EtapaReingresadoInventario, a.Etapa())
		require.Equal(t, domain.UbicacionAlmacenRevision, a.Ubicacion())
		require.NotNil(t, a.Desenlace())
		require.Equal(t, domain.DesenlaceReparado, *a.Desenlace())

		// Comprobación final del §8: la línea de tiempo leída de la base cuenta
		// la historia completa, en orden, con desde/hasta de etapa y ubicación,
		// y sin artículos huérfanos de evento.
		const o = "original"
		verificarLineaDeTiempo(ctx, t, erepo, cargada,
			map[string]uuid.UUID{o: articulo.ID()},
			[]eventoEsperado{
				{tipo: domain.TipoEventoFolioAbierto},
				{tipo: domain.TipoEventoArticuloAgregado, articulo: o},
				{tipo: domain.TipoEventoFolioEnProceso},
				// Piso nace en almacen_revision: entrar a revisión no lo mueve.
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaRegistrado, etapaHasta: domain.EtapaEnRevision,
				},
				{
					tipo: domain.TipoEventoDiagnosticoRegistrado, articulo: o,
					etapaDesde: domain.EtapaEnRevision, etapaHasta: domain.EtapaEnTaller,
					ubicDesde: domain.UbicacionAlmacenRevision, ubicHasta: domain.UbicacionTaller,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaEnTaller, etapaHasta: domain.EtapaReparadoTaller,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaReparadoTaller, etapaHasta: domain.EtapaListoEntrega,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaListoEntrega, etapaHasta: domain.EtapaReingresadoInventario,
					ubicDesde: domain.UbicacionTaller, ubicHasta: domain.UbicacionAlmacenRevision,
				},
				{tipo: domain.TipoEventoFolioListoEntrega},
				// La entrega de piso no mueve artículos: sólo el hecho del folio.
				{tipo: domain.TipoEventoFolioEntregado},
				{tipo: domain.TipoEventoFolioCerrado},
			})
	})
}

// TestE2E_ClientePorProveedor recorre la historia 2 del §8: recolección,
// diagnóstico proveedor, envío (ubicación proveedor), dictamen aceptada, regreso
// manual con MoverArticulo, listo_entrega, Entregar con GPS, desenlace reparado
// y cierre.
func TestE2E_ClientePorProveedor(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	grepo := garfb.NewGarantiaRepo(pool)
	erepo := garfb.NewEventoRepo(pool)
	fgen := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		clk := &relojPrueba{ahora: time.Date(2026, 10, 20, 16, 0, 0, 0, time.UTC)}
		svc := app.NewService(app.Deps{
			GarantiaRepo:   grepo,
			EventoRepo:     erepo,
			FolioGenerator: fgen,
			TxRunner:       txRunnerPrueba{db: pool},
			Identity:       identidadPrueba{usuario: "ruben"},
			Clock:          clk,
		})

		clave := func() string { return uuid.NewString() }

		clienteID := 982001
		ventaID := 982002

		g, err := svc.AbrirGarantia(ctx, app.AbrirGarantiaCmd{
			Origen:            domain.OrigenFolioCliente,
			ClienteID:         &clienteID,
			VentaID:           &ventaID,
			EstadoCuenta:      ptrEstadoCuenta(domain.EstadoCuentaLiquidada),
			Description:       "Sala con resortes vencidos",
			Calle:             "Avenida Juárez",
			NumeroExterior:    "125",
			Colonia:           "Centro",
			Localidad:         "Puebla",
			Ciudad:            "Puebla",
			CodigoPostal:      "72000",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		articuloMicrosipID := 982003
		g, err = svc.AgregarArticulo(ctx, app.AgregarArticuloCmd{
			GarantiaID:        g.ID(),
			ArticuloID:        &articuloMicrosipID,
			Clave:             "CLI-001",
			Description:       "Sala de tres piezas",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		id := g.ID()
		articulo := g.ArticulosForRepo()[0]

		_, err = svc.IniciarProceso(ctx, app.IniciarProcesoCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		// Cliente: la recolección es obligatoria.
		for _, etapa := range []domain.Etapa{
			domain.EtapaPendienteRecoleccion,
			domain.EtapaRecolectado,
			domain.EtapaEnRevision,
		} {
			_, err = svc.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
				GarantiaID:        id,
				ArticuloID:        articulo.ID(),
				EtapaDestino:      etapa,
				ClaveIdempotencia: clave(),
				DeviceCreatedAt:   clk.Now(),
			})
			require.NoError(t, err, "avanzar a %s", etapa)
		}

		// Diagnóstico por proveedor: en_revision -> orden_generada. El envío
		// avanza la etapa y la ubicación por R3.
		_, err = svc.RegistrarDiagnostico(ctx, app.RegistrarDiagnosticoCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			Ruta:              string(domain.RutaReparacionProveedor),
			RolDecisor:        string(domain.RolDecisorTecnica),
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		_, err = svc.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			EtapaDestino:      domain.EtapaEnviadoProveedor,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		cargada, err := grepo.Obtener(ctx, id)
		require.NoError(t, err)
		require.Equal(t, domain.UbicacionProveedor, cargada.ArticulosForRepo()[0].Ubicacion(),
			"enviado_proveedor mueve la ubicación sola")

		_, err = svc.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			EtapaDestino:      domain.EtapaDictamenRecibido,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		// El dictamen lo captura el proveedor: dictamen_recibido ->
		// reparado_proveedor (aceptada).
		_, err = svc.RegistrarDictamen(ctx, app.RegistrarDictamenCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			Dictamen:          string(domain.DictamenAceptada),
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		// El mueble sigue con el proveedor: el regreso es manual (R3).
		_, err = svc.MoverArticulo(ctx, app.MoverArticuloCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			Ubicacion:         string(domain.UbicacionAlmacenRevision),
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		_, err = svc.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
			GarantiaID:        id,
			ArticuloID:        articulo.ID(),
			EtapaDestino:      domain.EtapaListoEntrega,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		_, err = svc.MarcarListoEntrega(ctx, app.MarcarListoEntregaCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		lat, lon := 19.0414, -98.2063
		_, err = svc.Entregar(ctx, app.EntregarCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
			GPSLat:            &lat,
			GPSLon:            &lon,
		})
		require.NoError(t, err)

		_, err = svc.Cerrar(ctx, app.CerrarCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		final, err := grepo.Obtener(ctx, id)
		require.NoError(t, err)
		require.Equal(t, domain.EstadoFolioCerrado, final.Estado())

		fin := final.ArticulosForRepo()[0]
		require.Equal(t, domain.EtapaEntregado, fin.Etapa())
		require.Equal(t, domain.UbicacionEntregado, fin.Ubicacion())
		require.NotNil(t, fin.Desenlace())
		require.Equal(t, domain.DesenlaceReparado, *fin.Desenlace())

		const o = "original"
		evs := verificarLineaDeTiempo(ctx, t, erepo, final,
			map[string]uuid.UUID{o: articulo.ID()},
			[]eventoEsperado{
				{tipo: domain.TipoEventoFolioAbierto},
				{tipo: domain.TipoEventoArticuloAgregado, articulo: o},
				{tipo: domain.TipoEventoFolioEnProceso},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaRegistrado, etapaHasta: domain.EtapaPendienteRecoleccion,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaPendienteRecoleccion, etapaHasta: domain.EtapaRecolectado,
					ubicDesde: domain.UbicacionDomicilioCliente, ubicHasta: domain.UbicacionEnTransito,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaRecolectado, etapaHasta: domain.EtapaEnRevision,
					ubicDesde: domain.UbicacionEnTransito, ubicHasta: domain.UbicacionAlmacenRevision,
				},
				{
					tipo: domain.TipoEventoDiagnosticoRegistrado, articulo: o,
					etapaDesde: domain.EtapaEnRevision, etapaHasta: domain.EtapaOrdenGenerada,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaOrdenGenerada, etapaHasta: domain.EtapaEnviadoProveedor,
					ubicDesde: domain.UbicacionAlmacenRevision, ubicHasta: domain.UbicacionProveedor,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaEnviadoProveedor, etapaHasta: domain.EtapaDictamenRecibido,
				},
				{
					tipo: domain.TipoEventoDictamenRegistrado, articulo: o,
					etapaDesde: domain.EtapaDictamenRecibido, etapaHasta: domain.EtapaReparadoProveedor,
				},
				{
					tipo: domain.TipoEventoArticuloMovido, articulo: o,
					ubicDesde: domain.UbicacionProveedor, ubicHasta: domain.UbicacionAlmacenRevision,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaReparadoProveedor, etapaHasta: domain.EtapaListoEntrega,
				},
				{tipo: domain.TipoEventoFolioListoEntrega},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaListoEntrega, etapaHasta: domain.EtapaEntregado,
					ubicDesde: domain.UbicacionAlmacenRevision, ubicHasta: domain.UbicacionEntregado,
				},
				{tipo: domain.TipoEventoFolioEntregado},
				{tipo: domain.TipoEventoFolioCerrado},
			})
		require.Equal(t, domain.TipoEventoFolioCerrado, evs[len(evs)-1].Tipo())

		// El evento del folio entregado guarda el GPS de la entrega.
		var entregado *domain.Evento
		for _, e := range evs {
			if e.Tipo() == domain.TipoEventoFolioEntregado {
				entregado = e
			}
		}
		require.NotNil(t, entregado)
		require.NotNil(t, entregado.GPSLat())
		require.InDelta(t, lat, *entregado.GPSLat(), 1e-9)

		// R3: el MoverArticulo manual deja su propio evento con el par de
		// ubicaciones desde/hasta, leído de la base.
		var movido *domain.Evento
		for _, e := range evs {
			if e.Tipo() == domain.TipoEventoArticuloMovido {
				movido = e
			}
		}
		require.NotNil(t, movido, "el movimiento manual debe dejar su evento")
		require.NotNil(t, movido.UbicacionDesde())
		require.Equal(t, domain.UbicacionProveedor, *movido.UbicacionDesde())
		require.NotNil(t, movido.UbicacionHasta())
		require.Equal(t, domain.UbicacionAlmacenRevision, *movido.UbicacionHasta())
	})
}

// TestE2E_ClientePorTallerConCambioFisico recorre la historia 3 del §8: taller,
// AutorizarCambioFisico, se entrega el reemplazo (desenlace reemplazado), se
// cierra el folio con el original en standby y, después de cerrado,
// RegistrarDesenlace -> segunda_mano sobre el original.
func TestE2E_ClientePorTallerConCambioFisico(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	grepo := garfb.NewGarantiaRepo(pool)
	erepo := garfb.NewEventoRepo(pool)
	fgen := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		clk := &relojPrueba{ahora: time.Date(2026, 10, 20, 17, 0, 0, 0, time.UTC)}
		svc := app.NewService(app.Deps{
			GarantiaRepo:   grepo,
			EventoRepo:     erepo,
			FolioGenerator: fgen,
			TxRunner:       txRunnerPrueba{db: pool},
			Identity:       identidadPrueba{usuario: "ruben"},
			Clock:          clk,
		})

		clave := func() string { return uuid.NewString() }

		clienteID := 983001
		ventaID := 983002

		g, err := svc.AbrirGarantia(ctx, app.AbrirGarantiaCmd{
			Origen:            domain.OrigenFolioCliente,
			ClienteID:         &clienteID,
			VentaID:           &ventaID,
			EstadoCuenta:      ptrEstadoCuenta(domain.EstadoCuentaLiquidada),
			Description:       "Comedor con estructura rota",
			Calle:             "Calle Reforma",
			NumeroExterior:    "321",
			Colonia:           "Centro",
			Localidad:         "Puebla",
			Ciudad:            "Puebla",
			CodigoPostal:      "72000",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		articuloMicrosipID := 983003
		g, err = svc.AgregarArticulo(ctx, app.AgregarArticuloCmd{
			GarantiaID:        g.ID(),
			ArticuloID:        &articuloMicrosipID,
			Clave:             "TALLER-001",
			Description:       "Comedor de seis sillas",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		id := g.ID()
		original := g.ArticulosForRepo()[0]

		_, err = svc.IniciarProceso(ctx, app.IniciarProcesoCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		for _, etapa := range []domain.Etapa{
			domain.EtapaPendienteRecoleccion,
			domain.EtapaRecolectado,
			domain.EtapaEnRevision,
		} {
			_, err = svc.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
				GarantiaID:        id,
				ArticuloID:        original.ID(),
				EtapaDestino:      etapa,
				ClaveIdempotencia: clave(),
				DeviceCreatedAt:   clk.Now(),
			})
			require.NoError(t, err, "avanzar a %s", etapa)
		}

		// Taller: en_revision -> en_taller. El diagnóstico ya mueve la etapa;
		// el avance posterior a la misma etapa sería una transición inválida.
		_, err = svc.RegistrarDiagnostico(ctx, app.RegistrarDiagnosticoCmd{
			GarantiaID:        id,
			ArticuloID:        original.ID(),
			Ruta:              string(domain.RutaReparacionTaller),
			RolDecisor:        string(domain.RolDecisorTecnica),
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		// El cambio físico: el original se va a standby y nace el reemplazo.
		g, err = svc.AutorizarCambioFisico(ctx, app.AutorizarCambioFisicoCmd{
			GarantiaID:        id,
			ArticuloID:        original.ID(),
			RolDecisor:        string(domain.RolDecisorTecnica),
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		var reemplazo *domain.Articulo
		for _, a := range g.ArticulosForRepo() {
			if a.Rol() == domain.RolArticuloReemplazo {
				reemplazo = a
			}
		}
		require.NotNil(t, reemplazo, "AutorizarCambioFisico debe crear el reemplazo")
		require.Equal(t, domain.EtapaListoEntrega, reemplazo.Etapa())

		// El folio pasa a listo_entrega y se entrega el reemplazo.
		_, err = svc.MarcarListoEntrega(ctx, app.MarcarListoEntregaCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		lat, lon := 19.0414, -98.2063
		_, err = svc.Entregar(ctx, app.EntregarCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
			GPSLat:            &lat,
			GPSLon:            &lon,
		})
		require.NoError(t, err)

		// El original sigue en standby: el cierre no lo bloquea (R4).
		_, err = svc.Cerrar(ctx, app.CerrarCmd{
			GarantiaID:        id,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		cerrado, err := grepo.Obtener(ctx, id)
		require.NoError(t, err)
		require.Equal(t, domain.EstadoFolioCerrado, cerrado.Estado())

		var originalCerrado *domain.Articulo
		for _, a := range cerrado.ArticulosForRepo() {
			if a.ID() == original.ID() {
				originalCerrado = a
			}
		}
		require.NotNil(t, originalCerrado)
		require.Equal(t, domain.EtapaStandby, originalCerrado.Etapa())

		// Y después de cerrado, el destino final del original.
		_, err = svc.RegistrarDesenlace(ctx, app.RegistrarDesenlaceCmd{
			GarantiaID:        id,
			ArticuloID:        original.ID(),
			Desenlace:         string(domain.DesenlaceSegundaMano),
			RolDecisor:        string(domain.RolDecisorTecnica),
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		require.NoError(t, err)

		final, err := grepo.Obtener(ctx, id)
		require.NoError(t, err)
		require.Equal(t, domain.EstadoFolioCerrado, final.Estado(),
			"registrar el desenlace de un standby no reabre el folio")

		for _, a := range final.ArticulosForRepo() {
			if a.ID() != original.ID() {
				continue
			}
			require.Equal(t, domain.EtapaSegundaMano, a.Etapa())
			require.Equal(t, domain.UbicacionAlmacenSegundaMano, a.Ubicacion())
			require.NotNil(t, a.Desenlace())
			require.Equal(t, domain.DesenlaceSegundaMano, *a.Desenlace())
		}

		// El reemplazo salió entregado y con su desenlace reemplazado.
		for _, a := range final.ArticulosForRepo() {
			if a.Rol() != domain.RolArticuloReemplazo {
				continue
			}
			require.Equal(t, domain.EtapaEntregado, a.Etapa())
			require.NotNil(t, a.Desenlace())
			require.Equal(t, domain.DesenlaceReemplazado, *a.Desenlace())
		}

		// La línea de tiempo completa: el cierre va antes del desenlace del
		// original, porque el folio se cerró con el mueble todavía dentro.
		const o, r = "original", "reemplazo"
		evs := verificarLineaDeTiempo(ctx, t, erepo, final,
			map[string]uuid.UUID{o: original.ID(), r: reemplazo.ID()},
			[]eventoEsperado{
				{tipo: domain.TipoEventoFolioAbierto},
				{tipo: domain.TipoEventoArticuloAgregado, articulo: o},
				{tipo: domain.TipoEventoFolioEnProceso},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaRegistrado, etapaHasta: domain.EtapaPendienteRecoleccion,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaPendienteRecoleccion, etapaHasta: domain.EtapaRecolectado,
					ubicDesde: domain.UbicacionDomicilioCliente, ubicHasta: domain.UbicacionEnTransito,
				},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: o,
					etapaDesde: domain.EtapaRecolectado, etapaHasta: domain.EtapaEnRevision,
					ubicDesde: domain.UbicacionEnTransito, ubicHasta: domain.UbicacionAlmacenRevision,
				},
				{
					tipo: domain.TipoEventoDiagnosticoRegistrado, articulo: o,
					etapaDesde: domain.EtapaEnRevision, etapaHasta: domain.EtapaEnTaller,
					ubicDesde: domain.UbicacionAlmacenRevision, ubicHasta: domain.UbicacionTaller,
				},
				// El cambio físico son dos saltos del original y el alta del
				// reemplazo, cada uno con su evento.
				{
					tipo: domain.TipoEventoCambioAutorizado, articulo: o,
					etapaDesde: domain.EtapaEnTaller, etapaHasta: domain.EtapaCambioAutorizado,
				},
				{
					tipo: domain.TipoEventoCambioAutorizado, articulo: o,
					etapaDesde: domain.EtapaCambioAutorizado, etapaHasta: domain.EtapaStandby,
				},
				{tipo: domain.TipoEventoArticuloAgregado, articulo: r},
				{tipo: domain.TipoEventoFolioListoEntrega},
				{
					tipo: domain.TipoEventoEtapaAvanzada, articulo: r,
					etapaDesde: domain.EtapaListoEntrega, etapaHasta: domain.EtapaEntregado,
					ubicDesde: domain.UbicacionAlmacenRevision, ubicHasta: domain.UbicacionEntregado,
				},
				{tipo: domain.TipoEventoFolioEntregado},
				{tipo: domain.TipoEventoFolioCerrado},
				{
					tipo: domain.TipoEventoDesenlaceRegistrado, articulo: o,
					etapaDesde: domain.EtapaStandby, etapaHasta: domain.EtapaSegundaMano,
					ubicDesde: domain.UbicacionTaller, ubicHasta: domain.UbicacionAlmacenSegundaMano,
				},
			})
		require.Equal(t, domain.TipoEventoDesenlaceRegistrado, evs[len(evs)-1].Tipo())
	})
}

func ptrEstadoCuenta(e domain.EstadoCuenta) *domain.EstadoCuenta { return &e }
