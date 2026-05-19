# Manejo de Codificación de Caracteres

Estándar **obligatorio** para cualquier código que toque strings persistidas en msp-api. Hasta la migración `000005_msp_tables_to_utf8` el proyecto tenía una frontera de codificación frágil entre Go (UTF-8) y Firebird (columnas ISO8859_1 con bytes WIN1252) que producía mojibake en GET y rechazos espurios al escribir caracteres válidos (em-dash, smart quotes, emoji). Esta guía codifica el modelo nuevo: **UTF-8 everywhere**.

> **Decisión de arquitectura:** las tablas que pertenecen a msp-api (las que comienzan con `MSP_`) usan `CHARACTER SET UTF8`. Las tablas que pertenecen a Microsip (`CLIENTES`, `ARTICULOS`, `COBROS`, etc.) conservan su charset legacy (ISO8859_1 / WIN1252 según corresponda). La conexión del driver está en `charset=UTF8` y Firebird transliterar automáticamente al cruzar columnas con charsets distintos.

---

## TL;DR — Las 3 reglas

1. **En Go (domain, app, infra):** todo `string` es UTF-8 válido en forma normalizada NFC. Sin excepción.
2. **Para tablas `MSP_*` (nuestras):** pasa `string` directo a `sql.Exec` / `sql.Scan`. Cero conversión manual.
3. **Para tablas Microsip (legacy):** vive detrás de un adapter dedicado. El adapter valida que los strings de salida quepan en el charset legacy (subset de WIN1252 sin chars del gap 0x80-0x9F si la columna es ISO8859_1) antes de escribir. La lectura es passthrough: Firebird transliterar y el driver entrega UTF-8.

Si sigues estas tres reglas, los bugs de encoding desaparecen. Si te las saltas, regresan en silencio.

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

`firebird.Win1252` (el Scanner) **NO** debe usarse para columnas `MSP_*`. Se conserva en `internal/platform/firebird/` sólo para uso futuro en el adapter Microsip; cualquier nuevo callsite fuera de ese adapter es un bug.

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
