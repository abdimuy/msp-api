# Despliegue a producción — el API Go y el escritorio

> Producción vive en `C:\msp-api\`, **no tiene tarea programada** y se arranca a
> mano desde el `.bat` del escritorio del servidor. El runbook de pruebas
> ([`../deploy-test-server-runbook.md`](../deploy-test-server-runbook.md))
> describe otra carpeta, otra base y un arranque por `schtasks` que aquí **no
> existe**: aplicarlo a producción es buscar una parada que este procedimiento
> no necesita. Estrena este documento el despliegue de `POST /v2/usuarios`.

## Resumen ejecutable

Para seguirlo de noche. Cada paso tiene su sección con el porqué y las trampas.

1. Árbol **limpio** → `make build-windows` → anotar hash y bytes (§2).
2. `scp` a `msp-api.new.exe` → verificar bytes y `msp-api.new.exe version` (§3).
3. Respaldar `api.log` (§4).
4. `move` del vivo a `msp-api.bak-<motivo>.exe`, y del nuevo a `msp-api.exe` (§5).
   El API sigue sirviendo; nada cambia todavía.
5. **En la consola del servidor**: cerrar la ventana y correr `C:\msp-api\run.bat` (§6).
6. Verificar los **tres** puntos: log de arranque, `401` desde fuera, y que el PID
   del 3011 es el tuyo (§7).
7. Sólo entonces el escritorio — incluido **publicar el borrador** (§8).
8. Un alta real desde el escritorio: `MSP_USUARIOS` tiene que crecer (§11).

Si sale mal: §10. El binario anterior sigue en la misma carpeta.

---

## Cuál documento manda para qué entorno

| | Pruebas | Producción |
|---|---|---|
| Documento | `deploy-test-server-runbook.md` | **este** |
| Carpeta | `C:\msp-api-test\` | `C:\msp-api\` |
| Base | `MUEBLERA_TEST.FDB` | `MUEBLERA_SNP.FDB` |
| Arranque | tarea programada `msp-api-test` (`schtasks`) | `.bat` a mano, en la consola del servidor |
| Ventana de parada | detener la tarea → intercambiar → arrancar | sólo el reinicio manual (§6) |

Lo que **sí** se reutiliza del runbook de pruebas es todo lo que describe la
máquina, no el entorno: el acceso SSH (§2), la ubicación de `isql` y los nombres
cortos 8.3 (§3), y el catálogo de trampas operativas (§11) — el
`fx.NopLogger` que se traga los errores de arranque y el espacio final en un
`set` de `run.bat` muerden igual en producción.

Dos advertencias sobre ese documento, porque es anterior al cutover del
2026-08-13 y quedó desfasado:

- **`https://apidev.loclx.io` ya no es el túnel de pruebas: ES producción**, y el
  entorno de pruebas remoto quedó retirado
  ([`migracion-go-2026-08-13.md`](migracion-go-2026-08-13.md) §1). El runbook de
  pruebas todavía lo presenta como el túnel de test.
- Las dos configuraciones reclaman **el mismo puerto 3011**. Sólo una puede
  escuchar. Si alguna vez vuelve a levantarse el API de pruebas en este
  servidor y se apodera del puerto, el síntoma es
  `firebase_token_wrong_audience` en producción, no un puerto caído: valida
  contra el proyecto Firebase de desarrollo. **Un puerto ocupado no es un
  servicio sano — comprobar *quién* responde** (`GET /version`, §7).

---

## 1. Acceso al servidor

El acceso es el mismo del runbook de pruebas
([`../deploy-test-server-runbook.md`](../deploy-test-server-runbook.md) §2) —
misma máquina, misma llave `~/.ssh/ms_microsip`, mismo usuario `Administrador`,
mismo túnel pinggy que **expira ~60 min y rota host y puerto**. No se repite
aquí; sólo lo que cambia:

- **El arranque del API no se puede hacer por SSH.** Hay que estar en la consola
  del servidor. Todo lo demás de este procedimiento (compilar, subir, verificar,
  intercambiar) sí es remoto.
