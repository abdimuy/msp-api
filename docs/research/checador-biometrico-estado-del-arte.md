# Checador biométrico de asistencia — estado del arte y diseño del módulo

> **Fecha:** 2026-07-21
> **Alcance:** control de asistencia (entrada/salida) de empleados en oficinas y bodegas de MSP.
> **Fuera de alcance:** hardware y SDK del lector de huella; identidad de clientes; firma de contratos.
> **Método:** dos rondas de investigación con verificación adversarial (3 votos por afirmación, se descarta con 2/3 refutaciones). Cada hallazgo indica si es **verificado documentalmente** o **inferencia de diseño**.
>
> **Maqueta de interfaz:** [`checador-mockup.html`](checador-mockup.html) — ábrelo en el navegador; el kiosko es interactivo. Ver §9.
>
> **Catálogo de features para decidir alcance:** [`checador-features-checklist.md`](checador-features-checklist.md) — feature por feature, marcado v1/v2/fuera, con las trampas y la vanguardia del sector señaladas.

---

## 0. Resumen ejecutivo

Cinco conclusiones que deberían gobernar el diseño:

1. **La huella no es la arquitectura, es un accesorio.** En el reloj insignia de UKG/Kronos —uno de los líderes mundiales— el lector de huella es una **opción de pago**; lo que viene de serie es credencial/tarjeta. La biometría es un control anti-suplantación, no un requisito estructural. El dominio debe tratar el método de identificación como intercambiable desde el día uno.

2. **Separar el evento crudo del cálculo derivado.** El patrón convergente entre UKG, ZKTeco BioTime y Patriot es: el marcaje (*punch*) es un evento inmutable; la jornada es un resultado **recomputable** producido por un motor de reglas. Nunca guardar la jornada como dato original.

3. **La "divisoria del día" (*day divide*) es la decisión más cara de revertir.** El cruce de medianoche no tiene respuesta única: los líderes lo resuelven con una hora de corte configurable más una política explícita de a qué día pertenecen las horas. Agrupar checadas por fecha calendario es el error del que no se regresa sin migrar datos.

4. **Para el objetivo acotado de MSP existe un algoritmo mínimo ya probado en el mercado:** el modo de horario flexible de BioTime —2º marcaje menos 1º, 4º menos 3º— sin retardos, faltas ni horas extra. Es exactamente "a qué hora llegan y se van". Las políticas de retardo/falta/extra pueden esperar a v2.

5. **El marco legal mexicano cambió por completo en 2025 y sí altera el diseño de v1.** La LFPDPPP vigente es una **ley nueva** (DOF 20-03-2025) que abrogó la de 2010 y renumeró todo; el INAI se extinguió y la autoridad sancionadora es ahora la **Secretaría Anticorrupción y Buen Gobierno**. Tratar la huella como dato sensible obliga a **consentimiento expreso y por escrito con carga de la prueba del patrón**, a **priorizar verificación 1:1 sobre identificación 1:N**, y a **suprimir la plantilla al terminar la relación laboral**. Nada de eso es opcional ni postergable a v2.

**Consecuencia de diseño más importante del bloque legal:** el ciclo de vida de la **plantilla biométrica** (dato sensible, se purga al terminar la relación laboral) debe estar **separado** del ciclo de vida de las **checadas** (registro de asistencia que conviene conservar). Si se guardan juntos o se borran juntos, se incumple una cosa o la otra.

---

## 1. Catálogo de features de los líderes mundiales

### 1.1 La biometría es opcional en el líder del mercado — hallazgo verificado (3-0)

En el folleto oficial del **UKG InTouch DX**, el lector de huella (*UKG Touch ID Plus*) aparece bajo **"Options"**, junto al lector de proximidad externo y la batería de respaldo. Lo integrado de serie es lectura de credencial: código de barras, banda magnética, proximidad HID y *smart card* iCLASS/MiFare/Seos.

Literal del folleto: `Options: UKG Touch ID™ Plus finger-based biometrics / External proximity reader / ... / Backup battery` y `Optional biometric authentication to prevent time theft`.

El carácter de licencia facturada aparte está corroborado por catálogo de revendedor (SKU `8610012-101` — *Touch ID Plus Biometric Option, Subscription license*). La modalidad facial más reciente (*TouchFree ID*) se comercializa igual: como opción.

**Implicación de diseño (inferencia propia, no afirmación de UKG):** modelar el método de identificación como atributo del marcaje —huella, credencial, PIN, rostro— sin acoplar el dominio a la biometría.

Fuentes:
- <https://www.ukg.com/products/data-collection/intouch-dx>
- <https://primepoint.com/email-content/UKG_InTouch_DX.pdf>
- <https://www.ukg.com.au/sites/default/files/legacy/kronos/resources/pdf/en-GB/cv1123-usv3_intouch-dx-brochure%20UK.pdf>

### 1.2 Table stakes en el mercado mexicano — hallazgo verificado (3-0)

Turnos rotativos, horarios nocturnos, jornadas especiales y cálculo automático de horas trabajadas y extra son **requisitos de catálogo**, no diferenciadores. Están ofrecidos por al menos cinco proveedores independientes en dos ecosistemas distintos (SaaS de RH y software de dispositivo biométrico):

| Proveedor | Evidencia |
|---|---|
| Worky (MX) | "Turnos rotativos / Horarios nocturnos / Jornadas especiales" como políticas configurables; "cálculo automático de las horas trabajadas y extras" |
| Sesame HR (MX) | Turnos fijos, rotativos, mixtos o de guardia; jornadas partidas; horas extra conforme a LFT |
| Buk (MX) | Patrones de turno por área/equipo sincronizados con vacaciones e incidencias |
| ZKTeco BioTime Pro | Esquemas 24×24 / 24×48, horarios de hasta 72 horas, "cambio de día" para nocturnos |
| Anviz CrossChex Cloud | *Split Timesheets* |

**Salvedad importante:** el hallazgo dice que están **en el catálogo**, no que estén bien implementadas. Existe un caso de soporte documentado en Anviz CrossChex Cloud donde, con el corte por defecto a las 00:00, un turno 21:00–05:00 se computa como **15–16 horas en vez de 8**. El cruce de medianoche es table stakes de folleto y frágil en la práctica.

### 1.3 Lo que fue REFUTADO y no debe usarse como base de diseño

Siete afirmaciones plausibles murieron en verificación adversarial. Se listan porque son justamente las que un diseño apresurado daría por ciertas:

| Afirmación refutada | Voto |
|---|---|
| Que Truein evidencie una migración del mercado de huella hacia rostro | 0-3 |
| Que el anti-buddy-punching se resuelva combinando biometría con geocerca/GPS | 0-3 |
| Que Worky posicione su checador como multimodal (huella/rostro/QR/geolocalización) | 0-3 |
| Que "eliminar la suplantación" sea la justificación de negocio *table stakes* del biométrico en México | 0-3 |
| Que la salida esperada del sistema sea la prenómina y no el listado de checadas | 0-3 |
| Que programación de turnos, descansos excluidos, horas extra, ausencias y timesheet automatizado sean todos *table stakes* | 1-2 |
| Que el *Manual Log* de BioTime sea un objeto de primera clase con flujo de aprobación e inmutabilidad post-aprobación | 0-3 |

