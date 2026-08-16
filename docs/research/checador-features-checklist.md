# Checador de asistencia — catálogo de features para decidir alcance

> **Fecha:** 2026-07-22 · Complementa [`checador-biometrico-estado-del-arte.md`](checador-biometrico-estado-del-arte.md)
> **Propósito:** revisar feature por feature para no descubrir huecos a medio construir.

## Cómo leer este documento

| Marca | Significado |
|---|---|
| 🟢 | **v1** — sin esto no es un checador |
| 🔵 | **v2** — se necesita, pero no bloquea el arranque |
| ⚪ | **Fuera de alcance** — decidido que no |
| ❔ | **Sin decidir** — requiere que alguien resuelva |
| ⚡ | **Vanguardia del sector** — punta de lanza hoy; ver §11 antes de emocionarse |
| ⚠️ | **Trampa** — se olvida y cuesta caro corregirlo después |

---

## 1. Captura del marcaje

| | Feature | Nota |
|---|---|---|
| 🟢 | Marcaje por huella | Modalidad principal, decidida |
| 🟢 | Marcaje por número/NIP | Respaldo cuando el lector falla |
| 🟢 | Confirmación al trabajador | Pantalla, voz o LED. **No es opcional** |
| 🟢 | Confirmación hablada con nombre | Consecuencia de 1:N: es lo único que detecta una coincidencia falsa |
| 🟢 | Entrada/salida inferida (toggle) | El sistema deduce cuál toca; nadie elige botón |
| 🟢 | Ventana anti-rebote | Descarta el segundo dedazo en N minutos |
| 🟢 | Dispositivo y sede en cada marcaje | Sin esto no hay multi-sede ni diagnóstico |
| 🟢 | Método de identificación registrado | Huella / número / credencial. Alimenta la medición de fallas del lector |
| 🟢 | Operación sin red + sincronización | Bodega. La sincronización debe ser **idempotente** |
| ⚠️🟢 | **Reloj confiable del dispositivo** | Si el reloj se corre o alguien lo mueve, todo el registro es basura. NTP obligatorio + registrar desfase |
| 🟢 | **Marcaje de comida** | Decidido 2026-07-22. Pasa a 4 marcajes/día y **resuelve el hueco de bruto vs. neto**: las horas netas se miden, no se estiman |
| ⚪ | Credencial RFID / NFC | Descartado: no hay gafetes |
| ⚪ | App móvil con geocerca | No aplica: nadie marca desde la calle |
| ⚡⚪ | Reconocimiento facial sin contacto | Vanguardia real post-2020 (UKG *TouchFree ID*). Costo alto para 15 personas |
| ⚡❔ | **Detección de vida (liveness / PAD)** | Anti dedo de silicona. Norma ISO/IEC 30107. Viene incluido en terminales decentes — pregúntalo al comprar, no lo programes |
| ⚡⚪ | Foto del marcaje como evidencia | Cámara que guarda quién marcó. Resuelve disputas, pero multiplica el dato sensible que resguardas |

---

## 2. Enrolamiento y ciclo de vida de la plantilla

| | Feature | Nota |
|---|---|---|
| 🟢 | Enrolar varios dedos por persona | Dedo cortado o sucio no debe dejar a nadie fuera |
| 🟢 | Re-enrolar | Los dedos cambian; el desgaste en bodega es real |
| ⚠️🟢 | **Baja: borrar la plantilla de TODOS los dispositivos** | Con terminal autónoma la plantilla vive en el aparato. Dar de baja deja de ser un `DELETE` |
| ⚠️🟢 | **Consentimiento firmado ligado al enrolamiento** | Requisito legal con carga de la prueba del patrón. Debe existir **antes** de capturar la huella |
| 🟢 | Alternativa no biométrica operativa | Para quien no pueda o no quiera enrolarse |
| 🔵 | Calidad de captura en el enrolamiento | Rechazar una plantilla mala en el momento evita meses de falsos rechazos |
| ⚡🔵 | **Puntaje de calidad NFIQ** | Estándar del NIST para medir qué tan buena es una huella. Vanguardia en despliegues serios; en terminales comerciales suele venir simplificado a "buena/regular/mala" |
| ⚡⚪ | Plantillas cancelables / protección de plantilla | ISO/IEC 24745: transformar la plantilla para poder "revocarla" como una contraseña. Genuinamente punta de lanza y casi nadie lo despliega |

