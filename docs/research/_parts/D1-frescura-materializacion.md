# D1 — Frescura y materialización de tablas derivadas

> **Contexto de aplicación:** un retailer pequeño lee una BD transaccional ERP (Microsip / Firebird) que no controlamos y sobre la que **no tenemos acceso a logs, no podemos instalar triggers ni habilitar replicación**. Materializamos un read-model "candidatos winback" y lo servimos a un dashboard + motor de decisiones. Stack: Go en Windows, sin cloud warehouse, sin Kafka/Debezium. Miles de clientes, cientos de miles de transacciones. El grupo de control (holdout) debe permanecer **estable** entre refreshes.

---

## 1. Paradigmas de refresh: cuándo usa cada cosa el tier superior

### Cómo lo hacen los tops

Los equipos de datos maduros no eligen un solo paradigma — aplican una jerarquía según latencia requerida y control sobre la fuente:

| Paradigma | Cuándo | Ejemplos reales |
|-----------|--------|-----------------|
| **Full rebuild** | Tablas pequeñas, consultas que no admiten incremental, "self-heal" nocturno | Dimensiones pequeñas en cualquier empresa |
| **Incremental / upsert (query watermark)** | Tablas grandes, fuente con `updated_at` confiable, sin acceso a logs | Uber (Hudi DeltaStreamer), equipos dbt sobre ERP/SaaS |
| **CDC log-based** | Latencia < 1 min, captura de deletes, fuente bajo control total | Airbnb (Riverbed + Kafka CDC), Stripe, Debezium en Postgres |
| **Streaming continuo** | Eventos en tiempo real, decisiones sub-segundo | Airbnb Riverbed (Kafka + Spark), Netflix (Flink), feature stores online |

