# Manejo de Codificación de Caracteres

Estándar **obligatorio** para cualquier código que toque strings persistidas en msp-api. Hasta la migración `000005_msp_tables_to_utf8` el proyecto tenía una frontera de codificación frágil entre Go (UTF-8) y Firebird (columnas ISO8859_1 con bytes WIN1252) que producía mojibake en GET y rechazos espurios al escribir caracteres válidos (em-dash, smart quotes, emoji). Esta guía codifica el modelo nuevo: **UTF-8 everywhere**.

> **Decisión de arquitectura:** las tablas que pertenecen a msp-api (las que comienzan con `MSP_`) usan `CHARACTER SET UTF8`. Las tablas que pertenecen a Microsip (`CLIENTES`, `ARTICULOS`, `COBROS`, etc.) conservan su charset legacy. La conexión del driver está en `charset=UTF8`.
>
> **Cuidado con la frase "Firebird transliterar automáticamente".** Es cierta para las columnas con un charset declarado (`ISO8859_1`, `UTF8`, `ASCII`) y **falsa** para las `CHARACTER SET NONE`, que son la mayoría del esquema de Microsip: ahí no hay charset de origen y el servidor entrega los bytes tal cual. Tratar a las dos igual —en cualquiera de los dos sentidos— es el defecto recurrente de este repositorio. Lea la sección "La regla que más se rompe" antes de tocar una lectura de Microsip.

---

## TL;DR — Las 3 reglas

1. **En Go (domain, app, infra):** todo `string` es UTF-8 válido en forma normalizada NFC. Sin excepción.
2. **Para tablas `MSP_*` (nuestras):** pasa `string` directo a `sql.Exec` / `sql.Scan`. Cero conversión manual.
3. **Para tablas Microsip (legacy):** vive detrás de un adapter dedicado. El adapter valida que los strings de salida quepan en el charset legacy (subset de WIN1252 sin chars del gap 0x80-0x9F si la columna es ISO8859_1) antes de escribir. **La lectura depende del charset DE LA COLUMNA, no de la tabla** — ver la sección siguiente, que es donde está el filo.

Si sigues estas tres reglas, los bugs de encoding desaparecen. Si te las saltas, regresan en silencio.

---

## La regla que más se rompe: Microsip NO tiene un solo charset

> Esta sección se escribió después de encontrar **23 sitios** leyendo columnas
> `ISO8859_1` con un doble-decode Windows-1252. El arreglo ya se había hecho una
> vez —commit `4a1050a fix(analytics)`, 21-jun— y **nunca se propagó**; un día
> antes, `17ea809 fix(clientes)` había movido clientes en la dirección contraria.

Decir «las tablas de Microsip son legacy, decodifícalas» es **falso** y es
exactamente el atajo que produjo el defecto. Medido sobre el esquema real
(`RDB$RELATION_FIELDS × RDB$FIELDS × RDB$CHARACTER_SETS`, sin tablas de sistema):

| Charset | Columnas |
|---|---|
| `NONE` | 2,365 |
| `ASCII` | 155 |
| `ISO8859_1` | 100 |
| `UTF8` | 81 |

Con la conexión en `charset=UTF8` (`FB_CHARSET`, en **todos** los entornos) esas
columnas se parten en dos familias con tratamiento **opuesto**:

| Familia | Qué hace Firebird | Destino de escaneo | Si te equivocas |
|---|---|---|---|
| `ISO8859_1` / `UTF8` / `ASCII` | **Transliterar** a UTF-8 en el cable | `string` / `sql.NullString` | `firebird.Win1252` decodifica una **segunda** vez: `Ñ` → `Ã‘` |
| `NONE` | **Nada** — no hay charset de origen del cual convertir | `firebird.Win1252` | Un `string` plano deja **bytes crudos**, UTF-8 inválido, en el dominio |

Verificado en vivo, columna por columna:

| Columna | charset | escaneo plano | escaneo `Win1252` |
|---|---|---|---|
| `ARTICULOS.NOMBRE` | `ISO8859_1` | `NIÑO` ✅ | `NIÃ‘O` ❌ |
| `CLIENTES.NOMBRE` | `ISO8859_1` | `FARIÑO` ✅ | `FARIÃ‘O` ❌ |
| `DIRS_CLIENTES.CALLE` | `NONE` | bytes crudos ❌ | `SEÑORES` ✅ |
| `CONCEPTOS_CC.NOMBRE` | `NONE` | bytes crudos ❌ | `Interés` ✅ |

**El charset NO se deduce del nombre de la columna.** `CIUDADES.NOMBRE` es
`ISO8859_1`; `ZONAS_CLIENTES.NOMBRE` es `NONE`. Tampoco de la tabla:
`CLIENTES.NOMBRE` es `ISO8859_1` y `CLIENTES.NOTAS` es `NONE`.

### Consecuencias que sí importan

- **`COALESCE(col_NONE, '')` revienta.** El literal `''` está en el charset de la
  conexión (UTF-8) y fuerza una coerción de los bytes crudos: `SQL error code =
  -303 / Malformed string`. Esa restricción es **real y exclusiva** de la familia
  `NONE`. Sobre una columna `ISO8859_1` el mismo `COALESCE` funciona.
- **`COALESCE(col_ISO8859_1, col_NONE)` resuelve a `ISO8859_1`.** Firebird toma
  el charset no-`NONE` del par y transliterar **las dos** ramas. Una expresión
  así se escanea con **un solo** `sql.NullString`; no hace falta partir el SQL ni
  meter un `CAST`. Verificado rama por rama.
- **El camino de escritura también cuenta.** Mandar un parámetro re-codificado a
  Windows-1252 por una conexión UTF-8 rompe la comparación: buscar `"NIÑO"` con
  `CONTAINING ?` fallaba con `Malformed string`. Los parámetros van como `string`
  de Go; Firebird transliterar hacia el charset de la columna.
- **No es cosmético.** El nombre corrompido se persiste en Meilisearch: buscar
  `FARIÑO` daba **0** resultados y `MUÃ‘OZ` daba **261**. Y en `ventas`, la clave
  normalizada del catálogo de ciudades no cruzaba con la tecleada, así que
  **ninguna ciudad acentuada se resolvía** — sin error.

### El `NULL` es la otra mitad de la decisión

El charset dice **cómo** se decodifica; el JOIN dice **si puede faltar**. Son
independientes y hay que acertar en las dos:

| | La columna no puede ser NULL | Puede ser NULL (LEFT JOIN, subconsulta escalar) |
|---|---|---|
| Transliterada | `string` | `sql.NullString` |
| `NONE` | `firebird.Win1252` | `firebird.Win1252` (mapea `nil` → `""`) |

El renglón peligroso es el de arriba a la derecha. `firebird.Win1252` **absorbe
el NULL en silencio**, así que mientras una columna `NONE` se leyó con él, el
`LEFT JOIN` nunca dolió; el día que se corrige el charset a `string` pelado, el
mismo `LEFT JOIN` devuelve `converting NULL to string is unsupported` y se cae
**la consulta entera**, no una celda.

Pasó exactamente aquí: `queryVentasPorZona` maneja
`LEFT JOIN CLIENTES c ON c.CLIENTE_ID = s.CLIENTE_ID` sobre `MSP_SALDOS_VENTAS`,
que **no tiene FOREIGN KEY** a `CLIENTES` (verificado en
`RDB$RELATION_CONSTRAINTS`: sólo PK y `NOT NULL`) y filtra por
`s.ZONA_CLIENTE_ID`, no por el cliente. En la base de desarrollo hay **80 filas
huérfanas**. Lo fija `TestCobranzaRepo_VentasPorZona_ClienteHuerfano`.

Antes de poner `string`, mire el `FROM`. `NOT NULL` en el catálogo no basta: lo
que decide es el tipo de JOIN.

### `COALESCE` sobre una columna `NONE`: medido rama por rama

