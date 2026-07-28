# Módulo de asistencia — diseño

- **Fecha:** 2026-07-28
- **Estado:** aprobado, listo para plan de implementación
- **Módulo:** `internal/asistencia`
- **Migración:** `000048_create_msp_as_asistencia` (la `000047` es de garantías)
- **Prefijo de tablas:** `MSP_AS_*`
- **Base:** [ADR-0009](../../adr/0009-asistencia-sealed-module.md) · [investigación](../../research/checador-biometrico-estado-del-arte.md) · [catálogo de features](../../research/checador-features-checklist.md)

## 1. Objetivo y alcance

Registrar electrónicamente la jornada de cada trabajador: a qué hora llega, a qué hora sale, cuánto tiempo trabajó, y qué días faltó.

### Dentro del alcance

La **API** de asistencia: recepción de marcajes, custodia de plantillas biométricas cifradas, catálogo de empleados y equipos, horarios esperados, cálculo de la jornada, correcciones con bitácora, y las consultas de la consola.

### Fuera del alcance

El kiosko y la consola de escritorio, el SDK del lector de huella y la síntesis de voz. Todo eso vive en la aplicación de escritorio, que es proyecto aparte. La API no sabe qué marca de lector se usa ni cómo se ve la pantalla.

Esa separación es lo que permite que la misma API sirva a un equipo con monitor y a uno sin monitor. Las variantes de montaje son decisiones del cliente.

### Montaje decidido

PC con lector de huella conectado, **con o sin monitor**. Sin monitor, la confirmación es por voz y el respaldo por número entra desde un teclado numérico USB. Varias PCs, y cualquier empleado puede marcar en cualquiera. La administración vive en la aplicación de escritorio, que siempre tiene monitor.

## 2. Decisiones de arquitectura

### 2.1 Módulo sellado

Aplicación directa de ADR-0009, que se escribió para este módulo.

- Cero imports de otros módulos, incluidos sus paquetes de contratos. Solo stdlib, `uuid`, `decimal`, `internal/asistencia/…` e `internal/platform/…`.
- Catálogo de empleados propio. La duplicación respecto a Firestore o Microsip está aceptada en el ADR; el módulo puede guardar un identificador externo opcional, pero **nunca lo resuelve importando otro módulo**.
- Tablas `MSP_AS_*` sin llave foránea hacia afuera.
- `internal/asistencia/module.go` expone un `fx.Option` que el composition root incluye en una línea.
- Firebird queda detrás de los repositorios.
- Verificado por `make check-sealed`, que recorre los módulos de `SEALED_MODULES` y omite los que aún no existen. La regla estática `asistencia-sealed` de `.golangci.yml` rechaza el import antes de que compile. Ambos ya están en `main` (`f6c53ab`), verificados contra una fuga real, y `make check-sealed` corre en `pre-push`.

### 2.2 La frontera de confianza

El cliente compara la huella y decide **quién es**. Eso es todo lo que decide.

| Lo decide el cliente | Lo decide la API |
|---|---|
| Qué empleado es | Si el marcaje es entrada, comida, regreso o salida |
| Qué método se usó (huella o número) | Si es un rebote y se suprime del cálculo |
| El instante, **solo si está sin red** | El instante, siempre que haya red |
| | A qué jornada lógica pertenece |
| | Si hubo retardo, falta o marcaje sin horario |

El cliente **nunca** manda "esto es una entrada". Manda "el empleado 7 puso el dedo a las 8:03 en el equipo BODEGA-1" y la API infiere el resto. Si el cliente decidiera el tipo de marcaje, un error de la aplicación o una aplicación modificada corrompería el registro sin posibilidad de recalcularlo.

### 2.3 Dónde vive el dato biométrico

**La plantilla se guarda cifrada en la API. La comparación ocurre en la aplicación de escritorio.**

La comparación tiene que ocurrir en el cliente porque los SDK de lectores son DLL de Windows y `CLAUDE.md` §5 exige cross-compilar el binario Go con `CGO_ENABLED=0`. No hay forma de enlazarlos desde el servidor sin un segundo servicio nativo.

