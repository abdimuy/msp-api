# Runbook — Despliegue de la API de PRUEBA en el Windows Server (SERVERM)

Cómo está montado y cómo redesplegar el entorno **test** de `msp-api` (Go) en el
Windows Server legacy. Convive con la API de producción (Node, otra DB) sin
tocarla. NO confundir con producción.

> **Regla de oro:** la DB de producción es `C:\Microsip datos\MUEBLERA_SNP.FDB` →
> **NO TOCAR** (solo respaldo de lectura con gbak). El entorno test usa una copia:
> `C:\Microsip datos\MUEBLERA_TEST.FDB`.

---

## 1. Resumen de lo que corre

| Componente | Detalle | Puerto | Tarea programada (boot, SYSTEM) |
|---|---|---|---|
| **msp-api (Go, test)** | `C:\msp-api-test\msp-api.exe serve` | 3011 | `msp-api-test` (Running) |
| **Meilisearch** 1.47.0 | índice `clientes` (directorio) | 7700 | `meilisearch` (Running) |
| **Firebird** 5.0.3 | `MUEBLERA_TEST.FDB` (clon FB5 de prod) | 3050 | servicio Firebird |
| **llama-server** (RETIRADO) | qwen2.5-7b CPU — saturaba el server | 8088 | `llama_server` (**Disabled**) |
| **LLM activo: Gemini** | API hosted OpenAI-compat (reemplazó al llama) | — | — |

Exposición pública (túneles loclx): **`https://apidev.loclx.io`** → `localhost:3011`.
(Prod usa `msp2025.loclx.io`; hay `apiima.loclx.io` para otro test.)

El front (`sistema-cobro-web`) apunta al test con
`VITE_URL_API_V2=https://apidev.loclx.io` en `.env.development.local` (temporal).

---

## 2. Acceso al server (SSH)

- **Llave:** `~/.ssh/ms_microsip` (ed25519). Pública ya en `authorized_keys`.
- **Usuario:** `Administrador` (con tilde/español).
- **Túnel pinggy:** el server corre `ssh -p 443 -R0:localhost:22 tcp@a.pinggy.io`
  en PowerShell, que imprime `tcp://<HOST>.run.pinggy-free.link:<PUERTO>`. El free
  **expira ~60 min y cambia host/puerto** → pedirlo de nuevo cada vez.
- **Conexión desde Mac:**
  ```bash
  ssh -i ~/.ssh/ms_microsip -p <PUERTO> -o StrictHostKeyChecking=accept-new \
      Administrador@<HOST>.run.pinggy-free.link
  ```
- **Gotcha zsh:** zsh NO hace word-splitting de variables sin comillas. Si guardas
  flags en una var, expande con `${=VAR}`:
  ```bash
  KEY="-i ~/.ssh/ms_microsip -o StrictHostKeyChecking=no"
  ssh ${=KEY} -p <PUERTO> Administrador@<HOST> 'hostname'   # NO: ssh $KEY ...
  ```
- **scp es confiable; `ssh host cmd < file` NO** (el stdin no se reenvía bien por
  el túnel). Para correr SQL/scripts: `scp` el archivo y luego `isql -i C:\file.sql`.

---

## 3. Firebird (DB de prueba)

- **DB:** `C:\Microsip datos\MUEBLERA_TEST.FDB` — clon FB5 de producción.
- **Credenciales:** `SYSDBA` / `masterkey`.
- **isql (FB5):** `C:\Program Files\Firebird\Firebird_5_0\isql.exe`
  (también existe `Firebird_3_0` — NO usarlo con la DB FB5).
- **Nombres cortos 8.3** (evitan comillas por los espacios en rutas, cómodos vía SSH→cmd):
  - `C:\Microsip datos` → `C:\MICROS~1`
  - `C:\Program Files`  → `C:\PROGRA~1`
  ```bash
  # correr SQL sin pelear con comillas:
  scp ${=KEY} -P <PUERTO> q.sql Administrador@<HOST>:C:/q.sql
  ssh ${=KEY} -p <PUERTO> Administrador@<HOST> \
    'cmd /c "C:\PROGRA~1\Firebird\Firebird_5_0\isql.exe -user SYSDBA -password masterkey C:\MICROS~1\MUEBLERA_TEST.FDB -i C:\q.sql"'
  ```