La última es la más consecuente: **el flujo de corrección manual con aprobación y bitácora inmutable no tiene patrón documentado verificado** en ZKTeco ni en UKG. Hay que diseñarlo desde primeros principios (ver §7 y ronda 2).

---

## 2. Modelo de dominio y reglas de cálculo

### 2.1 Evento crudo vs. jornada calculada — hallazgo verificado (3-0)

UKG define el *punch* como `the entries on a timecard that mark the beginning (in-punch) or end (out-punch) of a work interval`. El marcaje **delimita** el intervalo; no *es* el intervalo. Los totales se calculan por reglas, al grado de que el sistema genera *punches sintéticos* en la divisoria del día:

> `Punches that cross the day divide display in black, along with system-generated midnight day-divide punches regardless of the fixed rule. The totals still display on the days defined by the fixed rule configuration.`

ZKTeco BioTime hace la misma separación de forma física: la tabla de transacciones (§5.12 *Transaction*) es distinta del paso de cálculo (§5.12 *Calculation*).

Fuentes:
- <https://communityfiles.ukg.com/support/kol/onlinehelp-workforcedimensions/en-us/content/MasterTopics/Glossary.htm>
- <https://communityfiles.ukg.com/support/KOL/OnlineHelp-WorkforceDimensions/en-us/Content/Timekeeping/Punches-managers.htm>
- <https://www.zkteco.co.id/wp-content/uploads/download-manager-files/BioTime%208.0%20User%20Manual-V2.0-ENG.pdf>

### 2.2 El motor de reglas como bloques componibles — hallazgo verificado (3-0)

Kronos construye el cálculo como *building blocks*: `Work rule building blocks are sets of parameters that control time and attendance functions`, reutilizables entre reglas para mantener consistencia. El catálogo documentado tiene **16 tipos**:

`interval round rules` · `punch round rules` · `shift guarantees` · `exception rules` · `core hours` · `break rules` · `auto-resolved exceptions` · `bonus/deduction` · `call-in` · `schedule deviation` · `rest between shifts` · `overtime` · `zone` · `majority` · `combination` · `pay code distributions`

Unos operan sobre marcajes crudos (redondeo, descansos) y otros sobre el resultado ya calculado (horas extra, zonas, mayorías) — evidencia de la separación evento→cálculo.

> **Caveat de vigencia:** la URL corresponde a Kronos Workforce Central (línea *legacy*), no a UKG Pro WFM actual. El patrón persiste; los nombres exactos pueden diferir.

Fuente: <https://customer2.kronos.com/support/KOL/onlinehelp/Subsystems/Help-STP/Content/SetupHelp/ConfigWorkRuleBldgBlocks.htm>

### 2.3 Configuración estable vs. variable — hallazgo verificado (3-0)

UKG separa las **fixed rules** (constantes: periodo de nómina, *day divide*, política de a qué día pertenecen las horas) de las **work rules** variables:

> `Unlike work rule assignments, which can change with employee schedules, fixed rule assignments do not change.`

Las *work rules* son temporales, **no se asignan al expediente del empleado**, y pueden sobrescribir el cálculo mediante *Work Rule Transfers* en la tarjeta o el horario. Un consultor especializado lo resume: `Pay Rules represent the umbrella that houses Work Rules`.

> **Precisión:** la documentación dice "por empleado", no "por empleado/sede" — la dimensión sede sería extensión nuestra.

### 2.4 El cruce de medianoche — hallazgo verificado (3-0), y la decisión más cara

No hay respuesta única. Se resuelve con dos piezas:

**a) Divisoria del día (*day divide*), configurable:**
> `Day Divide — Specifies the time when one day ends, and another day begins. The value of the Day Divide marks the first minute of the new day.`
> `By default, a day starts and ends at 12:00 midnight.`

El valor por defecto es medianoche, pero **no es un supuesto implícito**: es un parámetro.

**b) Política explícita "Hours Belong To", con cuatro opciones** (cinco en versiones recientes de UKG Pro WFM, que separa mayoría trabajada de mayoría programada):

| Opción | Atribución |
|---|---|
| *Day Actually Worked* | El día real en que se trabajaron las horas |
| *Scheduled In-Day* | El día en que inicia el turno programado |
| *Scheduled Out-Day* | El día en que termina el turno programado |
| *Day with Majority Hours* | El día donde cayó la mayoría de las horas |

La elección afecta además **cuándo se reinicia el conteo de horas extra** y cómo aplican zonas/diferenciales nocturnos:
> `When Hours belong to is: Day Actually Worked, the reset occurs at the day divide. Scheduled In-Day, the reset occurs at the end of the shift...`

**El mismo concepto con otros nombres:**
- ZKTeco BioTime → `Day Change Time: Set the time point to distinguish the punch records belonging to which day. Example, Day Change Time is set as 8:00 am, then the punch records before 8:00 am will belong to the previous day.`
- Anviz CrossChex → *Split Timesheets*

**Patrón alternativo (confianza media, 3-0 pero sin barrido adversarial):** Patriot Software **parte el turno automáticamente a las 12:00 AM** en dos entradas, atribuyendo cada tramo a su día calendario (21:00–00:00 al día 1; 00:00–06:00 al día 2). Es una decisión de atribución para nómina, no una regla universal.
> Caveats: la propia página declara `Currently in beta for select customers` y sólo disponible en portal web.
> **Traslado a msp-api:** conservar el marcaje crudo como fuente de verdad y **derivar** los tramos por día — no persistir el *split* como dato original.

Fuentes:
- <https://communityfiles.ukg.com/support/KOL/onlinehelp81/Subsystems/Help-STP/Content/SetupHelp/ConfigPayRules.htm>
- <https://communityfiles.ukg.com/support/KOL/OnlineHelp-WorkforceDimensions/en-us/Content/Timekeeping/TimeOfDayFormats.htm>
- <https://www.zkteco-ea.com/wp-content/uploads/2023/04/ZKBioTime-User-Manual-20230315.pdf>
- <https://help.patriotsoftware.com/en/articles/15383565-overnight-shifts-and-missed-time-punches>

### 2.5 Taxonomía de excepciones — hallazgo verificado (3-0)

El *table stakes* va mucho más allá del "marcaje huérfano". UKG Workforce Dimensions define **al menos 11 excepciones tipificadas**, en un modelo **híbrido**: unas se calculan contra el horario programado y otras marcaje-contra-marcaje.

| Familia | Excepciones |
|---|---|
| Generales | `Missed In Punch` · `Missed Out Punch` · `Long Interval` · `Short Shift` · `Unscheduled` |
| De entrada | `Very Early In` · `Early In` · `Late In` |
| De salida | `Early Out` · `Late Out` · `Very Late Out` |

`Long Interval` y `Short Shift` se calculan **entre marcajes**, no contra el horario:
> `if the time between the in-punch and the out-punch is equal to or greater than the long interval, the long exception appears` — usando `rounded time between the punches`.

`Unscheduled` cubre al empleado que checa sin turno programado — relevante para MSP, donde probablemente no habrá horarios cargados en v1.

