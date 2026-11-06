# Garantías — Tarea 12: lo que quedó de la tarea 10 y las pantallas de oficina

> **Repos:** dos. La parte 1 va en `msp-api`; la parte 2 en `sistema-cobro-web` (la app de escritorio).
> **Ramas:**
> - `msp-api`: `feat/garantias-t12-pendientes`, que creas tú desde `main` **una vez fusionado el PR #33** (`git fetch && git switch -c feat/garantias-t12-pendientes origin/main`). PR contra `main`.
> - `sistema-cobro-web`: `feat/garantias-oficina`, que creas tú desde `main` (`git fetch && git switch -c feat/garantias-oficina origin/main`). PR contra `main`.
>
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md): **§4** (estados y etapas), **§6** (idempotencia, imágenes y firma) y **§7** (permisos).
> **Plazo:** **una sola entrega, viernes 13 de noviembre, 1pm**, con las dos partes completas, los dos PR abiertos y el reporte. Todo se entrega al final, de una vez: no hay entregas parciales, avances obligatorios ni revisiones intermedias. No hay holgura.

## Qué construyes

Al terminar, el módulo de garantías cumple **todo** lo que pedía la tarea 10, y oficina puede llevar un folio de garantía de punta a punta desde el escritorio: ver la bandeja, abrir un folio, ver su historia con fotos y firma, y registrar cada decisión de oficina.

| Parte | Repo | Contenido | Orden sugerido |
|---|---|---|---|
| **1** | `msp-api` | Catálogos, prueba de arranque, punta a punta de la subida, controles que faltan y el reporte de la tarea 10 | Primero |
| **2** | `sistema-cobro-web` | Bandeja, detalle y comandos de oficina de garantías | Después |

La parte 2 usa `GET /garantias/catalogos`, que haces tú en la parte 1. Por eso va primero.

### Antes que nada

1. **El acceso a `abdimuy/sistema-cobro-web` se te da el lunes 9.** Mientras tanto avanzas la parte 1, que no depende de ese repo.
2. **El #33 se fusiona a `main` antes de que empieces la parte 1.** Si al empezar no está en `main`, avísame ese mismo día. No arranques desde `feat/garantias-integracion` ni desde otra rama.
3. **Instala los hooks en los dos repos antes del primer commit:** `lefthook install` en `msp-api`. En el #33 entraron dos commits `merge: actualize base de garantias` que el hook `commit-msg` habría rechazado (`merge` no es un tipo permitido). Ni `--no-verify` ni commits que el hook no aceptaría.
4. **Tu base local es la única donde se escribe.** Las migraciones 70 y 71 se aplican en la base de **tu** contenedor de `msp-api`. Nunca en una base compartida ni en el servidor de pruebas, y nada de producción.

---

## Leer antes de escribir

**Para la parte 1 (`msp-api`):**

1. Tu propio brief de la tarea 10 (`docs/superpowers/plans/garantias-task-10-brief.md`), **D5, D6 y D7** y la sección "Reporte". Esta tarea cierra lo que de ahí quedó sin entregar.
2. `internal/garantias/infra/garhttp/security_test.go`, en especial la comparación contra `api.OpenAPI().Paths` (alrededor de la línea 331).
3. `cmd/api/garantias_wiring_test.go` y `cmd/api/server.go`, el grupo de garantías.
4. `internal/garantias/infra/garhttp/e2e_comandos_test.go`: la prueba de punta a punta de Kevin contra `garfb`, con `WithTestTransaction`. La tuya sigue esa forma.
5. `internal/garantias/infra/garfb/bandeja_repo_test.go` (`FiltroEtapaYUbicacionMismoArticulo`) y `read_tx_test.go`.
6. `internal/garantias/domain`: `Etapas()`, `Ubicaciones()` y `SiguientesPorAvanzar`. De ahí sale todo el catálogo.

**Para la parte 2 (`sistema-cobro-web`):**

