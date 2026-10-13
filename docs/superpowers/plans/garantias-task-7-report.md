# Garantías — Tarea 7 — Reporte

## Entrega 1 — Repositorio de lectura

Se completó la parte de lectura de Firebird correspondiente a la bandeja de garantías, repositorio de imágenes y validación de claves duplicadas.

### Implementado

- `BandejaRepo.Listar`.
- Filtros por estado, origen, cliente, etapa, ubicación y fechas.
- Paginación por `CREATED_AT DESC, ID DESC`.
- Cursor con desempate por ID.
- `Desde` inclusivo.
- `Hasta` exclusivo.
- Carga de artículos en una segunda consulta.
- Uso de `EXISTS` para filtros de artículos.
- `ImagenRepo.Registrar`.
- `ImagenRepo.ListarPorEvento`.
- Validación de FK en imágenes.
- Validación de transacción requerida para registrar imágenes.
- Helper `esClaveDuplicada`.
- Lecturas protegidas con `RunInReadTx`.

## Consultas por página

La bandeja realiza exactamente 2 consultas por página:

1. Consulta de garantías.
2. Consulta de artículos usando `IN`.

La prueba utilizada fue:

```text
TestBandejaRepo_ListarUsaDosConsultasPorPagina
```

## Resultado Entrega 1

La primera entrega terminó correctamente.

```text
garfb: 86.3% de cobertura
golangci-lint: 0 issues
make check-sealed MODULE=garantias: ✔ garantias is sealed
```

---

## Entrega 2 — Consultas e interfaz HTTP

Se completó el lado de lectura desde la capa de aplicación hasta HTTP.

### Implementado

- `app.Consultas`.
- Consulta de bandeja.
- Consulta de detalle.
- Validación de usuario autenticado.
- Validación del permiso `garantias:leer`.
- Adaptador `identidad`.
- Rutas HTTP:
  - `GET /garantias`
  - `GET /garantias/{id}`
- Filtros:
  - `cursor`
  - `limit`
  - `estado`
  - `origen`
  - `cliente_id`
  - `etapa`
  - `ubicacion`
  - `desde`
  - `hasta`
- Respuestas JSON en `snake_case`.
- Fechas y horas en RFC3339 UTC.
- `vigencia_hasta` en formato `YYYY-MM-DD`.
- Traducción de errores HTTP.
- Registro de rutas en OpenAPI.
- Prueba E2E HTTP → Consultas → garfb → Firebird.

## Seguridad

Las consultas verifican primero la identidad y después el permiso `garantias:leer`.

Si el usuario no está autenticado:

```text
401 Unauthorized
```

Si el usuario no tiene permiso:

```text
403 Forbidden
```

En ambos casos se comprobó que los repositorios no fueran llamados.

Las pruebas incluidas fueron:

```text
TestGarHTTP_Seguridad_401_403
TestConsultas_Bandeja_SinUsuarioNoTocaRepos
TestConsultas_Bandeja_SinPermisoNoTocaRepos
TestConsultas_Detalle_SinUsuarioNoTocaRepos
TestConsultas_Detalle_SinPermisoNoTocaRepos
```

## Validaciones HTTP

Se verificaron los parámetros:

```text
cursor
limit
estado
origen
cliente_id
etapa
ubicacion
desde
hasta
```

Los valores inválidos producen:

```text
422 Unprocessable Entity
```

También se verificó que un UUID inválido en:

```text
GET /garantias/{id}
```

produzca 422.

Pruebas utilizadas:

```text
TestListarGarantias_QueryParamsInvalidos_422
TestListarGarantias_CursorInvalido_422
TestObtenerGarantia_UUIDInvalido_422
```

## Protección ante entradas similares a inyección SQL

Se utilizó el texto:

```text
' OR 1=1 --
```

en los filtros:

```text
estado
origen
cliente_id
etapa
ubicacion
desde
hasta
```

Todos fueron rechazados con 422.

La prueba utilizada fue:

```text
TestListarGarantias_InyeccionSQLFiltros_422
```

## Manejo de errores

Se verificó el mapeo requerido:

```text
Validación      → 422
No encontrado  → 404
Conflicto       → 409
Sin permiso     → 403
No autenticado  → 401
Otros errores   → 500
```

Para los errores 500 se devuelve un mensaje genérico y no se expone información interna.

La prueba utilizada fue:

```text
TestListarGarantias_ErrorInterno_500NoFiltraMensaje
```

Como texto interno conocido se utilizó:

```text
firebird: password=super-secreto tabla MSP_GA_GARANTIA
```

La respuesta HTTP no expone ese contenido.

También se verificó el caso de una garantía inexistente:

```text
TestObtenerGarantia_NoEncontrada_404
```

## JSON de la bandeja

Ejemplo completo utilizado en las pruebas HTTP:

```json
{
  "$schema": "https://example.com/v2/schemas/ListarGarantiasResponseBody.json",
  "items": [
    {
      "id": "11111111-1111-1111-1111-111111111111",
      "folio": "GA-000123",
      "origen": "cliente",
      "cliente_id": 25,
      "venta_id": 80,
      "estado_cuenta": "liquidada",
      "estado": "en_proceso",
      "description": "Garantía de sala",
      "vigencia_hasta": "2027-01-31",
      "calle": "Reforma",
      "numero_exterior": "123",
      "colonia": "Centro",
      "localidad": "Centro",
      "ciudad": "Ciudad de México",
      "codigo_postal": "06000",
      "gps_lat": 19.4326,
      "gps_lon": -99.1332,
      "abierto_por": "ruben",
      "created_at": "2026-10-13T15:00:00Z",
      "updated_at": "2026-10-13T15:30:00Z",
      "articulos": [
        {
          "id": "22222222-2222-2222-2222-222222222222",
          "garantia_id": "11111111-1111-1111-1111-111111111111",
          "rol": "original",
          "articulo_id": 150,
          "clave": "SALA-001",
          "description": "Respaldo dañado",
          "ruta": "taller",
          "etapa": "en_taller",
          "ubicacion": "taller",
          "created_at": "2026-10-13T15:00:00Z",
          "updated_at": "2026-10-13T15:30:00Z"
        }
      ]
    }
  ],
  "next_cursor": "cursor-siguiente",
  "has_more": true
}
```

La prueba compara el JSON completo mediante:

```text
TestListarGarantias_CaminoFeliz_JSONCompleto
```

## JSON del detalle

Ejemplo completo utilizado en las pruebas HTTP:

```json
{
  "$schema": "https://example.com/v2/schemas/ObtenerGarantiaResponseBody.json",
  "garantia": {
    "id": "11111111-1111-1111-1111-111111111111",
    "folio": "GA-000123",
    "origen": "cliente",
    "cliente_id": 25,
    "venta_id": 80,
    "estado_cuenta": "liquidada",
    "estado": "en_proceso",
    "description": "Garantía de sala",
    "vigencia_hasta": "2027-01-31",
    "calle": "Reforma",
    "numero_exterior": "123",
    "colonia": "Centro",
    "localidad": "Centro",
    "ciudad": "Ciudad de México",
    "codigo_postal": "06000",
    "gps_lat": 19.4326,
    "gps_lon": -99.1332,
    "abierto_por": "ruben",
    "created_at": "2026-10-13T15:00:00Z",
    "updated_at": "2026-10-13T15:30:00Z",
    "articulos": [
      {
        "id": "22222222-2222-2222-2222-222222222222",
        "garantia_id": "11111111-1111-1111-1111-111111111111",
        "rol": "original",
        "articulo_id": 150,
        "clave": "SALA-001",
        "description": "Respaldo dañado",
        "ruta": "taller",
        "etapa": "en_taller",
        "ubicacion": "taller",
        "created_at": "2026-10-13T15:00:00Z",
        "updated_at": "2026-10-13T15:30:00Z"
      }
    ]
  },
  "eventos": [
    {
      "id": "33333333-3333-3333-3333-333333333333",
      "garantia_id": "11111111-1111-1111-1111-111111111111",
      "articulo_ref": "22222222-2222-2222-2222-222222222222",
      "tipo": "diagnostico_registrado",
      "description": "Se envía al taller",
      "etapa_desde": "en_revision",
      "etapa_hasta": "en_taller",
      "usuario": "ruben",
      "rol_decisor": "tecnica",
      "gps_lat": 19.4326,
      "gps_lon": -99.1332,
      "created_at": "2026-10-13T15:30:00Z",
      "device_created_at": "2026-10-13T15:30:00Z",
      "clave_idempotencia": "44444444-4444-4444-4444-444444444444"
    }
  ]
}
```

La prueba utilizada fue:

```text
TestObtenerGarantia_CaminoFeliz_JSONCompleto
```

## Campos opcionales

Se verificó también la proyección de campos opcionales como:

```text
reemplaza_a
dictamen
desenlace
cerrado_en
```

y la omisión de campos vacíos mediante `omitempty`.

Prueba utilizada:

```text
TestListarGarantias_ProyectaCamposOpcionales
```

## Filtros tipados

Se comprobó que los parámetros válidos se conviertan a los tipos del dominio antes de llegar al repositorio.

La prueba utilizada fue:

```text
TestListarGarantias_FiltrosValidos_LleganTipadosAlRepo
```

Entre los valores verificados estuvieron:

```text
estado=en_proceso
origen=cliente
cliente_id=25
etapa=en_taller
ubicacion=taller
limit=50
```

También se verificó la conversión de fechas a UTC.

## OpenAPI

La prueba:

```text
TestOpenAPI_PathsRegistered
```

verifica que el documento OpenAPI incluya:

```text
/garantias
/garantias/{id}
listar-garantias
obtener-garantia
bearerAuth
```

## E2E HTTP → Firebird

La prueba E2E ejecutó el flujo completo:

```text
HTTP
→ garhttp
→ app.Consultas
→ garfb
→ Firebird
```

Se ejecutó contra:

```text
/firebird/data/MSPTEST.FDB
```

Comando utilizado:

```text
FB_HOST=localhost \
FB_PORT=3050 \
FB_DATABASE=/firebird/data/MSPTEST.FDB \
FB_USER=SYSDBA \
FB_PASSWORD=masterkey \
FB_CHARSET=UTF8 \
go test -v -count=1 \
  -run TestGarHTTP_E2E_BandejaFiltradaConFirebird \
  ./internal/garantias/infra/garhttp
```

Resultado:

```text
=== RUN   TestGarHTTP_E2E_BandejaFiltradaConFirebird
2026/10/13 10:20:04 INFO firebird: connected host=localhost database=/firebird/data/MSPTEST.FDB charset=UTF8 pool_size=5
--- PASS: TestGarHTTP_E2E_BandejaFiltradaConFirebird (0.26s)
PASS
ok github.com/abdimuy/msp-api/internal/garantias/infra/garhttp 0.259s
```

La prueba sembró 3 garantías dentro de:

```text
fbtestutil.WithTestTransaction
```

Dos garantías quedaron en estado:

```text
en_proceso
```

y una permaneció abierta.

La petición utilizada fue:

```text
GET /garantias?estado=en_proceso&limit=20
```

El resultado devolvió exactamente las dos garantías esperadas y en el orden correspondiente a la bandeja.

Todas las escrituras fueron revertidas al finalizar la prueba.

## Cobertura final

Se ejecutó:

```text
go clean -testcache
go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...
```

Resultado:

```text
ok github.com/abdimuy/msp-api/internal/garantias                         1.013s coverage: 100.0% of statements
ok github.com/abdimuy/msp-api/internal/garantias/app                     1.013s coverage: 100.0% of statements
ok github.com/abdimuy/msp-api/internal/garantias/domain                  1.033s coverage: 100.0% of statements
ok github.com/abdimuy/msp-api/internal/garantias/infra/garfb             5.552s coverage: 86.3% of statements
ok github.com/abdimuy/msp-api/internal/garantias/infra/garhttp           1.173s coverage: 92.6% of statements
ok github.com/abdimuy/msp-api/internal/garantias/infra/identidad         1.013s coverage: 100.0% of statements
ok github.com/abdimuy/msp-api/internal/garantias/infra/storage           1.029s coverage: 85.5% of statements
ok github.com/abdimuy/msp-api/internal/garantias/ports/outbound          1.013s coverage: 100.0% of statements
```

### Consultas

```text
NewConsultas       100.0%
Bandeja            100.0%
Detalle            100.0%
autorizarLectura   100.0%
```

### Identidad

```text
New                100.0%
UsuarioActual      100.0%
TienePermiso       100.0%
```

### HTTP

Cobertura total de `garhttp`:

```text
92.6%
```

Los mappers HTTP principales quedaron cubiertos completamente:

```text
garantiaDTO       100.0%
articuloDTO       100.0%
eventoDTO         100.0%
formatTime        100.0%
stringOpcional    100.0%
```

### Firebird

Cobertura total de `garfb`:

```text
86.3%
```

## Verificación sin Firebird

También se ejecutó la suite sin `FB_DATABASE`:

```text
env -u FB_DATABASE \
go test -race -count=1 -p 1 ./internal/garantias/...
```

Resultado:

```text
ok github.com/abdimuy/msp-api/internal/garantias
ok github.com/abdimuy/msp-api/internal/garantias/app
ok github.com/abdimuy/msp-api/internal/garantias/domain
ok github.com/abdimuy/msp-api/internal/garantias/infra/garfb
ok github.com/abdimuy/msp-api/internal/garantias/infra/garhttp
ok github.com/abdimuy/msp-api/internal/garantias/infra/identidad
ok github.com/abdimuy/msp-api/internal/garantias/infra/storage
ok github.com/abdimuy/msp-api/internal/garantias/ports/outbound
```

Los tests que requieren Firebird se omiten cuando `FB_DATABASE` no se encuentra definida.

## Mutation testing manual

Se realizaron los tres mutantes requeridos para la Entrega 2.

| Mutante | Cambio | Resultado |
|---|---|---|
| 6 | Eliminar la comprobación de `garantias:leer` | Killed |
| 7 | Permitir que `Identidad` llame a `PermisoFn` con un permiso inválido | Killed |
| 8 | Exponer `err.Error()` en un error HTTP 500 | Killed |

### Mutante 6

Se eliminó temporalmente la comprobación de permiso dentro de `autorizarLectura`.

La suite falló, entre otras, en:

```text
TestConsultas_Bandeja_SinPermisoNoTocaRepos
TestConsultas_Detalle_SinPermisoNoTocaRepos
TestConsultas_Bandeja_ErrorDePermisoSePropaga
TestConsultas_Bandeja_CaminoFeliz
TestConsultas_Detalle_CaminoFeliz
```

Resultado:

```text
FAIL
github.com/abdimuy/msp-api/internal/garantias/app
```

Después se restauró el archivo original:

```text
ok github.com/abdimuy/msp-api/internal/garantias/app
```

### Mutante 7

Se eliminó temporalmente la validación del permiso antes de llamar a `PermisoFn`.

La prueba que detectó el defecto fue:

```text
TestIdentidad_TienePermiso_InvalidoNoLlamaFuncion
```

Resultado:

```text
FAIL
github.com/abdimuy/msp-api/internal/garantias/infra/identidad
```

Después de restaurar:

```text
ok github.com/abdimuy/msp-api/internal/garantias/infra/identidad
```

### Mutante 8

Se modificó temporalmente el manejo de un error desconocido para devolver:

```text
err.Error()
```

La prueba que detectó la fuga fue:

```text
TestListarGarantias_ErrorInterno_500NoFiltraMensaje
```

El test detectó que la respuesta estaba exponiendo:

```text
firebird: password=super-secreto tabla MSP_GA_GARANTIA
```

Resultado:

```text
FAIL
github.com/abdimuy/msp-api/internal/garantias/infra/garhttp
```

Después de restaurar:

```text
ok github.com/abdimuy/msp-api/internal/garantias/infra/garhttp
```

Los tres mutantes quedaron eliminados por las pruebas.

## Gates finales

Se ejecutaron los comandos requeridos:

```text
gofmt -l internal/garantias
```

Resultado:

```text
sin salida
```

```text
go vet ./internal/garantias/...
```

Resultado:

```text
sin salida
```

```text
go build ./...
```

Resultado:

```text
sin salida
```

```text
golangci-lint run ./internal/garantias/...
```

Resultado:

```text
0 issues.
```

```text
make check-sealed MODULE=garantias
```

Resultado:

```text
✔ garantias is sealed
```

## Resultado final

La Tarea 7 quedó verificada con:

```text
app.Consultas     100.0%
identidad         100.0%
garhttp            92.6%
garfb              86.3%
golangci-lint      0 issues
go vet             OK
go build           OK
race tests         PASS
E2E Firebird       PASS
check-sealed       PASS
mutantes 6,7,8     Killed
```

## Desviaciones

No se modificaron archivos fuera de los permitidos por la Tarea 7.

No se realizaron cambios en dominio, puertos, migraciones ni `cmd/api`.

La prueba E2E utiliza `MSPTEST.FDB` y todas las escrituras están dentro de `fbtestutil.WithTestTransaction`, por lo que se revierten al finalizar.