Fuente: <https://customer2.kronos.com/support/kol/onlinehelp-workforcedimensions/en-us/Content/Timekeeping/Exceptions-hourlyTC.htm>

### 2.6 Comidas y descansos — hallazgo verificado (3-0)

Son un **tipo distinto** de marcaje y de excepción que **interactúa** con el par de jornada (no una dimensión independiente). Tres clases de regla:

- **Duración individual:** `Short Break`, `Long Break` — `when an employee takes a break longer or shorter than expected`.
- **Duración acumulada por jornada:** `Long Total Break`, `Short Total Break` — `for shifts of up to 3 hours and 59 minutes, there could be a long total break exception if the total of all breaks in the shift equal 30 minutes or more`.
- **Orden:** `Break Out of Sequence` — `for a deviation in the order that breaks should be taken, for example, rest, meal, rest`.

Además: `Punches for breaks reset the interval` en el cálculo de intervalos largos.

### 2.7 Redondeo y umbral de dirección — hallazgo verificado (3-0)

⚠️ **Advertencia terminológica crítica:** el *grace* de Kronos **NO** es el "periodo de gracia"/tolerancia antes de contar un retardo. Es el **umbral que define la dirección del redondeo**. Confundirlos produce reglas de negocio erróneas.

Mecánica verificada:
> `Rounds divide hours into even increments of minutes` · `Sixty must be a multiple of the round value` · `If the start or end time is less than or equal to the grace, it moves to the previous round increment. If the start or end time is greater than the grace, it moves to the next round increment` · `Change points determine when inside and outside rounds and graces apply`

Restricciones estructurales: 60 debe ser múltiplo del incremento; la gracia siempre menor que su redondeo asociado. Existen reglas separadas para marcaje de entrada, de salida, empleados sin horario y marcaje faltante.

### 2.8 De-duplicación (anti-rebote) — hallazgo verificado (3-0)

Práctica establecida en terminales de marcaje por verificación:
> `Duplicate Punch Period (m): To set the time period (unit: minutes) for duplicate punches. If a value '1' (minute) is set and if a user tries to do multiple punches within 1 minute, the system will accept only the first punch.`

Configurable globalmente en la regla y anulable por horario (modo "basado en regla" vs. "definido por el usuario"). Presente desde BioTime 7.0 (2018) hasta 8.0, y en FingerTec (máx. 60 min).

**Matices obligatorios:**
- El campo se llama `Duplicate Punch Period`, no *Interval*.
- Es práctica común **en terminales de verificación** (ZKTeco, FingerTec), no obligatoria en toda la categoría: Deputy, Jibble y When I Work no exponen el parámetro porque su marcaje es por estado/toggle en app.
- **Diseño:** en BioTime el duplicado **se pierde** al aceptar. Para auditoría defendible conviene **persistir todo marcaje crudo** y marcar el duplicado como *suprimido en el cálculo*, no borrarlo.

### 2.9 El algoritmo mínimo reutilizable para MSP — hallazgo verificado (3-0)

BioTime define un algoritmo determinista para **horarios flexibles**, directamente aplicable al objetivo acotado:

> `The attendance calculation for a flexible timetable is second punching time minus first punching time, fourth punching time minus third punching time, and so on... If four records exist, the daily report on that day has two lines. If six records exist, the daily report has three lines.`

En modo flexible **no se cuentan retardos, salidas anticipadas ni faltas**, y el retraso al salir no cuenta como tiempo extra. Es decir: es el modo **sin política**, que es precisamente lo que pidió el negocio para v1.

La unidad mínima de configuración no es el turno sino el ***timetable***, que define ventanas de validez:
> `The timetable is the minimum unit in the attendance time settings` · `Check-in/out records out of this time range are invalid. Set the cross days maximum of 3 days.`

**⚠️ Límite crítico:** el manual **guarda silencio sobre marcajes impares/huérfanos**. El determinismo sólo se sostiene con secuencias pares y ordenadas; con 3 o 5 marcajes el último queda sin par. *(Pendiente: ronda 2.)*

Doctrina estable: literales verificados en manuales de 2018, 2019 y 2023.

### 2.10 Multi-sede y continuidad — hallazgo verificado con voto dividido (2-1)

Un mismo empleado puede marcar, ver estatus, revisar checadas y transferirse entre puestos, ubicaciones o proyectos **en varios relojes durante un mismo turno**, con continuidad: `...at multiple clocks during a single shift` · `...seamless continuity`.

El emparejamiento entrada-salida se resuelve **centralmente** en la tarjeta de tiempo, no por dispositivo. La documentación atribuye los *unmatched/missing punches* a causas de identidad (sin gafete, PIN mal tecleado), **no** al hecho de checar en relojes distintos.

**Salvedades que motivan la confianza media:**
- La fuente dice `across your facility` (una instalación), **no** *across facilities*. El salto a multi-sede es extensión razonable, no afirmación de UKG.
- El juicio de *table stakes* se apoya principalmente en un proveedor.
- Permitir entrada en una bodega y salida en otra sin romper el par es **inferencia de diseño**, no patrón documentado.

### 2.11 Zonas horarias y DST — contexto local

México eliminó el horario de verano en 2022 y todas las sedes de MSP están en el mismo huso. La complejidad multi-huso/DST es **seguro barato**, no requisito validado. UKG permite editar la `time zone RULE` del marcaje —regla, no valor— lo que sugiere tratar el tratamiento de zona como atributo del evento en despliegues multi-sede; para MSP es en gran medida inaplicable.

> Guardar el instante en UTC de todos modos: es gratis y evita el problema si algún día hay sede en otro huso.

---

## 3. Operación en el mundo real

### 3.1 Offline y sincronización — hallazgo verificado (3-0)

El funcionamiento sin red con sincronización automática posterior es capacidad de producto **explícita y esperada** en checadores multi-sede.

> Truein: `stores data offline and automatically syncs once the device is reconnected to the internet.`
> Suprema BioStar 2 (KB técnica): sincronización automática al reconectar; `log upload is automatic regardless of the connection type` cuando *Log Upload* está en automático.
> Capacidad de hardware: ZKTeco IN01-A almacena 100,000 transacciones.

**Modos de falla documentados que obligan a defensa en profundidad:**
- Es **dependiente de configuración** (Suprema requiere habilitar auto-sync explícitamente).
- Suprema documentó un **bug de transferencia de log** al desconectar/reconectar, corregido apenas en BioStar 2.7.14.
- Nowsta limita la retención offline a **un solo día**.

**Conclusiones de diseño (inferencia):** hacer la sincronización **idempotente**, con búfer local durable de capacidad y retención explícitas, y **reconciliar** en lugar de confiar en el dispositivo.

### 3.2 Flujo de resolución del supervisor — verificado (3-0), confianza media

Tres acciones distintas:
1. **Marcar como revisada sin modificar el dato** — `show that you know about the exception, but have decided to take no further action`.
2. **Editar el marcaje** — hora, tipo de *override*, la regla de zona horaria, y cancelar deducciones.
3. **Añadir un comentario** con conteo.

**Salvedad decisiva:** la fuente describe las tres acciones pero **no acredita que se escriban a una bitácora inmutable**. La auditabilidad de correcciones quedó **sin evidencia verificada** en la ronda 1. *(Pendiente: ronda 2.)*