| Expresión | Resultado (medido en vivo) |
|---|---|
| `COALESCE(col_NONE, '')` con valor **ASCII** | Funciona |
| `COALESCE(col_NONE, '')` con valor **acentuado** | `-303 Malformed string` — **muere la consulta entera** |
| `COALESCE(col_NONE, '')` con `NULL` | Funciona, devuelve `''` |
| `COALESCE(col_ISO8859_1, '')` acentuado | Funciona |
| `COALESCE(col_ISO8859_1, col_NONE)` | Resuelve a `ISO8859_1` y translitera **las dos** ramas → un solo `sql.NullString` |
| `COALESCE(col_NONE, col_ISO8859_1)` | Idéntico: **el orden no importa** |
| `COALESCE(col_NONE, col_NONE)` | Sigue siendo `NONE` → `firebird.Win1252` |

La consecuencia práctica es que un `COALESCE(col_NONE, '')` es una **mina que
depende de la fila**: verde durante años y 500 el día que entra el primer
acento. Había una viva en `queryPagos` sobre `FORMAS_COBRO.NOMBRE` — no había
disparado porque `FORMAS_COBRO_DOCTOS.FORMA_COBRO_ID` no empata con **ninguna**
fila de `FORMAS_COBRO` (157/158/52569/137026 contra 67/68/71/27773), así que la
subconsulta siempre daba `NULL`. Se quitó el `COALESCE`; lo fija
`TestClientesRepo_Acentos_FormaCobroEnHistorialDePagos`.

Sobre una columna `NONE`: o se lee **desnuda** y decodifica Go, o se castea en
el SQL a un charset declarado —`CAST(dc.DESCRIPCION AS VARCHAR(200) CHARACTER
SET WIN1252)`, como hace `cobranza/infra/ventfb`— y entonces el destino es un
`string`. Lo que no se puede es mezclarla con un literal.

### Cómo se sostiene esto ahora

`internal/platform/fbcharset` tiene el registro de cada columna de texto de
Microsip que leen los repositorios, con su familia. Cuatro pruebas lo mantienen
honesto:

| Prueba | Qué verifica |
|---|---|
| `TestCatalogoCharsets_ClasificacionDeColumnas` | La familia declarada contra `RDB$FIELDS`, columna por columna. |
| `TestCatalogoCharsets_ControlPositivoPorFamilia` | Comportamiento con datos reales: la familia `NONE` debe llegar como UTF-8 **inválido**; la transliterada, como **válido**. |
| `TestBarridoDeFuentes_TodoWin1252EstaClasificado` | Recorre el código: todo destino `firebird.Win1252` tiene que estar anotado con su columna, y esa columna registrada como `NONE`. |
| `TestBarridoInverso_CadaColumnaNONETieneQuienLaDecodifique` | El sentido contrario: toda columna registrada como `NONE` tiene que conservar al menos un sitio que la decodifique (`firebird.Win1252` anotado, o un `CAST(... CHARACTER SET WIN1252)`). Cambiar ese destino a `string` la deja huérfana y la prueba falla nombrándola. |

**Lo que sigue sin cubrirse, dicho sin adornos:** una columna `NONE` que **nadie
registró nunca** y se lee como `string` plano. Ninguna de las cuatro la ve — un
`string` es el destino de casi todo y no hay nada en la sintaxis que lo
distinga. El barrido inverso sólo cierra el caso de las que **ya** están en el
registro. Si agrega una columna a una consulta, regístrela.

Residuo medido de ese hueco, hoy: `DOCTOS_CC.FOLIO` (`NONE`, `NOT NULL`) se lee
como `string` en `clientesfb`, y `DOCTOS_PV.FOLIO` (`NONE`) también en
`cobranza/infra/ventfb` y detrás de un `COALESCE(..., '')`. Es inofensivo
**porque está medido**: 0 filas con no-ASCII sobre 2,271,281 `DOCTOS_CC` y sobre
todos los `DOCTOS_PV`; el folio lo genera el sistema como `SERIE` + consecutivo.
Deja de serlo el día que alguien configure una serie acentuada en
`FOLIOS_CAJAS`. Si toca esas consultas, arréglelo entonces.

### Los fixtures llevan acentos, y no es opcional