Cada equipo se autentica al arrancar, descarga las plantillas cifradas y **las mantiene solo en memoria**. Nunca en disco. Esto es lo que evita que una PC de bodega perdida, vendida o robada se lleve una copia completa de la base biométrica — que es el riesgo real de haber decidido que cualquiera marca en cualquier equipo.

**Lo que este diseño no protege:** alguien con permisos de administrador sobre la PC de marcaje puede modificar la aplicación y enviar marcajes falsos. Cerrarlo exige una terminal autónoma con reloj y matching propios (arquitectura C de la investigación, §9), descartada por costo. Es una decisión consciente, no un descuido.

### 2.4 Identificación 1:N

El empleado pone el dedo primero; el sistema deduce quién es. Decisión del negocio del 2026-07-21, documentada en la investigación §7 en contra de la recomendación del INAI de priorizar verificación 1:1.

Se sostiene porque con ~15 personas el riesgo de coincidencia falsa es despreciable, y porque el número sigue existiendo como credencial de respaldo, así que el flujo se puede invertir sin rehacer el modelo de datos. **Revisar si la plantilla pasa de ~50 personas.**

Consecuencia obligatoria: **la confirmación debe decir el nombre en voz alta.** Con el dedo primero el sistema adivina la identidad; si acierta mal, la persona solo puede notarlo si el equipo dice "Bertha Solís, entrada". Sin eso, una coincidencia falsa se registra en silencio y aparece hasta la quincena. Es requisito del cliente, no de la API.

### 2.5 Alcance de cumplimiento — decisión explícita

Se construyen **solo las dos decisiones de esquema** que son gratis ahora y caras después:

1. **La plantilla biométrica vive en su propia tabla**, con política de borrado independiente de las checadas.
2. **El marcaje crudo es inmutable y append-only.**

Ambas son buena ingeniería aunque no existiera regulación. Lo demás — flujo de consentimiento firmado, aviso de privacidad versionado, ciclo retención→bloqueo→supresión, reportes de cumplimiento — **queda fuera de alcance por decisión del negocio**, pese a que la investigación (§5) documenta que la LFT art. 132 fr. XXXIV obliga al registro electrónico de jornada con multa de 250 a 5,000 UMA, y que la LFPDPPP trata la huella como dato sensible con agravante sancionador.

Queda registrado aquí para que la decisión sea localizable, no para reabrirla.

## 3. Modelo de datos

Diez tablas `MSP_AS_*`. Dos separaciones gobiernan el diseño.

**`MSP_AS_PLANTILLA` está sola.** Es la única tabla con dato biométrico. Al terminar la relación laboral se borra esa tabla y nada más: los marcajes se conservan. Si la plantilla fuera una columna de `MSP_AS_EMPLEADO`, dar de baja obligaría a elegir entre perder el historial o conservar la huella.

**`MSP_AS_MARCAJE` es el hecho; `MSP_AS_JORNADA` es una opinión recalculable.** La jornada se puede vaciar por completo y reconstruir. Nunca es fuente de verdad.

### 3.1 Catálogo

**`MSP_AS_SEDE`** — `ID` `CHAR(36)` ASCII PK · `NOMBRE` `VARCHAR(120)` UTF8 · `CREATED_AT` · `UPDATED_AT`

**`MSP_AS_EMPLEADO`**

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `NUMERO` | `VARCHAR(10)` ASCII | UNIQUE. El que se teclea cuando falla la huella |
| `NOMBRE` | `VARCHAR(200)` UTF8 | |
| `SEDE_ID` | `CHAR(36)` ASCII | FK → `MSP_AS_SEDE` |
| `REFERENCIA_EXTERNA` | `VARCHAR(64)` UTF8 | Identificador opcional de otro sistema. **Nunca se resuelve** |
| `FECHA_BAJA` | `DATE` | Nullable. No hay borrado lógico |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | |

**`MSP_AS_EQUIPO`**

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `CLAVE` | `VARCHAR(40)` ASCII | UNIQUE. Ej. `BODEGA-1` |
| `NOMBRE` | `VARCHAR(120)` UTF8 | |
| `SEDE_ID` | `CHAR(36)` ASCII | FK |
| `SECRETO_HASH` | `CHAR(64)` ASCII | Credencial del equipo |
| `ULTIMO_CONTACTO` | `TIMESTAMP` | Heartbeat. Es también prueba (§5.3) |
| `DESFASE_RELOJ_SEG` | `INTEGER` | Último desfase medido |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | |

### 3.2 `MSP_AS_PLANTILLA` — el dato biométrico

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `EMPLEADO_ID` | `CHAR(36)` ASCII | FK. Varios renglones por persona |
| `DEDO` | `VARCHAR(20)` ASCII | Cuál dedo |
| `DATOS` | `BLOB SUB_TYPE BINARY` | La plantilla **cifrada** |
| `ALGORITMO` | `VARCHAR(32)` ASCII | Qué SDK y versión la generó |
| `CALIDAD` | `SMALLINT` | Nullable |
| `ENROLADA_EN`, `ENROLADA_POR` | | |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | |

Se enrolan **varios dedos por persona**: uno cortado o sucio no debe dejar a nadie fuera, y el desgaste en bodega es real.

**`ALGORITMO` importa más de lo que parece.** Las plantillas no son portables entre marcas de lector. Si se cambia de proveedor, esa columna es lo que dice cuáles hay que volver a capturar.

Nunca se guarda la imagen de la huella, solo la plantilla.

### 3.3 `MSP_AS_MARCAJE` — el evento crudo, inmutable

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `EMPLEADO_ID` | `CHAR(36)` ASCII | |
| `EQUIPO_ID` | `CHAR(36)` ASCII | |
| `SEDE_ID` | `CHAR(36)` ASCII | Copiada del equipo al momento del marcaje, no por join |
| `METODO` | `VARCHAR(12)` ASCII | `huella` \| `numero` |
| `INSTANTE` | `TIMESTAMP` | El instante bueno (§5) |
| `INSTANTE_EQUIPO` | `TIMESTAMP` | Reloj del cliente, siempre se guarda |
| `INSTANTE_SERVIDOR` | `TIMESTAMP` | Recepción |
| `CLAVE_IDEMPOTENCIA` | `CHAR(36)` ASCII | UNIQUE |
| `ORIGEN` | `VARCHAR(12)` ASCII | `lector` \| `capturado` \| `sistema` |
| `SUPRIMIDO_POR` | `VARCHAR(16)` ASCII | Nullable: `rebote` |
| `CREATED_AT` | `TIMESTAMP` | |

**Sin `UPDATED_AT` y sin borrado.** Un rebote no se elimina: se marca `SUPRIMIDO_POR='rebote'` y deja de contar. BioTime lo descarta y lo pierde; aquí se conserva, porque un marcaje descartado es justo lo que hay que poder mostrar cuando alguien reclame.

**`ORIGEN` es la columna más importante de la tabla.** Distingue lo que atestiguó una persona (`lector`) de lo que capturó administración a mano (`capturado`) de lo que generó el cierre automático (`sistema`). Es lo que la consola pinta distinto, y lo que hace que el número sea defendible en vez de solo existir.

`SEDE_ID` se copia al momento del marcaje: si mañana mueven la PC de bodega a oficina, el histórico no debe cambiar de sede retroactivamente.

### 3.4 `MSP_AS_HORARIO` — el horario esperado, versionado

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `EMPLEADO_ID` | `CHAR(36)` ASCII | FK |
| `DIA_SEMANA` | `SMALLINT` | 1..7 |
| `ENTRADA` | `TIME` | NULL = día de descanso |
| `COMIDA_INICIO` | `TIME` | Nullable |
| `COMIDA_FIN` | `TIME` | Nullable |
| `SALIDA` | `TIME` | NULL = día de descanso |
| `VIGENTE_DESDE` | `DATE` | |
| `VIGENTE_HASTA` | `DATE` | Nullable = vigente |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | |

Siete renglones por persona y por vigencia.

**El rango de fechas resuelve una trampa concreta:** si hoy se cambia el horario de alguien, los retardos del mes pasado **no** se recalculan, porque el cálculo busca el horario vigente ese día. Como la jornada es recomputable, sin versionado un cambio de horario reescribiría el pasado en silencio.