### 3.3 Enrolamiento, anti-buddy-punching, altas/bajas

Sin hallazgos verificados en la ronda 1. *(Pendiente: ronda 2.)*

---

## 4. UX y producto

> ⚠️ **No investigado con evidencia verificada.** Ninguna de las dos rondas produjo afirmaciones verificadas sobre pantallas de kiosko, tablero de supervisor, vista de presencia en tiempo real, reportes efectivamente usados ni autoservicio del empleado. Lo que sigue es **inferencia de diseño**, no hallazgo. Ver §8 si se quiere cerrar esta brecha.

Lo único con anclaje verificado es la §5.4: la recomendación del INAI de **priorizar verificación 1:1 sobre identificación 1:N** tiene consecuencia directa en la pantalla del kiosko — el empleado debe **identificarse primero** (número de empleado, credencial o PIN) y la huella **confirma** que es quien dice ser. Un kiosko que sólo pide "pon el dedo" y busca contra toda la base es 1:N, justamente el modo que la guía desaconseja para control de acceso.

---

## 5. Cumplimiento y privacidad en México

> **Esta sección sí está sustentada en fuentes primarias**: texto consolidado oficial de la Cámara de Diputados, decretos del DOF y guías del INAI, verificados verbatim (descarga del PDF + extracción de texto, no resúmenes).
>
> ⚠️ **Fechado 2026-07-21.** El marco cambió en diciembre 2024, marzo 2025, noviembre 2025 y mayo 2026. Re-verificar antes de usarlo como base de una decisión.

### 5.1 La ley aplicable es NUEVA, no la que todos citan — verificado (3-0)

La LFPDPPP vigente es una **ley expedida, no reformada**, publicada en el DOF el **20 de marzo de 2025**, en vigor el **21-03-2025**. Su transitorio segundo **abrogó expresamente** la LFPDPPP de 2010, además de la LGTAIP 2015, la LFTAIP 2016 y la LGPDPPSO 2017. Última reforma: **DOF 14-11-2025** (homologación al Código Nacional de Procedimientos Civiles y Familiares; sólo tocó el art. 4).

> ⚠️ **Trampa de citas — el error más probable en cualquier memo redactado con material anterior:** la ley de 2025 **renumeró todo**.
>
> | Tema | Ley 2010 (abrogada) | **Ley 2025 (vigente)** |
> |---|---|---|
> | Glosario / definiciones | art. 3 | **art. 2** |
> | Definición de datos sensibles | art. 3 fr. VI | **art. 2 fr. VI** |
> | Consentimiento para sensibles | art. 9 | **art. 8** |
> | Infracciones | art. 63 | **art. 58** |
> | Sanciones | art. 64 | **art. 59** |
> | Delitos | arts. 67-69 | **arts. 62-64** |

Fuentes:
- <https://www.diputados.gob.mx/LeyesBiblio/pdf/LFPDPPP.pdf> (texto consolidado oficial)
- <https://www.dof.gob.mx/nota_detalle.php?codigo=5752569&fecha=20/03/2025>
- <https://www.diputados.gob.mx/LeyesBiblio/ref/lfpdppp.htm>

### 5.2 Ya no es el INAI: la autoridad es la Secretaría Anticorrupción y Buen Gobierno — verificado (3-0)

- **LFPDPPP art. 2 fr. XV:** `Secretaría: Secretaría Anticorrupción y Buen Gobierno`.
- **Art. 54:** `La Secretaría verificará el cumplimiento de la presente Ley... de oficio o a petición de parte`.
- **Art. 59:** `Las infracciones a la presente Ley serán sancionadas por la Secretaría con:`.
- **Transitorio cuarto:** las menciones al INAI `se entenderán hechas o conferidas a los entes públicos que adquieren tales atribuciones`.

*Transparencia para el Pueblo* es un **órgano desconcentrado** de esa Secretaría y atiende **acceso a la información pública**, no datos personales del sector privado (LGTAIP art. 3 fr. III). La extinción del INAI se ordenó antes, en la reforma constitucional de simplificación orgánica del **DOF 20-12-2024**; el paquete de marzo 2025 es la capa secundaria.

> Un `grep` de "INAI" sobre el texto vigente sólo devuelve coincidencias en transitorios de extinción.

### 5.3 La huella NO está listada como dato sensible — pero se trata como tal — verificado (3-0)

Hallazgo contraintuitivo y con consecuencias: **el art. 2 fr. VI no menciona los datos biométricos**. La lista (origen racial o étnico, salud, información genética, creencias religiosas/filosóficas/morales, opiniones políticas, preferencia sexual) es explícitamente **"enunciativa más no limitativa"**. Un `grep` de `biom` sobre el texto íntegro de la ley vigente da **cero coincidencias**.

La huella califica como sensible **por criterio funcional y caso por caso**, no por mención textual. La Guía del INAI lo dice con todas sus letras:

> `Si bien los datos biométricos no están mencionados de manera expresa en el listado de datos personales sensibles... ello no implica que no se puedan considerar como tales bajo ciertas circunstancias`

...bajo tres supuestos: (a) que afecten la esfera más íntima, (b) riesgo de discriminación, (c) riesgo grave. La nota al pie 15 cita la resolución del Pleno `ACT-PRIV-20/01/2016.03.01.01`, donde **las huellas digitales se consideraron datos personales sensibles en el caso concreto**, con "protección reforzada".

> **Traslado honesto al reporte:** el hallazgo verificado es que la ley no las lista y que el criterio es funcional. La conclusión operativa —**tratarlas como sensibles**— es **inferencia prudente**, no automatismo legal. Dado el régimen sancionador (§5.6), es la inferencia correcta.

Fuente: <https://inicio.inai.org.mx/DocumentosdeInteres/GuiaDatosBiometricos_Web_Links.pdf>

### 5.4 Qué dispara tratarla como sensible — verificado (3-0)

**a) Consentimiento expreso Y POR ESCRITO, con carga de la prueba del patrón.**

> LFPDPPP art. 8: `Tratándose de datos personales sensibles, el responsable deberá obtener el consentimiento expreso y por escrito de la persona titular... a través de su firma autógrafa, firma electrónica, o cualquier mecanismo de autenticación... No podrán crearse bases de datos que contengan datos personales sensibles, sin que se justifique la creación de las mismas para finalidades legítimas, concretas y acordes con las actividades o fines explícitos que persigue el sujeto regulado.`

Crear la base en contravención de ese segundo párrafo es **infracción tipificada** (art. 58 fr. XVIII).

El Reglamento (art. 19) admite acreditar el consentimiento escrito `mediante un documento con su firma autógrafa, huella dactilar o cualquier otro mecanismo autorizado`, y el art. 20 pone `la carga de la prueba... en todos los casos, en el responsable`.

La Guía del INAI añade el orden de los pasos: `Solicitar el consentimiento expreso y por escrito de los titulares de los datos biométricos, previo a que se recaben`; `Generar pruebas para acreditar que se cumplió con el principio de consentimiento`; `previo a su obtención, es necesario que el titular conozca el aviso de privacidad`.

