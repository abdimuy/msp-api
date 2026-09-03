# Despliegue — que los pagos dejen de perderse (2026-09-02)

Runbook de **una** puesta en producción concreta. El procedimiento general, con
sus trampas y su razonamiento, está en
[`despliegue-produccion.md`](despliegue-produccion.md); aquí sólo va lo que
cambia para este despliegue y lo que hay que verificar que es propio de él.

**Sólo binario. Ninguna de las dos ramas trae migraciones** — comprobado con
`git diff --name-only main..<rama> -- migrations-firebird/` sobre las dos: cero
archivos.

---

## Lo que ya está hecho

| | |
|---|---|
| Fusión | `fix/pago-rechazado-visible` → `main`, luego `fix/precios-y-captura-401` |
| Commit de `main` | **`47e4494`** |
| Binario | `bin/api.exe` |
| Tamaño | **48 904 704** bytes |
| sha256 | `813bedd8ecdfe002caebaf6e518928af8266eab3724930c2a7fdff4d56efd6f8` |

> **La Parte 2 tiene ahora control positivo.** La ráfaga (50 pagos
> concurrentes de clientes distintos) sigue sin reproducir el fallo, pero ya se
> demostró que **sí vería** uno: metiéndole contención real falla exactamente
> el pago bloqueado, con `firebird_timeout`. Así que su cero significa algo
> acotado y verdadero — sin una sesión ajena estorbando no hay contención entre
> clientes distintos — y deja a **Cxc.exe escribiendo al mismo tiempo** como la
> sospecha principal. Ver el paso «qué vigilar».

Compuertas corridas sobre `main` ya fusionado, todas en verde:

```
golangci-lint run ./...        → 0 issues
go test -race -short ./...     → sin fallos
make test-firebird-all         → EXIT=0, 20 paquetes ok, 0 FAIL
```

**Falta empujar `main` al remoto.** No se hizo: es una acción hacia afuera y la
decisión es del dueño del repositorio. El despliegue compila de local, así que
no bloquea nada.

```bash
git push origin main
```

---

## Paso 0 — el censo, antes de tocar nada

Lleva días sin mirarse y el worker sigue muerto, así que la lista de hoy puede
ser más larga que la del plan. **Todo lo que salga se aplicará en el primer
minuto tras arrancar**, así que hay que saber qué es antes de arrancar, no
después.

```sql
SELECT ID, CARGO_DOCTO_CC_ID, CLIENTE_ID, COBRADOR, IMPORTE,
       FECHA, INTENTOS, RECEIVED_AT
FROM MSP_PAGOS_RECIBIDOS WHERE ESTADO = 'P' ORDER BY RECEIVED_AT;
```

Por **cada cargo que no esté en la lista de siete de abajo**, comprobar que no
tenga ya el abono — si lo tiene, es un duplicado y no debe aplicarse:

```sql
SELECT COUNT(*) FROM IMPORTES_DOCTOS_CC
WHERE DOCTO_CC_ACR_ID = <CARGO> AND DOCTO_CC_ID <> <CARGO>;
```

> `isql` en producción exige `localhost:` y credenciales, y `ssh <HOST> cmd <
> archivo` **no funciona** (el stdin no cruza el túnel): hay que `scp` el `.sql`
> y correr `isql -i`. Ver `despliegue-produccion.md` §11.

### Los siete que entran solos

Medidos uno a uno: todos con cargo con saldo suficiente, ninguno cancelado,
ninguno cobrado por otra vía. **$1,650** en total.

| Cliente | Importe | Fecha | Cargo |
|---|---|---|---|
| Romina Jocelyn de la Luz | $300 | 29-ago | 15474411 |
| Juana Méndez Morales | $150 | 30-ago | 13705244 |
| Araceli García Juárez | $500 | 1-sep | 15655374 |
| Isidro Rodríguez Jiménez | $300 | 1-sep | 14394259 |
| Laura Moreno Leandro | $150 | 1-sep | 14611617 |
| María Rebeca de Jesús Leyva | $150 | 1-sep | 15101701 |
| Celso Facundo Alejo Domínguez | $100 | 1-sep | 14942233 |

### Las dos del 19 de agosto — no requieren acción

Son duplicados de pagos ya aplicados. Sus cargos están en saldo cero, Microsip
las rechazará, el worker se rendirá al llegar a 10 intentos y dejará el motivo
escrito. **Se mencionan para que nadie se alarme al verlas fallar en el log.**

---

## Pasos 1 a 5 — el binario

Idénticos a `despliegue-produccion.md` §3 a §7. Sólo los valores de este
despliegue:

```bash
# §3 — subir con otro nombre y verificar ANTES de intercambiar
scp ${=KEY} -P $PUERTO bin/api.exe Administrador@$HOST:C:/msp-api/msp-api.new.exe
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'dir C:\msp-api\msp-api.new.exe'
#   → tiene que decir 48,904,704 EXACTO. «Casi igual» es un scp truncado.
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'cmd /c "C:\msp-api\msp-api.new.exe version"'
#   → msp-api 47e4494 (built …)   ← no arranca el servidor, se puede correr con el viejo sirviendo

# §4 — respaldar el log ANTES de reiniciar: run.bat lo TRUNCA en cada arranque
curl -s https://apidev.loclx.io/version          # ← anota el HASH_SALIENTE
ssh ${=KEY} -p $PUERTO Administrador@$HOST \
  'copy /Y C:\msp-api\api.log C:\msp-api\api.log.antes-<HASH_SALIENTE>'

# §5 — el intercambio, con el API corriendo. LOS DOS `move` EN LA MISMA LÍNEA.
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'cmd /c "move /Y C:\msp-api\msp-api.exe C:\msp-api\msp-api.bak-<HASH_SALIENTE>.exe & move /Y C:\msp-api\msp-api.new.exe C:\msp-api\msp-api.exe"'
```

**§6 — el reinicio va en la consola física del servidor**, cerrando la ventana
donde corre `msp-api.exe` y ejecutando el `.bat` del escritorio, que termina en
`C:\msp-api\run.bat`. **Prohibido crear tareas programadas**, ni temporales, ni
«sólo por esta noche».

**§7 — verificar son TRES comprobaciones, no una:**

1. `http: listening addr=0.0.0.0:3011` y los `lifecycle: started` en el log
   nuevo. Un `api.log` **vacío** con el proceso muerto es `fx.NopLogger`
   tragándose un error de arranque, no un log que falta.
2. `curl -s https://apidev.loclx.io/version` → **`47e4494`**.
3. Cruzar el PID: `netstat -ano | findstr ":3011" | findstr LISTENING`.

---

## Paso 6 — que el worker vive y drena

Es la prueba de que el arreglo funciona, y es lo único de este despliegue que no
está en el procedimiento general.

```bash
ssh ${=KEY} -p $PUERTO Administrador@$HOST \
  'powershell -NoProfile -Command "Select-String -Path C:\msp-api\api.log -Pattern pago_retry"'
```

Lo que debe verse en el **primer minuto** — el ticker es de 60 s y no dispara
al arrancar, dispara al cumplirse el primer intervalo:

```
pago_retry.tick  scanned=<n del censo>  applied=<subiendo>  skipped=0  failed=<las 2 del 19-ago>
```

> ### Corrección al plan: el silencio de `pago_retry.tick` es el éxito, no la muerte
>
> El plan pedía vigilar que `pago_retry.tick` «aparezca repetidamente, no una
> sola vez», y tratar el silencio como señal de que el worker volvió a morir.
> **Eso está al revés**, y se ve en el código:
>
> ```go
> // internal/cobranza/app/pago_retry_worker.go:176-178
> if len(pendientes) == 0 {
>     return                 // ← sale ANTES de escribir el log
> }
> ```
>
> `ListPendientes` filtra `WHERE ESTADO='P' AND INTENTOS < 10`
> (`pagos_recibidos_repo.go:133-137`). Cuando los siete se apliquen y las dos
> del 19 de agosto lleguen a 10 intentos, la consulta devolverá vacío y
> **`pago_retry.tick` dejará de aparecer del todo**. Eso es exactamente lo que
> tiene que pasar.
>
> Así que la señal correcta es:
>
> - **al arrancar**, con el censo no vacío: el tick tiene que aparecer. Si no
>   aparece ninguno, el arreglo no entró.
> - **después de drenar**: el silencio es correcto. Para saber que el worker
>   sigue vivo hace falta que exista una fila en `ESTADO='P'`, no leer el log.

**Y la columna `'A'` no es la prueba.** Hay que confirmar en Microsip que el
abono existe de verdad:

```sql
-- los siete deben quedar en 'A'
SELECT ID, ESTADO, INTENTOS, ULTIMO_ERROR FROM MSP_PAGOS_RECIBIDOS
WHERE CARGO_DOCTO_CC_ID IN (15474411,13705244,15655374,14394259,14611617,15101701,14942233);

-- y el abono tiene que existir en Microsip. TIPO_IMPTE='R' es obligatorio:
-- sin él se cuenta también el renglón del propio cargo y el número sale al doble.
SELECT DOCTO_CC_ACR_ID, COUNT(*) FROM IMPORTES_DOCTOS_CC
WHERE TIPO_IMPTE = 'R'
  AND DOCTO_CC_ACR_ID IN (15474411,13705244,15655374,14394259,14611617,15101701,14942233)
GROUP BY DOCTO_CC_ACR_ID;
-- → siete filas, cada una con COUNT(*) = 1
```