1. `src/modules/desempeno/`, completo. Es la referencia de arquitectura: `domain/`, `application/ports` y `usecases`, `infrastructure/http` (`apiClient.ts`, `HttpDesempenoAdapter.ts`, `dtos.ts`) y `mappers`, `presentation/composition/DesempenoContainer.tsx`, `context/` y `hooks/` con `useCargaRemota`. Y sus pruebas, con `FakeDesempenoPort`.
2. `src/modules/desempeno/infrastructure/mappers/errorMapper.ts`: cómo se lee el `detail` en español y el código de error que manda `msp-api`. También `normalizarErrorBlob`, para las respuestas de error cuando se pidió un `Blob`.
3. `src/modules/desempeno/infrastructure/http/HttpDesempenoAdapter.ts` (alrededor de la línea 131, `responseType: "blob"`) y `src/modules/failedIntents/components/VisorDeImagenes.tsx`: cómo se baja una imagen con token y se muestra con `URL.createObjectURL`, y cómo se libera.
4. `src/modules/bandeja/presentation/hooks/useAccionesBorrador.ts`: comandos desde una pantalla, con la guarda contra doble clic en una `ref` y el aviso con `sonner`.
5. `src/modules/failedIntents/components/ConfirmarAccionDialog.tsx`: el diálogo de confirmación del repo.
6. `src/constants/modules.ts`, `src/App.tsx` y `src/utils/permissions.ts`: cómo se registra una pantalla y su interruptor `MODULOS_DESKTOP`.
7. `src/utils/tiempoDeNegocio.ts` y el comentario de `vitest.config.ts` sobre la zona horaria: las fechas se anclan a la zona del negocio, y la suite corre en UTC a propósito.
8. `src/test/msw/` y `src/test/fronterasDeModulos.test.ts`.
9. Del lado del API, en `msp-api`: `internal/garantias/infra/garhttp/dto.go`, `dto_comandos.go`, `dto_imagenes.go`, `routes.go`, `routes_comandos.go` e `idempotencia.go`. Y `internal/garantias/app/huella.go`: **qué campos entran en la huella** (abajo, decisión E4).

---

## Las decisiones ya tomadas — no se re-litigan

Si alguna te parece equivocada, dilo **antes** de escribir las pruebas.

### Parte 1

#### P1. `GET /garantias/catalogos` (`obtener-catalogos`)

Es la D5 de la tarea 10, sin cambios:

- Permiso **`leer`**.
- Devuelve `etapas` y `ubicaciones` en el orden de `domain.Etapas()` y `domain.Ubicaciones()`, los estados de folio, los tipos de evento y `transiciones` por origen y etapa, sacadas de `domain.SiguientesPorAvanzar`.
- Nada escrito a mano: todo sale del dominio. Una prueba recorre `domain.Etapas()` y compara contra la respuesta, de modo que una etapa nueva en el dominio aparece sola o pone la prueba en rojo.
- Archivos: `handlers_catalogos.go` y `dto_catalogos.go`, y el registro en `routes.go`.

Con esto el OpenAPI montado queda con **diecinueve** operaciones.

#### P2. El barrido de seguridad cuenta contra un número fijo

Hoy `security_test.go` compara 18 contra 18 y pasa: la tabla y el OpenAPI pueden perder la misma fila y la prueba no se entera.

- Agrega la fila de `obtener-catalogos`.
- Agrega una aserción contra la constante `operacionesEsperadas = 19`, además de la comparación tabla contra OpenAPI.
- Agrega la lista de los diecinueve `operationId` escrita en la prueba, comparada contra el OpenAPI. Una operación que falte o sobre la pone en rojo.

#### P3. Prueba de arranque sobre el router real

Va en `cmd/api/garantias_wiring_test.go`. Construye el router con la misma función que usa `server.go`, no una copia. Si para eso hace falta extraer la construcción del router a una función, se extrae y se declara en el reporte. Sobre ese router:

- `GET /v2/garantias` sin token → **401**.
- `/v2/openapi.json` (o donde quede montado el OpenAPI de garantías) trae las **diecinueve** operaciones, por `operationId`.
- **Sin el middleware `idem`:** un `POST` de garantías repetido con la misma `Idempotency-Key` **no** trae el encabezado `Idempotent-Replay`. Esto sustituye la lectura del texto de `server.go` (líneas 140–147 de la prueba actual), que no detecta un segundo `r.Use(idem)` ni un `r.With(idem)`. Esa lectura se borra.

#### P4. Punta a punta de la subida contra Firebird

Va en `garhttp/e2e_imagenes_test.go`, contra `garfb` real, dentro de `WithTestTransaction`:

1. Siembra un folio y un evento.
2. Sube una imagen por `POST /garantias/{id}/eventos/{eid}/imagenes`.
3. Lee el detalle por `GET /garantias/{id}`: la imagen aparece en ese evento con `tipo = "evidencia"`, sin huella.
4. Descárgala por la `url` que devolvió la subida: el `Content-Type` y los bytes son **exactamente** los que subiste.
5. Repite la subida con la misma clave y los mismos bytes: misma imagen, una sola fila contada en `MSP_GA_IMAGEN` para ese evento.

#### P5. Los controles que faltan

- **`FiltroEtapaYUbicacionMismoArticulo`, con control positivo.** Siembra también un folio con **un solo** artículo que cumpla la etapa **y** la ubicación. La prueba exige que ese folio aparezca y que el de los dos artículos cruzados no aparezca. Compara **sólo los IDs que sembraste**: el `require.Empty` sobre toda la ventana de 2099 se quita.
- **Transacciones abiertas:** `ImagenRepo.Obtener` y `ImagenRepo.ListarPorEventos` entran en `TestLecturasPublicas_NoDejanTransaccionesAbiertas`.
- **Casos HTTP de la subida** que faltaban (`adjunto_imagen_http_test.go`):
  - un archivo que no es imagen → **422**;
  - reintento con la misma clave y otros bytes → **409**;
  - evento de otra garantía → **404**;
  - en el camino feliz, revisa también `created_at`.
- **Permiso de la descarga en `app`:** `Consultas.ObtenerImagen` sin usuario → 401 y sin `leer` → 403, sin tocar el almacenamiento. Hoy sólo lo protege el barrido HTTP: quitar `autorizarLectura` deja `app` en verde.
- **`Makefile`:** `test-mutation-garantias-domain` y `-app` llevan `$(GREMLINS_TMPDIR)` delante, como los demás targets de mutación.
- **`candado_test.go:91`** dice "This is the only test that commits", pero `detalle_query_count_test.go` también hace commit. Corrige el comentario.

#### P6. El reporte de la tarea 10

`docs/superpowers/plans/garantias-task-10-report.md` no existe. Lo escribes ahora, **veraz**, sobre el SHA que quedó en `main` al fusionar el #33. Lleva:

- ese SHA y la salida literal de la verificación de la tarea 10 sobre él;
- el ciclo up → down → up de la `000071` en una base **desechable** restaurada de `msp-test-db.fbk.gz`, con la salida, y el `git grep` del punto 4 de "Errores que no se repiten" de la tarea 10;
- la tabla **R1–R16**, cada mutante con su parche `diff`, la salida roja y la verde. Los que dependen de Firebird (R1–R8 y R13) se corren con la base arriba. **R15 hoy sobrevive**: en el reporte de la tarea 10 queda como sobreviviente, con la salida, y remite a esta tarea, donde P3 lo mata;
- las consultas del detalle, contadas;
- un ejemplo real de petición y respuesta de cada ruta tuya;
- la tabla del barrido con las operaciones que había en ese SHA (dieciocho), dicho así;
- cada afirmación con su nivel: **MEDIDO** (se corrió, con fecha, dónde y comando), **LEÍDO** (`archivo:línea`) o **INFERIDO** (y qué lo volvería MEDIDO);
- lo que se entregó fuera de plazo y lo que no se entregó, dicho tal cual, con las fechas de los commits.

**Sin contraseñas, tokens ni rutas personales** en el reporte: `FB_PASSWORD="$FB_PASSWORD"`, nunca el valor.

### Parte 2

#### E1. Dónde vive

- Un módulo nuevo, `src/modules/garantiasOficina/`, con las capas de `desempeno`: `domain/`, `application/ports` + `usecases`, `infrastructure/http` + `mappers`, `presentation/composition` + `context` + `hooks`, y `components/`.
- Pantalla nueva en `/garantias-oficina`, con interruptor `GARANTIAS_OFICINA` en `DESKTOP_MODULES` y `ROUTE_TO_MODULE`, y su `ProtectedRoute` en `App.tsx`.
- **No se toca la pantalla vieja de garantías** (`src/modules/garantias/`, `src/components/garantias/`, `src/services/api/*Garantia*`, ruta `/garantias`). Habla con el API viejo y sigue en uso hasta que el nuevo se despliegue. Tampoco se borra ni se redirige.
- Ningún otro módulo importa archivos internos de `garantiasOficina`: si algo se exporta, se exporta por `src/modules/garantiasOficina/index.ts`.