- **Por qué.** `msp-api.exe` vive en la **sesión de consola (1)**, no en la de
  servicios (0): al cerrar la sesión SSH el proceso se va con ella. Meilisearch y
  el Node legacy sí corren como servicios (sesión 0) y sobreviven solos — por eso
  un reinicio del API no los toca.
- **La configuración de producción vive en `run.bat`, en líneas `set`. NO hay
  `.env` en producción.** Cambiar una variable es editar `run.bat` y reiniciar
  (§6), con la trampa del espacio final (runbook de pruebas §11) puesta.
- **`ssh <HOST> comando < archivo` no funciona**: el stdin no se reenvía por el
  túnel. Para correr SQL: `scp` del `.sql` y luego `isql -i` (§11).
- **El túnel hay que pedirlo antes de empezar.** Lo levanta quien esté en el
  servidor (`ssh -p 443 -R0:localhost:22 tcp@a.pinggy.io` en PowerShell, que
  imprime `tcp://<HOST>.run.pinggy-free.link:<PUERTO>`). Caduca a los ~60 min y
  al relevantarlo **cambian host y puerto**, así que si un paso largo falla a
  media conexión, lo primero que hay que descartar es eso.
- Los pasos remotos se escriben aquí como `ssh <SERVER>` y `scp … <SERVER>:` para
  no repetir la llave y el puerto en cada línea:

  ```bash
  KEY="-i ~/.ssh/ms_microsip -o StrictHostKeyChecking=accept-new"
  PUERTO=<PUERTO DEL TÚNEL>
  HOST=<HOST>.run.pinggy-free.link
  # zsh no hace word-splitting sin comillas: expandir con ${=KEY}
  ssh ${=KEY} -p $PUERTO Administrador@$HOST 'hostname'
  ```

---

## 2. Paso 1 — compilar el binario

> ### Si el árbol no está limpio, el binario miente
>
> El sello de versión sale de `git rev-parse --short HEAD` (`Makefile:9`), que
> **ignora todo lo que no esté commiteado**. Un árbol sucio compila sin una sola
> queja, `/version` devuelve el hash del último commit, y después **no hay forma
> de distinguir ese binario del correcto**: la verificación del §7 daría verde
> sobre código que ese commit no contiene.
>
> **No pases al §3 si `git status --short` imprime algo.** Commitea primero.

Desde el repo `msp-api`, en el Mac:

```bash
git status --short                  # tiene que estar LIMPIO — ver abajo
git rev-parse --short HEAD          # anótalo: es lo que /version tendrá que devolver
make build-windows                  # deja bin/api.exe
wc -c < bin/api.exe                 # anota el tamaño en BYTES
```

`make build-windows` (`Makefile:43-45`) es exactamente:

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=<hash corto de HEAD> -X main.buildTime=<UTC del build>" \
  -o bin/api.exe ./cmd/api
```

**Cómo estampa la versión, con precisión:** la variable `LDFLAGS`
(`Makefile:9`) se define con `:=` y resuelve `$(shell git rev-parse --short
HEAD)` **al leer el Makefile**, así que el sello es el hash corto de `HEAD` — no
del árbol de trabajo. Ésa es la razón del aviso de arriba. Si no hay repo git, el
sello cae a `dev`.

La salida es `bin/api.exe`: el target escribe en `$(API_BIN).exe` y
`API_BIN := bin/api` (`Makefile:5`). Es el archivo que sube el §3.

Ese sello aterriza en `main.version` / `main.buildTime` (`cmd/api/main.go:33-34`),
y de ahí en dos sitios: el subcomando `msp-api.exe version`
(`cmd/api/main.go:50-58`) y el endpoint `GET /version` (`versionHandler`,
`cmd/api/server.go:196` y `:377`).

---

## 3. Paso 2 — subir a `msp-api.new.exe` y verificar el tamaño

**Un `scp` truncado por la caída del túnel se ve idéntico a uno bueno.** Por eso
se sube con otro nombre y se comprueba antes de intercambiar.

```bash
scp ${=KEY} -P $PUERTO bin/api.exe Administrador@$HOST:C:/msp-api/msp-api.new.exe
```

Dos comprobaciones, y las dos son baratas:

```bash
# 1. tamaño en bytes contra el `wc -c` local
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'dir C:\msp-api\msp-api.new.exe'