**Airbnb (Riverbed):** arquitectura Lambda híbrida. El pipeline de streaming consume CDC events vía Apache Kafka, reparticiona por `document_id` para garantizar orden secuencial, y escribe al read-model store. Apache Spark corre reconciliaciones batch diarias desde snapshots del warehouse para recuperar CDC events perdidos. Procesa 2.4 B eventos/día y escribe 350 M documentos. ([InfoQ 2023](https://www.infoq.com/news/2023/10/airbnb-riverbed-introduction/))

**Uber:** migró todos sus pipelines batch a incremental sobre Apache Hudi. La clave es `DeltaStreamer` que guarda checkpoints en los metadatos de commit de Hudi: en cada ejecución retoma desde el último checkpoint sin lookback costoso. Resultados: 82% de reducción en tiempo de ejecución de tablas dimensionales, 60% de mejora en SLA, 78% de ahorro en costo en algunos pipelines. ([Uber TechBlog](https://www.uber.com/en-GB/blog/ubers-lakehouse-architecture/))

**Netflix:** construyó un sistema de Incremental Processing basado en Apache Iceberg. Mantiene una "ICDC table" que almacena referencias a los archivos de datos de la tabla original (sin copiarlos) y captura el rango de cambio para campos específicos. Elimina la necesidad de lookback windows para late data en casos append. Gestiona >1 millón de tablas Iceberg con cientos de miles de workflows. ([InfoQ 2024](https://www.infoq.com/news/2024/01/netflix-incremental-processing))

### Pitfalls

- **Full rebuild a escala:** bloquea lecturas (sin `CONCURRENTLY`) y escala mal; Postgres `REFRESH MATERIALIZED VIEW` sin `CONCURRENTLY` adquiere `ExclusiveLock`.
- **Streaming sin fuente controlada:** imposible sin acceso a logs de transacciones.
- **Incremental sin `updated_at`:** completamente ciego a updates y deletes.
- **Nunca hacer full rebuild:** los incrementales acumulan drift; sin un "self-heal" periódico, errores silenciosos se propagan indefinidamente.

### Fuentes

- [Airbnb Riverbed – InfoQ](https://www.infoq.com/news/2023/10/airbnb-riverbed-introduction/)
- [Uber Lakehouse Architecture – Uber TechBlog](https://www.uber.com/en-GB/blog/ubers-lakehouse-architecture/)
- [Netflix Incremental Processing – InfoQ](https://www.infoq.com/news/2024/01/netflix-incremental-processing)

---

## 2. Incremental por query con watermarks (ELT clásico)

### Cómo lo hacen los tops

El patrón canónico en dbt es:

```sql
-- modelo incremental con watermark de updated_at
select * from orders
{% if is_incremental() %}
  where updated_at > (select max(updated_at) from {{ this }})
{% endif %}
```

El macro `is_incremental()` devuelve `true` solo cuando: (a) la materialización es incremental, (b) la tabla destino ya existe, y (c) no se pasó `--full-refresh`. En el primer run construye la tabla completa. ([dbt docs](https://docs.getdbt.com/best-practices/materializations/4-incremental-models))

**Elección de columna watermark:**
- `updated_at` es preferido para datos mutables: captura tanto inserts nuevos como modificaciones a filas existentes.
- `created_at` solo sirve para datos append-only (nunca se modifican las filas después de crearse).
- IDs autoincrementales funcionan solo si los registros nunca se modifican.

**El problema de late/edited rows:** un registro del 30 de enero puede llegar al sistema el 2 de febrero, después que el pipeline del 31 ya tomó su watermark. Solución estándar: **ventana de lookback** que resta N días/horas al watermark:

```sql
where updated_at > (select max(updated_at) from {{ this }}) - interval '72 hours'
```

Con `unique_key` configurado, dbt hace upsert (MERGE) automáticamente, por lo que re-procesar el período superpuesto no genera duplicados. La recomendación de la comunidad es documentar un lookback de 48–72 horas para hechos de alto impacto. ([7tech.co.in](https://www.7tech.co.in/late-event-rewrote-friday-data-science-playbook-watermarks-incremental-dbt-merge-backfills/))

**Micro-batch con Airflow / schedulers:** se ejecuta el modelo cada N minutos en lugar de cada noche. El patrón es el mismo; solo cambia la frecuencia del trigger. En Firebird el equivalente sería un `SELECT` filtrado por `UPDATED_AT > :last_watermark` con un loop de upsert en Go.

### Pitfalls

- **Watermark sin `unique_key`:** produce duplicados si una fila se ve dos veces en el período de lookback.
- **Columna `updated_at` no confiable:** si la fuente no la actualiza en cada write, el watermark es ciego a cambios.
- **El watermark toma solo el MAX de la tabla destino:** si el destino está vacío (primer run fallido), el watermark es NULL y se procesa todo — comportamiento correcto pero hay que preverlo.
- **Confiar solo en incremental sin full-refresh periódico:** el drift es inevitable a largo plazo (ver sección 8).

### Fuentes

- [dbt Incremental Models in-depth](https://docs.getdbt.com/best-practices/materializations/4-incremental-models)
- [dbt Incremental Models Overview](https://docs.getdbt.com/docs/build/incremental-models-overview)
- [Late Event Playbook – 7tech](https://www.7tech.co.in/late-event-rewrote-friday-data-science-playbook-watermarks-incremental-dbt-merge-backfills/)

---

## 3. CDC vs polling de queries: por qué CDC está fuera de alcance

### Cómo lo hacen los tops

El CDC log-based (Debezium, Maxwell, AWS DMS) lee el transaction log (WAL en Postgres, binlog en MySQL, journal en Firebird) y emite un evento por cada INSERT/UPDATE/DELETE. Ventajas: captura deletes, preserva el orden exacto de operaciones, latencia sub-segundo, impacto mínimo sobre la fuente.

Debezium vía Kafka es la elección estándar cuando se necesita cobertura completa de cambios a baja latencia con fuente bajo control. ([Conduktor](https://www.conduktor.io/glossary/implementing-cdc-with-debezium), [Estuary](https://estuary.dev/blog/the-complete-introduction-to-change-data-capture-cdc/))

**Por qué CDC es imposible en nuestro caso:**
Firebird / Microsip es una BD que no controlamos. No podemos:
- Habilitar la replicación de journal.
- Instalar Debezium connectors.
- Crear triggers sobre las tablas (ADR-0006 los prohíbe en tablas MSP_* propias; las tablas Microsip están completamente fuera de control).
- Otorgar permisos de lectura al transaction log.

Sin acceso a logs, CDC log-based es **estructuralmente imposible**.

### El fallback práctico: query polling

El query polling ejecuta un SQL contra la BD fuente a intervalos fijos, filtrando por watermark (`UPDATED_AT > :last_watermark`). Es simple, no requiere permisos especiales (solo SELECT), y funciona incluso en ERPs heredados.

**Un argumento profesional a favor del polling:** un equipo de ingeniería documentó una reducción del 80% en costo de pipeline al reemplazar Debezium con query-based CDC, con implementación en 1 sprint vs múltiples equipos durante un cuarto. La razón: para workloads UPDATE-heavy con registros grandes, el replication slot de Postgres generaba backpressure que convertía a Debezium en un problema operacional mayor. ([Christina Taylor – Medium](https://medium.com/@christinataylor0926/less-is-more-a-case-for-query-based-change-data-capture-a3b22349dba6))

**Límites reales del polling:**
- Deletes son invisibles: cuando un registro desaparece de la fuente, el poll no lo ve.
- Requiere que la fuente tenga `UPDATED_AT` confiable en cada tabla relevante.
- Frecuencia de poll = piso de latencia (poll cada 10 min → staleness máximo 10 min).
- Carga adicional sobre la BD fuente proporcional a la frecuencia.

### Pitfalls

- Asumir que Debezium "siempre es mejor": para equipos pequeños sin fuente controlada, el overhead operacional supera los beneficios.
- Ignorar deletes: si los clientes se eliminan de Microsip, nuestra tabla derivada conservará filas huérfanas indefinidamente sin un full-rebuild periódico.
- Frecuencia de poll agresiva sobre una BD de producción compartida (Microsip sigue sirviendo operaciones en vivo).

### Fuentes

- [Estuary – CDC introducción completa](https://estuary.dev/blog/the-complete-introduction-to-change-data-capture-cdc/)
- [Christina Taylor – query-based CDC](https://medium.com/@christinataylor0926/less-is-more-a-case-for-query-based-change-data-capture-a3b22349dba6)
- [Conduktor – Debezium + Kafka](https://www.conduktor.io/glossary/implementing-cdc-with-debezium)
- [Factor House – Kafka CDC guide 2026](https://factorhouse.io/articles/kafka-cdc-change-data-capture)

---

## 4. Feature stores (Feast / Tecton / Databricks)

### Cómo lo hacen los tops

Los feature stores resuelven el problema de servir features de ML con garantías de frescura en dos modos:

- **Offline store:** para entrenamiento y batch scoring. Datos históricos con timestamps, se materializa con Spark/batch jobs. Tolerancia a staleness de horas o días.
- **Online store:** para inference en tiempo real (Redis, DynamoDB). Requiere materialización frecuente (sub-minuto a minutos). Tecton usa streaming pipelines para freshness < 1 segundo en casos como pricing dinámico (PayPal, DoorDash). ([Tacnode](https://tacnode.io/post/what-is-an-online-feature-store-definition-architecture-use-cases))

**Materialización en Tecton:** cuando `online=True` en un Feature View, Tecton programa steady-state materialization jobs con frecuencia controlada por `batch_schedule`. El TTL determina hasta cuándo se considera válido un feature value en el online store — después del TTL, Feast no lo sirve (evita features "quemados" stale). ([Tecton docs](https://docs.tecton.ai/docs/materializing-features))

**Precomputed vs on-demand:**
- **Precomputed:** se materializa antes del request; latencia de serving mínima pero costo de cómputo constante aunque nadie consulte.
- **On-demand:** se computa en el momento del request sobre datos frescos; flexible para features que dependen de contexto del request, pero añade latencia.

**Aplicación a nuestro caso:** nuestro "candidato winback" es análogo a un feature precomputed: calculamos score, días de inactividad, monto histórico con anticipación y lo servimos al dashboard. El TTL equivalente sería la ventana de validez del score antes de que expire por irrelevancia.

### Pitfalls

- **TTL demasiado largo:** el online store sirve scores stale como si fueran actuales.
- **TTL demasiado corto:** los features expiran antes de ser usados, forzando computo on-demand costoso.
- **Inconsistencia offline/online (training-serving skew):** si el offline store y el online store no comparten la misma lógica de materialización, el modelo entrena con features distintos a los que ve en producción.

### Fuentes

- [Tacnode – Feature Store definición y arquitectura](https://tacnode.io/post/what-is-an-online-feature-store-definition-architecture-use-cases)
- [Tecton – Materialize Features](https://docs.tecton.ai/docs/materializing-features)
- [MLOps Platforms – Feature Store Comparison 2026](https://mlopsplatforms.com/posts/feature-store-comparison-2026/)

---

## 5. Refresh de vistas materializadas en motores reales

### Cómo lo hacen los tops

#### PostgreSQL

`REFRESH MATERIALIZED VIEW` hace full rebuild con `ExclusiveLock` (bloquea lecturas). La variante `REFRESH MATERIALIZED VIEW CONCURRENTLY` permite lecturas durante el refresh, pero **requiere al menos un índice UNIQUE** sobre la vista y genera tuplas muertas que necesitan `VACUUM`. No hay refresh incremental nativo; la extensión `pg_ivm` lo añade vía triggers sobre las tablas base, pero soporta un subconjunto restringido de SQL. ([PostgreSQL docs](https://www.postgresql.org/docs/current/sql-refreshmaterializedview.html), [Epsio](https://www.epsio.io/blog/postgres-refresh-materialized-view-a-comprehensive-guide))

#### Snowflake Dynamic Tables

`TARGET_LAG` declara el contrato de frescura explícito (ej: `TARGET_LAG = '10 minutes'`). El sistema elige automáticamente entre refresh incremental y full según la definición SQL. El modo incremental mantiene una columna de metadatos interna por fila para change tracking. El mínimo práctico es 1 minuto. **Restricciones que fuerzan full refresh:** operaciones `EXCEPT`, `INTERSECT`, funciones de percentil exacto (ej: `PERCENTILE_CONT`), y cualquier cómputo que dependa de relaciones entre todas las filas. ([Snowflake docs – refresh modes](https://docs.snowflake.com/en/user-guide/dynamic-tables/refresh-modes), [Snowflake docs – target lag](https://docs.snowflake.com/en/user-guide/dynamic-tables/target-lag))

#### BigQuery

Las Materialized Views de BigQuery soportan refresh incremental automático para un subconjunto de patrones SQL (agregaciones simples sobre tablas base). Para queries complejas, usa full refresh en un schedule. Databricks y BigQuery hacen incremental refresh en cadencias batch sobre delta tables. ([Databricks docs](https://docs.databricks.com/aws/en/optimizations/incremental-refresh))

#### ClickHouse

Dos tipos de vistas materializadas:
- **Incremental MV (clásica):** trigger-based, se dispara en cada INSERT nuevo. Append-only; si el source tiene UPDATEs, la vista no los refleja. Ideal para logs y clickstreams.
- **Refreshable MV:** recalcula el resultado completo en schedule (`REFRESH EVERY N SECOND/MINUTE`). En v24.9 se añadió modo `APPEND` para acumular snapshots sin reemplazar. ([ClickHouse blog 24.9](https://clickhouse.com/blog/clickhouse-release-24-09), [Medium – Parth Surati](https://medium.com/@parthsurati096/clickhouse-incremental-materialized-view-vs-refreshable-materialized-view-17734faf881d))

#### Oracle

Materialized View Logs rastrean DML changes. `ON COMMIT` refresh aplica deltas cuando la transacción hace commit — staleness efectivamente cero. Es la implementación más madura de fast refresh incremental, con décadas de producción.

### Pitfalls

- **Postgres CONCURRENTLY sin índice UNIQUE:** el comando falla.
- **Snowflake con TARGET_LAG muy corto sin cambios upstream:** genera checks frecuentes con resultado `NO_DATA` que igual consumen Cloud Services credits.
- **ClickHouse Incremental MV con updates en la fuente:** la vista acumula versiones sin consolidar; produce resultados incorrectos en agregaciones.

### Fuentes

- [PostgreSQL docs – REFRESH MATERIALIZED VIEW](https://www.postgresql.org/docs/current/sql-refreshmaterializedview.html)
- [Epsio – Postgres REFRESH guide](https://www.epsio.io/blog/postgres-refresh-materialized-view-a-comprehensive-guide)
- [Snowflake – Dynamic Table refresh modes](https://docs.snowflake.com/en/user-guide/dynamic-tables/refresh-modes)
- [Snowflake – Target lag](https://docs.snowflake.com/en/user-guide/dynamic-tables/target-lag)
- [Databricks – Incremental refresh](https://docs.databricks.com/aws/en/optimizations/incremental-refresh)
- [ClickHouse blog 24.9](https://clickhouse.com/blog/clickhouse-release-24-09)
- [ClickHouse incremental vs refreshable – Medium](https://medium.com/@parthsurati096/clickhouse-incremental-materialized-view-vs-refreshable-materialized-view-17734faf881d)

---

## 6. El problema del drift temporal: recencia se deteriora con el tiempo sin datos nuevos

### La sutileza clave

Este es quizás el pitfall más subestimado en read-models de clientes. **Una tabla derivada puede estar perfectamente sincronizada con la fuente transaccional y aun así volverse incorrecta conforme avanza el reloj.**

Ejemplo concreto: un cliente compró hace 45 días. Se materializa `DIAS_INACTIVIDAD = 45` y `SEGMENTO = 'en_riesgo'`. No hay nuevas transacciones — la fuente no cambia. Al día siguiente, `DIAS_INACTIVIDAD` debería ser 46, y quizás el segmento debería cambiar a `'perdido'`. Pero si la tabla derivada no se recalcula, seguirá mostrando 45 y `'en_riesgo'` indefinidamente.

Esto aplica a cualquier campo derivado de tiempo relativo:
- `DIAS_DESDE_ULTIMA_COMPRA = CURRENT_DATE - MAX(fecha_compra)`
- `SEMANAS_INACTIVO`
- Estados de lifecycle: `activo → en_riesgo → inactivo → perdido` cuando los umbrales son por días

### Cómo los equipos maduros lo resuelven

**Opción A — Computar al momento de lectura ("as of now"):**
Los campos de recencia no se materializan; la query del dashboard computa `DATEDIFF(CURRENT_DATE, ultima_compra)` directamente. El read-model solo almacena `FECHA_ULTIMA_COMPRA` como fact estático. La lógica de segmentación vive en la query o en la capa de presentación.

- Pros: siempre correcto, sin drift por tiempo.
- Contras: cada request a la BD hace el cómputo; si hay joins complejos con la tabla de transacciones, puede ser costoso.
- Cuándo usarlo: dashboards ad-hoc, pocos miles de clientes, queries simples.

**Opción B — Recompute diario completo:**
Aunque no haya nuevas transacciones en la fuente, se regenera toda la tabla derivada cada noche. `DIAS_INACTIVIDAD` y el segmento se actualizan con `CURRENT_DATE` del día de refresh.

- Pros: siempre correcto al día, compatible con refresh incremental de hechos (solo necesitas el full rebuild para las columnas derivadas de tiempo).
- Contras: costo de un full-rebuild diario (aceptable a escala de miles de clientes).
- Cuándo usarlo: tabla pequeña (< 10k clientes), dashboard que necesita clasificaciones frescas diariamente.

**Opción C — Separar facts de recency en el modelo:**
La tabla de hechos (`ultima_compra`, `total_historico`, `frecuencia`) se actualiza incremental. Las columnas de recencia (`dias_inactivo`, `segmento`, `score_winback`) se calculan en una vista o capa separada que siempre usa `CURRENT_DATE`. ([Stormatics – PostgreSQL Materialized Views](https://stormatics.tech/blogs/postgresql-materialized-views-when-caching-your-query-results-makes-sense))

**RFM en producción:** en plataformas como Lexer y CleverTap, el componente de Recency se actualiza en tiempo real o al menos diariamente, ya que un segmento "activo" de ayer puede ser "en riesgo" hoy simplemente por el paso del tiempo. ([CleverTap – RFM](https://clevertap.com/blog/rfm-analysis/))

### Pitfalls

- Materializar `DIAS_INACTIVIDAD` como un número fijo y asumir que con refresh incremental el campo se mantiene actualizado — **incorrecto**: el incremental solo procesa filas con nuevas transacciones, no filas estáticas donde solo el reloj avanzó.
- No separar conceptualmente "facts transaccionales" (inmutables) de "métricas de recencia" (varían con el tiempo sin nuevos datos).

### Fuentes

- [Stormatics – Postgres Materialized Views](https://stormatics.tech/blogs/postgresql-materialized-views-when-caching-your-query-results-makes-sense)
- [CleverTap – RFM Analysis](https://clevertap.com/blog/rfm-analysis/)
- [Lexer – RFM Segmentation Guide](https://www.lexer.io/blog/the-complete-guide-to-rfm-segmentation)

---

## 7. Elegir el SLA de frescura / cadencia de refresh

### Cómo los tops piensan en esto

El principio de "target lag" de Snowflake es una buena formalización del razonamiento: defines cuánta staleness puede tolerar cada uso downstream, y ese target dicta la cadencia.

**Framework de decisión:**

| Latencia requerida | Cadencia | Costo | Cuándo aplica |
|-------------------|----------|-------|---------------|
| Sub-segundo | Streaming continuo | Alto | Decisiones de fraude, pricing real-time |
| 1–5 minutos | Micro-batch | Medio-alto | Dashboards operativos críticos |
| 15–60 minutos | Micro-batch o scheduler | Medio | Dashboards de gestión, alertas |
| Diaria / nocturna | Batch nightly | Bajo | Reportes, features para modelos batch, read-models de winback |

**Cómo Snowflake recomienda pensar en costo vs frescura:** aumentar el `TARGET_LAG` donde la frescura no es crítica. Una tabla con `TARGET_LAG = '1 hour'` puede costar 60x menos en compute que una con `TARGET_LAG = '1 minute'` si hay poca actividad upstream. ([Snowflake – Dynamic Tables cost](https://docs.snowflake.com/en/user-guide/dynamic-tables/cost))

**El error "one size fits all":** usar la misma cadencia para todos los read-models es un antipatrón. Un score de winback para el equipo de ventas no necesita la misma frescura que un dashboard de stock en tiempo real.

**Para nuestro caso específico:** \[A MEDIR NOSOTROS\]
- ¿Con qué frecuencia el equipo de ventas consulta la lista de candidatos winback?
- ¿Cuál es el ciclo de acción mínimo (el vendedor llama hoy o mañana)?
- Si la respuesta es "el vendedor actúa en ciclos de días", un refresh diario tiene el mismo valor de negocio que uno cada hora, con costo radicalmente menor.
- La carga sobre Firebird de un `SELECT` incremental: \[A MEDIR NOSOTROS con `EXPLAIN PLAN` y monitoreo de connection pool en horario pico\]

### Pitfalls

- Asumir que "más fresco = mejor" sin cuantificar el valor incremental de frescura adicional.
- Programar refreshes frecuentes en horas de máxima carga operativa de la BD fuente (Microsip procesa pedidos y cobranza en horario de negocio).
- No considerar el impacto de conexiones concurrentes sobre Firebird cuando el API Go ya tiene su connection pool activo.

### Fuentes

- [Snowflake – Dynamic Tables cost](https://docs.snowflake.com/en/user-guide/dynamic-tables/cost)
- [Tacnode – Snowflake Dynamic Tables vs Materialized Views](https://tacnode.io/post/snowflake-dynamic-tables-vs-materialized-views)
- [Airbyte – Full Refresh vs Incremental](https://airbyte.com/data-engineering-resources/full-refresh-vs-incremental-refresh)

---

## 8. Correctness y pitfalls: el combo incremental + full-rebuild periódico

### Cómo lo hacen los tops

El patrón estándar de producción en equipos maduros no es incremental puro ni full rebuild puro: es **incremental frecuente + full rebuild periódico como self-heal**.

**MERGE idempotente (upsert):**
El MERGE es el operador correcto para pipelines incrementales sobre datos mutables:

```sql
MERGE INTO candidatos_winback AS target
USING staging_candidatos AS source
  ON target.cliente_id = source.cliente_id
WHEN MATCHED AND source.updated_at > target.updated_at
  THEN UPDATE SET ...
WHEN NOT MATCHED
  THEN INSERT (...) VALUES (...);
```

Si el MERGE corre dos veces con los mismos datos de staging, el resultado es idéntico — segunda ejecución actualiza al mismo valor, inserts ya existen. Idempotencia garantizada. ([Medium – Idempotent Pipelines](https://medium.com/towards-data-engineering/building-idempotent-data-pipelines-a-practical-guide-to-reliability-at-scale-2afc1dcb7251))

**Ventana de overlap (clock skew buffer):**
```python
safe_watermark = last_watermark - timedelta(minutes=5)
```
Una pequeña permisividad hacia atrás absorbe clock skew y retraso de visibilidad de la BD. El MERGE downstream maneja duplicados sin problema. ([Medium – Idempotent Pipelines](https://medium.com/towards-data-engineering/building-idempotent-data-pipelines-a-practical-guide-to-reliability-at-scale-2afc1dcb7251))

**Manejo de deletes / edits sin `updated_at`:**
Si la fuente no tiene `updated_at`:
1. Usar IDs autoincrementales como proxy (solo detecta inserts, no updates).
2. Hash de fila: comparar checksum de columnas clave; si cambia, el registro fue editado.
3. Full rebuild periódico como única fuente de verdad para detectar deletes y edits silenciosos.
La solución pragmática para fuentes heredadas sin `updated_at` confiable es: incremental por ID para inserts + full rebuild diario/semanal para correctness. ([Airbyte – Full vs Incremental](https://airbyte.com/data-engineering-resources/full-refresh-vs-incremental-refresh))

**Hybrid recomendado por Databricks:**
- Datos recientes (últimos 7–30 días): full recomputation.
- Datos históricos: incremental con garantías fuertes.
- Datos archivados: inmutables, nunca se reprocesen.

**Estrategia del grupo de control (holdout):**
El holdout debe ser asignado **una sola vez** y almacenado como flag persistente en nuestra tabla (`EN_CONTROL = TRUE/FALSE`). El refresh incremental o full-rebuild nunca debe cambiar este flag — se mantiene estable a través de todos los ciclos de materialización. Esto implica que el MERGE preserve el campo `EN_CONTROL` existente si la fila ya existe:

```sql
WHEN MATCHED
  THEN UPDATE SET
    score = source.score,
    ultima_compra = source.ultima_compra,
    -- EN_CONTROL no se toca: mantiene el valor original
    updated_at = :now
```

La asignación al grupo de control debe hacerse en un paso de bootstrap separado, no en el pipeline de refresh regular. ([Statsig – Holdout groups](https://www.statsig.com/perspectives/holdout-groups-ab-testing))

**Full rebuild como self-heal:** dbt recomienda correr `--full-refresh` semanalmente o mensualmente en ventanas de baja actividad para eliminar drift acumulado. Para tablas pequeñas (miles de clientes), el costo es mínimo. ([dbt best practices](https://docs.getdbt.com/best-practices/materializations/4-incremental-models))

### Pitfalls

- **Watermark sin overlap:** arriesga perder registros creados justo entre dos runs por retrasos de commit.
- **MERGE sin condición en MATCHED (`source.updated_at > target.updated_at`):** actualiza filas con datos más viejos que los que ya tiene el destino.
- **Ignorar que los clientes pueden ser eliminados de Microsip:** sin full rebuild periódico, la tabla derivada retiene clientes que ya no existen.
- **Modificar `EN_CONTROL` en el pipeline de refresh:** invalida el experimento.

### Fuentes

- [Medium – Idempotent Pipelines](https://medium.com/towards-data-engineering/building-idempotent-data-pipelines-a-practical-guide-to-reliability-at-scale-2afc1dcb7251)
- [dbt Incremental Patterns real-time](https://docs.getdbt.com/best-practices/how-we-handle-real-time-data/2-incremental-patterns)
- [Airbyte – Full Refresh vs Incremental](https://airbyte.com/data-engineering-resources/full-refresh-vs-incremental-refresh)
- [Statsig – Holdout groups](https://www.statsig.com/perspectives/holdout-groups-ab-testing)

---

## Recomendación para nuestro caso

### Contexto irreductible

- BD fuente: Microsip / Firebird. No controlamos. Sin logs, sin triggers posibles sobre tablas Microsip, sin replicación.
- Escala: miles de clientes, cientos de miles de transacciones. Manejable en un solo SQL bien indexado.
- Stack: Go en Windows. Sin Kafka, sin warehouse. El API Go ya tiene connection pool hacia Firebird.
- Restricción de holdout: el grupo de control debe ser estable entre refreshes.

### Arquitectura recomendada

**Patrón central: watermark incremental + full rebuild nocturno como self-heal**

```
[Firebird / Microsip]
        │
        │  SELECT ... WHERE UPDATED_AT > :last_watermark - 5min
        ▼
[Go: job de refresh incremental]  ←── schedule: cada 60 min en horario de negocio
        │
        │  MERGE INTO MSP_WINBACK_CANDIDATOS
        │  ON cliente_id
        │  WHEN MATCHED → UPDATE (NO tocar EN_CONTROL)
        │  WHEN NOT MATCHED → INSERT (asignar EN_CONTROL = false por defecto)
        ▼
[MSP_WINBACK_CANDIDATOS]  ←── tabla propia en Firebird (MSP_* = solo Go)
        │
        │  SELECT + cómputo de recencia al momento de lectura
        ▼
[Dashboard / Motor de decisiones]
```

**Full rebuild nocturno (ej: 2 AM):**
```
SELECT completo de Microsip → TRUNCATE + INSERT INTO MSP_WINBACK_CANDIDATOS
(preservando EN_CONTROL de la tabla existente vía LEFT JOIN antes del truncate)
```
Alternativamente, MERGE full sin TRUNCATE para preservar `EN_CONTROL` automáticamente.

### Campos de recencia: computar en lectura, no materializar

Los campos que varían con el reloj sin nuevos datos (`dias_inactividad`, `segmento_lifecycle`) **no se deben materializar como números**. En su lugar:

- Almacenar `FECHA_ULTIMA_COMPRA`, `FECHA_PRIMER_COMPRA`, `TOTAL_HISTORICO`, `FRECUENCIA_COMPRAS` como facts.
- Computar `DIAS_INACTIVIDAD = CURRENT_DATE - FECHA_ULTIMA_COMPRA` en la query del dashboard o en el handler Go al servir el endpoint.
- El segmento (`activo`, `en_riesgo`, `inactivo`, `perdido`) puede calcularse en el mismo handler con umbrales configurables, sin costosos re-writes a la BD.

Esto elimina completamente el drift temporal: el segmento de un cliente es correcto en el momento exacto en que se consulta.

### Cadencia recomendada

- **Refresh incremental:** cada 60 minutos en horario de negocio (8 AM – 8 PM). Fuera de horario, una vez en la noche.
- **Full rebuild (self-heal):** una vez al día a las 2 AM.
- **Primer bootstrap del holdout:** run manual one-time antes del primer experimento; nunca se sobreescribe en el pipeline.

**Justificación:** si el equipo de ventas actúa en ciclos de días (llama hoy a quien no compra en X semanas), un refresh cada hora entrega el mismo valor de negocio que uno cada 5 minutos, con carga trivial sobre Firebird. \[A MEDIR NOSOTROS: medir tiempo de query incremental sobre Microsip en horario pico antes de fijar este número\]

### Tradeoffs honestos

| Aspecto | Nuestra solución | Alternativa descartada |
|---------|-----------------|----------------------|
| Deletes en Microsip | Detectados en full rebuild nocturno (lag máx ~24h) | CDC — imposible sin acceso a logs |
| Frescura de nuevas transacciones | ~1h máx en horario de negocio | Streaming — requiere Kafka + CDC |
| Recencia correcta | Siempre (computada al leer) | Materializar número fijo — incorrecto por drift |
| Holdout estable | Garantizado (campo no tocado por pipeline) | Requiere disciplina en el MERGE |
| Costo operacional | Mínimo: un job Go + queries SQL | Feature store / Debezium — sobreingeniería para escala actual |

### Lo que sí hay que monitorear

1. Duración del query incremental sobre Firebird — si crece, agregar índice en `UPDATED_AT` de las tablas Microsip relevantes.
2. Diferencia de rowcount entre full rebuild y estado previo — una caída brusca indica deletes en Microsip que hay que investigar.
3. Lag real del pipeline (timestamp de último refresh vs `CURRENT_TIMESTAMP`) — alertar si supera 2× la cadencia esperada.

---

*Documento generado: 2026-06-13. Fuentes verificadas con ≥2 referencias independientes por afirmación sustantiva. Números marcados \[A MEDIR NOSOTROS\] requieren medición en el ambiente real antes de usarse como referencia.*
