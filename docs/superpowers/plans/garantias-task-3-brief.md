# Garantías — Tarea 3: catálogos del artículo y tabla de transiciones

> **Rama:** `feat/garantias-base` (la misma de la tarea 2, encima de lo ya entregado)
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md)
> **Tanda:** 0.3b — completa el paquete `domain/` salvo las entidades
> **Plazo:** entrega el **martes 11 al final de tu jornada**. Ver el calendario al final.

## Dónde encaja

La migración `000050` creó tres columnas cuyo conjunto de valores válidos **hoy no está definido en ningún lado de Go**: `MSP_GA_ARTICULO.ETAPA`, `.UBICACION` y `.DESENLACE`. La regla dura #1 de `CLAUDE.md` dice que la regla canónica vive en el dominio, no en la base — así que mientras esos catálogos no existan, esas columnas admiten cualquier cosa.

Esta tarea los define, y además escribe la tabla de transiciones de etapa, que es la pieza central del módulo. El spec §4.5 explica por qué está en un solo archivo y no repartida en condicionales:

> Cuando el levantamiento en taller obligue a cambiarlo, el cambio debe ser **editar una tabla en Go y correr los tests** — sin migración, sin datos que tocar, sin `CHECK` en la base que actualizar.

Sin esto no se pueden escribir las entidades, y sin entidades no se abre nada de la tanda 1. Es la ruta crítica del módulo.

---

## Leer antes de escribir (obligatorio, en este orden)

1. **`docs/module-standards/02-value-objects-errors.md`** — define las tres categorías de value object. Esta tarea usa la **Categoría 1 (Enum VO)** para los tres catálogos y la **Categoría 2 (State VO)** como referencia para el mapa. Léelo completo antes de escribir la primera línea.
2. **`internal/ventas/domain/tipo_venta.go`** — el molde de un Enum VO. **Éste es el molde, no `inventario/domain/tipo_movimiento.go`.** El brief de la tarea 2 te mandó a ese último y estaba mal: es el único enum del repositorio escrito con la forma equivocada, y de ahí salió la reconversión que acabas de hacer. El error fue mío, no tuyo, pero no lo repitas.
3. **`docs/module-standards/02-value-objects-errors.md`, sección "VO Categoría 2 — State VO"** — de ahí sale la forma de `transiciones.go`: el mapa, `CanTransitionTo` e `IsTerminal`. La cita a `internal/ventas/domain/estado_registro.go` que traía ese apartado estaba mal —ese archivo no tiene mapa de transiciones— y ya se corrigió en `main`. Si la ves en algún lado, ignórala.
4. **El spec, secciones §3.2, §4.2, §4.3 y §4.5.** El §4.2 tiene los diagramas de los que sale el mapa. Léelo con calma: es la parte que decide si esta entrega sirve.
5. **Tu propio `internal/garantias/domain/estado_folio.go`**, ya entregado. Es el **único ejemplo funcionando de un mapa de transiciones en todo el repositorio** — lo escribiste tú ayer. `transiciones.go` sigue exactamente esa forma, un nivel más arriba de complejidad: mismo `map[X][]X`, mismo `CanTransitionTo`, mismo `IsTerminal`.

---

## Entregable 1 — Los tres catálogos

Tres archivos, un Enum VO cada uno, con la forma de `tipo_venta.go`: `type X string`, constantes **tipadas**, `Parse{X}` / `IsValid` / `String`, más los ayudantes que se indican.

### `internal/garantias/domain/etapa.go` — `Etapa`

Diecinueve valores, del §4.2. Agrupados por fase (el agrupamiento es para que se lea, no una jerarquía):

| Fase | Valores |
|---|---|
| Tronco común | `registrado` · `pendiente_recoleccion` · `recolectado` · `en_revision` |
| Ruta proveedor | `orden_generada` · `enviado_proveedor` · `dictamen_recibido` · `reparado_proveedor` · `espera_respuesta_cliente` |
| Ruta taller | `en_taller` · `reparado_taller` |
| Convergencia | `cambio_autorizado` · `listo_entrega` · `entregado` · `reingresado_inventario` |
| Flujo paralelo | `standby` · `segunda_mano` · `desarmado` · `merma` |

