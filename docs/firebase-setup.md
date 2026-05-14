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

El bootstrap es manual: primero creas el usuario en Firebase Auth (consola
o sign-in inicial del FE), luego registras su `firebase_uid` en la base
local con el binario `auth-bootstrap`:

```bash
# Después de un sign-in en Firebase, copia el UID del usuario.
./tmp/api auth-bootstrap \
    --firebase-uid <uid-real-de-firebase> \
    --email admin@local.test \
    --nombre "Admin"
```

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