#### E2. Cómo se llama al API

- Su propio `infrastructure/http/apiClient.ts`, igual al de `desempeno`: `baseURL` `${URL_API_V2}/v2`, token de Firebase con `auth.authStateReady()` antes de leerlo, y un tiempo límite.
- Un puerto `GarantiasPort` en `application/ports`, un `HttpGarantiasAdapter` y un `FakeGarantiasPort` para las pruebas. Las pantallas sólo ven el puerto.
- Los DTO en `dtos.ts` copian **exactamente** los nombres JSON de `garhttp/dto*.go`, incluidos los que no se esperan, como `description` en el folio y `descripcion` en la imagen.
- Los errores pasan por un `errorMapper.ts` como el de `desempeno`. El mensaje que se pinta es el `detail` en español del servidor. Los mensajes propios son sólo para lo que el servidor no explica: sin conexión y tiempo agotado.

#### E3. La pantalla sólo pinta

Regla del dueño: **toda lógica, cálculo y filtro vive en el servidor.**

- **Filtros y paginación** van como parámetros de `GET /garantias`: `estado`, `etapa`, `ubicacion`, `desde`, `hasta`, `limit` y `cursor`. La paginación es la del servidor: se pide la siguiente con el `next_cursor` y se deja de ofrecer cuando `has_more` es falso. La pantalla **nunca** filtra, ordena ni cuenta la lista que recibe.
- **Fechas del filtro:** el día que elige el usuario se convierte a un instante RFC3339 con `src/utils/tiempoDeNegocio.ts` (`rangoDeDiasDeNegocio` o su equivalente), anclado a la zona del negocio y no a la del navegador. Al pintar, las fechas del servidor (UTC) se muestran en la zona del negocio.
- **Las opciones de etapa, ubicación y estado**, en los filtros y en los diálogos, salen de `GET /garantias/catalogos`. Igual las etapas destino posibles de un artículo, que salen de `transiciones` para su origen y su etapa actual. **No hay en el cliente ninguna lista de etapas, ubicaciones o estados escrita a mano**, ni regla de qué sigue a qué. Las etiquetas visibles de cada código sí son del cliente (un mapa código → texto); un código sin etiqueta se pinta tal cual, no se esconde.
- **Qué acción procede en qué estado lo decide el servidor.** La pantalla no deduce del estado o la etapa si un comando "aplica": lo manda y, si el servidor lo rechaza (409 o 422), pinta su mensaje. Si te parece que eso deja demasiadas acciones que el servidor rechaza, dilo antes de inventar una regla en el cliente. La solución, si hace falta, es que el catálogo la exponga.
- **Distancia y auditoría GPS** se pintan como vienen en el evento `folio_cerrado` (`distancia_entrega_m`, `auditoria_gps`). La pantalla no calcula distancias. Una ubicación GPS es una **estimación**: se dice "ubicación aproximada" y nunca se rotula como "Casa" ni "Trabajo".

#### E4. Comandos de oficina y su clave de idempotencia

Los comandos de esta pantalla son:

| Acción | Operación | Permiso (§7) |
|---|---|---|
| Diagnóstico (fija la ruta: proveedor o taller) | `registrar-diagnostico` | `actualizar` |
| Dictamen | `registrar-dictamen` | `actualizar` |
| Autorizar cambio físico | `autorizar-cambio-fisico` | `autorizar` |
| Desenlace | `registrar-desenlace` | `autorizar` |
| Marcar listo para entrega | `marcar-listo-entrega` | `actualizar` |
| Cancelar | `cancelar-garantia` | `cerrar` |
| Cerrar | `cerrar-garantia` | `cerrar` |

La entrega con firma es de campo y no va en esta pantalla. Las rutas y los cuerpos exactos están en `routes_comandos.go` y `dto_comandos.go`.

**La clave de idempotencia** (`Idempotency-Key`, un UUID con `v4` de `uuid`, que ya es dependencia del repo):