Ninguna prueba atrapó el defecto durante meses por una sola razón: **ningún
fixture tenía una `Ñ`**. Sobre ASCII puro la lectura correcta y el doble-decode
dan exactamente los mismos bytes. Una sola eñe en `microsipseed.Cliente` habría
hecho fallar los 23 sitios.

- Los nombres que siembra `internal/platform/microsipseed` llevan acentos.
- `microsipseed.CobradorConNombre` y `microsipseed.ZonaConNombre` **exigen** que
  el nombre traiga no-ASCII.
- `microsipseed.ArticuloConAcento` y `microsipseed.FormaCobroAcentuada` eligen a
  propósito una fila acentuada del catálogo: sólo 121 de 6,113 artículos y 1 de
  4 formas de cobro lo son, y dejarlo al azar da ~2% de cobertura.
- **Sembrar en una columna `NONE` exige `firebird.EncodeWin1252`.** Mandarle
  UTF-8 deja una semilla FALSA: la lectura correcta parece rota y la incorrecta
  parece buena. Lo hacen así `DOCTOS_CC.DESCRIPCION`, `DIRS_CLIENTES.TELEFONO1`
  y `ZONAS_CLIENTES.NOMBRE`.

Dos columnas no tienen **ni una** fila acentuada en la base
(`ZONAS_CLIENTES.NOMBRE`, 0 de 46; `DIRS_CLIENTES.TELEFONO1`, 0 de 43,835), así
que ninguna comparación contra datos reales las verifica: pasan igual con el
destino de escaneo equivocado —comprobado rompiéndolas—. Su control positivo
tiene que **sembrar** el acento, y es lo que hacen
`TestConfigRepo_Acentos_ZonaSembrada` y
`TestClientesRepo_Acentos_TelefonoEsColumnaNONE`.

---

## Glosario

| Término | Definición |
|---|---|
| **UTF-8** | Codificación variable-byte (1-4 bytes/codepoint) compatible con ASCII. La interna de Go (`string` siempre es UTF-8 si el código está bien escrito). |
| **NFC** | Unicode Normalization Form C. La forma "compuesta" canónica: `é` = U+00E9 (1 codepoint, 2 bytes) en lugar de `e` + U+0301 (2 codepoints, 3 bytes). Indispensable para que comparaciones byte-equal funcionen. |
| **WIN1252 / ISO8859_1** | Codificaciones 8-bit legacy. WIN1252 es superset de ISO8859_1 (llena el gap 0x80-0x9F con puntuación: em-dash, smart quotes, €). Microsip usa estas para sus columnas porque fue escrito en Delphi/Windows pre-Unicode. |
| **Transliteración** | Conversión automática que hace Firebird entre el charset de la columna y el charset de la sesión. Si una columna ISO8859_1 recibe un byte 0x97 (em-dash en WIN1252, undefined en ISO8859_1), Firebird devuelve error SQLSTATE 22021. |
| **Frontera Microsip** | El adapter en `internal/microsip/...` (o equivalente) que media reads y writes a tablas legacy. Es el ÚNICO lugar donde se aplica validación charset-aware. |

---

## Por qué este modelo

Tres restricciones que llevaron a este diseño:

1. **Microsip no se puede modificar.** Sus tablas tienen su charset histórico (ISO8859_1) y el cliente Windows escribe bytes WIN1252 directamente. Cambiarlas rompería al cliente Delphi.

2. **UTF-8 es el consenso 2026.** Postgres, MySQL `utf8mb4`, SQL Server `_UTF8`, MongoDB, etc. — toda la industria moderna asume UTF-8 internamente. Mantener un charset legacy en nuestro lado nos aísla del ecosistema y reintroduce las clases de bug que UTF-8 elimina.

3. **Firebird transliterar gratis.** Cada columna conoce su charset; el server traduce al/del charset de la sesión sin código de aplicación. Un solo `SELECT MSP_VENTAS.NOMBRE_CLIENTE, CLIENTES.NOMBRE FROM MSP_VENTAS JOIN CLIENTES ...` devuelve ambas columnas como UTF-8 al driver. **Eso significa que la frontera vive en el server, no en Go.**