`COMIDA_INICIO` y `COMIDA_FIN` existen por la decisión de §4.2: los marcajes se clasifican por cercanía al evento esperado, no por orden de llegada.

### 3.5 `MSP_AS_JORNADA` — el cálculo derivado

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `EMPLEADO_ID` | `CHAR(36)` ASCII | |
| `FECHA` | `DATE` | La jornada lógica |
| `ENTRADA`, `SALIDA` | `TIMESTAMP` | Nullable |
| `MINUTOS_PRESENCIA` | `INTEGER` | Bruto: salida − entrada |
| `MINUTOS_COMIDA` | `INTEGER` | Medido, no estimado |
| `MINUTOS_NETOS` | `INTEGER` | Presencia − comida |
| `ESTADO` | `VARCHAR(20)` ASCII | `completa` \| `incompleta` \| `falta` \| `sin_horario` \| `descanso` |
| `RETARDO_MINUTOS` | `INTEGER` | |
| `MARCAJES_GENERADOS` | `SMALLINT` | Cuántos puso el sistema |
| `DESFASE_SOSPECHOSO` | `SMALLINT` | 1 si algún marcaje excedió el umbral de reloj |
| `REVISADA_EN`, `REVISADA_POR` | | Nullable |
| `CALCULADA_EN` | `TIMESTAMP` | |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | |

Es una **proyección materializada**, no un dato original. Se puede truncar entera y reconstruir desde `MSP_AS_MARCAJE`.

`REVISADA_EN` implementa el *"ya lo vi, decidí no hacer nada"*. Sin esa acción la lista de revisión nunca se vacía y la gente deja de mirarla — que es la forma en que estos módulos mueren.

### 3.6 `MSP_AS_CORRECCION` — bitácora, inmutable

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `MARCAJE_ID` | `CHAR(36)` ASCII | El marcaje creado, ajustado o suprimido |
| `EMPLEADO_ID` | `CHAR(36)` ASCII | |
| `TIPO` | `VARCHAR(16)` ASCII | `alta` \| `ajuste` \| `supresion` |
| `VALOR_ANTERIOR` | `VARCHAR(40)` ASCII | Nullable |
| `VALOR_NUEVO` | `VARCHAR(40)` ASCII | |
| `MOTIVO` | `VARCHAR(300)` UTF8 | **NOT NULL** |
| `USUARIO` | `VARCHAR(64)` UTF8 | |
| `CREATED_AT` | `TIMESTAMP` | |

Corregir nunca borra: se agrega un marcaje con `ORIGEN='capturado'` o se suprime uno existente, y en ambos casos queda el renglón de bitácora con motivo obligatorio.

**Regla de dominio: nadie corrige sus propios marcajes.** Se resuelve comparando `REFERENCIA_EXTERNA` del empleado contra el usuario que devuelve el puerto `Identity`. Cuesta una línea y cierra el único conflicto de interés real del módulo. Se conserva incluso habiendo un solo administrador — precisamente porque hay uno solo.

### 3.7 Configuración y festivos

**`MSP_AS_CONFIG`** — un solo renglón:

| Columna | Tipo | Usada en |
|---|---|---|
| `HORA_CORTE_JORNADA` | `TIME` | §4.1 — a qué día pertenece un marcaje |
| `VENTANA_REBOTE_MIN` | `SMALLINT` | §4.3 paso 1 — supresión de rebotes |
| `VENTANA_CLASIFICACION_MIN` | `SMALLINT` | §4.2 — cercanía al evento esperado |
| `TOLERANCIA_RETARDO_MIN` | `SMALLINT` | §4.3 paso 6 — retardo |
| `COMIDA_MAXIMA_MIN` | `SMALLINT` | §4.3 paso 5 — comida excedida |
| `JORNADA_ABIERTA_MAX_H` | `SMALLINT` | §4.4 — corte de la entrada sin salida. Valor inicial 20 |
| `DESFASE_MAXIMO_SEG` | `INTEGER` | §5.3 — umbral de reloj sospechoso |

