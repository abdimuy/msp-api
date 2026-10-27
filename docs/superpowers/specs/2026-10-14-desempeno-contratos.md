# Desempeño de cobradores — contratos (fuente única para API y escritorio)

Versión 2.1 (2026-10-14): motivo `inicio_semana_futuro` (inicio de semana después de ahora → fila no configurada, nunca 422 del tablero).
Versión 2 (2026-10-14): agrega `docto_pv_id`, `captura_tardia`, `trato`/`etiqueta` de visita,
datos de popover en `puntos`, `cobro.falta`, `generado_at` en jornada, nulos de no configurado,
valores de filtros, `GET /v2/desempeno/catalogos`. El enlace "sin configurar" lleva a Usuarios
(`/settings`), donde se editan zona e inicio de semana.

Complemento de `2026-10-14-desempeno-cobradores-design.md`. Lo que aquí se nombra es **ley** para
los dos planes: el API lo produce byte a byte y el escritorio lo consume byte a byte. Cambiar un
nombre exige cambiar este archivo primero.

## 0. Convenciones

- JSON `snake_case`. Dinero: string decimal con 2 decimales (`"1250.00"`). Horas: RFC3339 UTC
  (`"2026-10-14T16:31:00Z"`); el escritorio las muestra en `America/Mexico_City`.
- Fechas de día (`fecha`, `dia`): `YYYY-MM-DD` del calendario de `America/Mexico_City`.
- `null` explícito cuando no hay dato (nunca se omite el campo).
- Errores: formato Huma/`apperror` existente (`code` en inglés snake_case, `message` en español).
  Códigos nuevos: `desempeno_usuario_not_found` (404), `desempeno_usuario_sin_zona` (409),
  `desempeno_usuario_sin_inicio_semana` (409), `desempeno_parametro_invalido` (422),
  `desempeno_firestore_unavailable` (503), `desempeno_export_demasiadas_filas` (422).
- Permiso: `desempeno:ver` en todo `/v2/desempeno/*`.
- Parámetros de jornada comunes: `pausa_min` entero 5–240 (default 45); `desde_hora`,
  `hasta_hora` `HH:MM` opcionales, ambos o ninguno, `desde_hora < hasta_hora`.

## 1. Tipos compartidos

```jsonc
// RegistradoPor
{ "usuario_uid": "abc123" | null,   // uid Firestore si se pudo resolver
  "cobrador_id": 11502 | null,
  "nombre": "JUAN CARLOS MENDEZ",    // "Oficina" si origen = "oficina"
  "origen": "app" | "oficina" }

// Actividad
{ "id": "pago:15082369" | "visita:412cc2fc-...",   // estable y único
  "tipo": "pago" | "visita" | "condonacion",
  "hora": "2026-10-14T16:31:00Z",          // hora registrada por el teléfono (o de Microsip)
  "recibido_at": "2026-10-14T16:31:05Z" | null,  // llegada al servidor
  "cliente_id": 1544855, "cliente_nombre": "JUANA ANDRADE LECHUGA",
  "venta_id": 13785217 | null,             // DOCTO_CC_ID del cargo
  "folio": "Z1677035" | null,
  "registrado_por": RegistradoPor,
  "lat": 18.4617 | null, "lng": -97.3928 | null, "con_gps": true,
  "num_fotos": 2,
  "docto_pv_id": 9921 | null,              // para abrir el detalle de venta de Clientes
  "captura_tardia": false,                 // recibido_at − hora ≥ 2 h (regla del servidor)
  "pago": null | {
    "importe": "150.00",
    "forma_cobro": "efectivo" | "transferencia" | "cheque" | "condonacion" | "otra",
    "forma_cobro_id": 157,
    "aplicado": true, "rechazado": false,
    "docto_cc_id": 15082369 | null,
    "pago_recibido_id": "7a1f39e4-..." | null },
  "visita": null | {
    "visita_id": "412cc2fc-...", "tipo_visita": "No se encontraba",
    "nota": "Vecina dice que regresa el lunes" | null,
    "estado": EstadoCuenta, "trato": Trato,
    "etiqueta": "No estaba" } }               // misma etiqueta que el teléfono

// EstadoCuenta (códigos fijos; etiqueta y detalle los arma el servidor en español)
"pago" | "abono_parcial" | "visite_vuelvo" | "prometio_proxima" | "se_nego"
| "cita_a_una_hora" | "no_estaba" | "sin_tocar"

// Trato
"pagado" | "parcial" | "regresas" | "diferido" | "escalar" | "cita" | "nadie" | "sin_trabajar"

// Segmento
"sin_visitar" | "volver" | "despues" | "pagados"   // null si no cae en ninguno

// Pausa
{ "desde": "...Z", "hasta": "...Z", "minutos": 72,
  "anterior_id": "pago:..." | null, "siguiente_id": "visita:..." | null,
  "en_curso": false }

// Tramo (vista Bloques)
{ "desde": "...Z", "hasta": "...Z",
  "pagos": { "n": 12, "importe": "3150.00" }, "visitas": 5, "condonaciones": 0, "clientes": 14 }

// Hora (vista Por hora): una entrada por hora 7..19 (13 entradas, [h, h+1))
{ "hora": 7, "pagos": 3, "visitas": 1, "condonaciones": 0, "importe": "450.00" }

// Senal
{ "tipo": "pausa_larga" | "inicio_tarde" | "fin_temprano" | "mismo_punto" | "captura_tardia" | "sin_gps",
  "actividad_ids": ["pago:..."], "desde": "...Z" | null, "hasta": "...Z" | null,
  "detalle": "4 cuentas desde el mismo punto en 6 min",
  "regla": "≥ 4 clientes distintos en 25 m dentro de 10 min" }

// LineaDelDia
{ "dia": "2026-10-14",
  "puntos": [ { "id": "...", "tipo": "pago", "hora": "...Z",
                "cliente_nombre": "...", "importe": "150.00" | null,   // null en visitas
                "resultado": "No se encontraba" | null,               // literal en visitas
                "num_fotos": 0 } ],
  "tramos": [Tramo], "por_hora": [Hora], "pausas": [Pausa] }
```