# 2. que el binario corra y diga el hash esperado
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'cmd /c "C:\msp-api\msp-api.new.exe version"'
# → msp-api <hash corto> (built <fecha UTC>)
```

`dir` imprime el tamaño en bytes con separadores de miles: la comparación es
dígito a dígito contra el `wc -c`, ignorando los puntos o comas. Tiene que
coincidir **exacto**; «casi igual» es un `scp` truncado.

La segunda es la fuerte: `version` es un subcomando de cobra que sólo imprime y
sale (`cmd/api/main.go:50-58`) — **no arranca el servidor ni abre la base**, así
que se puede correr con el API viejo sirviendo. Un `.exe` truncado no llega a
imprimir nada.

Si el tamaño no coincide: borrar `msp-api.new.exe`, relevantar el túnel y volver
a subir. **No intercambiar.**

---

## 4. Paso 3 — respaldar `api.log`

**`run.bat` trunca `api.log` en cada arranque** (`Tee-Object` sin `-Append`; está
anotado en [`ventas-atoradas-2026-08-13.md`](ventas-atoradas-2026-08-13.md) §6).
Reiniciar sin copiar el log pierde la evidencia de lo que estaba pasando justo
antes del despliegue — que es precisamente lo que se quiere leer si el
despliegue sale mal.

```bash
ssh ${=KEY} -p $PUERTO Administrador@$HOST \
  'copy /Y C:\msp-api\api.log C:\msp-api\api.log.antes-<motivo>'
```

`<motivo>` es la misma etiqueta que llevará el respaldo del binario en el §5 —
el hash saliente sirve—, para que log y binario se puedan emparejar después.
Este `copy` es la forma verificada en producción y funciona con el API
corriendo: el proceso mantiene el archivo abierto en modo compartido.

---

## 5. Paso 4 — el intercambio, con el API corriendo

```bash
# el hash saliente sale de /version ANTES de tocar nada:
curl -s https://apidev.loclx.io/version