La solución: forzar **UTF-8 en columnas nuestras + sesión + dominio**. Microsip queda aislada en un adapter que conoce sus limitaciones.

---

## Helpers canónicos

Definidos en `internal/ventas/domain/safe_string.go` (y futuros equivalents en otros módulos):

```go
// validateSafeChars rechaza NUL byte, ASCII control chars (excepto \t \n \r)
// e invalid UTF-8. Acepta todo lo demás — accents, emoji, CJK, em-dash.
func validateSafeChars(s string) error

// normalizeNFC devuelve s en Unicode NFC (forma compuesta canónica).
func normalizeNFC(s string) string

// requireBounded trims, normaliza NFC, valida charset-safety y mide la
// longitud en CODEPOINTS (no bytes) contra el límite — alineado con la
// declaración VARCHAR(N) CHARACTER SET UTF8.
func requireBounded(s string, maxLen int, errRequired, errTooLong error) (string, error)

// trimOptionalBounded es la variante para campos opcionales (pointer in / out).
func trimOptionalBounded(p *string, maxLen int, errTooLong error) (*string, error)
```

`firebird.Win1252` (el Scanner) **NO** debe usarse para columnas `MSP_*`, ni para columnas de Microsip declaradas `ISO8859_1`/`UTF8`/`ASCII`. Su único uso correcto es una columna de Microsip **`CHARACTER SET NONE`** — ver "La regla que más se rompe" arriba. Todo callsite tiene que estar anotado con su columna y registrado en `internal/platform/fbcharset`, o falla `TestBarridoDeFuentes_TodoWin1252EstaClasificado`.

`firebird.EncodeWin1252` (la función) **NO** debe llamarse para escribir a columnas `MSP_*`. Igual reservada para el adapter Microsip.

---

## Layer por layer — qué hacer

### Domain

```go
type Nota struct{ value string }

func NewNota(s string) (Nota, error) {
    v, err := requireBounded(s, maxNotaLen, ErrNotaRequerida, ErrNotaTooLong)
    if err != nil {
        return Nota{}, err
    }
    return Nota{value: v}, nil
}
```

El VO normaliza NFC, mide en codepoints, y aplica `validateSafeChars`. Todos los strings que pasan por aquí están garantizados UTF-8 NFC sin chars de control.

### App

No toques strings. Pasalos del DTO al domain como recibiste y deja que el VO los normalice. Si tu app necesita comparar strings entre input del usuario y persistido (p.ej. búsqueda case-insensitive), aplica `strings.EqualFold` o normaliza por tu cuenta — pero el almacenamiento canónico es NFC.

### Infra — tablas `MSP_*` (nuestras)

```go
// Write
q.ExecContext(ctx, insertVenta,
    v.ID().String(),
    v.Cliente().Nombre().Value(),   // pass string directo — UTF8 column lo acepta
    v.Direccion().Calle(),           // mismo
    nullableStringArg(v.Nota()),     // nil → SQL NULL, else *s
    ...)

// Read
var (
    nombreCliente string
    nota          sql.NullString
)
row.Scan(&idRaw, &nombreCliente, &nota, ...)
// nombreCliente es UTF-8 NFC, listo para el dominio
```

### Infra — tablas Microsip (legacy)

```go
// internal/microsip/adapter.go (futuro)

// Read: passthrough — el driver transliterar columna legacy → UTF-8.
var nombre string
row.Scan(&nombre)
return nombre // UTF-8 NFC garantizado

// Write: validar primero que el string quepa en el charset destino.
if err := microsip.ValidateWritable(nombre); err != nil {
    return apperror.NewValidation("microsip_unwritable_chars",
        "el texto contiene caracteres que Microsip no acepta").WithError(err)
}
q.ExecContext(ctx, insertClienteMicrosip, ..., nombre, ...)
// Firebird transliterar UTF-8 → ISO8859_1 al escribir. Si nombre tiene
// un em-dash, ValidateWritable ya lo habría rechazado.
```

---

## Reglas duras