---

## 3. Modelo de tiempo — el corazón

| | Feature | Nota |
|---|---|---|
| ⚠️🟢 | **Evento de marcaje inmutable (append-only)** | Decisión cara de revertir. Nunca borrar, ni siquiera duplicados |
| ⚠️🟢 | **Jornada lógica con hora de corte configurable** | *Day divide*. Agrupar por fecha calendario es el error del que no se regresa |
| 🟢 | Jornada derivada y **recomputable** | El cálculo no se persiste como verdad; se puede volver a correr |
| 🟢 | Emparejamiento entrada-salida | Primer y último marcaje del día lógico |
| 🟢 | **Marcajes impares / huérfanos** | **Resuelto 2026-07-22 — ver §13.1** |
| 🟢 | Horario esperado por día de la semana | Fijo por persona, siete renglones. Decidido |
| ⚠️🔵 | **Versionado del horario esperado** | Si le cambias el horario a alguien hoy, ¿se recalculan los retardos del mes pasado? Casi siempre la respuesta correcta es **no**, y casi siempre se implementa mal |
| 🟢 | Tolerancia antes de contar retardo | Sin esto la palabra "retardo" no significa nada |
| 🔵 | Redondeo de marcajes | Con umbral de dirección. **Ojo:** el *grace* de Kronos es dirección de redondeo, no tolerancia de retardo |
| 🔵 | Turnos que cruzan medianoche | Solo si algún día hay velador o turno nocturno |
| ⚪ | Turnos rotativos / programación semanal | Los horarios son fijos por persona |
| ⚪ | Zonas horarias múltiples | Todas las sedes en el mismo huso; México sin horario de verano desde 2022 |

---

## 4. Excepciones e incidencias

| | Feature | Nota |
|---|---|---|
| 🟢 | Sin salida / sin entrada | El caso más común en la vida real |
| 🟢 | Retardo | Contra horario esperado + tolerancia |
| 🟢 | Falta | No marcó en día laborable |
| 🟢 | Marcaje sin horario (`Unscheduled`) | Marcó en su día de descanso |
| 🔵 | Salida anticipada | |
| 🔵 | Jornada anormalmente corta o larga | Se calcula marcaje-contra-marcaje, no contra horario |
| 🔵 | Catálogo de incidencias | Permiso, incapacidad, vacaciones, falta justificada |
| 🔵 | Días festivos oficiales | Calendario mexicano; sin esto el 16 de septiembre son 15 faltas |
| 🔵 | Justificar una incidencia con documento | Incapacidad del IMSS, por ejemplo |
| ⚪ | Reglas de orden de descansos | "Descanso, comida, descanso". Overkill aquí |

---

## 5. Correcciones y auditoría

> **Decidido en §13.2: bitácora sí, flujo de aprobación no.** La ronda inicial refutó (0-3) que existiera un patrón de referencia documentado; la búsqueda del 22-07 sí recuperó las tres acciones de UKG (`Mark as Reviewed` / `Edit` / `Comments`) y su marca visual de lo generado por el sistema.

| | Feature | Nota |
|---|---|---|
| ⚠️🟢 | **Corregir sin borrar** | El marcaje original se conserva; la corrección se agrega encima |
| ⚠️🟢 | **Bitácora: quién, cuándo, qué cambió** | Es lo que hace que el registro valga algo si alguien lo cuestiona |
| 🟢 | Motivo obligatorio en la corrección | Un campo libre vacío no sirve de defensa |
| 🟢 | Distinguir marcaje real de marcaje capturado | Que se vea en pantalla cuál fue puesto a mano |
| 🔵 | Cierre de periodo | Después de cerrar, nada cambia sin reabrir explícitamente |
| ⚡🔵 | Bitácora de **lectura** de datos biométricos | Registrar quién consultó, no solo quién modificó. Vanguardia en cumplimiento; exigible si el dato es sensible |
| ⚪ | Flujo de solicitud por el empleado | Decidido: el personal no tiene pantalla |

---

## 6. Consola de administración