ssh ${=KEY} -p $PUERTO Administrador@$HOST 'cmd /c "move /Y C:\msp-api\msp-api.exe C:\msp-api\msp-api.bak-<HASH_SALIENTE>.exe & move /Y C:\msp-api\msp-api.new.exe C:\msp-api\msp-api.exe"'
```

Es `move`, no `copy`: el vivo se **renombra**, no se duplica. Y son los dos
`move` en la misma línea — dejar el primero sin el segundo deja la carpeta sin
`msp-api.exe`, y el `.bat` del §6 no encontraría qué arrancar.

**Por qué funciona.** Windows deja renombrar un `.exe` en uso: el proceso vivo
tiene un handle sobre el *archivo*, no sobre la ruta, así que sigue sirviendo
desde el archivo renombrado sin enterarse. Comprobado en producción — el
despliegue del corte por etapa de intentos fallidos (2026-08-26) se hizo así, sin
caída. El binario nuevo queda en su sitio, **inerte**, hasta que alguien
reinicie.

**El único momento de parada es el reinicio manual del §6.** No hay ventana de
mantenimiento que planear: se elige cuándo, y dura lo que tarde el arranque.

**El nombre del respaldo es `msp-api.bak-<motivo>.exe`** — la convención que ya
se usa en producción (`msp-api.bak-predeploy.exe`, `msp-api.bak-gemini.exe`;
runbook de pruebas §5). **Aquí el motivo es el hash saliente**, y por eso el
comando de arriba dice `<HASH_SALIENTE>`: con él, la reversión (§10)
nombra entonces un archivo sin ambigüedad y el historial de despliegues queda
legible en el `dir`. Lo que hay que evitar es el `msp-api.bak.exe` fijo que
aparece en el comando del runbook de pruebas §10: pisa el respaldo anterior en
cada despliegue y deja sin saber a qué commit corresponde.

---

## 6. Paso 5 — el reinicio (en la consola del servidor)

**No hay tarea programada. No hay `schtasks`. Esto no se puede hacer por SSH.**

En la consola del servidor (físicamente o por escritorio remoto):

1. Cerrar la ventana de consola donde corre `msp-api.exe`.
2. Ejecutar el `.bat` de arranque que está en el **escritorio del servidor**.

Lo que acaba corriendo es **`C:\msp-api\run.bat`** — es el que trae la
configuración en líneas `set` y lanza `msp-api.exe serve`. El icono del
escritorio es un lanzador que termina ahí.

> El icono vive en el escritorio del servidor y su nombre es del orden de
> «LEVANTAR PROCESOS» o «LEVANTAR SERVICIOS» — así lo recuerda el dueño del
> sistema, pero no con certeza literal. El nombre exacto (con extensión y
> mayúsculas) se lee directamente en el escritorio al momento de operar; no se
> da por verificado aquí. [`migracion-go-2026-08-13.md`](migracion-go-2026-08-13.md)
> §1 nombra un `LEVANTAR-SERVICIOS.bat` como lanzador, que probablemente sea el
> mismo icono, pero tampoco ahí se verificó contra el escritorio real ni se
> confirmó si levanta sólo el API o toda la pila. Lo que **sí** está
> confirmado sin reservas es el destino: el icono termina ejecutando
> `C:\msp-api\run.bat`.

Si la ventana no está visible (sesión desconectada), la parada equivalente es
`taskkill /F /IM msp-api.exe`, que **sí** se puede lanzar por SSH. Pero eso sólo
para; **arrancar sigue exigiendo la consola**, así que no lo uses salvo que ya
estés frente al servidor o vayas a estarlo.

> ### Prohibido crear tareas programadas para arrancar el API
>
> Existió un atajo —`schtasks /create … msp-api-restart-tmp`— que quedó anotado
> como «resuelto» en agosto de 2026. **El dueño del sistema lo prohibió
> expresamente el 25 de agosto de 2026.** No se crean tareas programadas para
> arrancar ni reiniciar `msp-api.exe`, **ni siquiera temporales**, ni «sólo por
> esta noche». Si estás leyendo esto porque no puedes llegar a la consola, la
> respuesta es esperar a poder, no crear la tarea.
>
> Ojo con el runbook de pruebas §11: propone `schtasks /Create … /RU SYSTEM /F`
> para desacoplar operaciones largas del túnel (gbak, migraciones). Eso sigue
> siendo válido para **esas** operaciones. La prohibición es sobre cualquier
> tarea que arranque o reinicie el API.

Consecuencia de que nadie lo arranque solo: **si el servidor se reinicia, el API
no vuelve** (`migracion-go-2026-08-13.md` §5 lo tiene abierto desde el cutover).
Eso no es un problema de este procedimiento, pero es la razón por la que el
reinicio pide una persona presente.

---

## 7. Paso 6 — verificar

**`GET /version` no basta.** Dice que *un* proceso responde y qué hash trae, no
que sea el tuyo el que está escuchando el 3011. Son **tres** comprobaciones y las
tres hacen falta.

### 1. El arranque, en el log

El que se lee es el log **nuevo** — el anterior quedó respaldado en el §4:

```bash
ssh ${=KEY} -p $PUERTO Administrador@$HOST \
  'powershell -NoProfile -Command "Get-Content C:\msp-api\api.log -Tail 40"'
```

Tiene que aparecer `http: listening addr=0.0.0.0:3011` y los componentes en
`lifecycle: started`.

Un `api.log` **vacío** con el proceso muerto no es un log que falta: es
`fx.NopLogger` tragándose un error de arranque (config, Firebird, un `OnStart`).
El catálogo de esa trampa está en el runbook de pruebas §11 y aplica igual aquí.

### 2. Que responde desde fuera, y con la autenticación sana

```bash
curl -s -o /dev/null -w "%{http_code}\n" https://apidev.loclx.io/v2/analytics/winback
# 401 = arriba y con la autenticación sana.  503 = caído.