### Clonar/refrescar la DB de test desde prod (gbak online, NO corrompe prod)
```cmd
REM Respaldo en caliente de prod (consistente, no bloquea):
C:\PROGRA~1\Firebird\Firebird_5_0\gbak.exe -b -user SYSDBA -password masterkey "C:\Microsip datos\MUEBLERA_SNP.FDB" C:\mueblera.fbk
REM Restaurar como la DB de test:
C:\PROGRA~1\Firebird\Firebird_5_0\gbak.exe -c -user SYSDBA -password masterkey C:\mueblera.fbk "C:\Microsip datos\MUEBLERA_TEST.FDB"
```

### Colisión de tablas MSP_* (al migrar sobre un clon de prod)
El clon de prod ya trae tablas legacy `MSP_USUARIOS`, `MSP_ROLES`, `MSP_PERMISOS`,
`MSP_ROLES_PERMISOS`, `MSP_USUARIOS_ROLES` (del sistema viejo). La migración 000001
choca con esas 5. Solución: **dropear esas 5 + `MSP_MIGRATIONS`** antes de migrar
(MSP_USUARIOS tiene self-FKs `FK_MSP_USUARIOS_CREATED_BY/UPDATED_BY` → dropear las
constraints primero). El resto de tablas `MSP_*` de nuestra API son nuevas, no chocan.

---

## 4. Migraciones (de nuestra API Go)

Las migraciones viven en el repo `migrations-firebird/` (41 al momento de escribir).
Aplicarlas todas tras un clon fresco. La forma robusta contra el túnel flaky:
concatenar todos los `.up.sql` en un archivo, subirlo y correrlo en una conexión:

```bash
# en el repo, armar el bundle (respetando el orden numérico) y subirlo:
cat migrations-firebird/0*_*.up.sql > /tmp/msp_migs.sql
scp ${=KEY} -P <PUERTO> /tmp/msp_migs.sql Administrador@<HOST>:C:/msp_migs.sql
ssh ${=KEY} -p <PUERTO> Administrador@<HOST> \
  'cmd /c "C:\PROGRA~1\Firebird\Firebird_5_0\isql.exe -user SYSDBA -password masterkey C:\MICROS~1\MUEBLERA_TEST.FDB -i C:\msp_migs.sql"'
```
Tracking en tabla `MSP_MIGRATIONS`. Verificar con `SELECT COUNT(*) FROM MSP_MIGRATIONS;`.

---

## 5. La API (`C:\msp-api-test\`)

- **Arranque:** `run.bat` setea env vars (el binario lee del SO, no `.env`) y corre
  `msp-api.exe serve > C:\msp-api-test\api.log 2>&1`.
- **Tarea programada:** `msp-api-test` (boot trigger, SYSTEM). Operar con:
  ```cmd
  schtasks /Run /TN msp-api-test     REM arrancar
  schtasks /End /TN msp-api-test     REM detener (luego taskkill /F /IM msp-api.exe)
  ```
- **Backups del binario:** `msp-api.bak-gemini.exe`, `msp-api.bak-predeploy.exe`, etc.

### `run.bat` — variables clave (valores reales, secretos como placeholder)
```bat
set APP_ENV=staging
set APP_PORT=3011
set FB_HOST=localhost
set FB_PORT=3050
set FB_DATABASE=C:\Microsip datos\MUEBLERA_TEST.FDB
set FB_USER=SYSDBA
set FB_PASSWORD=masterkey
set FB_CHARSET=UTF8
set FIREBASE_PROJECT_ID=msp-dev-96ff5
set FIREBASE_SERVICE_ACCOUNT_PATH=C:\msp-api-test\serviceAccountKey.json
set FIREBASE_DEV_MODE=false
set STORAGE_DIR=C:\msp-api-test\var\uploads
set MICROSIP_SYNC_ENABLED=true
set CORS_ALLOWED_ORIGINS=...,http://tauri.localhost,...   REM WebView2 Windows necesita http://tauri.localhost
set MEILISEARCH_URL=http://localhost:7700
REM --- LLM: Gemini hosted (OpenAI-compat) ---
set LLM_ENABLED=true
set LLM_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai
set LLM_MODEL=gemini-2.5-flash-lite
set LLM_API_KEY=<SECRETO — key de Google AI Studio, NO commitear>
set LLM_TIMEOUT=30s
```

---

## 6. Meilisearch

- **Arranque:** `C:\meilisearch\meili-run.bat`:
  ```bat
  set MEILI_NO_ANALYTICS=true
  "C:\meilisearch\meilisearch.exe" --db-path "C:\meilisearch\data" --http-addr 127.0.0.1:7700 --env development
  ```