**`MSP_AS_FESTIVO`** — `FECHA` `DATE` PK · `NOMBRE` `VARCHAR(120)` UTF8

`HORA_CORTE_JORNADA` queda en `00:00` porque nadie cruza medianoche, pero **existe como parámetro**. La investigación llama a esta la decisión más cara de revertir; dejarla implícita como "agrupar por fecha calendario" es el error del que no se regresa sin migrar datos.

`MSP_AS_FESTIVO` es lo único adelantado de v2, por una razón concreta: sin él, el 16 de septiembre son quince faltas.

### 3.8 Índices

```
MSP_AS_EMPLEADO   UNIQUE(NUMERO), (SEDE_ID)
MSP_AS_EQUIPO     UNIQUE(CLAVE), (SEDE_ID)
MSP_AS_PLANTILLA  UNIQUE(EMPLEADO_ID, DEDO), (EMPLEADO_ID)
MSP_AS_MARCAJE    (EMPLEADO_ID, INSTANTE), (EQUIPO_ID, INSTANTE), UNIQUE(CLAVE_IDEMPOTENCIA)
MSP_AS_HORARIO    (EMPLEADO_ID, DIA_SEMANA, VIGENTE_DESDE)
MSP_AS_JORNADA    UNIQUE(EMPLEADO_ID, FECHA), (FECHA, ESTADO)
MSP_AS_CORRECCION (EMPLEADO_ID, CREATED_AT), (MARCAJE_ID)
```

## 4. El cálculo

### 4.1 La jornada lógica

Un marcaje pertenece al día **D** si su instante cae en `[D + HORA_CORTE, D+1 + HORA_CORTE)`. Con el corte en `00:00` coincide con la fecha calendario, pero la regla se escribe así porque agrupar por fecha y agrupar por corte son cosas distintas: una es reversible y la otra no.

### 4.2 Clasificación de marcajes — por horario esperado

Cada marcaje se asigna al evento esperado más próximo — entrada, salida a comer, regreso, salida — dentro de `VENTANA_CLASIFICACION_MIN`. El que no cae cerca de ninguno queda sin clasificar y señala el día.

**Se descartó explícitamente el algoritmo secuencial de BioTime** (`2º−1º, 4º−3º`, investigación §2.9). Su fragilidad es concreta: si alguien olvida marcar la salida a comer, el regreso pasa a ser el segundo marcaje y se lee como "se fue a comer a las 14:30". La comida sale absurda y el día entero queda mal, no solo el dato faltante. Con cuatro marcajes diarios el olvido no es excepcional: es rutina, y el de la comida es el que más se olvida.

### 4.3 Orden del cálculo

Entrada: los marcajes de la jornada, el horario vigente **ese día**, la configuración y el calendario de festivos.

1. **Suprimir rebotes.** Un marcaje dentro de `VENTANA_REBOTE_MIN` del anterior se marca `SUPRIMIDO_POR='rebote'`. No se borra.
2. **Clasificar** cada marcaje contra su evento esperado (§4.2).
3. **Completar lo que falta** con la hora del horario, creando marcajes `ORIGEN='sistema'` y contándolos en `MARCAJES_GENERADOS`.
4. **Clasificar el día:** `descanso` · `sin_horario` · `falta` · `incompleta` · `completa`.
5. **Calcular** presencia, comida y netos. Si la comida excede `COMIDA_MAXIMA_MIN`, **se registra el valor real y se señala el día** — no se recorta. Recortar automáticamente sería tomar una decisión con efecto de pago, y eso está fuera de v1.
6. **Retardo** = entrada real − entrada esperada − tolerancia, solo si es positivo.

### 4.4 Dos redes de seguridad distintas

**Cierre de jornada.** Al terminar el día lógico se completan los marcajes faltantes con la hora programada, marcados como generados por el sistema. Es la decisión cerrada en la investigación §13.1, y su fundamento es operativo: administración **solo revisa**, no corrige. Un día incompleto que espera a que alguien haga clic se queda en cero para siempre. Es mejor un número razonable y señalado que un hueco.