Ayudantes: `EsTerminal()` — verdadero para `entregado`, `reingresado_inventario`, `segunda_mano`, `desarmado` y `merma`. `standby` **no** es terminal: es una sala de espera de la que se sale a los otros tres.

### `internal/garantias/domain/ubicacion.go` — `Ubicacion`

Ocho valores, del §4.3: `domicilio_cliente` · `en_transito` · `almacen_revision` · `taller` · `proveedor` · `almacen_segunda_mano` · `entregado` · `baja`.

Sin ayudantes.

> `ETAPA` y `UBICACION` son ortogonales a propósito (§3.2): un artículo puede estar en etapa `dictamen_recibido` y ubicación `proveedor` porque todavía no lo regresan. No hay relación entre ambos catálogos y **no** se debe escribir código que derive uno del otro.

### `internal/garantias/domain/desenlace.go` — `Desenlace`

Seis valores, del §3.2: `reparado` · `reemplazado` · `devuelto` · `segunda_mano` · `desarmado` · `merma`.

Sin ayudantes.

**Fuera de alcance de esta tarea:** quién fija el `Desenlace` y en qué momento. Notarás que tres de esos valores coinciden con etapas del flujo paralelo y que el endpoint `POST .../desenlace` (§6) solo menciona tres. Es correcto y no es contradicción: aquí solo se declara el catálogo. Las reglas de asignación son de la tarea de las entidades. **No escribas lógica que decida cuándo se fija.**

### Errores centinela

Tres nuevos en `internal/garantias/domain/errors.go`, siguiendo el patrón de los siete que ya escribiste — código en inglés snake_case, mensaje en español minúsculas sin punto final:

| Variable | Código | Mensaje |
|---|---|---|
| `ErrEtapaInvalida` | `warranty_stage_invalid` | `etapa inválida` |
| `ErrUbicacionInvalida` | `warranty_location_invalid` | `ubicación inválida` |
| `ErrDesenlaceInvalido` | `warranty_outcome_invalid` | `desenlace inválido` |

### Comprobación que debes hacer tú

Cada valor tiene que caber en su columna de la migración `000050`: `ETAPA VARCHAR(28)`, `UBICACION VARCHAR(28)`, `DESENLACE VARCHAR(16)`. Cuéntalos. Si alguno no cabe, **no cambies la migración** — avisa.

---

## Entregable 2 — `internal/garantias/domain/transiciones.go`

Un mapa de etapa → etapas permitidas, más la función que lo consulta. La forma sale de tu propio `estado_folio.go`:

```go
var validEtapaTransitions = map[Etapa][]Etapa{
    // …
}

// CanTransitionTo reporta si un artículo en la etapa e puede pasar a la etapa t.
func (e Etapa) CanTransitionTo(t Etapa) bool { … }
```

El mapa sale de los cuatro diagramas del §4.2: tronco común, ruta proveedor, ruta taller y cierre. **Transcríbelos, no los interpretes.** Si el diagrama no dibuja una flecha, esa transición no existe.

### Los tres puntos que el spec no cierra — ya decididos, no los adivines

El §4.2 deja tres cosas ambiguas. Van resueltas aquí para que no tengas que suponer:

**1. `espera_respuesta_cliente` no se puede volver a entrar.** Tiene exactamente tres salidas (`listo_entrega` si acepta, `cambio_autorizado` si no acepta, `standby` si no se pudo devolver) y ninguna entrada que no sea `dictamen_recibido` con dictamen `sin_falla`. Si el cliente responde después de que el artículo pasó a `standby`, eso **no** es una vuelta atrás: es una decisión de negocio que se resuelve fuera del mapa. El propio spec dice que esa salida existe *"para que el artículo no quede atorado indefinidamente"* — si se pudiera regresar, volvería a atorarse.

**2. `standby` tiene exactamente dos entradas:** desde `espera_respuesta_cliente` (rama *no se pudo devolver*) y desde `cambio_autorizado` (el artículo original cuando se autoriza un cambio físico). Ninguna otra etapa entra a `standby`.