- **Tarea:** `meilisearch` (boot, SYSTEM). `--env development` = sin master key.
- **Índice:** `clientes` (lo crea/configura el bootstrap de la API; lo puebla el
  reconcile worker).

---

## 7. LLM (narrativa "Lectura del analista")

**Decisión (jun 2026): NO inferencia local.** El llama-server en CPU **saturaba el
procesador** del server y bloqueaba el `narrativa_worker`. Se cambió a **Gemini**
(API hosted OpenAI-compat). La tarea `llama_server` quedó **Disabled**.

- Config en `run.bat` (sección LLM arriba). Modelo `gemini-2.5-flash-lite`.
- **El cliente OpenAI-compat soporta `LLM_API_KEY`** (commit `feat(llm): soporte de
  LLM_API_KEY`) → manda `Authorization: Bearer`. Vacío = sin auth (modo local).
- **Free tier:** ~15-30 RPM, **1,000 req/día**, 250k TPM. Devuelve **HTTP 503
  "high demand" intermitente (~1 de cada 3 pega)**; nuestro cliente lo trata como
  TRANSITORIO → el worker reintenta cada 1 min hasta que cae un 200.
- **Para producción: Tier 1 (billing on).** Sigue pay-per-use (~$1-2/mes; backfill
  45k <$10), **quita los 503** y **NO entrena con los datos** (el free tier SÍ
  entrena — y mandamos notas reales de clientes). Alternativas válidas: Groq
  (sub-segundo), DeepSeek (más barato, datos en China), OpenAI gpt-5-nano.
- La narrativa **solo se llama por cliente encolado**, y se encola **al abrir una
  ficha** (lazy, cacheada en `MSP_AN_CLIENTE_NARRATIVA`, invalidada por hash de
  hechos+nota). NO hay backfill automático del padrón.

---

## 8. Auth (Firebase + roles en Firebird)

- **Firebase project:** `msp-dev-96ff5` (NUEVO). Los **roles/permisos nuevos viven
  en Firebird** (`MSP_USUARIOS`, `MSP_USUARIOS_ROLES`), NO en Firestore (eso es lo
  viejo, se migrará después). Auto-provisioner crea `MSP_USUARIOS` al primer login.
- **Usuario de prueba:** `noe@gmail.com` → rol `super_admin` (otorgado por SQL
  directo en `MSP_USUARIOS_ROLES` tras crear el rol al arranque).
- **`super_admin` al arranque:** solo se crea si ya existe ≥1 usuario; el
  `auth-bootstrap` se niega en sistema no vacío → usar grant por SQL.
- **Obtener un ID token real para curls** (dev mode está OFF, hay que token real):
  ```bash
  # Web API key del proyecto (pública, está en sistema-cobro-web/firebase.ts):
  APIKEY=AIzaSyAx9Ts4kGoqEmzDiI-mQCp8Jd4FZhczxos
  curl -s -X POST "https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=$APIKEY" \
    -H "Content-Type: application/json" \
    -d '{"email":"noe@gmail.com","password":"<pass de noe>","returnSecureToken":true}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["idToken"])'
  # Token válido ~1h. Usar como: -H "Authorization: Bearer <token>"
  ```

---

## 9. Poblar datos tras un deploy fresco

El directorio y los scores NO se calculan solos al instante:

1. **Scores determinísticos (Segmento/Riesgo/Recencia/Frecuencia/Estado pago)** →
   los materializa el **refresh worker** (cada 1h, sin tick inmediato). Forzar ya:
   ```bash
   curl -s -X POST https://apidev.loclx.io/v2/analytics/winback/refresh \
     -H "Authorization: Bearer <token>" -H "Content-Type: application/json" \
     -d '{"full":true}'    # 202 iniciado; tarda ~10 min sobre ~45k (agrega anclas de Microsip)
   ```
   Materializa `MSP_AN_WINBACK_CANDIDATOS` (escribe en 1 txn → cuenta queda en 0
   hasta `background_done procesados=N` en el log).
2. **Directorio (Meilisearch)** → lo puebla el **reconcile worker** (cada ~5 min;
   un tick tarda ~6 min sobre 38k). Enriquece cada doc con el pulso de analytics.
   Forzar reindex:
   ```bash
   curl -s -X POST https://apidev.loclx.io/v2/clientes/_search/refresh -H "Authorization: Bearer <token>"
   ```