*Riesgo aceptado:* si alguien se fue a las 14:00 y no marcó, el sistema le acredita hasta su hora de salida. **Disparador para revisar la decisión:** el día en que de estos registros salga un descuento, un pago o una llamada de atención.

**Corte de las 20 horas.** Una entrada sin salida después de veinte horas se cierra. No es regla de negocio, es higiene del estado en vivo: sin eso alguien aparece "adentro" en la consola durante días y la vista deja de servir.

### 4.5 Cuándo se recalcula

Al recibir un marcaje · al aplicar una corrección · al cambiar un horario, solo las jornadas dentro de la vigencia afectada · bajo demanda por rango.

**Invariante verificada por test:** `MSP_AS_JORNADA` se puede vaciar por completo y reconstruir desde `MSP_AS_MARCAJE` obteniendo exactamente lo mismo. Si ese test falla, se coló un dato que solo existe en el cálculo y la recomputabilidad se perdió.

### 4.6 Lo que no se calcula en v1

Horas extra con efecto de pago · redondeo con umbral de dirección · salida anticipada · jornada anormalmente corta o larga · incidencias justificadas (permiso, incapacidad, vacaciones).

> **Nota sobre redondeo, porque es donde más se equivoca la gente:** la tolerancia de retardo y el *grace* de Kronos **no son lo mismo**. Uno es cuánto puedes llegar tarde antes de que cuente; el otro es hacia qué lado redondea el minutero (investigación §2.7). v1 implementa solo el primero.

**La omisión más visible de v1** es el catálogo de incidencias: sin él, toda ausencia es falta y hay que explicarlo a mano. Es una decisión, no un descuido.

## 5. Tiempo y manipulación del reloj

El reloj del equipo es el punto débil del diseño, porque de él sale la hora del hecho. Cuatro capas, de la más fuerte a la más débil.

### 5.1 Con red, el reloj del cliente no manda

Si el equipo está en línea al momento del marcaje, `INSTANTE` toma el valor del **servidor**; la latencia de red son milisegundos. `INSTANTE_EQUIPO` se guarda igual, como dato de auditoría. Esto elimina el problema para la gran mayoría de los marcajes.

### 5.2 Sin red, reloj monotónico en el cliente

La aplicación se sincroniza con el servidor al arrancar y calcula la hora como **hora del servidor + tiempo transcurrido en un reloj monotónico**. Un reloj monotónico no se puede mover desde el panel de control: cambiar la fecha de Windows no lo altera. Es requisito del cliente (§8).

### 5.3 El servidor sabe cuándo habló contigo por última vez

`ULTIMO_CONTACTO` no es solo diagnóstico, es prueba. Dos validaciones que no cuestan nada:

- Un marcaje **anterior al último contacto exitoso** del equipo es imposible: si a esa hora estaba en línea, lo habría enviado entonces.
- Marcajes que **abarcan más tiempo del que el equipo estuvo desconectado** son incoherentes.

Ambos se marcan; el marcaje entra igual, pero señalado en `DESFASE_SOSPECHOSO`.

### 5.4 Configuración fuera del código

No lo resuelve la API y se documenta para que no se dé por hecho:

- **El empleado no debe tener cuenta de administrador en la PC de marcaje.** En Windows, cambiar la hora del sistema es privilegio administrativo por defecto; con cuenta estándar simplemente no puede. Es la defensa que más importa y es media hora de configuración.
- Servicio de hora de Windows apuntando a NTP.
- La PC arranca directo en la aplicación, sin escritorio a la vista.

### 5.5 Lo que aun así queda abierto

Alguien con permisos de administrador, sin red, que reinicie la aplicación, puede falsificar marcajes **dentro de la ventana en que estuvo desconectado**. §5.3 acota esa ventana; no la cierra. Cerrarla exige terminal autónoma con reloj propio.

> **Efecto secundario útil:** medir el desfase en cada marcaje distingue dos cosas que se ven igual. Un reloj que se atrasa de a poco todos los días es una pila de placa madre agotada. Un reloj que salta cuarenta minutos y regresa es una persona.

## 6. Puertos outbound