## 2. `GET /v2/desempeno/usuarios`

Query: `periodo=hoy|semana` (default `hoy`), `pausa_min`, `desde_hora`, `hasta_hora`.

```jsonc
{ "generado_at": "...Z", "periodo": "hoy", "pausa_min": 45,
  "usuarios": [ {
    "uid": "abc123", "nombre": "Juan Carlos Méndez", "email": "...",
    "zona_id": 21563, "zona_nombre": "RUTA 21",
    "configurado": true, "motivo_no_configurado": null | "sin_zona" | "sin_inicio_semana" | "inicio_semana_futuro",
    "inicio_semana": "2026-10-09T13:46:00Z" | null,
    "ventas_universo": 232,
    "estado_ahora": "activo" | "sin_actividad" | "no_ha_empezado",
    "estado_desde": "...Z" | null,
    "ultima": { "id": "...", "tipo": "pago", "hora": "...Z", "cliente_nombre": "..." } | null,
    "primera_actividad": "...Z" | null, "ultima_actividad": "...Z" | null,
    "minutos_activos": 346,
    "pausas": { "n": 1, "minutos": 72, "en_curso": false },
    "pagos": { "n": 31, "importe": "8600.00" },
    "pagos_por_forma": { "efectivo": {"n":28,"importe":"7800.00"},
                         "transferencia": {"n":3,"importe":"800.00"},
                         "cheque": {"n":0,"importe":"0.00"} },
    "condonaciones": { "n": 0, "importe": "0.00" },
    "no_aplicados": { "n": 0, "importe": "0.00" },
    "visitas": 13,
    "cobertura": { "con_algo": 186, "total": 232 },
    "cobertura_por_segmento": { "sin_visitar": 46, "volver": 20, "despues": 3, "pagados": 140, "sin_nada": 46 },
    "cobro": { "cobrado": "8600.00", "esperado": "13050.00", "falta": "4450.00", "pct": "65.90" | null },
    "lineas": [LineaDelDia]    // 1 en "hoy"; un día por cada día de la semana en "semana"
  } ] }
```

- Usuario no configurado: `configurado=false`, métricas en cero/`null`, `lineas=[]`;
  `zona_id`, `zona_nombre`, `inicio_semana` y `cobro.pct` pueden venir `null`.
- `cobertura_por_segmento.sin_nada` == `cobertura.total − cobertura.con_algo`, siempre de la semana
  del usuario (no incluye la semana anterior).
- **Sin totales del equipo** (D14): no hay campos agregados fuera de cada usuario.
- `cobertura`, `cobertura_por_segmento` y `cobro` son siempre de **la semana del usuario**,
  aunque `periodo=hoy`.

## 3. `GET /v2/desempeno/usuarios/{uid}/jornada`

Query: `fecha` (default hoy), `pausa_min`, `desde_hora`, `hasta_hora`.

```jsonc
{ "uid": "abc123", "fecha": "2026-10-14", "pausa_min": 45, "generado_at": "...Z",
  "actividades": [Actividad],      // solo las que registró ESTE usuario, orden por hora asc
  "linea": LineaDelDia,
  "senales": [Senal],
  "resumen": { "primera_actividad": "...Z" | null, "ultima_actividad": "...Z" | null,
               "minutos_activos": 346, "pausas": {"n":1,"minutos":72,"en_curso":false},
               "pagos": {"n":31,"importe":"8600.00"}, "visitas": 13, "condonaciones": 0,
               "clientes": 41 } }
```

## 4. `GET /v2/desempeno/usuarios/{uid}/actividad`

Query (todo opcional): `desde`, `hasta` (RFC3339; default: inicio de semana del usuario → ahora;
rango máximo 92 días), `tipo` (repetible: pago|visita|condonacion), `forma_cobro` (repetible),
`tipo_visita` (repetible, literal exacto), `registrado_por` (repetible: uid o `oficina`),
`monto_min`, `monto_max`, `con_fotos` (true|false), `con_gps` (true|false), `q` (cliente o folio,
sin acentos ni mayúsculas), `orden=hora|cliente|monto|tipo|registrado_por` (default `hora`),
`dir=asc|desc` (default `desc`), `cursor` (opaco), `limit` 1–500 (default 100).
Valores de `forma_cobro`: los códigos `efectivo|transferencia|cheque|condonacion|otra` (no ids).
`totales.importe` = solo dinero (formas 157/158/52569, no rechazados); las condonaciones NO
suman ahí. Agrupar por venta = `orden=cliente` y agrupación en pantalla.