- Se genera **una vez por intento**: al abrir el diálogo de la acción.
- Se reutiliza en **todo** reenvío de ese mismo diálogo: tiempo agotado, sin conexión o el usuario que vuelve a pulsar.
- Se descarta al tener éxito o al cerrar el diálogo. Abrirlo otra vez es otro intento y lleva clave nueva.
- **`device_created_at` se fija junto con la clave y viaja igual en cada reenvío.** La huella del servidor (`app/huella.go`) incluye `device_created_at`: con la misma clave y otra hora, el servidor responde **409** `warranty_idempotency_key_mismatch` en vez de "ya está hecho". Por eso la hora no se recalcula al reenviar.
- Un **422** no escribe nada en el servidor. El usuario puede corregir el diálogo y reenviar con la misma clave.
- Un **409** se pinta con el mensaje del servidor y el diálogo queda abierto. **No** se reintenta solo.
- Al cerrar, el servidor puede pedir `justificacion_desvio` (**422** `warranty_deviation_justification_required`). Entonces el diálogo de cerrar muestra el campo de justificación con el mensaje del servidor y se reenvía con la **misma** clave. Ese código es lo único que el cliente interpreta, y sólo para mostrar el campo.
- Después de cada comando exitoso se vuelve a pedir el detalle. La pantalla no arma el nuevo estado por su cuenta.

#### E5. Permisos en la pantalla

- **Quién ve la pantalla:** el interruptor `GARANTIAS_OFICINA` de `MODULOS_DESKTOP`, como todas las pantallas del escritorio.
- **Qué acciones se ofrecen:** los permisos del API del usuario, de `GET /v2/me` (campo `permisos`, ver `internal/auth/infra/authhttp/dto.go`, `CurrentUserResponse`). Se piden una vez al entrar a la pantalla.
- Sin el permiso de una acción, la acción **no se muestra**. "Autorizar cambio físico" y "Desenlace" sólo aparecen con `garantias:autorizar`.
- Esto es sólo presentación: el servidor sigue siendo quien autoriza. Un 403 se pinta con su mensaje.

#### E6. Imágenes y firma

- Las imágenes se bajan **con token** por el `apiClient` del módulo, con `responseType: "blob"`, y se muestran con `URL.createObjectURL`. El object URL se libera al desmontar o al cambiar de imagen. Nunca se usa un `<img src>` directo a la URL del API, porque saldría sin token.
- La `url` de la imagen viene sin `/v2` (`/garantias/...`). Al pedirla por el `apiClient`, el `baseURL` ya lo agrega.
- La firma es la imagen con `tipo = "firma"` colgada del evento `folio_entregado`, y se pinta como "Firma de conformidad".
- Error al bajar una imagen: un aviso pequeño en el lugar de la imagen, sin tumbar el detalle. Lee el cuerpo del error con `normalizarErrorBlob`.

#### E7. Forma de la pantalla

- **Bandeja:** la tabla de folios con los filtros de E3 arriba y "Cargar más" con el cursor. Cada fila abre el detalle.
- **Detalle:** el folio con sus artículos (etapa, ubicación, ruta, dictamen, desenlace), la línea de tiempo de eventos en el orden que manda el servidor, y en cada evento sus imágenes. La firma y la auditoría se ven en su evento.
- **Las acciones son pequeñas y van junto a lo que afectan:** las de artículo en la fila del artículo, las de folio en el encabezado del folio. **Nunca** un botón genérico dentro de una caja ni una barra de "Acciones".
- **Textos mínimos:** un verbo por acción ("Diagnóstico", "Dictamen", "Cerrar"), sin explicaciones en pantalla. Nunca la palabra "ciclo": se dice "semana" si hace falta.
- **Colores y tipografía:** los tokens HSL de `src/index.css` (`--background`, `--muted`, `--destructive`…) y las clases de Tailwind que ya usa el repo. Sin colores nuevos sueltos. Componentes de `src/components/ui` (shadcn/Radix) y avisos con `sonner`.

#### E8. Contra qué se prueba a mano