| Puerto | Operaciones |
|---|---|
| `CatalogoRepo` | Sedes, empleados y equipos |
| `PlantillaRepo` | `Registrar` · `ListarCifradas` · `BorrarPorEmpleado` |
| `MarcajeRepo` | `Registrar` · `AplicarCorreccion` · `ListarPorJornada` · `ListarRecientes` |
| `CorreccionRepo` | `ListarPorEmpleado` — **solo lectura** |
| `HorarioRepo` | `VigenteEn(empleado, fecha)` · `Historial` · `Guardar` |
| `JornadaRepo` | `Guardar` · `Listar` · `ObtenerPorEmpleadoYFecha` · `BorrarRango` |
| `ConfigRepo` | Configuración y festivos |
| `Cifrador` | `Cifrar` · `Descifrar` |
| `Identity` | `UsuarioActual` · `TienePermiso` |
| `Clock` | `Now` |
| `IDGenerator` | `Nuevo` |

**`CorreccionRepo` es de solo lectura por diseño.** Una corrección nunca se escribe sola: `MarcajeRepo.AplicarCorreccion` guarda el marcaje nuevo o suprimido **y** el renglón de bitácora en la misma transacción. No existe camino para tocar un marcaje sin dejar bitácora.

**`Cifrador` es un puerto y no una función suelta** porque la llave y su rotación son problema de infraestructura. El dominio nunca ve bytes cifrados ni sabe con qué algoritmo se protegieron.

## 7. Endpoints y autenticación

El módulo tiene **dos esquemas de autenticación**, lo cual es distinto al resto del repositorio.

### 7.1 Rutas de equipo — credencial de equipo, sin usuario

```
POST /v2/asistencia/equipos/sesion      el equipo se autentica al arrancar
GET  /v2/asistencia/plantillas          plantillas cifradas del equipo autenticado
POST /v2/asistencia/marcajes            registrar marcaje (idempotente)
POST /v2/asistencia/equipos/latido      heartbeat + desfase de reloj
```

### 7.2 Rutas de administración — usuario y permiso

```
GET  /v2/asistencia/consola                    presencia + pendientes + últimos marcajes
GET  /v2/asistencia/jornadas                   rejilla del periodo, filtros y paginado
GET  /v2/asistencia/empleados/{id}/jornadas    detalle de persona
POST /v2/asistencia/jornadas/{id}/revisar      "ya lo vi, no hago nada"
POST /v2/asistencia/marcajes/{id}/corregir
POST /v2/asistencia/marcajes/{id}/suprimir
POST /v2/asistencia/marcajes/capturar          alta manual del que faltó
POST /v2/asistencia/recalcular                 por rango
```

### 7.3 Catálogos, horarios y biometría

```
      /v2/asistencia/empleados · /sedes · /equipos     alta, edición, listado
GET   /v2/asistencia/empleados/{id}/horario
PUT   /v2/asistencia/empleados/{id}/horario
POST  /v2/asistencia/empleados/{id}/plantillas         enrolar
DELETE/v2/asistencia/empleados/{id}/plantillas         baja biométrica
      /v2/asistencia/config · /festivos
```

### 7.4 El cruce está prohibido en ambos sentidos

Un token de equipo no puede tocar una ruta de administración, y un token de usuario **no puede registrar marcajes**. Lo segundo importa más de lo que parece: si un usuario con sesión pudiera llamar a `POST /marcajes`, cualquiera con credenciales de oficina marcaría desde su casa y toda la arquitectura del kiosko sería decorativa.

Wiring con Huma + chi (`HUMA_WIRING.md`). Fechas RFC3339 UTC en el contrato (`DATETIME_HANDLING.md`). UTF-8 en todas las columnas `MSP_AS_*` (`ENCODING_HANDLING.md`).

## 8. Contrato de la aplicación cliente

La API no puede forzar nada de esto. Se documenta aquí y se verifica al construir la aplicación de escritorio.

- Las plantillas viven **solo en memoria**, jamás en disco.
- La hora se calcula como **hora del servidor + reloj monotónico**, nunca leyendo el reloj de Windows.
- Cada marcaje lleva **clave de idempotencia** generada en el cliente.
- La confirmación **dice el nombre en voz alta** — consecuencia obligatoria de 1:N (§2.4).
- El empleado **no** tiene cuenta de administrador en la PC de marcaje.

