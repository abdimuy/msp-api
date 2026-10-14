// Package desempenofb implementa los puertos de lectura del módulo desempeno
// contra Firebird. Solo lee. Sus consultas son PROPIAS: no usa ni modifica
// pagoFromClause, selectPagoColsP ni el WHERE de sync/digest/by-ids
// (docs/COBRANZA-SYNC.md).
//
// Fechas: firebird.ToWallClock al enlazar y firebird.ScanUTCTime al leer
// (DATETIME_HANDLING.md). Texto: el escaneo depende del charset DE LA COLUMNA
// (ENCODING_HANDLING.md), anotado en cada consulta.
package desempenofb