- Tu `msp-api` local en docker, con **tu** base, las migraciones 70 y 71 aplicadas (`make fb-migrate-up`) y la rama de la parte 1.
- En tu `.env` de `msp-api`, `CORS_ALLOWED_ORIGINS` incluye `http://localhost:5173` (Vite) y `http://tauri.localhost` (Tauri en Windows). Después del cambio, el contenedor se **recrea** (`docker compose up -d --force-recreate`): un `restart` no relee el `.env`.
- El escritorio en modo desarrollo: `npm run tauri:dev`, que toma `.env.development` y apunta a `http://localhost:3001`. Si tu API escucha en otro puerto, usa un `.env.development.local`, que no se commitea.
- Si compilas el escritorio para probar, el `Cargo.toml` ya trae la feature `devtools` de `tauri`. No se la quites.
- Dos usuarios en tu base local: uno con los cinco permisos `garantias:*` y otro **sin** `garantias:autorizar`. El segundo es el control de E5.
- **No se publica ninguna versión del escritorio**: nada de `release`, de tags ni del flujo de `.github/workflows/release*.yml`. No se toca producción ni el servidor de pruebas.

---

## Parte 1 — `msp-api`

P1 a P6.

### Pruebas de la parte 1

1. `obtener-catalogos`: el JSON **completo** contra lo que sale del dominio; sin usuario → 401; sin `leer` → 403.
2. Barrido con diecinueve operaciones, contadas contra la constante y contra la lista escrita (P2).
3. Arranque sobre el router real (P3): 401, las diecinueve operaciones y la ausencia de `Idempotent-Replay`.
4. Punta a punta de la subida contra Firebird (P4).
5. Los controles de P5.

### Mutantes de la parte 1

Los corres tú y los reportas, cada uno como **parche `diff`** con la salida roja de la prueba que lo mata y la verde al revertirlo.

| # | Mutante |
|---|---|
| S1 | no registrar `obtener-catalogos` en `routes.go` (y su fila del barrido) |
| S2 | el catálogo omite la última etapa de `domain.Etapas()` |
| S3 | `obtener-catalogos` sin revisar `leer` |
| S4 | un segundo `r.Use(idem)` en el grupo de garantías de `server.go` (el R15 de la tarea 10) |
| S5 | `r.With(idem)` delante de `garhttp.MountRouter` |
| S6 | el `AND` del `EXISTS` del filtro etapa+ubicación cambiado a `OR` (`bandeja_repo.go`, alrededor de las líneas 101–104) |
| S7 | el filtro etapa+ubicación cambiado para no devolver nada |
| S8 | `ImagenRepo.Obtener` deja la transacción abierta |
| S9 | `Consultas.ObtenerImagen` sin `autorizarLectura` |
| S10 | el detalle no adjunta las imágenes de los eventos (lo mata P4) |

S6 y S7 juntos son el control: una prueba que sólo exige "no aparece" mata S6 pero no S7.

---

## Parte 2 — `sistema-cobro-web`

E1 a E8.

### Pruebas de la parte 2

Con Vitest, Testing Library y `msw`, como el resto del repo. Las de componentes, sobre el `FakeGarantiasPort`. Cada prueba lleva su **control positivo**: el caso que sí debe pasar junto al que no. Por ejemplo, la acción oculta sin permiso **y** visible con él, en la misma prueba.

**Capa de API** (`HttpGarantiasAdapter`, con `msw` o con el `axios` simulado como en `HttpDesempenoAdapter.test.ts`):
- cada comando va a la ruta y con el cuerpo exactos de `dto_comandos.go`, con `Idempotency-Key`;
- la bandeja manda los filtros y el `cursor` como parámetros y no filtra la respuesta;
- la descarga de imagen pide un `Blob`, con `Authorization`;
- los mappers DTO → dominio, campo por campo, con nulos;
- el `errorMapper` con 401, 403, 404, 409 y 422 del servidor (con el `detail` y el código en la forma de Huma), sin conexión y tiempo agotado.

**Clave de idempotencia:**
- dos envíos del mismo diálogo (el primero termina en tiempo agotado) llevan la **misma** clave y el **mismo** `device_created_at`;
- cerrar y reabrir el diálogo cambia la clave;
- un 409 deja el diálogo abierto con el mensaje y no reenvía solo;
- un 422 de justificación de desvío muestra el campo y el reenvío lleva la misma clave.