3. **Narrativa IA** → al abrir fichas (encola) + worker drena vía Gemini.

---

## 10. Procedimiento de redeploy (binario nuevo)

Desde el Mac, en el repo `msp-api`:
```bash
# 1. cross-compile para Windows
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/msp-api.exe ./cmd/api

# 2. subir a un temporal (no se puede sobrescribir el .exe en uso)
scp ${=KEY} -P <PUERTO> /tmp/msp-api.exe Administrador@<HOST>:C:/msp-api-test/msp-api.new.exe

# 3. detener API, swap con backup, arrancar
ssh ${=KEY} -p <PUERTO> Administrador@<HOST> 'cmd /c "schtasks /End /TN msp-api-test & timeout /t 2 & taskkill /F /IM msp-api.exe 2>nul & move /Y C:\msp-api-test\msp-api.exe C:\msp-api-test\msp-api.bak.exe & move /Y C:\msp-api-test\msp-api.new.exe C:\msp-api-test\msp-api.exe & schtasks /Run /TN msp-api-test"'

# 4. verificar boot
ssh ${=KEY} -p <PUERTO> Administrador@<HOST> 'powershell -NoProfile -Command "Start-Sleep 16; Get-Content C:\msp-api-test\api.log | Select-String real_client_ready,\"start failed\",\"lifecycle: started\" | Select-Object -Last 6"'
curl -s -o /dev/null -w "%{http_code}\n" https://apidev.loclx.io/v2/analytics/winback   # 401 = arriba/auth OK; 503 = caído
```

---

## 11. Gotchas operativos conocidos

- **Boot loop por Meilisearch bootstrap** (YA CORREGIDO en código, commit
  `fix(meilisearch): el bootstrap del índice ya no aborta el boot`): el bootstrap
  aplicaba settings inline; sobre el índice de 38k disparaba un reindex > StartTimeout
  de fx → boot abortaba y reintentaba en loop. **Si corres un binario VIEJO** y
  cae en el loop, el workaround manual es cancelar la task de settings y arrancar
  con Meilisearch idle:
  ```bash
  # ver la task atorada:  GET http://localhost:7700/tasks?statuses=processing
  # cancelarla:
  Invoke-WebRequest -Uri "http://localhost:7700/tasks/cancel?uids=<UID>" -Method POST
  # esperar isIndexing=false (GET /indexes/clientes/stats) y luego schtasks /Run /TN msp-api-test
  ```
- **Workers que no tickean** (YA CORREGIDO, commit `fix(workers): ... OnStart de
  fx`): los workers de fondo derivaban su ctx del OnStart de fx (que fx cancela tras
  el arranque) → la goroutine salía sin tickear. Fix: `context.WithoutCancel`.
- **PowerShell vía SSH:** `Invoke-WebRequest` bufferea el archivo entero en RAM
  (para descargas grandes usar `Start-BitsTransfer`). El comillado anidado se rompe
  fácil; preferí `Get-Content -Tail` + filtrar local con grep.
- **Consola en CP850:** salidas con acentos pueden venir en CP850; si importa,
  `iconv -f CP850 -t UTF-8//TRANSLIT`.
- **firebirdsql driver:** SUM/agregados NUMERIC vienen sin escalar (castear
  `CAST(SUM(..) AS NUMERIC(18,s))`); no soporta `?` dentro de `MERGE USING (SELECT ?)`
  (usar UPDATE-luego-INSERT / EXECUTE BLOCK).

---

## 12. Checklist de smoke tras deploy

```bash
TOKEN=<id token de noe>
curl -s -o /dev/null -w "winback %{http_code}\n"  https://apidev.loclx.io/v2/analytics/winback -H "Authorization: Bearer $TOKEN"   # 200
curl -s "https://apidev.loclx.io/v2/clientes/2575806" -H "Authorization: Bearer $TOKEN" | python3 -c 'import sys,json;p=json.load(sys.stdin)["pulso"];print("segmento",p["segmento"],"| narrativa?",bool(p["narrativa"]))'
# en server: SELECT COUNT(*) FROM MSP_AN_WINBACK_CANDIDATOS;  (~45k)
#            SELECT COUNT(*) FROM MSP_AN_CLIENTE_NARRATIVA;   (crece al abrir fichas)
```

---

_Última actualización: jun 2026. Mantener sincronizado con [[reference_windows_server_ssh]]
y la memoria del proyecto narrativa IA._
