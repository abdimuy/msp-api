# Runbook — Ingeniería inversa de Microsip en el Windows Server (trace)

> Cómo capturar y analizar lo que hace Microsip (Firebird) sobre la base de datos,
> para entender flujos (alta de venta, cobros, etc.). Escrito después de batallar
> mucho con esto — **lee los "callejones sin salida" antes de intentar nada**.
>
> Resultado de aplicar este runbook: ver `microsip-venta-flow.md` y
> `microsip-crear-venta-paso-a-paso.md`.

## Entorno

| Cosa | Valor |
|---|---|
| Server | Windows Server 2016, Firebird **3.0.11**, ServerMode **Super** (un solo engine ve toda la actividad) |
| Servicio Firebird | `FirebirdServerDefaultInstance` (arranca solo al bootear) |
| isql (en el server) | `C:\PROGRA~1\Firebird\Firebird_3_0\isql.exe` |
| DB de desarrollo | `C:\Microsip datos\DESARROLLO.FDB` (short: `C:\MICROS~1\DESARROLLO.FDB`) |
| DB de producción (NO TOCAR) | `MUEBLERA_SNP.FDB` |
| Credenciales | `SYSDBA` / `masterkey` |
| Acceso | SSH a Windows vía **túnel pinggy** + llave `/tmp/ms-ssh/id_ms` |
| Cliente Microsip PV | `C:\Program Files (x86)\Microsip\2025\PVenta.exe` |

### Copia en Docker (para esquema y pruebas con rollback, sin tocar el server)
Hay una copia de la base en un contenedor local:
```bash
docker exec -i mueblera-firebird /usr/local/firebird/bin/isql \
  -user SYSDBA -password masterkey /firebird/data/MUEBLERA.FDB < query.sql
```
- Imagen `jacobalberty/firebird:v4.0` (¡FB **4.0**, no 3.0.11! — el esquema/SP es igual, pero ojo con diferencias de motor).
- Ideal para: leer esquema, leer triggers/SP, y **probar inserts con `ROLLBACK`** sin riesgo. Úsala antes de escribir en el server real.

## Paso 0 — Levantar el túnel SSH (pinggy)

El túnel **se corre EN el Windows Server** (PowerShell/cmd), expone su SSH:
```
ssh -p 443 -R0:localhost:22 tcp@a.pinggy.io
```
- Imprime `tcp://XXXX.run.pinggy-free.link:PUERTO` → ese es el endpoint.
- ⚠️ **El free expira a los ~60 min y cambia de host/puerto cada vez.** Si a media
  tarea "Connection refused" o "could not resolve hostname", el túnel se cayó →
  pídelo de nuevo. Hay que dejar la ventana abierta.

Conexión desde el Mac:
```bash
ssh -i /tmp/ms-ssh/id_ms -p <PUERTO> -o StrictHostKeyChecking=accept-new \
  Administrador@<HOST>.run.pinggy-free.link "echo OK"
```

## Cómo correr SQL en el server (sin infierno de comillas)

Pipea el SQL por **stdin** a isql (evita escapar comillas a través de SSH→PowerShell):
```bash
ssh -i /tmp/ms-ssh/id_ms -p <PUERTO> Administrador@<HOST> \
  '"C:\PROGRA~1\Firebird\Firebird_3_0\isql.exe" -user SYSDBA -password masterkey "C:\MICROS~1\DESARROLLO.FDB"' \
  < query.sql
```
Para leer un SP/trigger (BLOB de texto): `SET BLOB ALL;` + `SET HEADING OFF;` arriba del `SELECT RDB$PROCEDURE_SOURCE ...`.

## El trace — qué SÍ funciona y qué NO

### ❌ Callejón sin salida #1: Audit trace a archivo (`AuditTraceConfigFile`)
**NO sirve en Firebird 3.0.11 en Windows.** El `log_filename` con cualquier ruta
absoluta tira `"pattern is invalid"`. El valor se procesa con sintaxis sed y
`fixupSeparators` colapsa los separadores a un único `\` antes de compilar el
patrón, dejando `\a` (de `\audit…`) como escape inválido. Probamos `\\`, `\\\\`,
`/`, `//`, comillas — **nada**. Es el bug FirebirdSQL/firebird#8238, sin fix en
3.0.11. **No pierdas tiempo aquí.**

### ❌ Callejón sin salida #2: `fbtracemgr` lanzado directo por SSH
Muere a los ~2 seg (`TRACE_FINI` prematuro) porque el proceso queda atado al árbol
de la sesión SSH y se mata cuando el comando retorna. Captura fragmentos pero
nunca el guardado completo.

### ✅ Lo que SÍ funciona: `fbtracemgr` como Tarea Programada SYSTEM
Desprende el proceso de la sesión SSH (sobrevive) y escribe por **stdout**
redirigido a archivo (esquiva el bug de `log_filename`).

**Estos pasos son escrituras/servicios en el server → los corre el USUARIO**
(el clasificador de seguridad bloquea al agente para infra de producción).

1. **Config de sesión** `C:\trace_dev.cfg` (SIN `log_filename`):
   ```
   database = %DESARROLLO.FDB
   {
       enabled = true
       log_connections = true
       log_statement_finish = true
       log_procedure_finish = true
       log_trigger_finish = true
       time_threshold = 0
       max_sql_length = 65536
       max_arg_length = 8192
       max_arg_count = 1000
   }
   ```
   - `%DESARROLLO.FDB` (matcher SIMILAR TO) → **solo** loguea esa DB, no producción.
   - `time_threshold = 0` → todo. `max_*` grandes → no trunca SQL ni params (clave
     para ver los parámetros de los `EXECUTE_PROCEDURE`).