**Componentes:**
- la bandeja pinta lo que devuelve el puerto, en ese orden, y "Cargar más" pide con el `next_cursor` y desaparece con `has_more` falso;
- los selectores muestran lo que trae un catálogo **inventado en la prueba**, distinto del real, para que una lista escrita a mano en el cliente no pase;
- el detalle pinta artículos, eventos, imágenes, firma y auditoría;
- un error al bajar una imagen no tumba el detalle;
- cada acción oculta sin su permiso y visible con él;
- doble clic en enviar → una sola llamada al puerto.

**Lo de siempre:** `npm run lint`, `npm run build` (incluye `tsc`) y `npm test` completos en verde, no sólo el módulo nuevo. `src/test/fronterasDeModulos.test.ts` sigue en verde.

**Pisos de cobertura** del módulo nuevo, medidos con `npx vitest run --coverage src/modules/garantiasOficina`: `domain`, `application` e `infrastructure` ≥ **90%** de líneas; `presentation` y `components` ≥ **80%**.

### Mutantes de la parte 2

Igual que en la parte 1: el parche, la salida roja y la verde.

| # | Mutante |
|---|---|
| F1 | el adaptador no manda `Idempotency-Key` |
| F2 | el reenvío genera una clave nueva |
| F3 | el reenvío recalcula `device_created_at` |
| F4 | el `errorMapper` pinta un texto fijo en vez del `detail` del servidor |
| F5 | "Autorizar cambio físico" se muestra sin `garantias:autorizar` |
| F6 | la bandeja deja de mandar `etapa` y filtra la lista en el cliente |
| F7 | "Cargar más" pide sin `cursor` |
| F8 | la imagen se pide sin pasar por el `apiClient` (sin token) |
| F9 | el object URL no se libera al desmontar |
| F10 | las etapas destino salen de una lista escrita en el cliente |
| F11 | el 409 reintenta solo |

### Prueba a mano, con capturas

Contra tu API local (E8), con un folio que tú abres y llevas desde la pantalla. Cada paso lleva su captura en el reporte:

1. la bandeja con un filtro aplicado y la segunda página cargada;
2. el detalle con artículos, línea de tiempo e imágenes;
3. diagnóstico, dictamen y listo para entrega, cada uno con su resultado;
4. un 409 forzado (misma clave, otro contenido) con el mensaje del servidor;
5. el usuario sin `garantias:autorizar`: no ve "Autorizar cambio físico" ni "Desenlace"; y el otro usuario sí los ve;
6. cerrar un folio con desvío: el servidor pide la justificación y la pantalla la recibe.

Para tener un folio con evidencia y firma puedes sembrarlo por el API (con `curl`, desde tu máquina, a tu API local). Pon los comandos en el reporte.

---

## Archivos que puedes tocar

**`msp-api`:**

```
internal/garantias/infra/garhttp/routes.go, handlers_catalogos.go, dto_catalogos.go
internal/garantias/infra/garhttp/security_test.go, adjunto_imagen_http_test.go,
                                 e2e_imagenes_test.go, *_catalogos_test.go
internal/garantias/infra/garfb/bandeja_repo_test.go, read_tx_test.go, candado_test.go (el comentario)
internal/garantias/app/consultas.go (sólo si P1 lo necesita), consultas_*test.go
cmd/api/garantias_wiring_test.go, y server.go sólo si hay que extraer la construcción del router (P3)
Makefile (los dos targets de mutación de garantías)
docs/superpowers/plans/garantias-task-10-report.md
docs/superpowers/plans/garantias-task-12-report.md y sus capturas en docs/superpowers/plans/garantias-task-12/
```

**`sistema-cobro-web`:**

```
src/modules/garantiasOficina/**
src/test/msw/handlers/garantiasOficina.ts, src/test/msw/fixtures/garantiasOficina.ts
src/constants/modules.ts (la entrada GARANTIAS_OFICINA y su ruta)
src/App.tsx (la ruta)
```

Nada más. En particular: nada en `domain/` ni en los archivos de Kevin en `msp-api` (`routes_comandos.go`, `handlers_comandos.go`, `dto_comandos.go`, `*_comandos_test.go`); nada de la pantalla vieja de garantías; nada de `src-tauri/`, de versiones ni de `release`. En el #33 tocaste `internal/auth/auth_contracts.go`, que no estaba en la lista, sin decirlo. Si necesitas un archivo fuera de la lista, pídelo antes.