> **Requisito de producto, no de papeleo:** el módulo debe **almacenar la evidencia de consentimiento por empleado** —quién, cuándo, contra qué versión del aviso de privacidad, con qué artefacto firmado— y esa evidencia debe existir **antes** del primer enrolamiento. El aviso debe identificar cuáles datos son sensibles y qué finalidades requieren consentimiento (art. 15 fr. II y III).

**b) Proporcionalidad y minimización — base normativa para exigir alternativas menos invasivas.**

> LFPDPPP art. 12: `El tratamiento de datos personales será el que resulte necesario, adecuado y relevante en relación con las finalidades previstas en el aviso de privacidad; para los datos personales sensibles, el responsable deberá realizar esfuerzos razonables para limitar el periodo de tratamiento de los mismos a efecto de que sea el mínimo indispensable.`

Y la Guía del INAI, sección de proporcionalidad, con dos recomendaciones que aterrizan directo en el diseño:

> `Evaluar si la recolección de datos biométricos es necesaria para la finalidad pretendida.`
> `Priorizar el uso de datos que no sean biométricos para lograr la misma finalidad sin restarle efectividad.`
> `Para los procesos de control de acceso, es recomendable priorizar sistemas biométricos de verificación sobre los de identificación.`

La misma guía define **verificación = 1:1**, **identificación = 1:N**, y pone el registro de asistencia como el ejemplo típico de 1:1:

> `Como ejemplos de este método están los registros de asistencia, donde se compara uno o más datos biométricos contra el mismo registro almacenado para comprobar que un empleado es quien dice ser.`

### 5.5 Retención y supresión: hay un criterio explícitamente laboral — verificado (3-0)

Este es el hallazgo más accionable de todo el bloque legal:

> Guía INAI, principio de calidad: `si los datos biométricos de un empleado han sido recolectados para controlar el acceso a las instalaciones o sistemas informáticos del empleador, dichos datos deberían eliminarse tan pronto como concluya el plazo en el que se puedan utilizar para un procedimiento jurídico o administrativo, o bien, se termine la relación laboral.`

**Prueba de vigencia:** el INAI repitió ese párrafo **palabra por palabra** en *Recomendaciones de Seguridad para el Tratamiento de Datos Biométricos*, **edición diciembre 2024**, recomendación núm. 14. El criterio sobrevive a la edición de 2018.

Anclaje en ley vigente:

| Artículo | Contenido |
|---|---|
| art. 10 §2 | Supresión previo bloqueo cuando los datos dejan de ser necesarios |
| art. 24 §3 | `El periodo de bloqueo será equivalente al plazo de prescripción de las acciones derivadas de la relación jurídica que funda el tratamiento` |
| art. 12 | Mínimo indispensable para sensibles |
| art. 18 | Medidas administrativas, técnicas y físicas ponderando `el riesgo existente, las posibles consecuencias para las personas titulares, la sensibilidad de los datos y el desarrollo tecnológico` |

Ciclo obligatorio: **retención → bloqueo → supresión**, con procedimientos documentados (Reglamento arts. 37-39, 107-108) y carga de la prueba del cumplimiento de plazos en el responsable.

### 5.6 Régimen sancionador — verificado (3-0)

| Concepto | Monto |
|---|---|
| Infracciones art. 58 fr. II–VII | 100 a **160,000 UMA** |
| Infracciones art. 58 fr. VIII–XVIII | 200 a **320,000 UMA** |
| Reiteración | 100 a **320,000 UMA** adicionales |
| **Agravante por datos sensibles** | las sanciones `podrán incrementarse hasta por dos veces` |

Las dos infracciones que más aplican a un checador mal implementado caen **en el rango alto**:
- **fr. XIII** — recabar o transferir datos sin el consentimiento expreso cuando es exigible.
- **fr. XVIII** — crear bases de datos sensibles sin finalidad legítima (art. 8 §2).

En materia penal (arts. 62-64): 3 meses a 3 años por vulneración de seguridad con ánimo de lucro; 6 meses a 5 años por tratamiento mediante engaño con lucro indebido. **`Tratándose de datos personales sensibles, las penas a que se refiere este Capítulo se duplicarán`.**

> **Dos matices que hay que conservar:** (a) la frase del agravante está tipográficamente **dentro** de la fracción IV (reiteración); la lectura general —que aplica a todos los montos— es la mayoritaria y replica la estructura del art. 64 de la ley de 2010, pero es **interpretación**, no texto inequívoco. (b) El art. 58 tiene una fr. XIX ("cualquier incumplimiento...") que no está cubierta por ninguna fracción de multa del art. 59.
>
> No se verificó el valor de la UMA 2026, por lo que este documento **no convierte los montos a pesos**.

### 5.7 Ley Federal del Trabajo — sólo se verificó la vigencia del texto

**Verificado (3-0):** la LFT vigente (DOF 01-04-1970) incorpora reformas **hasta el DOF 14-05-2026**. La reforma que cambia el control de jornada **no** es la del 14 de mayo (esa es sobre personas trabajadoras artistas intérpretes, arts. 304-306) sino la del **DOF 01-05-2026**, que reformó el **art. 59** —`La duración máxima de la jornada ordinaria de trabajo será de cuarenta horas semanales`— junto con los arts. 58, 61, 66, 67, 68 (tope de 12 h diarias sumando ordinaria y extraordinaria), 69 y 71. El **art. 804** (conservación y exhibición de documentos en juicio) **no fue tocado** por ningún decreto de 2026.

**Verificado el 2026-07-22 (texto primario, Cámara de Diputados):**

#### ⚠️ El registro electrónico de jornada ahora es OBLIGATORIO

El decreto DOF 01-05-2026 adicionó el **art. 132 fracción XXXIV**, entre las obligaciones del patrón:

> `Registrar de manera electrónica la jornada laboral de cada persona trabajadora, incluyendo el horario de inicio y finalización; así como proporcionarlo a la autoridad cuando se le requiera... El contenido del registro electrónico hará prueba plena si se acredita que fue acordado entre la persona trabajadora y empleadora.`

- **Sanción por incumplir:** art. 994 fr. IV Bis — multa de **250 a 5,000 UMA**.
- **Vigencia:** la obligación corre desde el 01-05-2026, pero las **disposiciones generales de la STPS** que definirán ámbito y excepciones entran en vigor hasta el **01-01-2027** (Transitorio Quinto). Es decir: la obligación existe, su reglamentación fina todavía no.
- **La "prueba plena" está condicionada a que se acredite un ACUERDO** trabajador-patrón sobre el registro. No basta que el registro exista.

> Esto reencuadra el proyecto: **deja de ser solo una herramienta de gestión y pasa a ser cumplimiento de una obligación legal.**

#### Carga de la prueba y conservación

**Art. 784** — corresponde al patrón probar su dicho en 14 supuestos; los relevantes, literales:
> `III. Faltas de asistencia del trabajador;`
> `VIII. Jornada de trabajo ordinaria y extraordinaria, cuando ésta no exceda de nueve horas semanales;`

**Art. 804** — documentos que el patrón debe conservar y exhibir. Los controles de asistencia **sí están listados**:
> `III. Controles de asistencia, cuando se lleven en el centro de trabajo`

Condicionado a "cuando se lleven": el 804 no obliga a crearlos —esa obligación ahora viene del 132-XXXIV—, solo a conservarlos si existen.

