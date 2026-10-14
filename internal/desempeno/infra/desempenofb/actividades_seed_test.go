package desempenofb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Sembradores propios de este paquete. Todo corre dentro de
// fbtestutil.WithTestTransaction, que siempre revierte.

// usuarioSembrado inserta un MSP_USUARIOS autorreferenciado (CREATED_BY es FK
// a sí misma) y devuelve su ID y su FIREBASE_UID.
func usuarioSembrado(ctx context.Context, t *testing.T, q firebird.Querier) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	uid := "fb-desempeno-" + id.String()[:8]
	now := firebird.ToWallClock(time.Now().UTC())
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS, CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'Juan Carlos Méndez', TRUE, 'FIREBASE_USER', ?, ?, ?, ?)`,
		id.String(), uid, "juan."+id.String()[:8]+"@muebleriamsp.mx", now, now, id.String(), id.String())
	require.NoError(t, err, "sembrar MSP_USUARIOS")
	return id, uid
}

// impteDe devuelve el IMPTE_DOCTO_CC_ID del abono sembrado por microsipseed.
func impteDe(ctx context.Context, t *testing.T, q firebird.Querier, doctoCCID int) int {
	t.Helper()
	var id int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT IMPTE_DOCTO_CC_ID FROM IMPORTES_DOCTOS_CC WHERE DOCTO_CC_ID = ?`, doctoCCID).Scan(&id))
	return id
}

type pagoRecibidoSembrado struct {
	id           uuid.UUID
	impteID      int // 0 = pendiente (sin Microsip)
	doctoCCID    int
	cargoID      int
	clienteID    int
	cobradorID   int
	importe      decimal.Decimal
	formaCobroID int
	conceptoID   int
	fecha        time.Time
	recibido     time.Time
	estado       string
	intentos     int
	createdBy    uuid.UUID
}

func sembrarPagoRecibido(ctx context.Context, t *testing.T, q firebird.Querier, p pagoRecibidoSembrado) {
	t.Helper()
	var impte, docto any
	if p.impteID != 0 {
		impte, docto = p.impteID, p.doctoCCID
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_PAGOS_RECIBIDOS
		 (ID, FECHA, IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, CARGO_DOCTO_CC_ID, CLIENTE_ID, COBRADOR_ID, COBRADOR,
		  IMPORTE, FORMA_COBRO_ID, CONCEPTO_CC_ID, LAT, LON, ESTADO, INTENTOS, RECEIVED_AT, UPDATED_AT, CREATED_BY)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'JUAN CARLOS MENDEZ', ?, ?, ?, '18.4617', '-97.3928', ?, ?, ?, ?, ?)`,
		p.id.String(), firebird.ToWallClock(p.fecha), impte, docto, p.cargoID, p.clienteID, p.cobradorID,
		p.importe, p.formaCobroID, p.conceptoID, p.estado, p.intentos,
		firebird.ToWallClock(p.recibido), firebird.ToWallClock(p.recibido), p.createdBy.String())
	require.NoError(t, err, "sembrar MSP_PAGOS_RECIBIDOS")
}

func sembrarImagenPago(ctx context.Context, t *testing.T, q firebird.Querier, pagoID uuid.UUID) {
	t.Helper()
	now := firebird.ToWallClock(time.Now().UTC())
	id := uuid.New()
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_PAGOS_IMAGENES (ID, PAGO_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, 'FILESYSTEM', ?, 'image/jpeg', 1024, ?, ?, ?, ?)`,
		id.String(), pagoID.String(), "pagos/"+pagoID.String()+"/"+id.String()+".jpg", now, now, pagoID.String(), pagoID.String())
	require.NoError(t, err, "sembrar MSP_PAGOS_IMAGENES")
}

type visitaSembrada struct {
	id         uuid.UUID
	clienteID  int
	zonaID     int
	ventaID    int // 0 = sin venta
	cobradorID int
	tipo       string
	fecha      time.Time
	lat, lng   float64
}

func sembrarVisita(ctx context.Context, t *testing.T, q firebird.Querier, v visitaSembrada) {
	t.Helper()
	var venta any
	if v.ventaID != 0 {
		venta = v.ventaID
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_VISITAS (ID, COBRADOR, COBRADOR_ID, FECHA, FORMA_COBRO_ID, LAT, LNG, NOTA,
		  TIPO_VISITA, ZONA_CLIENTE_ID, CLIENTE_ID, IMPTE_DOCTO_CC_ID)
		 VALUES (?, 'JUAN CARLOS MÉNDEZ', ?, ?, 0, ?, ?, 'Vecina dice que regresa el lunes', ?, ?, ?, ?)`,
		v.id.String(), v.cobradorID, firebird.ToWallClock(v.fecha), v.lat, v.lng, v.tipo, v.zonaID, v.clienteID, venta)
	require.NoError(t, err, "sembrar MSP_VISITAS")
}

func sembrarImagenVisita(ctx context.Context, t *testing.T, q firebird.Querier, visitaID uuid.UUID) uuid.UUID {
	t.Helper()
	now := firebird.ToWallClock(time.Now().UTC())
	id := uuid.New()
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_VISITAS_IMAGENES (ID, VISITA_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
		  DESCRIPCION, CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, 'FILESYSTEM', ?, 'image/jpeg', 2048, 'fachada', ?, ?, ?, ?)`,
		id.String(), visitaID.String(), "visitas/"+visitaID.String()+"/"+id.String()+".jpg", now, now,
		visitaID.String(), visitaID.String())
	require.NoError(t, err, "sembrar MSP_VISITAS_IMAGENES")
	return id
}

// segundaZona devuelve una zona del catálogo distinta de la primera.
func segundaZona(ctx context.Context, t *testing.T, q firebird.Querier, distintaDe int) int {
	t.Helper()
	var id int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT FIRST 1 ZONA_CLIENTE_ID FROM ZONAS_CLIENTES WHERE ZONA_CLIENTE_ID <> ? ORDER BY ZONA_CLIENTE_ID`,
		distintaDe).Scan(&id), "el catálogo ZONAS_CLIENTES necesita al menos dos zonas")
	return id
}