curl -s https://apidev.loclx.io/version
# {"version":"<hash del paso 1>","build_time":"…","started_at":"…"}
```

`GET /version` se registra fuera del prefijo `/v2` y antes de cualquier
middleware de autenticación (`cmd/api/server.go:196`), así que **no pide token**;
devuelve exactamente esos tres campos (`cmd/api/server.go:377-381`). `started_at`
es el arranque del proceso: sirve para distinguir "desplegué" de "creí que
desplegué".

Si `version` sigue devolviendo el hash viejo, el proceso que escucha es el
anterior: o no se cerró la ventana, o el `.bat` levantó otra cosa.

### 3. Que el que responde es TU proceso

Un puerto ocupado no es un servicio sano. Si el API de **pruebas** se apodera del
3011, valida los tokens contra el proyecto Firebase de desarrollo y el síntoma es
`firebase_token_wrong_audience` — no un puerto caído. Dos formas de cerrarlo, y
conviene hacer las dos:

- que **la petición que acabas de hacer aparezca en ese `api.log`** (repetir el
  `Get-Content -Tail` y buscar el acceso recién registrado);
- cruzar el PID que escucha:

```bash
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'netstat -ano | findstr ":3011" | findstr LISTENING'
```

---

## 8. El escritorio

El procedimiento vive en **`sistema-cobro-web/DEPLOY.md`** y no se duplica aquí.
Resumen de la forma: subir la versión en los **cuatro** archivos que lista
`DEPLOY.md` §1 (`package.json`, `.env.production` → `VITE_APP_VERSION`,
`src-tauri/tauri.conf.json`, `src-tauri/Cargo.toml`), commit, `git tag vX.Y.Z`,
`git push origin main --tags`. `src/constants/version.ts` **ya no se edita**
(`DEPLOY.md:14-15`).

La última versión publicada del escritorio fue la **1.17.0**: el tag nuevo tiene
que ordenar por encima de ésa o el auto-updater no lo ofrece.

Lo que hay que tener presente desde el lado del API, y que este documento existe
para no volver a olvidar:

- **Lo dispara el tag, no el push.** `.github/workflows/release.yml` engancha
  `push: tags: v*` (excluyendo `v*-test*`, que van por `release-test.yml`). Un
  `git push origin main` sin tag no compila nada.
- **El workflow crea un BORRADOR** (`releaseDraft: true` en `release.yml`).
- **El auto-updater no ve borradores.** Mientras nadie abra
  `github.com/abdimuy/sistema-cobro-web/releases` y lo pase de *Draft* a
  *Published*, el Action se ve verde, los instaladores están ahí, y **nadie
  recibe nada**. `DEPLOY.md` §4 lo advierte; lo que faltaba era decirlo donde se
  decide el orden de despliegue, porque el modo de fallo es silencioso: el API
  queda desplegado, el escritorio no, y no hay error en ninguna pantalla.
- Publicado el release, los usuarios con la app abierta ven la actualización en
  **hasta 30 minutos**, o al reiniciar la app (`DEPLOY.md` §5). O sea que "ya
  publiqué" tampoco es "ya llegó".
- Desfase conocido en `DEPLOY.md` §3: dice que compila macOS ARM + Intel,
  Windows y Linux; el `release.yml` de hoy tiene matriz de **macOS ARM y
  Windows** solamente.

---

## 9. El orden para este cambio: `POST /v2/usuarios`

El cambio son dos piezas: la ruta nueva en el API y, en el escritorio, la
llamada a esa ruta al dar de alta un usuario.

| # | Paso | Por qué ahí |
|---|---|---|
| 1 | **API** (§2–§7) | La ruta tiene que existir antes de que alguien la llame |
| 2 | Los tres puntos del §7, **más** el permiso sembrado (§11) | Es lo único que confirma que el binario que corre trae la ruta *y* que el alta no dará `403`. Anota aquí el conteo de `MSP_USUARIOS` **antes** — el «después» es del paso 4 |
| 3 | **Escritorio** (§8), incluido **publicar el borrador** | El botón de alta empieza a llamar a una ruta que ya responde |
| 4 | Un alta real → el conteo de `MSP_USUARIOS` sube (§11) | Confirma las dos piezas a la vez: el endpoint responde y el escritorio lo llama |

**Si el escritorio va primero**, el botón de alta llama a una ruta que aún no
existe y **toda alta nueva avisa de un fallo**. El código no es `404` sino
**`405 Method Not Allowed`**: `/v2/usuarios` ya existe con `GET`, y chi devuelve
405 cuando la ruta coincide pero el método no. (Comprobado contra
`chi/v5 v5.2.5`, la versión de `go.mod:13`: un `POST` autenticado a una ruta
registrada sólo con `GET` da 405; el 404 se reserva a rutas inexistentes. Sin
token da `401`, porque la autenticación corre antes.) Es el mismo error que ya se
cometió con el APK y la migración `000056`, anotado en
[`intentos-fallidos-resumen.md`](intentos-fallidos-resumen.md) («Orden de
despliegue»). El orden inverso no tiene ningún coste: el API con la ruta nueva y
el escritorio viejo se comportan exactamente como hoy.

### Este cambio NO lleva migración

Verificado en el código, no supuesto:

- `usuarios:crear` se declara en Go como `PermUsuariosCrear`
  (`internal/auth/domain/permission_codes.go:35`) y entra en
  `domain.AllPermissions()` (`:169`).
- Al arrancar, `invokeAuthCatalogSync` (`cmd/api/auth_wiring.go:125`) corre dos
  cosas en el `OnStart` de fx:
  1. `SyncPermissionCatalog` — UPSERT en `MSP_PERMISOS` de **todos** los códigos
     del catálogo en código (`internal/auth/app/permisos_catalog_sync.go:19`).
  2. `SyncRolesCatalog(ctx, uuid.Nil)` — UPSERT del rol inmutable `super_admin` y
     `SyncPermisos` con la lista **completa** de códigos
     (`internal/auth/app/roles_catalog_sync.go:29`).
- Efecto: `super_admin` tiene `usuarios:crear` **el día uno, sin conceder nada a
  mano**. Basta arrancar el binario nuevo.
- La ruta exige ese permiso: `r.With(idem, RequirePermission(domain.PermUsuariosCrear)).Post("/", h.CrearUsuario)`
  (`internal/auth/infra/authhttp/routes.go:58`), dentro del grupo que ya pasa por
  la autenticación — sin token es `401` antes de mirar el permiso.

Tres matices que hay que conocer antes de confiar en esto:

- **Cualquier otro rol sí necesita el grant.** La siembra automática sólo toca
  `super_admin`. Para otro rol, la vía es
  `POST /v2/roles/{id}/permisos` (permiso `roles:asignar_permiso`), no un INSERT
  a mano en `MSP_ROLES_PERMISOS`.
- **Si la siembra falla, el arranque NO falla.** `invokeAuthCatalogSync` loguea
  y devuelve `nil` — a propósito, para que una base recién provisionada pueda
  levantar (`cmd/api/auth_wiring.go:125-138`). El síntoma sería un `403` al dar
  de alta con un API aparentemente sano, y **nada más**: ni el `/version` ni el
  `401` del §7 lo detectan. Por eso la comprobación SQL del §11 no es opcional.
  Lo que hay que buscar en `api.log` son estas tres líneas:
  `auth.permission_catalog_sync_failed`, `auth.roles_catalog_sync_failed` y
  `auth.roles_catalog_skipped`.
- **`SyncRolesCatalog` se salta si no hay ningún usuario** en `MSP_USUARIOS`
  (necesita un `CREATED_BY` válido). Ese caso loguea `auth.roles_catalog_skipped`
  con nivel WARN y devuelve `nil` — o sea que tampoco rompe el arranque
  (`internal/auth/app/roles_catalog_sync.go:34-39`). No es el caso de producción:
  hay 69 usuarios.

---

## 10. Reversión

El binario anterior sigue en la carpeta, con el nombre que le puso el §5:
`C:\msp-api\msp-api.bak-<HASH_SALIENTE>.exe`.

```bash
# 1. respaldar el api.log del intento fallido (§4), con otro sufijo
# 2. renombrar de vuelta — se puede hacer con el API arrancado o parado
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'cmd /c "move /Y C:\msp-api\msp-api.exe C:\msp-api\msp-api.fallido-<HASH_NUEVO>.exe & move /Y C:\msp-api\msp-api.bak-<HASH_SALIENTE>.exe C:\msp-api\msp-api.exe"'
# 3. en la CONSOLA del servidor: cerrar la ventana y correr el .bat (§6)
# 4. curl -s https://apidev.loclx.io/version  → tiene que dar <HASH_SALIENTE>
```

**El binario que falló no se borra**: se queda como `msp-api.fallido-<hash>.exe`.
Es lo único que permite reproducir el fallo después, y ocupa unas decenas de MB.

**Cómo se sabe que hace falta revertir.** Cualquiera de éstas, tras el §7:

- El proceso no levanta: `api.log` vacío o sin `lifecycle: started`, y el `.exe`
  muere a los segundos (la trampa de `fx.NopLogger`).
- `apidev.loclx.io` devuelve `503` en vez de `401`.
- **Arrancó pero está mal**: `GET /version` da el hash nuevo y aun así el humo del
  §11 falla. Éste es el caso peligroso, porque *todo* se ve verde y nada avisa
  solo. Si el humo no pasa, se revierte igual: un API que responde con datos
  equivocados es peor que uno caído, porque nadie lo reporta.

Y una que **no** se arregla revirtiendo: si el alta responde `403` y
`MSP_USUARIOS` no crece, lo que no corrió es la siembra del permiso (§9). Eso se
resuelve reiniciando otra vez y leyendo el `api.log`, no volviendo al binario
anterior — el anterior tampoco tiene la ruta.

**Nada de esto es posible si el §5 se saltó.** El respaldo es lo único a lo que
volver.

**Este cambio no tiene nada que revertir en la base.** No hay migración, y las
filas que `SyncPermissionCatalog` sembró en `MSP_PERMISOS` son inertes para el
binario viejo: no las lee nadie. `usuarios:crear` se queda en el catálogo y en
`super_admin` — eso es correcto y no estorba.

Un detalle por si alguien mira el outbox: cada alta encola un evento
`user.created` en `MSP_OUTBOX_EVENTS` que **ningún handler consume**, así que la
fila se queda en `new` para siempre. Es por diseño —el despachador sólo reclama
los tipos registrados (`internal/platform/outboxfb/dispatcher.go:200-216`)— y no
genera reintentos ni errores. No es señal de un despliegue roto.

Cuando **sí** haya migración, el orden de reversión no es este: se revierte
**primero el binario y después la migración**, por la razón que explican
[`intentos-fallidos-dedup.md`](intentos-fallidos-dedup.md) y
[`intentos-fallidos-resumen.md`](intentos-fallidos-resumen.md) — un binario nuevo
vivo contra un esquema ya revertido rompe cada escritura con *column unknown*.

---

## 11. Qué mirar después (sólo lectura)

Nada de esta sección escribe. Las lecturas contra producción se hacen con `isql`
en el servidor, y **producción exige `localhost:` delante de la ruta de la base y
credenciales explícitas**:

```bat
C:\PROGRA~1\Firebird\Firebird_5_0\isql.exe -user <USUARIO> -password <CONTRASEÑA> ^
  localhost:C:\MICROS~1\MUEBLERA_SNP.FDB -i C:\q.sql
