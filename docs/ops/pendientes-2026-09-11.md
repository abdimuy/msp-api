# Pendientes abiertos — corte del 2026-09-11

Registro de lo que estaba vivo sólo en la conversación. Ordenado por lo que
cuesta dejarlo como está.

Cada punto dice **qué se midió hoy y cómo**, y qué se toma del relato sin
comprobar. Esa separación no es formalismo: una ausencia no es hallazgo hasta
demostrar que la consulta habría encontrado la fila.

## Estado del que se parte

| | | Cómo se midió |
|---|---|---|
| Producción corre | `a4f1a18` | `curl https://apidev.loclx.io/version` — 2026-09-11 |
| Commiteado y sin empujar | `536c490` | `git log origin/main` se queda en `a4f1a18` |
| Árbol | sin ningún `.go` sucio | `git status --porcelain \| grep '\.go$'` vacío |
| Escritorio | 1.25.0, con «Editar y reenviar» | del relato, sin verificar aquí |
| App Android | fix B en `main`, sin APK; falta el fix A | otro repo, sin verificar aquí |

---

## 1 · El saneado del charset, compilado y sin desplegar ⚠️

**Es lo único de esta lista que está corriendo mal en producción ahora mismo.**

`536c490` arregla que el mensaje de una `EXCEPTION` de Microsip llega en
Windows-1252 y, al escribirse en columnas nuestras que son UTF8, Firebird lo
rechaza con `-303 Malformed string` y **deshace la transacción entera**: la fila
queda sin motivo **y** con `INTENTOS` en 0.

Mientras no se despliegue siguen pasando las dos cosas:

- Los motivos de rechazo no se guardan. El log los tiene, pero `run.bat` trunca
  `api.log` en cada arranque.
- Sin `INTENTOS` que suba no hay backoff, así que el worker **reintenta cada 60
  segundos y no llega nunca a `MaxIntentos`**. Dos pagos sobregirados del 19 de
  agosto llevan desde entonces dándole vueltas a la base.

### Lo que ya está hecho

Binario cruzado y verificado el 2026-09-11:

| | |
|---|---|
| Sello | `536c490` |
| Tamaño | **48 917 504 bytes** |
| SHA-256 | `3dfc6ecdbf979019f4d5cd36341d25f72e4e6573e4aaf9e87c46340b8ede11ac` |
| Ruta | `bin/api.exe` |

El sello es fiable: `LDFLAGS` resuelve `git rev-parse --short HEAD` al leer el
Makefile, y hoy no hay ningún `.go` fuera del commit. Lo que el árbol tiene
sucio es un `.md` y dos archivos sin rastrear, que no entran al binario (ver §10).

### Lo que falta, y por qué no se pudo hacer desde aquí

Los dos pasos que quedan **exigen una persona**:

1. El túnel pinggy lo levanta quien esté en el servidor
   (`ssh -p 443 -R0:localhost:22 tcp@a.pinggy.io`), caduca a ~60 min y al
   relevantarlo cambian host y puerto.
2. **El arranque no se puede hacer por SSH.** `msp-api.exe` vive en la sesión de
   consola (1): al cerrar la sesión SSH el proceso se va con ella. Y está
   **expresamente prohibido** crear tareas programadas para arrancarlo, ni
   temporales.

Procedimiento completo en [`despliegue-produccion.md`](despliegue-produccion.md).
El resumen, con el túnel ya puesto:

```bash
KEY="-i ~/.ssh/ms_microsip -o StrictHostKeyChecking=accept-new"
PUERTO=<PUERTO>; HOST=<HOST>.run.pinggy-free.link

scp ${=KEY} -P $PUERTO bin/api.exe Administrador@$HOST:C:/msp-api/msp-api.new.exe
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'dir C:\msp-api\msp-api.new.exe'          # 48 917 504, dígito a dígito
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'cmd /c "C:\msp-api\msp-api.new.exe version"'  # → 536c490
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'copy /Y C:\msp-api\api.log C:\msp-api\api.log.antes-a4f1a18'
ssh ${=KEY} -p $PUERTO Administrador@$HOST 'cmd /c "move /Y C:\msp-api\msp-api.exe C:\msp-api\msp-api.bak-a4f1a18.exe & move /Y C:\msp-api\msp-api.new.exe C:\msp-api\msp-api.exe"'
# …y entonces, EN LA CONSOLA DEL SERVIDOR: cerrar la ventana y correr el .bat.
```