**Hacer:**
- ✅ Declarar columnas nuevas en `MSP_*` como `CHARACTER SET UTF8`.
- ✅ Pasar `string` Go directo en `sql.Exec` / `sql.Scan`.
- ✅ Aplicar `requireBounded` / `trimOptionalBounded` en cada VO de string.
- ✅ Medir longitudes en codepoints (`utf8RuneLen(s)`), no bytes (`len(s)`).
- ✅ Si necesitas escribir a Microsip, hazlo via un adapter que aplique `microsip.ValidateWritable` antes.

**No hacer:**
- ❌ Llamar `firebird.EncodeWin1252` fuera del adapter Microsip.
- ❌ Usar el tipo `firebird.Win1252` como Scanner. Doble-decode → mojibake.
- ❌ Crear columnas nuevas con `CHARACTER SET ISO8859_1` o WIN1252 en tablas `MSP_*`.
- ❌ Validar contra "WIN1252 representable" en el dominio. Eso era el guard viejo y rechazaba caracteres legítimos (em-dash, emoji).
- ❌ Comparar strings sin pasar por NFC. `"é" == "e + U+0301"` es `false` byte-a-byte; ambos parecen iguales en pantalla.

---

## Cómo funciona la coexistencia

```
                          ┌──────────────────────────────┐
                          │       Go (UTF-8 NFC)         │
                          └──────────────┬───────────────┘
                                         │
                          ┌──────────────▼───────────────┐
                          │  Driver firebirdsql          │
                          │  charset=UTF8                │
                          └──────┬──────────┬────────────┘
                                 │          │
                ┌────────────────▼────┐  ┌──▼────────────────────┐
                │ MSP_* (CHARACTER    │  │ Microsip CLIENTES,     │
                │ SET UTF8)           │  │ ARTICULOS, COBROS      │
                │                     │  │ (CHARACTER SET         │
                │ passthrough         │  │ ISO8859_1 / WIN1252)   │
                │ ↕ no transcoding    │  │                        │
                └─────────────────────┘  │ ↕ Firebird transliterar │
                                         │   server-side          │
                                         └────────────────────────┘
```

Lectura cross-tabla: un join `MSP_VENTAS JOIN CLIENTES` con columnas de texto de ambos lados devuelve UTF-8 al driver. Cero código de aplicación.

Escritura: a tablas nuestras pasa verbatim. A tablas Microsip se transliterar UTF-8 → ISO8859_1 server-side; si el char no cabe, Firebird devuelve error SQLSTATE 22021 — por eso el adapter Microsip valida primero (`ValidateWritable`) para devolver 422 limpio en vez de 500.

---

## Cómo verificar que el contrato se respeta

```bash
# Test E2E que pinea el contrato — corre con FB_DATABASE seteado:
FB_DATABASE=/firebird/data/MUEBLERA.FDB \
  go test -count=1 -run TestE2E_Encoding -v ./internal/ventas/infra/venthttp/...
```

Cubre:
- Spanish accents round-trip byte-equal
- WIN1252 punctuation (em-dash, smart quotes, €) round-trip byte-equal
- Emoji round-trip byte-equal
- Cyrillic, CJK round-trip byte-equal
- NUL byte rejected con 422 `string_unsafe_chars`
- ASCII control char rejected con 422 `string_unsafe_chars`
- nota de 400 codepoints multibyte round-trip
- PATCH con emoji + accents round-trip

Si cualquiera de estos falla, alguien está re-introduciendo la frontera vieja. El test es la regresión-defense.

---

## Si tienes que cambiar esto

No.

Si crees que tienes que cambiarlo:
- ¿Es para escribir a Microsip? Pon la validación en el adapter Microsip, no rebajes la regla en `MSP_*`.
- ¿Es para "optimizar storage"? UTF-8 es ~equivalente a WIN1252 en bytes para texto en español; los chars frecuentes (a-z) ocupan 1 byte igual. No hay optimización real.
- ¿Para compatibilidad con un cliente que no entiende UTF-8? Ese cliente está roto. UTF-8 es ASCII-compatible.

Si después de todo eso sigues creyendo que hay que cambiar el modelo, abre un ADR y discute con el equipo antes de tocar código.