| | Feature | Nota |
|---|---|---|
| 🟢 | Presencia en vivo por sede | "Quién está adentro" |
| 🟢 | Pendientes de resolver | La lista de trabajo, no un reporte |
| 🟢 | Rejilla del periodo | Todos × todos los días, con totales |
| 🟢 | Detalle de una persona | Barra por día, esperado vs. real |
| 🟢 | Últimos marcajes | Diagnóstico inmediato: "¿está marcando la bodega?" |
| 🟢 | Filtros por sede y rango de fechas | |
| 🟢 | Búsqueda por nombre o número | |
| 🔵 | Exportar a Excel/CSV | Lo primero que va a pedir quien haga nómina |
| 🔵 | Reporte de retardos y ausentismo | |
| 🔵 | Reporte de horas por periodo | |
| 🔵 | Alertas | "Nadie ha marcado en Ajalpan a las 9:00" vale; "fulano llegó 3 min tarde" es ruido |
| ⚪ | Tablero de indicadores tipo RH | Rotación, clima, headcount. Otro producto |

---

## 7. Administración y catálogos

| | Feature | Nota |
|---|---|---|
| 🟢 | Alta y baja de empleado | La baja dispara el borrado de plantilla |
| 🟢 | Horario por empleado | |
| 🟢 | Sedes | |
| 🟢 | Dispositivos y a qué sede pertenecen | |
| 🟢 | Roles y permisos | El módulo respeta el esquema `asistencia:*` del proyecto |
| 🔵 | Tolerancias y reglas configurables | Empezar con una global; por persona si hace falta |
| 🔵 | Calendario de días festivos | |
| ❔ | Áreas o departamentos | ¿Sirve agrupar más allá de la sede? |

---

## 8. Cumplimiento (obligatorio, no negociable)

| | Feature | Nota |
|---|---|---|
| ⚠️🟢 | Consentimiento expreso y por escrito, guardado | Carga de la prueba del patrón |
| ⚠️🟢 | Aviso de privacidad **versionado** | Hay que poder decir qué versión firmó cada quien |
| ⚠️🟢 | Ciclo retención → bloqueo → supresión | No basta un `DELETE`; el bloqueo va atado al plazo de prescripción |
| 🟢 | Plantilla ≠ imagen de la huella | Nunca guardar la imagen |
| 🟢 | Cifrado del dato biométrico en reposo | |
| ⚠️🟢 | **Ciclos de vida separados: plantilla vs. checadas** | La plantilla se purga al terminar la relación; las checadas se conservan |
| 🔵 | Reporte de cumplimiento | Quién está enrolado, quién firmó, qué se ha borrado |

---

## 9. Operación e infraestructura

| | Feature | Nota |
|---|---|---|
| ⚠️🟢 | **Salud de dispositivos (heartbeat)** | Un lector muerto se descubre en la quincena si nadie vigila. "Último contacto hace 3 h" evita el desastre |
| 🟢 | Cola offline con capacidad y retención explícitas | |
| 🟢 | Sincronización idempotente | Reintentar no debe duplicar marcajes |
| 🔵 | Métrica de tasa de rechazo del lector | Detecta a quien necesita re-enrolarse **antes** de que se queje |
| 🔵 | Respaldo y restauración | |

---

## 10. Integraciones

| | Feature | Nota |
|---|---|---|
| 🔵 | Exportar para nómina | Formato por definir con quien la calcula |
| ❔ | Reusar el catálogo de empleados existente | ¿Hay uno en Microsip o se captura aparte? **Decidir temprano** |
| ⚪ | Integración con nómina automática | No hay sistema de nómina que integrar hoy |

---

## 11. Sobre la vanguardia del sector

Las features marcadas ⚡ son lo que hoy define la punta de lanza. **Casi ninguna es para ti**, y conviene saber por qué:

**Vale la pena exigirla al comprar (no programarla):**
- **Detección de vida (PAD, ISO/IEC 30107).** Es la defensa contra el dedo de silicona. Viene incluida en terminales decentes. No la construyes: la pides.
- **Puntaje de calidad en el enrolamiento.** Evita que una plantilla mala se convierta en un año de falsos rechazos.