**3. Las etapas terminales no tienen salidas.** `entregado`, `reingresado_inventario`, `segunda_mano`, `desarmado` y `merma` van en el mapa con lista vacía y comentario, igual que hiciste con `cerrado` y `cancelado` en `estado_folio.go`.

**Si encuentras una cuarta ambigüedad, pregunta antes de escribir los tests.** No la resuelvas por tu cuenta: un valor de más en un catálogo cerrado es de las cosas que no revientan y quedan mal para siempre.

---

## Pruebas

En `package domain_test`, caja negra, tabla-driven, con la misma estructura que ya usaste.

Para cada catálogo:
- `Test{X}_WireValues` — fija el literal de cada constante contra su string. **Esto no es opcional**: es lo que impide que un cambio accidental de constante corrompa la columna en silencio. Ya lo hiciste bien en la tarea 2; mantenlo.
- `TestParse{X}_HappyPath` y `TestParse{X}_RejectsInvalid`, con `errors.Is` contra el centinela concreto. Los casos inválidos incluyen cadena vacía, mayúsculas, espacios adosados y algún valor plausible pero falso.
- Para `Etapa`, la tabla completa de `EsTerminal()`: los 19 valores contra el resultado esperado.

Para `transiciones.go`:
- **Toda transición válida del mapa, afirmada una por una.** Sin recorrer el mapa en un bucle: si el test itera sobre la misma estructura que prueba, no prueba nada.
- **Transiciones inválidas representativas**, incluidas las tres decisiones de arriba: que no se pueda volver a `espera_respuesta_cliente`, que solo `espera_respuesta_cliente` y `cambio_autorizado` entren a `standby`, y que ninguna etapa terminal tenga salida.
- Que `CanTransitionTo` sobre una etapa desconocida devuelva `false` sin entrar en pánico.

Meta de cobertura: **≥ 99%** en `internal/garantias/domain` (`docs/module-standards/TESTING_REQUIREMENTS.md`). Hoy está en 100%; no la bajes.

---

## Archivos que puedes tocar

```
internal/garantias/domain/etapa.go
internal/garantias/domain/ubicacion.go
internal/garantias/domain/desenlace.go
internal/garantias/domain/transiciones.go
internal/garantias/domain/errors.go          (solo agregar los tres centinelas)
+ un _test.go por cada archivo nuevo
docs/superpowers/plans/garantias-task-3-report.md
```

**Cualquier cambio fuera de esa lista se rechaza sin revisar.** En particular: no toques la migración `000050`, no toques los siete value objects ya entregados, no toques `.golangci.yml`.

---

## Verification

Con la caché de tests limpia. Los seis tienen que devolver 0:

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go clean -testcache && go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/
go tool cover -func=cov.out | tail -1        # ≥ 99.0%
make check-sealed MODULE=garantias
```

`go clean -testcache` no es opcional: un `ok` cacheado no prueba que tu cambio corrió.

---

## Reporte

`docs/superpowers/plans/garantias-task-3-report.md`, con la salida **literal** de esos comandos pegada, y una sección que describa lo que entregaste de verdad. Si a la hora de escribirlo el reporte no coincide con el código, gana el código: corrige el reporte.

---

## Puntos de control

| Cuándo | Qué |
|---|---|
| **Lunes 10, fin de jornada** | Los tres catálogos entregados y **en verde**. Si a esa hora no están, avisa: significa que el mapa no cabe el martes y es mejor saberlo el lunes. |
| **Martes 11, a media jornada** | **Punto de control: `transiciones.go` escrito, antes de sus pruebas.** Manda el archivo. Es donde salen las preguntas, y contestarlas con el mapa en la mano cuesta minutos; después de cuarenta casos de prueba, cuesta rehacerlos. |
| **Martes 11, fin de jornada** | **Entrega.** |

El plazo sale de tu propio ritmo: la migración `000050` más los siete value objects con sus pruebas te tomaron dos días. Esto es del mismo tamaño.

Si te atoras más de dos horas en una sola cosa, avisa. Esa regla existe para que nadie queme dos días peleando con un linter.
