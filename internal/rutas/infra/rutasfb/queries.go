// Package rutasfb implements the Firebird-backed RutasRepo.
//
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

// queryListarRutas returns one row per zona, joining cobrador and two
// pre-aggregated derived tables to avoid fan-out / double counting.
//
// CAST(SUM(...) AS NUMERIC(18,2)) is required — firebirdsql v0.9.x returns
// NUMERIC aggregates unscaled without the explicit cast.
const queryListarRutas = `
SELECT z.ZONA_CLIENTE_ID,
       z.NOMBRE,
       cfg.COBRADOR_ID,
       cob.NOMBRE AS COBRADOR_NOMBRE,
       COALESCE(nc.N, 0) AS NUM_CLIENTES,
       COALESCE(sv.SALDO, 0) AS SALDO_TOTAL
FROM ZONAS_CLIENTES z
LEFT JOIN MSP_CFG_ZONA_CAJA cfg ON cfg.ZONA_CLIENTE_ID = z.ZONA_CLIENTE_ID
LEFT JOIN COBRADORES cob        ON cob.COBRADOR_ID     = cfg.COBRADOR_ID
LEFT JOIN (SELECT ZONA_CLIENTE_ID, COUNT(*) N FROM CLIENTES
           WHERE ESTATUS IN ('A', 'B') GROUP BY ZONA_CLIENTE_ID) nc
       ON nc.ZONA_CLIENTE_ID = z.ZONA_CLIENTE_ID
LEFT JOIN (SELECT ZONA_CLIENTE_ID, CAST(SUM(SALDO) AS NUMERIC(18,2)) SALDO
           FROM MSP_SALDOS_VENTAS WHERE CARGO_CANCELADO <> 'S'
           GROUP BY ZONA_CLIENTE_ID) sv
       ON sv.ZONA_CLIENTE_ID = z.ZONA_CLIENTE_ID
ORDER BY z.NOMBRE`