**Vale la pena, pero es v2:**
- **Bitácora de lectura de datos biométricos.** Registrar quién *consultó*, no solo quién modificó. Con dato sensible es una defensa barata.

**Vanguardia real que aquí sería desperdicio:**
- **Reconocimiento facial sin contacto.** Es el movimiento genuino del sector desde 2020 y los líderes lo empujan fuerte. Para 15 personas, el costo no se justifica.
- **Plantillas cancelables (ISO/IEC 24745).** Poder "revocar" una huella como si fuera contraseña. Es lo más avanzado que existe en el tema y prácticamente nadie lo despliega en producción.
- **Analítica predictiva de ausentismo.** Existe en las suites grandes. Con 15 personas, el patrón lo ve el dueño mejor que un modelo.

**Y una advertencia sobre lo que el mercado vende como innovación y no lo es:** la investigación **refutó** (voto 0-3) que exista una migración del sector de huella hacia rostro, y que el anti-suplantación se resuelva combinando biometría con geocerca. Son argumentos de venta, no hallazgos.

---

## 12. Huecos que siguen abiertos

| # | Hueco | Estado |
|---|---|---|
| 1 | Marcajes impares/huérfanos | ✅ **Cerrado** — §13.1 |
| 2 | Patrón de corrección con bitácora defendible | ✅ **Cerrado** — §13.2 |
| 3 | Presencia bruta vs. horas netas | ✅ **Cerrado** — se marca la comida, se miden las netas |
| 4 | Valor probatorio en juicio laboral | ✅ **Cerrado** — §13.3. **Hallazgo mayor: el registro electrónico ahora es obligatorio (LFT 132-XXXIV)** |
| 5 | Tasas de falso rechazo en dedos de bodega | Abierto |
| 6 | ¿De dónde sale el catálogo de empleados? | Abierto |
| 7 | ¿El marcaje llega tipado del aparato (F1–F4) o se infiere? | Abierto — **pregunta de compra**, no de programación |

---

## 13. Decisiones cerradas

### 13.1 Marcajes impares y huérfanos — cerrado 2026-07-22

> **Dato operativo que determinó la decisión:** administración **solo revisa** los registros; no se va a poner a corregirlos a mano. Cualquier diseño que dependa de que alguien haga clic en una lista de pendientes se degrada a "nadie lo cierra nunca" y esos días se quedan en cero para siempre. Se diseña para la conducta real, no para la deseable.

**Decisión: cierre automático con la hora programada, visiblemente marcado, revisable después.**

1. Al cerrar el día lógico, el sistema **completa el marcaje faltante con la hora del horario esperado** y lo marca como generado por el sistema.
2. Los días completados así aparecen en una **lista de revisión** —no de tareas—: "esto lo completó el sistema, échale ojo si quieres". Si nadie la mira, el número sigue siendo razonable en vez de cero.
3. Cualquiera con permiso puede **corregir después**, cuando quiera, y esa corrección se firma.

**Por qué el problema es más chico de lo que parece:** lo que el negocio pidió saber es **la hora de llegada**, y la llegada casi nunca es el marcaje que falta —nadie olvida marcar al entrar, con la fila detrás. El que se olvida es el de salida y el de regreso de comida. El hueco afecta al cálculo de horas, no al dato que originó el proyecto.

**Riesgo aceptado explícitamente:** si alguien se fue a las 14:00 y no marcó, el sistema le acredita hasta su hora de salida y nadie se entera.
**⚠️ Disparador para revisar esta decisión:** el día que de estos registros salga un descuento, un pago o una llamada de atención. Ahí el número tiene que haberlo validado una persona.

**Qué del mercado se está copiando y qué no** (investigación 2026-07-22):

- ✅ **El valor de relleno sale del horario programado.** Deputy y Homebase hacen exactamente eso. Nadie rellena con cero ni con el corte de jornada.
- ✅ **Lo generado por el sistema se distingue visualmente.** UKG literal: `When an exception or punch is system-generated, the icon is purple with one diagonal line and the punch displays in purple.`
- ✅ **Requiere aprobación antes de contar.** Universal entre los proveedores revisados.
- ✅ **La red de seguridad tardía existe y tiene propósito.** Deputy cierra a las **23 h** — no es regla de negocio, es higiene del estado en vivo. De ahí sale nuestro umbral de 20 h.
- ❌ **No se copia el flujo de UKG donde el empleado inicia la corrección**, porque aquí el personal no tiene pantalla. La inicia administración.