## 9. Permisos

| Código | Cubre |
|---|---|
| `asistencia:leer` | Consola, rejilla, detalle |
| `asistencia:administrar` | Catálogos, horarios, configuración, festivos |
| `asistencia:corregir` | Correcciones, captura manual, recálculo |
| `asistencia:enrolar` | Alta y baja de plantillas biométricas |
| `asistencia:marcar` | Lo porta el equipo, no una persona |

`asistencia:enrolar` va separado de `administrar` a propósito: tocar el dato biométrico no debería venir de regalo con "puedo editar el catálogo de sedes". Es el único permiso del módulo que da acceso a datos de otra naturaleza.

## 10. Verificación

Compuertas estándar de `TESTING_REQUIREMENTS.md`, sin descuento:

| Paquete | Piso |
|---|---|
| `internal/asistencia/domain` | ≥ 99% |
| `internal/asistencia/app` | ≥ 90% |
| `internal/asistencia/infra/asisfb` | ≥ 80% (requiere `FB_DATABASE`) |
| `internal/asistencia/infra/asishttp` | ≥ 70% |
| Mutación (`domain` + `app`) | kill-rate ≥ 80% |

Y cuatro pruebas específicas que son las que sostienen el módulo:

**Recomputabilidad.** Vaciar `MSP_AS_JORNADA` y reconstruirla desde los marcajes debe dar exactamente lo mismo.

**El clasificador, exhaustivo.** Tabla con todas las combinaciones: cuatro marcajes completos · sin salida a comer · sin regreso · sin salida · marcaje en día de descanso · rebote · marcajes fuera de toda ventana de clasificación. Es donde se van a esconder los errores.

**Las validaciones de reloj.** Marcaje anterior al último contacto del equipo · marcajes que abarcan más tiempo del que el equipo estuvo desconectado · desfase por encima del umbral.

**Barrido de seguridad cruzado.** Además del habitual por permiso: un token de equipo recibe 403 en toda ruta de administración, y un token de usuario recibe 403 en `POST /marcajes`.

Más `make check-sealed`, que corre en cada entrega vía `pre-push` y no al final.

Los tests de integración se envuelven en `fbtestutil.WithTestTransaction`.

## 11. Fuera de alcance de v1

**Se construye después:** incidencias justificadas con catálogo y documento · horas extra · redondeo con umbral de dirección · salida anticipada · exportación a Excel/CSV · reportes de retardos y ausentismo · alertas · cierre de periodo.

**No se construye:** nómina o prenómina · programación de turnos rotativos · horarios flexibles sin hora fija · geocercas · múltiples husos horarios · portal o app del empleado · tablero de indicadores de RH.

**Aportación rescatada del documento del equipo:** el estado de sincronización de permisos con dispositivos (`pendiente` / `aplicado` / `fallido`). Bajo la arquitectura elegida — PC con lector y plantillas en la API — **no aplica**, porque no hay estado que empujar a un aparato. Volvería a ser necesario si algún día se migra a terminales autónomas.

## 12. Referencias

- [`checador-biometrico-estado-del-arte.md`](../../research/checador-biometrico-estado-del-arte.md) — investigación, marco legal, recomendación de diseño, maqueta
- [`checador-features-checklist.md`](../../research/checador-features-checklist.md) — catálogo feature por feature con v1/v2/fuera y decisiones cerradas §13
- [`checador-mockup.html`](../../research/checador-mockup.html) — maqueta interactiva de las cinco pantallas
- [ADR-0009](../../adr/0009-asistencia-sealed-module.md) — módulo sellado y extraíble
- `CLAUDE.md` §1 (sin lógica en la base), §2 (vertical slices), §3 (idioma), §5 (restricciones de stack)
- `docs/module-standards/` — `MODULE_TEMPLATE.md`, `AGGREGATE_PATTERNS.md`, `CQRS_PATTERN.md`, `HUMA_WIRING.md`, `DATETIME_HANDLING.md`, `ENCODING_HANDLING.md`, `TESTING_REQUIREMENTS.md`