**Plazo de conservación (fr. II, III y IV):** `durante el último año y un año después de que se extinga la relación laboral`.
> 📌 **Dato de diseño:** ese es el mínimo legal de retención de las checadas. Distinto del ciclo de la plantilla biométrica (§5.5), que se purga al terminar la relación.

**Art. 805** — consecuencia de no exhibir:
> `El incumplimiento a lo dispuesto por el artículo anterior, establecerá la presunción de ser ciertos los hechos que el actor exprese en su demanda, en relación con tales documentos, salvo la prueba en contrario.`

#### Valor probatorio de un registro sin firma

Tesis localizadas (vía réplica de bases de la SCJN; `sjf2.scjn.gob.mx` bloqueó el acceso automatizado):

| Registro | Contenido |
|---|---|
| **247594** (1986) | Si el patrón no lleva controles de asistencia, no exhibirlos "por imposibilidad material" no le perjudica. *Nota: superado en parte por la obligación del 132-XXXIV.* |
| **185577** (2002) | Matiza lo anterior: el simple argumento de que no se llevan controles "no es suficiente". Texto truncado en la fuente disponible. |
| **818664** | Quien objeta un documento privado debe probar su objeción, con presunción de autenticidad `máxime si el documento contiene al calce la firma del objetante`. **La presunción reforzada se ancla en la firma** — sin ella no opera igual. |
| **2025677** (2023) | La **huella** se trata como medio de autenticación equiparable a la firma en materia laboral. |

**No existe tesis localizada** sobre checador biométrico como registro de asistencia, ni que lo valore como prueba plena por sí mismo.

**Conclusión razonada (análisis, no jurisprudencia):** un registro biométrico sin firma ni acuse es un documento privado unilateral del patrón. Conserva su valor bajo el 804-III, pero sin firma del trabajador sobre el documento no aplica la presunción reforzada del criterio 818664 — quedaría como indicio más fácilmente objetable, **salvo que se acredite el acuerdo que exige el 132-XXXIV**.

#### Consecuencia de diseño

El propio texto legal dice cómo obtener prueba plena: **acreditar el acuerdo**. Y eso es barato, porque **ya se necesita un documento firmado para el consentimiento biométrico** (§5.4a). El mismo papel puede cubrir las dos cosas: consentimiento para el tratamiento de la huella **y** acuerdo sobre el registro electrónico de jornada. Un solo trámite al enrolar, dos obligaciones cubiertas.

> ⚠️ **Huecos declarados:** no se localizó NOM específica sobre checadores. Las disposiciones generales de la STPS (ámbito, excepciones) no existían al 22-07-2026. No se verificó el contenido completo de los registros 185577, 2025339 ni 181969 por bloqueo de las fuentes oficiales.

Fuentes: <https://www.diputados.gob.mx/LeyesBiblio/pdf/LFT.pdf> · <https://www.diputados.gob.mx/LeyesBiblio/ref/lft.htm>

### 5.8 Lo que quedó REFUTADO en el bloque legal

Dos afirmaciones sobre el Reglamento murieron 0-3 y **no deben aparecer en ningún memo**:

1. Que el art. 56 del Reglamento limite la creación de bases con datos sensibles a tres supuestos tasados.
2. Que el art. 17 del Reglamento establezca que la excepción de consentimiento por relación jurídica no cubre finalidades distintas, y que el contrato de trabajo por sí solo no legitima el tratamiento biométrico.

**Consecuencia:** la pregunta **"¿puede el patrón condicionar el empleo al uso de huella, y es obligatorio ofrecer alternativa no biométrica?"** quedó **sin respuesta con fuente**. La prohibición que **sí** está verificada es la del art. 8 §2 (no crear bases sensibles sin finalidad legítima, concreta y acorde con los fines explícitos).

> **Inferencia, marcada como tal:** proporcionalidad + minimización + la recomendación del INAI de priorizar datos no biométricos apuntan a **ofrecer una alternativa no biométrica**. Es la lectura prudente y coincide con lo que el diseño ya quiere hacer por otras razones (§1.1), pero **no tiene respaldo textual directo** en este expediente.

### 5.9 Fragilidad conocida: la vigencia del Reglamento de 2011

Es el punto jurídico más débil de todo el análisis. El Reglamento **no fue abrogado** por el transitorio segundo del decreto de 2025, y la nueva ley lo define y remite a él (arts. 17, 40, 41, 49, 55, 57). Pero **es el reglamento de una ley abrogada**, y el transitorio décimo segundo ordenó al Ejecutivo expedir adecuaciones en 90 días naturales. **Su vigencia es derivada, no declarada**, y no se pudo verificar si entre noviembre 2025 y julio 2026 se expidió un reglamento nuevo.

> **Regla de redacción:** encabezar siempre con el artículo de la **ley de 2025** y usar el Reglamento sólo como norma complementaria. Lo que depende del Reglamento —la carga de la prueba "en todos los casos" (art. 20) y las reglas de bloqueo/supresión (arts. 107-108)— tiene respaldo en la ley por otras vías (arts. 10, 12, 24), así que el diseño no se cae.

---

## 6. Riesgos y anti-patrones

> ⚠️ **No investigado con evidencia verificada.** Ninguna de las dos rondas produjo afirmaciones verificadas sobre: el *settlement* de Kronos/UKG bajo BIPA, demandas contra patrones por relojes biométricos, sanciones de la AEPD española o la CNIL francesa, fallos de proyectos de checador, resistencia sindical, ni migraciones de huella a otras modalidades.
>
> Tampoco hay dato verificado sobre **tasas de falso rechazo (FRR) ni *failure-to-enroll* en trabajadores manuales** —dedos desgastados, secos, con callos o grasa— que es justamente lo que determina si la huella es viable en bodega.
>
> Las fuentes se descargaron en la ronda 1 (`biometricupdate.com` sobre el acuerdo de $15M de Kronos; `commerciallitigationupdate.com` sobre litigio BIPA; una resolución de la AEPD española) pero **no llegaron a la fase de verificación** porque el presupuesto se agotó en los claims técnicos. Están identificadas, no validadas.

**Lo que sí se sabe, de la ronda 1:** existe un caso de soporte documentado en Anviz CrossChex Cloud donde un turno 21:00–05:00 se computó como 15-16 horas por tener el corte de jornada en su valor por defecto. Es el único anti-patrón con evidencia en este expediente, y es de cálculo, no de biometría.

---

## 7. Recomendación de diseño

> Síntesis del analista derivada de los hallazgos anteriores. **No es afirmación citable de ningún proveedor.** Sujeta a revisión tras cerrar la brecha legal.

### v1 — mínimo defendible

**Bloque técnico:**

| # | Decisión | Razón |
|---|---|---|
| a | **Marcaje como evento inmutable, append-only:** `{empleado, instante UTC, dispositivo, sede, método de identificación, resultado de verificación}`. Conservar **siempre** el crudo, incluidos duplicados —marcados como suprimidos en el cálculo, nunca borrados. | §2.1, §2.8 |
| b | **Método de identificación intercambiable desde el día uno** (huella, credencial, PIN de respaldo). | §1.1 + §5.4b |
| c | **"Jornada lógica" con hora de corte configurable**, nunca agrupación por fecha calendario. | §2.4 |
| d | **Cálculo derivado y recomputable**, separado del evento: algoritmo flexible (2º−1º, 4º−3º), primer/último marcaje como llegada/salida. | §2.9 |
| e | **De-duplicación por ventana configurable.** | §2.8 |
| f | **Sincronización idempotente con búfer offline durable.** | §3.1 |