2. **Wrapper** `C:\run_fbtrace.cmd` (ASCII, sin BOM):
   ```
   "C:\Program Files\Firebird\Firebird_3_0\fbtracemgr.exe" -se localhost/3050:service_mgr -user SYSDBA -password masterkey -start -name capdev -config C:\trace_dev.cfg > C:\capture.log 2>&1
   ```

3. **Crear y lanzar la tarea como SYSTEM** (PowerShell Admin):
   ```powershell
   Remove-Item 'C:\capture.log' -ErrorAction SilentlyContinue
   schtasks /Create /TN fbtrace_dev /TR "C:\run_fbtrace.cmd" /SC ONCE /ST 00:00 /RU SYSTEM /RL HIGHEST /F
   schtasks /Run /TN fbtrace_dev
   ```
   (El warning "/ST es anterior a la hora actual" es normal; igual corre con `/Run`.)

4. **Verificar que la sesión está activa** (read-only, desde el Mac) y anotar el `id`:
   ```bash
   ssh ... '"C:\Program Files\Firebird\Firebird_3_0\fbtracemgr.exe" -se localhost/3050:service_mgr -user SYSDBA -password masterkey -list'
   ```
   Busca `name: capdev` con `flags: active, trace`. (Aparece también `Firebird Audit`
   = sesión de sistema; ignórala.)

5. **Ejecutar la acción a capturar** en Microsip (crear la venta, etc.).

6. **Detener** (flushea el archivo) — usuario, con el `id` del paso 4:
   ```powershell
   & 'C:\Program Files\Firebird\Firebird_3_0\fbtracemgr.exe' -se localhost/3050:service_mgr -user SYSDBA -password masterkey -stop -id <ID>
   Start-Sleep 2
   Get-Process fbtracemgr -ErrorAction SilentlyContinue | Stop-Process -Force
   ```

7. **Traer el log al Mac** y analizar (read-only):
   ```bash
   scp -i /tmp/ms-ssh/id_ms -P <PUERTO> Administrador@<HOST>:'C:/capture.log' /tmp/ms-trace/capture.log
   ```

## Analizar el `capture.log`

Formato: bloques que empiezan con `<timestamp> (pid:thread) <EVENTO>`, luego
líneas indentadas (DB/usuario/charset, exe, transacción), luego el cuerpo
(`Procedure NOMBRE:`, `Statement N:` + SQL, o `TRIG... FOR tabla (timing)`).

Recetas útiles:
```bash
# tipos de evento
grep -oE '\) [A-Z_]+_(START|FINISH)' capture.log | sort | uniq -c
# encontrar el INSERT del header y su transacción
grep -nE 'INSERT INTO DOCTOS_PV[^_]' capture.log
# transacción más activa (la del save suele ser la de los writes)
grep -oE 'TRA_[0-9]+' capture.log | sort | uniq -c | sort -rn | head
```
Para reconstruir el flujo cronológico de UNA transacción (procedures + statements
+ triggers en orden) conviene un parser; ver `parse.py`/`writes.py` que se usaron
(dividir por la línea de timestamp, filtrar por `TRA_xxxxx`, y para cada bloque
sacar `Procedure`/`Statement`/`TRIG`). Ojo: el trace loguea **también** los
procedures anidados (PSQL), no solo los que llama el cliente.

## Gotchas (cosas que nos quemaron tiempo)

- **Túnel pinggy expira (~60 min) y rota host/puerto.** Si algo deja de conectar,
  es eso. Relevántalo y usa el nuevo endpoint.
- **`isql` trunca CHAR/columnas largas** con `arithmetic exception ... string
  right truncation`. Solución: `CAST(TRIM(col) AS VARCHAR(n))` con `n` suficiente,
  o `SET LIST ON;` para ver fila vertical.
- **`RDB$` en SQL por SSH**: si va dentro de comillas dobles de bash/PowerShell, el
  `$` se expande. Pipea por stdin (mejor) o escapa `\$`.
- **El clasificador de seguridad bloquea al agente** para: editar `firebird.conf`,
  `Restart-Service`, y en general escrituras/commits a la DB de producción. Esos
  pasos los hace el usuario (o autoriza explícitamente). Las lecturas (SELECT,
  `-list`, scp del log) sí las hace el agente.
- **Reinicio = corte server-wide.** `Restart-Service FirebirdServerDefaultInstance`
  desconecta a TODOS (todas las empresas, incl. producción) ~5 seg. Hacerlo sin
  actividad crítica. (Para el método que funciona, **no hace falta reiniciar**.)
- **Para pruebas de escritura**: hazlas en una transacción y termina con
  `ROLLBACK` (o usa el Docker). Para capturar IDs autogenerados sin variables de
  isql, usa un `EXECUTE BLOCK ... RETURNS (...) ... SUSPEND;`.

## Limpieza al terminar (paso F)

Parte sin reinicio (la puede hacer el agente):
```powershell
schtasks /Delete /TN fbtrace_dev /F
Remove-Item 'C:\trace_dev.cfg','C:\run_fbtrace.cmd','C:\capture.log' -Force -ErrorAction SilentlyContinue
```
Parte con reinicio (usuario, cuando no haya actividad crítica) — solo si se usó el
audit (`AuditTraceConfigFile`):
```powershell
$fb='C:\Program Files\Firebird\Firebird_3_0\firebird.conf'
$raw=[IO.File]::ReadAllText($fb) -replace 'AuditTraceConfigFile = C:\\audit_dev\.conf','#AuditTraceConfigFile ='
[IO.File]::WriteAllText($fb,$raw,(New-Object System.Text.UTF8Encoding($false)))
Remove-Item 'C:\audit_dev.conf' -Force
Restart-Service FirebirdServerDefaultInstance
```