---

## Errores que no se repiten

Cada uno ya pasó. Si la entrega repite uno, rebota.

1. **Entregar tarde.** La tarea 10 vencía el martes 3 a la 1pm. La subida de evidencia y el barrido llegaron el 4, y el arreglo de permisos el 5. El plazo es el viernes 13 a la 1pm. Si ves que no llegas, avísame **ese mismo día**, no el día de la entrega.
2. **Empujar directo a una rama compartida.** Tu trabajo va en tus ramas (`feat/garantias-t12-pendientes` y `feat/garantias-oficina`) y entra por PR. Nada se empuja a `main` ni a una rama de otro.
3. **Saltarse los hooks.** Los commits `merge: actualize base de garantias` y `merge: actualice base de garantias` no los habría aceptado el hook `commit-msg`. Todo commit sigue `<tipo>(<alcance>): <asunto>`. Para traer `main` a tu rama usa `git rebase origin/main`, o un merge con el mensaje que pone git (`Merge …`, con mayúscula). Nunca `--no-verify`.
4. **Entregar sin reporte.** La tarea 10 se entregó sin `garantias-task-10-report.md` y por eso no se pudo aceptar: no había ninguna afirmación del autor que verificar. Esta vez son **dos** reportes (el de la 10, P6, y el de la 12), y sin ellos la entrega no cuenta.
5. **Piezas del brief que faltan sin avisar.** En la tarea 10 faltaron la ruta de catálogos, la prueba de arranque y la punta a punta, y el barrido pasaba porque contaba 18 contra 18. Si algo del brief no lo haces, va en el reporte, en una sección "No entregado", con el porqué. Una ausencia que yo descubro es peor que una que tú declaras.

Y los de la tarea 10 siguen vigentes: huella completa, pruebas que se ponen rojas al revertir el arreglo, evidencia que sale del SHA commiteado, y pruebas que comparan sólo lo que sembraron.

---

## Verificación, antes de entregar

**`msp-api`**, sobre el SHA que entregas, con el árbol limpio:

```sh
gofmt -l internal/garantias cmd/api
go vet ./internal/garantias/... ./cmd/api/...
go build ./...
golangci-lint run --allow-parallel-runners --timeout 20m ./internal/garantias/... ./cmd/api/...
go clean -testcache && go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/... ./cmd/api/...
go tool cover -func=cov.out | grep -E 'garantias/(app|infra)'
make coverage-garantias
make test-mutation-garantias-app
make check-sealed MODULE=garantias
grep -rn 'Ã' internal/garantias        # debe salir vacío
```

Con la base arriba, y una vez más sin `FB_DATABASE`: lo de Firebird se salta y nada falla.

**`sistema-cobro-web`**, sobre el SHA que entregas:

```sh
npm ci
npm run lint
npm run build
npm test
npx vitest run --coverage src/modules/garantiasOficina
```

---

## Reporte

Son dos:

1. **`docs/superpowers/plans/garantias-task-10-report.md`** en `msp-api`, como dice P6.
2. **`docs/superpowers/plans/garantias-task-12-report.md`** en `msp-api`, uno solo para las dos partes. Lleva:
   - el **SHA de cada repo** que entregas y el número de cada PR;
   - la salida **literal** de la verificación de los dos repos sobre esos SHA;
   - la tabla de mutantes S1–S10 y F1–F11, cada uno con su parche y su salida roja y verde;
   - la cobertura por capa del módulo del escritorio contra sus pisos;
   - un ejemplo real de petición y respuesta de `obtener-catalogos`;
   - las capturas de la prueba a mano, en `docs/superpowers/plans/garantias-task-12/`, y los comandos con que sembraste;
   - cada afirmación con su nivel (MEDIDO, LEÍDO o INFERIDO);
   - lo que hiciste distinto al brief, con el porqué, y lo no entregado.

Sin contraseñas, tokens ni rutas personales.

---

## Calendario

| Cuándo | Qué |
|---|---|
| **Viernes 13 de noviembre, 1pm** | **Entrega única**: las dos partes completas, los dos PR abiertos (contra `main` de cada repo), los dos reportes, S1–S10, F1–F11 y las capturas |

Si en algún momento ves que no llegas, avísame ese mismo día.

Si necesitas algo, dime.
