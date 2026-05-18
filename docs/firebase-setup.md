# Firebase Auth — setup local y producción

Este documento describe cómo configurar `msp-api` para autenticar contra
Firebase Authentication usando el `RealClient`. Para la decisión de diseño,
ver [ADR-0004](adr/0004-firebase-real-client.md).

## 1. Obtener el service account

1. Firebase Console → Project Settings → **Service accounts**.
2. Botón **Generate new private key**. Descarga un JSON con campos
   `type`, `project_id`, `private_key`, `client_email`, etc.
3. Copia el archivo a la raíz del repo como `./serviceAccountKey.json`.
   Está incluido en `.gitignore` — no se commitea jamás.

> El archivo es equivalente a una credencial root: NO lo subas a
> servicios de terceros, NO lo dejes en un Drive compartido. Bórralo
> cuando termines la sesión si trabajas en una máquina compartida.

## 2. Variables de entorno

En `.env` (copiado de `.env.example`):

```env
FIREBASE_PROJECT_ID=tu-proyecto-firebase
FIREBASE_SERVICE_ACCOUNT_PATH=./serviceAccountKey.json
FIREBASE_DEV_MODE=false
FIREBASE_ALLOW_UNCONFIGURED=false
```

Validación: al arrancar, el factory selecciona `RealClient` y loguea
`auth.firebase_real_client_ready` con el `project_id`. Si falla:
`firebase_service_account_missing` o `firebase_app_init_failed`.

## 3. Crear el primer admin

El subcomando `auth-bootstrap` provisiona el primer admin con el rol
inmutable `super_admin` y todos los permisos del catálogo
(`domain.AllPermissions()`). Tres modos según escenario:

### Modo A — dev desde cero (recomendado)

Crea el usuario en Firebase Auth y la fila en la DB en una sola
operación. Idempotente para el lookup en Firebase, así que reintentar
no duplica.

```bash
go run ./cmd/api auth-bootstrap \
    --email admin@muebleriamsp.mx \
    --nombre "Administrador MSP" \
    --create-in-firebase
```

Password default: `MspDev2026!`. Cámbialo en Firebase Console o pasa
`--password <otro>` al crear.

### Modo B — Firebase user ya existe

Pasa el UID si el usuario fue creado a mano (Console, FE, import):

```bash
go run ./cmd/api auth-bootstrap \
    --firebase-uid <uid-real> \
    --email admin@local.test \
    --nombre "Admin"
```

### Modo C — reset destructivo (solo dev)

Borra **todos** los usuarios de Firebase Auth excepto `--email`, vacía
las tablas auth de la DB (`MSP_USUARIOS`, `MSP_ROLES`,
`MSP_USUARIOS_ROLES`, `MSP_ROLES_PERMISOS`), y vuelve a hacer bootstrap.
Usar cuando quieras empezar de cero:

```bash
go run ./cmd/api auth-bootstrap \
    --email admin@muebleriamsp.mx \
    --nombre "Administrador MSP" \
    --create-in-firebase --reset
```

Nunca uses `--reset` contra un proyecto Firebase compartido o de
producción — borra usuarios irreversiblemente.

### Sin flag de Firebase: rechaza si ya hay admin

Sin `--reset`, el bootstrap se niega a correr si ya existe algún
usuario en `MSP_USUARIOS`. Es deliberado: el subcomando solo está
pensado para crear el PRIMER admin. Para administración subsecuente
(crear más usuarios, asignar roles) usa los endpoints
`/v2/usuarios` y `/v2/roles`.

## 4. Probar end-to-end

```bash
# 1. Obtener un idToken vía Firebase Auth REST API.
FB_WEB_API_KEY=<tu-web-api-key>  # Firebase Console → Project Settings → General
TOKEN=$(curl -sX POST \
    "https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=$FB_WEB_API_KEY" \
    -H "Content-Type: application/json" \
    -d '{"email":"admin@local.test","password":"...","returnSecureToken":true}' \
    | jq -r .idToken)

# 2. Llamar un endpoint autenticado.
curl -H "Authorization: Bearer $TOKEN" http://localhost:3001/v2/ventas
```

## 5. Tests integration contra el emulador

El emulator no requiere service-account key — el SDK lo detecta vía
`FIREBASE_AUTH_EMULATOR_HOST` y omite la verificación de firma criptográfica.

```bash
# 1. Levantar el emulator (Docker).
make fb-emu-up

# 2. Correr los tests integration.
FIREBASE_AUTH_EMULATOR_HOST=localhost:9099 \
    go test -count=1 -v -run Integration ./internal/auth/infra/firebase/...

# 3. Tumbar el emulator.
make fb-emu-down
```

Sin `FIREBASE_AUTH_EMULATOR_HOST` los tests integration hacen `t.Skip`.

## 6. Trampa: DevMode sigue activo

Si `FIREBASE_DEV_MODE=true` en tu `.env`, el factory ignora `PROJECT_ID`
y usa `DevModeClient` (acepta tokens `dev:<uid>`). Para probar el flujo
real, set explícitamente `FIREBASE_DEV_MODE=false`.