**Bloque legal — no es papeleo, es esquema de datos y flujo:**

| # | Decisión | Razón |
|---|---|---|
| g | **Ciclos de vida separados:** la *plantilla biométrica* se purga al terminar la relación laboral; las *checadas* se conservan como registro de asistencia. Nunca en la misma tabla ni bajo la misma política de retención. | §5.5 |
| h | **Identificación 1:N (huella primero), con número como respaldo.** *Decisión del negocio 2026-07-21, en contra de la recomendación del INAI.* Ver el recuadro abajo. | §5.4b |
| i | **Registro de consentimiento por empleado:** quién, cuándo, versión del aviso de privacidad, artefacto firmado. Debe existir **antes** del primer enrolamiento, y el patrón carga con la prueba. | §5.4a |
| j | **Ciclo retención → bloqueo → supresión** implementado, con el bloqueo atado al plazo de prescripción. No basta un `DELETE`. | §5.5 |
| k | **Alternativa no biométrica operativa** (credencial o PIN) para quien no pueda o no quiera enrolarse. | §5.4b — inferencia prudente, ver §5.8 |

> **Nota sobre 1:N — decisión consciente, no descuido.**
>
> La guía del INAI recomienda priorizar **verificación (1:1)** sobre **identificación (1:N)** para control de acceso, y pone el registro de asistencia como el ejemplo típico de 1:1 (§5.4b). Poner el dedo primero, sin que el empleado se identifique antes, es 1:N por definición.
>
> **Se eligió 1:N de todos modos**, por dos razones: (a) el riesgo de coincidencia falsa en 1:N escala con el tamaño de la plantilla, y con ~15 empleados es despreciable; (b) la fricción de teclear cuatro dígitos dos veces al día por persona es un costo operativo real y diario.
>
> **Condiciones que hacen defendible la decisión:**
> 1. Queda **documentada aquí** — si alguien pregunta, la respuesta es que se evaluó, no que se ignoró.
> 2. El **número sigue existiendo como credencial** en el mismo kiosko, así que el flujo se puede invertir sin rehacer la pantalla ni el modelo de datos.
> 3. **Revisar si la plantilla crece** por encima de ~50 personas o si se suman sedes: ahí el argumento (a) deja de sostenerse.

### v2

Excepciones tipificadas contra turno programado · tolerancias · redondeo con umbral de dirección · descansos (duración individual, acumulada y orden) · políticas de retardo, falta y horas extra · flujo de corrección con aprobación y bitácora inmutable.

### NO construir en v1

Nómina/prenómina · programación de turnos · geocercas · multi-huso horario · cualquier cálculo de horas extra con efecto de pago.

### Decisiones caras de revertir — tomarlas ANTES de escribir código

1. **La hora de corte de jornada** y la política de asignación de horas del turno nocturno.
2. **La inmutabilidad del evento crudo.**
3. **El desacoplamiento del método de identificación.**
4. **La separación del ciclo de vida plantilla vs. checada** — si nacen acopladas, desacoplarlas después implica migración de datos sensibles.
5. **1:1 vs. 1:N** — condiciona la UX del kiosko y el contrato del endpoint de marcaje. *Resuelto: 1:N con número de respaldo (ver recuadro en v1).*

---

## 8. Brechas abiertas

Lo que **no** quedó resuelto en dos rondas, en orden de importancia para decidir:

| # | Pregunta abierta | Por qué importa |
|---|---|---|
| 1 | ¿Puede el patrón **condicionar el empleo** al uso de huella? ¿Es obligatorio ofrecer alternativa no biométrica? | Los dos claims que lo sustentaban fueron refutados. Determina si (k) es obligación o cortesía. |
| 2 | ¿Qué **valor probatorio** tienen los registros de asistencia ante los Tribunales Laborales (arts. 784 y 804 LFT, criterios de la SCJN)? | Si el objetivo incluye defensa en juicio, cambia los requisitos de la bitácora. |
| 3 | ¿Cómo resuelven los sistemas serios los **marcajes impares/huérfanos**? | Requisito explícito del brief; el algoritmo de BioTime sólo funciona con secuencias pares. |
| 4 | ¿Cuál es el patrón defendible de **corrección manual con aprobación y bitácora inmutable**? | Refutado 0-3 en ronda 1; sin evidencia en ronda 2. Hay que diseñarlo de primeros principios. |
| 5 | ¿**Tasas de falso rechazo** en personal de bodega (dedos desgastados)? | Sin este dato no se puede dimensionar el costo operativo ni justificar huella frente a credencial. |
| 6 | ¿Sigue vigente el **Reglamento de 2011** o se expidió adecuación tras el transitorio décimo segundo? | Afecta la carga de la prueba del consentimiento y las reglas de bloqueo/supresión. |
| 7 | ¿Qué más contienen las **Recomendaciones de Seguridad del INAI (dic. 2024)** sobre plantilla vs. imagen, cifrado y almacenamiento en dispositivo vs. servidor? | Sólo se verificó su recomendación 14. |

---

## 9. Maqueta de interfaz

Archivo: [`checador-mockup.html`](checador-mockup.html) — autocontenido, sin dependencias externas. Ábrelo en cualquier navegador.

### Alcance de superficies (decisión 2026-07-21)

**El personal solo ve el kiosko.** No hay app ni portal del empleado: quien quiera revisar sus horas las pide en administración. Una sola superficie que mantener y un solo lugar donde puede fallar algo. Por lo mismo, la consola de administración carga con toda la densidad —la usa quien sabe lo que está viendo.

### Las cinco pantallas

| Pantalla | Quién la usa | Qué resuelve |
|---|---|---|
| **Kiosko** | Personal | Marcar entrada/salida. Huella primero, número como respaldo en la misma pantalla. |
| **Consola del día** | Administración | Presencia por sede + pendientes de resolver + últimos marcajes, sin cambiar de vista. |
| **Rejilla del periodo** | Administración | 15 personas × 7 días en una pantalla, con totales por renglón. La vista de "ver todo de una". |
| **Detalle de persona** | Administración | Barra por día: horario esperado (contorno) vs. real (sólido). Aquí se corrige. |
| **Horario esperado** | Administración | Siete renglones por empleado, uno por día de la semana. Descanso como estado. |

### Decisiones de diseño que se sostienen solas