---

## Paso 7 — el escritorio

**API antes que escritorio. Al revés da 405.** Versión actual **1.21.0**: bump
en los cuatro archivos, `git tag vX.Y.Z`, `git push --tags` dispara
`release.yml`.

**La release nace en borrador y el actualizador no ve los borradores: hay que
pasarla a publicada a mano.** Los usuarios la reciben en ≤30 min o al reiniciar.

---

## Paso 8 — la app

Cuando estén las Partes 2 y 3. No en este viaje.

---

## Reversión

`despliegue-produccion.md` §10. El `move` inverso:

```bash
ssh ${=KEY} -p $PUERTO Administrador@$HOST \
  'copy /Y C:\msp-api\api.log C:\msp-api\api.log.fallido-47e4494'
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'cmd /c "move /Y C:\msp-api\msp-api.exe C:\msp-api\msp-api.bak-47e4494.exe & move /Y C:\msp-api\msp-api.bak-<HASH_SALIENTE>.exe C:\msp-api\msp-api.exe"'
# reiniciar EN LA CONSOLA, y confirmar:
curl -s https://apidev.loclx.io/version   # → <HASH_SALIENTE>
```

Los commits están separados por pieza: **el arreglo del worker (`5170b7e`) se
puede revertir solo** sin tocar el resto.

---

## Qué vigilar la primera semana

- **Filas nuevas con `ESTADO='P'`, y su `ULTIMO_ERROR`.** Cada una es un pago
  que antes se perdía en silencio.

  **La firma que hay que buscar es `firebird_timeout`** — o
  `firebird_lock_conflict`. Eso ya no es una corazonada: el control positivo de
  la ráfaga lo midió. Basta con que **una** sesión ajena sostenga la fila de
  `SALDOS_CC` de un cliente para que el pago de ese cliente muera con
  `firebird_timeout`, mientras los demás pasan intactos. Y `SALDOS_CC` es por
  cliente y **mes**, así que una caja cobrando le estorba a cualquier pago del
  mismo cliente.

  Ojo con una diferencia al leer los motivos: en la prueba el corte lo dio el
  **techo del servidor**, puesto a 2 s a propósito. En producción el techo por
  omisión es de **diez minutos** (`FB_STATEMENT_TIMEOUT`, `config.go:379`), así
  que ahí corta primero el lado del llamador y entra al mismo código por la
  **otra** rama de `MapError` (`context.Canceled` / `DeadlineExceeded`,
  `errors.go:41-46`). Mismo código, distinto productor — no confundir uno con
  otro al diagnosticar.

  Si en cambio los motivos NO son de contención, la sospecha de la ráfaga cae
  entera y hay que volver a empezar con lo que digan. Que es para lo que sirve
  desplegar.

```sql
SELECT ID, CARGO_DOCTO_CC_ID, CLIENTE_ID, INTENTOS, ULTIMO_ERROR, RECEIVED_AT
FROM MSP_PAGOS_RECIBIDOS WHERE ESTADO = 'P' ORDER BY RECEIVED_AT DESC;
```
- **La pantalla de intentos fallidos** — un rechazo nuevo debe aparecer con su
  motivo.
- **El directorio de blobs** — la captura del 401 escribe el cuerpo por cada
  intento sin autenticar, y el path de pagos no manda clave de idempotencia.
  Consultas de vigilancia en [`captura-del-401.md`](captura-del-401.md).
- **`pago_retry.tick`** — con la lectura corregida de arriba: presente mientras
  haya pendientes, silencioso cuando no los haya.

---

## Lo que este despliegue NO arregla

- **Por qué Microsip rechaza un pago que liquida el saldo al peso exacto.**
  Medido en dos casos del 19 de agosto; falta leer el resto de `SALDO_CARGO_CC`.
- **Los precios ya escritos en Microsip.** La venta de las 8 sillas y las que el
  detector encuentre tienen `PRECIO_DE_CONTADO` y `MONTO_A_CORTO_PLAZO`
  multiplicados de más, y eso alimenta el «Hoy liquida con» del cobrador y el
  ticket. Reparación fila por fila, después de medir. La regla nueva impide que
  entren más; no repara las que ya están.
- **Las cinco ventas con parcialidad al 2.5% del techo de Microsip.**