```

(`C:\MICROS~1` es el nombre corto 8.3 de `C:\Microsip datos`, y `C:\PROGRA~1` el
de `C:\Program Files` — evitan pelearse con las comillas por SSH→cmd. El detalle
está en el runbook de pruebas §3. Para subir el `.sql`: `scp`, nunca
`ssh host cmd < file`.) Las trampas de `isql` que ya están escritas —la
truncación de columnas largas, el `$` de `RDB$` que expande el shell— están en
[`../microsip-trace-runbook.md`](../microsip-trace-runbook.md), sección
«Gotchas»; no se repiten aquí. **De ese documento se toman las trampas, no los
comandos**: describe otra base (`DESARROLLO.FDB`), otra instalación
(`Firebird_3_0`) y recomienda pipear el SQL por stdin — que en este servidor no
funciona.

Y una que muerde tarde: **una consulta larga sostiene una transacción abierta**,
y eso ya congeló una vez el watermark del sync de cobranza. Si una lectura se
pasa de tiempo, comprobar después de cerrarla:

```sql
SELECT COUNT(*) FROM MON$STATEMENTS WHERE MON$STATE = 2;
```

El `.sql` se escribe en el Mac y se sube antes de correrlo:

```bash
scp ${=KEY} -P $PUERTO q.sql Administrador@$HOST:C:/q.sql
```

**El conteo de usuarios, antes y después:**

```sql
SELECT COUNT(*) FROM MSP_USUARIOS;
-- 69 antes del despliegue (2026-08-27).
```

El alta desde el escritorio tiene que **subir ese conteo**. Es la única
verificación que confirma las dos piezas a la vez: el endpoint responde y el
escritorio lo está llamando.

**El permiso sembrado solo, sin tocar nada:**

```sql
SELECT COUNT(*) FROM MSP_PERMISOS WHERE CODIGO = 'usuarios:crear';   -- 1