> **Esto desbloquea el §2.** El plan de «desplegar y leer los motivos reales» no
> funcionaba porque los motivos no se guardaban.

---

## 2 · La ráfaga de pagos sigue sin explicación

Se midió y **no se reprodujo**: 50 pagos concurrentes de clientes distintos, con
pool de 10 y de 50, cero fallos las dos veces — y con el control positivo puesto,
o sea que la prueba sí vería un fallo si lo hubiera.

Descartado midiendo:

- Que los pagos se estorben entre sí. Ni entre clientes distintos, ni entre pagos
  del mismo cliente, ni compartiendo el cargo.
- `GEN_FOLIO_TEMP`, leyendo el procedimiento: es `GEN_ID`, sin bloqueos.

Vivo: **contención con una sesión externa** —Cxc.exe sosteniendo la fila mientras
una persona trabaja—, que el control positivo demostró que sí mata un pago, con
firma `firebird_timeout`.

**Lo que no encaja:** el patrón observado es de tres milisegundos entre pagos, y
una caja no concentra sus víctimas en los pagos más juntos entre sí. La
correlación temporal sigue sin mecanismo. Conviene no darla por explicada.

**Siguiente paso:** desplegar §1 y leer los `ULTIMO_ERROR` reales. Si salen
`firebird_timeout`, la hipótesis de Cxc.exe queda confirmada y el arreglo va en
el servidor, sin publicar APK. Si salen otra cosa, se empieza por lo que digan.

---

## 3 · App Android — el fix A pendiente

El **fix B** (que el sync deje de pisar el `SALDO_REST` local) está en `main`,
sin APK.

Falta el **fix A**: que el cobrador vea el rechazo. Hoy `GUARDADO_EN_MICROSIP` es
booleana, así que un pago rechazado y uno aplicado se ven idénticos en la tarjeta
— los dos dicen ENVIADO. Lleva migración Room `29→30`, leer el `estado` del
`PagoRecibidoDTO` (que ya viaja y nadie lee) y que la tarjeta distinga.

**A cierra un límite conocido de B**: hoy un pago rechazado se resta para siempre
del saldo mostrado, porque sin A no hay forma de distinguir «en vuelo» de
«rechazado». Conviene publicarlos juntos, y **después** de §1, para que el texto
que se le muestre al cobrador salga de motivos reales y no inventados.

Ojo: otro agente tiene trabajo de pagos en vuelo en `feat/pagos-y-visitas`,
tocando `V2PaymentsApi.kt` —que es justo donde vive el `estado`—,
`PaymentsPendingSynchronizer` y `PaymentsWorkManagerEnqueuer`.

---

## 4 · PR #20 de Kevin, sin revisar desde el 2 de septiembre

Medido hoy con `gh pr view 20`:

| | |
|---|---|
| Título | `feat(comprobantes): storage and local delivery channel` |
| Rama | `feat/comprobantes-infra` |
| Autor | KevinA007 |
| Abierto desde | 2026-09-02 |
| Tamaño | **+2 641 / −0** |
| Estado | `OPEN`, `REVIEW_REQUIRED` |

Storage + LocalSender + los dos renderizadores de PDF, con los dos PDF de ejemplo
adjuntos como pedía el brief.

Un intento previo de revisarlo con `/code-review 20` **falló**: la herramienta
cayó a revisar el árbol de trabajo en vez del PR. Hay que relanzarla apuntando
bien. Conviene cerrarlo antes de que Kevin se meta de lleno en garantías (§6).

---

## 5 · Comprobantes — la tarea 6 nunca se hizo

**Verificado hoy:** no existe `migrations-firebird/000049_*`, y **ninguna
migración del repositorio menciona `MSP_CM_`** (`grep -rl "MSP_CM_"
migrations-firebird/` no devuelve nada; la numeración salta de `000047` a
`000050`, y la última es `000062`).

El brief está escrito y el plan marca esa migración como «bloquea todo». Sin ella
nada del módulo se ha probado nunca contra Firebird real — incluida toda la tanda
3, que exige integración con `WithTestTransaction`.

Se construyeron encima el dominio, los puertos y la infraestructura. El PR #20
(§4) es más de lo mismo: infraestructura sobre tablas que no existen.

---

## 6 · Garantías — tarea 4 con Kevin, entrega el 15 de septiembre