```jsonc
{ "items": [Actividad],             // TODA la zona del usuario, cualquier registrador
  "siguiente_cursor": "opaco" | null,
  "totales": { "n": 1188, "importe": "289450.00",
               "por_tipo": { "pago": 980, "visita": 200, "condonacion": 8 } } }
```

`GET /v2/desempeno/usuarios/{uid}/actividad.xlsx` con los mismos filtros (sin `cursor`/`limit`):
`Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`, tope 50,000
filas (si se excede → 422 `desempeno_export_demasiadas_filas`).

## 4b. `GET /v2/desempeno/catalogos`

```jsonc
{ "formas_cobro": [ { "codigo": "efectivo", "etiqueta": "Efectivo" }, ... ],
  "tipos_visita": [ { "literal": "No se encontraba", "estado": "no_estaba", "grupo": "No estaba" }, ... ] }
```
El escritorio arma los filtros con esto; no copia catálogos.

## 5. `GET /v2/desempeno/usuarios/{uid}/cuentas`

Query: `segmento=sin_visitar|volver|despues|pagados|sin_nada` (opcional), `q`,
`orden=cliente|saldo|atrasos|ultima_actividad` (default `cliente`), `dir`.

```jsonc
{ "inicio_semana": "...Z",
  "conteos": { "sin_visitar": 46, "volver": 20, "despues": 3, "pagados": 140, "sin_nada": 46, "total": 232 },
  "cuentas": [ {
    "venta_id": 13785217, "docto_pv_id": 9921 | null, "folio": "Z1677035" | null,
    "cliente_id": 1544855, "cliente_nombre": "...", "producto": "ROPERO LABRADO CALIFORNIA VINO" | null,
    "estado": EstadoCuenta, "trato": Trato, "etiqueta": "Pagó esta semana", "detalle": "No hace falta volver",
    "segmento": Segmento | null, "con_algo": true,
    "determinado_por": "pago:..." | "visita:..." | null,
    "ultima_actividad": { "id": "...", "tipo": "pago", "hora": "...Z" } | null,
    "saldo": "16650.00", "parcialidad": "150.00", "abono_semana": "150.00",
    "num_atrasos": 0, "nueva": false } ] }
```

## 6. Fotos y detalle de pago (módulos existentes)

- `GET /v2/visitas/{id}/imagenes` → `[{ "id", "mime", "size_bytes", "descripcion", "created_at", "updated_at" }]`
  (misma forma que `ImagenPagoDTO`).
- `GET /v2/visitas/{id}/imagenes/{img_id}` → binario, `ETag`, `Cache-Control: private, max-age=31536000, immutable`, 304 con `If-None-Match`; 404 si la imagen no es de esa visita.
- `GET /v2/cobranza/pagos/{id}/imagenes/{img_id}` → 404 si la imagen no es de ese pago (arreglo).
- `GET /v2/clientes/{id}/pagos/{doctoCcId}` agrega `"pago_recibido_id": "uuid" | null` y `"num_fotos": 0`.
- Permisos: lectura de fotos con `cobranza:ver_pagos`; crear pago y subir/borrar foto de pago con
  el nuevo `cobranza:registrar_pagos` (despliegue en dos pasos, spec §6).

## 7. Juego de casos compartido (estado por venta)

Directorio `testdata/estado-cuenta/` en msp-api (fuente) y copia idéntica en
`msp-app-kt/core/common/src/test/resources/estado-cuenta/`. Un archivo por caso, `NNN-nombre.json`,
más `VERSION` con el SHA-256 de la concatenación ordenada de los `.json`.

```jsonc
{ "nombre": "cliente con dos ventas, no estaba marca ambas",
  "ventana": { "inicio": "2026-10-09T13:46:00Z", "fin": "2026-10-14T22:40:00Z" },
  "cuentas": [ { "venta_id": 1, "cliente_id": 10, "parcialidad": "150.00" },
               { "venta_id": 2, "cliente_id": 10, "parcialidad": "200.00" } ],
  "pagos":   [ { "venta_id": 1, "importe": "150.00", "forma_cobro_id": 157,
                 "fecha_hora": "2026-10-10T15:00:00Z", "rechazado": false } ],
  "visitas": [ { "cliente_id": 10, "venta_id": null, "tipo_visita": "No se encontraba",
                 "fecha_hora": "2026-10-11T16:00:00Z", "fecha_cita": null, "hora_cita": null,
                 "fecha_promesa": null } ],
  "esperado": [ { "venta_id": 1, "estado": "pago", "con_algo": true },
                { "venta_id": 2, "estado": "no_estaba", "con_algo": true } ] }
```