- **Identidad tomada de la tarjeta checadora:** cifras monoespaciadas (que además alinean en columnas), renglones reglados, y el sello como gesto. Acento índigo de tinta, no el terracota o el verde ácido de siempre.
- **Estado por forma + color**, nunca solo por tono: disco sólido (adentro), medio anillo (retardo), anillo hueco (no ha llegado). Se lee en blanco y negro. Criterio de Nielsen Norman Group; ~8% de los hombres no distingue bien los tonos.
- **Una sola animación protagonista:** el sello, 375 ms. Todo lo demás entre 150 y 250 ms, o quieto. Arriba de 500 ms se percibe como ruido en una pantalla que se actualiza sola.
- **La excepción no grita:** falta de salida es un bloque abierto con borde punteado; el retardo es una etiqueta. Ningún renglón se pinta de rojo.
- **Kiosko oscuro, consola clara.** Uno vive en pared de bodega con luz mala; el otro en un monitor de oficina.
- **Corregir no es borrar.** El marcaje original se conserva; la corrección se agrega firmada con usuario y hora.

### Confirmación hablada y la pregunta del monitor

**¿Se puede operar sin monitor?** Sí, siempre que exista *alguna* confirmación. Si la persona pone el dedo y no pasa nada perceptible, va a repetir el marcaje, o se va con la duda, y no habrá cómo resolver un reclamo de quincena. La confirmación no es decoración de la pantalla: es la mitad del producto.

Tres arquitecturas posibles:

| | Montaje | Confirmación | Respaldo por número | Dónde vive la plantilla |
|---|---|---|---|---|
| **A** | PC + lector + monitor | Pantalla | Sí, con teclado en pantalla | Nuestra base |
| **B** | PC + lector, sin monitor | **Voz + LED** | ✗ se pierde | Nuestra base |
| **C** | Terminal autónoma (ZKTeco, Suprema) | Pantallita + bocina del aparato | Sí, teclado físico | **En el aparato** |

**Recomendación para bodega: C.** Sale más barata que PC con monitor, está hecha para pared con polvo, resuelve la confirmación sin que nosotros programemos nada, y **elimina el kiosko de nuestro alcance** —esa interfaz la pone el fabricante; nosotros solo construimos la consola.

Dos consecuencias que hay que aceptar conscientemente:
- **En B se cae el respaldo por número:** sin pantalla no hay dónde teclear ni dónde ver lo tecleado. Quien tenga el dedo gastado tendría que ir a la oficina todos los días.
- **En C la plantilla se muda al aparato:** dar de baja a alguien deja de ser un `DELETE` en Firebird y pasa a ser también borrarlo de cada terminal donde esté enrolado. Manejable, pero hay que diseñarlo desde el principio (ver §5.5, supresión al terminar la relación laboral).

**La API es indiferente a las tres.** Ya recibe marcajes con dispositivo y sede de origen; la elección de montaje no amarra el backend.

**Sobre la voz — no es un adorno, es consecuencia de haber elegido 1:N.** Con el dedo primero el sistema *adivina* la identidad. Si acierta mal, la persona solo puede darse cuenta si el aparato **dice el nombre en voz alta**; sin eso, una coincidencia falsa se registra en silencio y aparece hasta la quincena. Reglas:

- Decir **nombre + acción** ("Bertha Solís, entrada"), no "gracias" —que no confirma nada. Corto: la fila no espera un discurso.
- El **error no dice nombre** (no hay ninguno que decir, y no se trata de exhibir a nadie): tono grave y "intenta otra vez".
- **Generación del audio sin grabar por persona:** Windows trae síntesis integrada (SAPI). Al enrolar se genera el clip una vez y se guarda. Funciona sin internet.

La maqueta ya lo demuestra: el botón *Voz activada* bajo la tablet la enciende y apaga.

### Dos cosas que decidir antes de construir

1. **Presencia bruta vs. horas netas.** Lo que mide el kiosko es de la entrada a la salida —la comida va incluida. Para horas netas hay dos caminos: restar una comida fija (una resta) o que la gente marque al salir a comer (dos marcajes más por persona por día, mucha más fricción). No es lo mismo y el número sale distinto.
2. **Cómo se presenta el total frente a la jornada de 40 horas.** Con la reforma del DOF 01-05-2026 (§5.7), una semana de ~48 h de presencia va a leerse como jornada excedida por quien no conozca la diferencia del punto anterior. Conviene que la etiqueta lo diga.

---

## Apéndice A — Limitaciones de la investigación

**Sesgo de fuentes:** prácticamente toda la evidencia de la ronda 1 es documentación de producto de los propios fabricantes (UKG/Kronos, ZKTeco, Truein, Patriot, Worky). Es la fuente primaria correcta para responder "qué ofrece el producto X", pero **no aporta evidencia de eficacia, adopción real ni satisfacción**. Ninguna afirmación de desempeño fue verificada. El material mexicano consultado es *copy* comercial de landing pages.

**Vigencia y URLs:** varias citas provienen de la línea *legacy* Kronos Workforce Central (rutas `/Help-STP/`), no de UKG Pro WFM actual. URLs inestables detectadas y sustituidas: `dcus11-hlp01.gss.mykronos.com` (404, host por *tenant* que caduca → usar `communityfiles.ukg.com`); `zkteco.eu/.../biotime-8.0-user-manual-v4.0` (403 → usar manual 8.0 V1.0/V2.0 o ZKBio Time 2023); Patriot (301 → `help.patriotsoftware.com`). El folleto de InTouch DX es de 2021-2022.

**Límites del proceso:** en ambas rondas el presupuesto de búsqueda web se agotó (200/200) durante la verificación, por lo que varios verificadores no pudieron ejecutar barrido adversarial de fuentes contradictorias y lo compensaron descargando los PDF primarios y comparando verbatim con `pdftotext`.

> Esa compensación es **dispositiva para afirmaciones sobre el contenido de un texto legal** —o el texto dice lo que se afirma o no lo dice— pero **no lo es para doctrina, criterios judiciales ni práctica de mercado**. Y esas son precisamente las que faltan (§8).

**Sobre la ronda 2 en particular:**
- La guía principal del INAI consultada es la edición de **marzo 2018**, no la de diciembre 2024 que pedía el encargo. Sí se verificó que las *Recomendaciones de Seguridad* de **diciembre 2024** reproducen textualmente el criterio de borrado al terminar la relación laboral (rec. 14), pero el resto de esa edición no se revisó.
- Ambas guías son **soft law de una autoridad extinta**: su fuerza hoy es orientativa/heredada (transitorio cuarto), no vinculante por sí misma. La exigibilidad viene de la ley.
- `dof.gob.mx` falló repetidamente por cadena de certificados TLS; se usó la compilación oficial de la Cámara de Diputados como fuente primaria equivalente. Se corrigió un código de nota del DOF erróneo heredado de la ronda 1 (`5752076` → `5752569`).

## Apéndice B — Estadísticas

| Ronda | Ángulos | Fuentes | Claims extraídos | Verificados | Confirmados | Refutados | Agentes | Tokens |
|---|---|---|---|---|---|---|---|---|
| 1 — features + dominio | 5 | 23 | 108 | 25 | 18 | 7 | 105 | 4.1 M |
| 2 — legal + brechas | 6 | 27 | 128 | 25 | 23 | 2 | 110 | 4.9 M |
| **Total** | | **50** | **236** | **50** | **41** | **9** | **215** | **9.0 M** |

Método: descomposición en ángulos → búsqueda paralela → *fetch* y extracción de afirmaciones falsables → verificación adversarial con 3 votantes independientes instruidos para **refutar** (se descarta con 2/3) → síntesis.