SELECT COUNT(*)
FROM MSP_ROLES_PERMISOS rp
JOIN MSP_ROLES r ON r.ID = rp.ROL_ID
WHERE r.NOMBRE = 'super_admin' AND rp.PERMISO_CODIGO = 'usuarios:crear';   -- 1
```

Si el segundo da `0`, la siembra no corrió: buscar en `api.log`
`auth.roles_catalog_sync_failed` o `auth.roles_catalog_skipped` (§9). Sin esa
fila el botón de alta responde `403` aunque todo lo demás esté verde.

**Y lo de siempre tras cualquier binario nuevo:**

- `GET /version` con el hash esperado (§7).
- `api.log` sin errores de arranque en las primeras líneas.
- El chequeo de humo del runbook de pruebas §12 sigue valiendo **tal cual, sin
  cambiar el host**: el `apidev.loclx.io` que usa ya es producción (ver el aviso
  del encabezado). Lo que no sirve son los conteos que cita entre paréntesis:
  son del entorno de pruebas. Vale la forma, no la cifra. En particular el flujo
  de editar una venta: un `PATCH /v2/ventas/{id}` con el cuerpo completo que
  manda la app —incluido `montos`— debe responder `200`, no `422`.

---

## 12. Lo que este procedimiento NO arregla

- **El servidor sigue sin arrancar el API solo.** No hay tarea programada ni
  servicio: un reinicio del Windows Server deja el API abajo hasta que alguien
  entre a la consola y corra el `.bat`. Este documento describe cómo desplegar
  con esa restricción, no la levanta — y **taparla con una tarea programada está
  prohibido** (§6).
- **El arranque necesita presencia física** (o escritorio remoto). No hay forma
  de completar un despliegue sólo por SSH.
- **`api.log` se sigue truncando en cada arranque.** El respaldo del §4 es un
  parche manual: se olvida una vez y la evidencia se pierde. El arreglo de
  verdad es `-Append` en el `Tee-Object` de `run.bat`, y no está hecho.
- **No cubre despliegues con migración.** El orden aquí es «binario y ya»; con
  migración el orden lo fijan el cambio y su documento
  ([`intentos-fallidos-resumen.md`](intentos-fallidos-resumen.md),
  [`intentos-fallidos-dedup.md`](intentos-fallidos-dedup.md),
  [`lapidas-huerfanas-despliegue.md`](lapidas-huerfanas-despliegue.md)). Las
  migraciones de producción se aplican a mano y **no corren al arrancar el API**:
  el servidor tiene `C:\msp-api\migrar-prod.bat`, que espera la migración en
  `C:\msp_migs.sql` y un conteo en `C:\qcount.sql`, y deja el resultado en
  `C:\migracion-prod.log` (se busca `MIGRACION_ERRORLEVEL=0`).
- **No cubre el APK de campo.** La app Android tiene su propio canal, su propia
  adopción por flota y su propia compuerta de versión mínima; nada de eso está
  aquí.
- **No hay respaldo de la base en este procedimiento**, y es deliberado: este
  cambio no toca el esquema ni escribe nada al desplegarse. `make fb-snapshot` es
  un target de **desarrollo** (habla con el contenedor Docker `mueblera-firebird`
  y no existe en producción). En producción el respaldo en caliente es `gbak`
  contra `MUEBLERA_SNP.FDB` — la forma está en el runbook de pruebas §3. Un
  despliegue con migración sí lo pide.