Reasignada el 8 de septiembre. Rama `feat/garantias-dominio` (existe local y en
`origin`), brief en `docs/superpowers/plans/garantias-task-4-brief.md` — es el
único brief de garantías **sin su `-report.md`**, las tareas 1 a 3 sí lo tienen.

**Verificado hoy** en `internal/garantias/domain/`: hay diez value objects con su
prueba —`desenlace`, `dictamen`, `estado_cuenta`, `estado_folio`, `etapa`,
`origen_folio`, `rol_articulo`, `rol_decisor`, `ruta_reparacion`, `ubicacion`—
más `transiciones.go` y `errors.go`. **Ninguna de las tres entidades.** El módulo
lleva parado exactamente ahí desde el 12 de agosto.

Detrás sólo queda `GA 0.4`, y con eso se abren cinco tareas en paralelo.

---

## 7 · `MSP_RX_MENSAJES.ERROR` — sin acotar

Encontrado al barrer para §1, y **confirmado hoy contra la migración**:

`migrations-firebird/000045_create_msp_rx_mensajes.up.sql:40` declara
`ERROR VARCHAR(500)` **sin `CHARACTER SET`** — o sea `NONE`, que cuenta bytes. El
contraste está en la misma tabla: la línea 37 sí declara `SENDER_KIND VARCHAR(12)
CHARACTER SET ASCII`, así que la omisión no es una convención del archivo.

Y `Mensaje.MarcarFallido` (`internal/reactivacion/domain/mensaje.go:136`)
asigna el motivo **sin acotar nada**:

```go
func (m *Mensaje) MarcarFallido(motivo string, now time.Time) {
	m.estado = EstadoFallido
	m.errorMotivo = motivo
	m.markUpdated(now)
}
```

No da `-303` —el saneado de §1 cubre el charset—, pero un error de Meta de más de
500 bytes rompe su `UPDATE` igual. Es además la única columna `MSP_*` nueva que
viola la regla de que todo lo nuestro va en UTF8.

---

## 8 · ANASTACIA HERNANDEZ CID y tres ventas más

**55 intentos desde el 28 de agosto**, por *«el artículo no tiene existencia
suficiente en el almacén origen»*. Es inventario, no captura: no se arregla con
código. Alguien tiene que hacer el traspaso o cerrarlas.

Que lleven dos semanas reintentando sin que nadie decida es, por sí solo, señal
de que a esa pantalla le falta una forma de decir «esto ya no va a entrar».

---

## 9 · Menores, anotados para no perderlos

- **`ERROR_CODE` no es filtrable** en el listado de intentos fallidos.
  `buildListQuery` (`internal/platform/failedintent/firebird/store.go`) sólo
  soporta `Status`, `UsuarioID`, `Modulo`, `SinExtraer`, `RutasRaiz` y cursor.
  Hoy encontrar los bloqueados por una causa concreta exige SQL directo contra
  producción. Se evaluó y **se decidió no hacerlo**: es una corrección de una
  sola vez. Si vuelve a pasar, o si el censo sale grande, se reconsidera —
  serían ~30 líneas más el parámetro HTTP, sin migración ni índice nuevo.
- **El escritorio muestra el texto crudo del API.** El mensaje de la nota decía
  «ocupa 174 bytes y microsip sólo guarda 99» — correcto, y en bytes, que no
  significa nada para quien captura. Llegó al dueño del sistema deformado como
  «la venta es muy grande», tres días después.
- **Los acentos se rompen en el Hub de Clientes** (pendiente viejo, sin tocar).

---

## 10 · Del otro agente, no de este trabajo

El árbol tiene, sin rastrear ni ignorar (medido hoy):

| Archivo | Tamaño |
|---|---|
| `winback` (binario compilado) | **18 MB** |
| `cfr2045.html` | 100 KB |
| `docs/adr/0010-…md` (modificado) | +69 / −4 |

**Un `git add -A` mete los 18 MB al historial**, de donde no salen. Conviene
ignorarlos o borrarlos antes de cualquier commit amplio.

Una revisión encontró de paso, sobre ese trabajo:

- **El mensaje de apertura no se puede enviar**: `CloudAPISender.Enviar` sólo
  manda texto libre y Meta exige plantilla aprobada para el primer contacto. Cada
  apertura devolvería 131047 → `fallido`, que es terminal. El piloto entregaría
  **cero** aperturas.
- **El consentimiento no existe en el código**: ni campo, ni columna, ni
  compuerta. El papel firmado no lo lee nadie.

No es trabajo de este corte, pero conviene que llegue a quien corresponda.