**Corrección a una afirmación previa:** el *day divide* **no** es un mecanismo de relleno de marcaje — es la regla de a qué día pertenece el turno. Ningún proveedor cierra el día en el corte de jornada.

**Sin consenso de industria:** la investigación buscó explícitamente una fuente que declarara mejor práctica 2025-2026 en este tema y **no existe**. Cada proveedor hace lo suyo; lo anterior es síntesis de lo que coinciden, no doctrina citable.

### 13.2 Correcciones y bitácora — cerrado 2026-07-22

**Decisión: bitácora sí, flujo de aprobación no.**

Se desprende del mismo dato operativo: si nadie corrige a mano, montar aprobación de dos pares de ojos es teatro. **No se construye flujo de aprobación para un proceso que nadie ejecuta.**

Lo que **sí** se construye, porque es barato y se escribe solo:

1. **Bitácora por corrección:** qué marcaje se tocó, valor anterior y nuevo, quién, cuándo, motivo, y **origen del valor** — real / generado por el sistema / capturado a mano.
2. **Distinción visual** entre lo que atestiguó una persona y lo que calculó la máquina. Referencia verificada de UKG: `When an exception or punch is system-generated, the icon is purple with one diagonal line and the punch displays in purple.`
3. **Nadie corrige sus propios marcajes.** Se conserva incluso habiendo un solo administrador —precisamente porque hay uno solo: sus correcciones escalan al dueño. Cuesta una línea y cierra el único conflicto de interés real del módulo.

**De UKG se rescata una acción que parece trivial y no lo es:** `Mark as Reviewed` — "ya lo vi, decidí no hacer nada". Sin esa acción, la lista de revisión nunca se vacía y la gente deja de mirarla.

**No verificado:** si la pestaña de auditoría de UKG registra el **nombre de quien aprobó**. Es justo el dato que hace defendible una bitácora, y no se pudo confirmar en la documentación pública.

### 13.3 Marco legal del registro de jornada — cerrado 2026-07-22

**El hueco se abrió como "valor probatorio" y se cerró con algo más grande: el registro electrónico de jornada es ahora una obligación legal con multa.** Detalle completo y citas en §5.7 del reporte.

Lo que cambia para el producto:

| Hallazgo | Consecuencia de diseño |
|---|---|
| **LFT art. 132-XXXIV** (DOF 01-05-2026): obliga a registrar electrónicamente la jornada, con hora de inicio y fin, y a entregarlo a la autoridad cuando lo requiera | El módulo deja de ser opcional. Debe poder **exportar el registro para la autoridad**, no solo para uso interno |
| **Multa: 250 a 5,000 UMA** (art. 994 fr. IV Bis) | El costo de no tenerlo es real y cuantificado |
| **"Prueba plena si se acredita que fue acordado"** entre trabajador y patrón | 📌 **Se resuelve con el mismo papel del consentimiento biométrico.** Un solo documento al enrolar cubre las dos obligaciones |
| **Art. 804 fr. III + art. 805**: los controles de asistencia se conservan `durante el último año y un año después de que se extinga la relación laboral`; no exhibirlos presume ciertos los hechos del trabajador | 📌 **Retención mínima de las checadas**, distinta y más larga que el ciclo de la plantilla biométrica |
| **Disposiciones generales de la STPS entran en vigor el 01-01-2027** (Transitorio Quinto) | La obligación ya corre; su reglamentación fina no existe aún. **Revisar en enero de 2027** |
| **Sin firma no aplica la presunción reforzada** de autenticidad (criterio 818664) | Confirma el valor del acuerdo firmado. El diseño sin pantalla del empleado se sostiene *porque* el acuerdo va aparte, en papel |

**Nota sobre informalidad:** el proyecto se abordó asumiendo que la formalidad del empleo hacía irrelevante lo probatorio. No lo hace: la relación de trabajo nace del hecho de trabajar subordinado, no del contrato. Y un registro biométrico diario **documenta esa relación**, sin importar cómo esté dada de alta.
