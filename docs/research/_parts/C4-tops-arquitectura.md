# C4 — Tops & Arquitectura de Analytics para Retail

> Investigacion de estado del arte sobre como los grandes del retail estructuran sus capas analiticas, que patrones aplican universalmente, y como traducirlos a nuestra escala real: miles de clientes, cientos de miles de transacciones, Go sobre Firebird, Windows Server, sin cloud ni Docker en produccion.
>
> **Nota de escala:** Walmart procesa 2.5 petabytes por hora y tiene 250 millones de clientes semanales. Coppel opera ~1,980 sucursales. Target tiene 2,000+ tiendas. Nosotros tenemos una sola sucursal con rutas definidas. Esta diferencia de escala es el filtro principal para decidir que adoptar y que descartar.

---

## 1. Como estructuran analytics los grandes del retail

### Patron / como lo hacen

**Walmart — Retail Link + Data Cafe**

Walmart construyo dos capas diferenciadas: Retail Link (portal de acceso para proveedores, orientado a sell-through y reabastecimiento) y el Data Cafe (*Collaborative Analytics Facilities for Enterprise*), un hub de analytics interno en Bentonville. El Data Cafe conecta mas de 200 fuentes de datos: datos transaccionales propios (hasta 40 petabytes de historial reciente), datos meteorologicos, precios de gasolina, datos de Nielsen, señales de redes sociales, y bases de eventos locales. El resultado es que una consulta que antes tomaba dos o tres semanas hoy tarda 20-30 minutos. La arquitectura combina analytics proactivo (modelos predictivos) y reactivo (respuesta a eventos), todo unificado desde la misma capa de datos. [[1]](https://www.retailitinsights.com/doc/walmart-turns-to-data-caf-analytics-hub-to-make-sense-of-data-0001) [[2]](https://bernardmarr.com/walmart-big-data-analytics-at-the-worlds-biggest-retailer/)

**Target — Guest ID + CORE**

Target asigna a cada comprador un Guest ID vinculado a su tarjeta de credito, nombre o correo. Eso construye una base de datos de comportamiento de compra y demograficos que alimenta modelos predictivos. Encima de esos perfiles, Target construyo CORE (*Contextual Offer Recommendation Engine*): un modelo de multi-armed bandit contextual alimentado por historial de transacciones, interacciones con promociones y comportamiento de navegacion. El modelo usa factorizacion de matrices (Non-Negative Matrix Factorization) para extraer features latentes de la interaccion cliente-oferta, y una red neuronal que combina esos features para recomendar. En 2023 CORE sirvio millones de ofertas personalizadas. [[3]](https://tech.target.com/blog/contextual-offer-recommendation-engine)

**Mercado Libre — BigQuery + Looker**

Mercado Libre procesa cientos de terabytes diarios y ejecuta cientos de miles de consultas diarias usando BigQuery como capa de computo analitico y Looker como capa de BI/visualizacion. Su filosofia: "los datos deben ser oportunos, creibles y disponibles para analisis." Ingestan datos de streaming, trafico web (Google Analytics, App Annie), logs de almacen y red, APIs internas y sistemas de gestion. Los equipos de negocio, transporte y operaciones reciben datos en tiempo real integrados con Slack, email y Google Sheets. [[4]](https://dev.to/cloudnomics/mercado-libre-goes-bigquery-moneyball-goes-cloud-plus-plugging-a-300b-retail-search-hole-ecb)

**Shopify — analitica para comerciantes (modelo SaaS embebido)**

Shopify rediseno completamente su suite de analytics en el Summer '24 Edition: dashboards en tiempo real, cohort reports de retencion, y un asistente conversacional (Sidekick) que responde preguntas como "cual fue mi producto mas vendido la semana pasada." El dato clave de arquitectura: Shopify usa un modelo de datos unificado para todos los canales de venta, lo que reduce el TCO hasta 37% segun ellos mismos. La metrica de negocio central es la misma independientemente del canal (web, POS, app). [[5]](https://badger.blue/blogs/ecommerce-unpacked/shopify-analytics-2024-update) [[6]](https://www.shopify.com/enterprise/blog/sales-analytics-guide)

**Coppel / Elektra — el modelo de credito al consumo como dato**

Coppel (1,980 sucursales) y Elektra (1,277 sucursales) son esencialmente financieras disfrazadas de mueblerias. Su ventaja competitiva no es el producto sino el historial crediticio: cada pago semanal es un dato que calibra el riesgo del cliente y permite ampliar o contraer el credito. Ambas empresas estan en proceso de transformacion hacia ecosistemas omnicanal (Elektra con la superapp Baz, 12 millones de usuarios). No hay documentacion tecnica publica de su arquitectura de datos, pero el patron es claro: el dato de pago es el nucleo, y la analitica de cliente gira alrededor del comportamiento de pago, no del producto. [[7]](https://www.espressomatutino.com/p/2025-12-18) [[8]](https://americasmi.com/insights/digitization-mexico-retailers-finance/)

**Patron comun: tres capas**

En todos los casos, independientemente de la escala, aparece la misma estructura de tres capas:

1. **Capa de datos operacionales** (OLTP): la base transaccional (POS, ERP, CRM) — optimizada para escrituras concurrentes, registros individuales.
2. **Capa analitica derivada** (OLAP o modelo dimensional): tablas pre-calculadas o un data warehouse separado — optimizado para lecturas agregadas, scans de columnas, joins de millones de filas.
3. **Capa semantica / de metricas**: definiciones de negocio (que cuenta como "venta cerrada", que es un "cliente activo") centralizadas y reutilizadas por todos los dashboards y consumidores.

### Como aterrizarlo a nuestra escala

Los tres patrones aplican. La diferencia es el tamano de cada capa:

- **Capa 1 (OLTP):** ya existe — es Firebird con las tablas de Microsip + nuestras tablas `MSP_*`.
- **Capa 2 (OLAP):** no necesitamos BigQuery ni Redshift. Necesitamos tablas de resumen (`MSP_ANALYTICS_*`) refrescadas por un job de Go en batch. Ver seccion 4.
- **Capa 3 (semantica):** no necesitamos dbt ni Cube. Necesitamos que las definiciones de metricas vivan como constantes/queries nombradas en Go, no dispersas en SQL ad-hoc por cada handler. Ver seccion 3.

El patron de Coppel/Elektra es el mas relevante: el dato del pago/credito es el nucleo, y la analitica de cliente gira alrededor del comportamiento de pago. Es exactamente nuestro caso.

### Pitfalls / overkill

- **OVERKILL:** Multi-source data ingestion (meteorologia, Nielsen, redes sociales). Para una sola sucursal con rutas geograficamente acotadas, los datos externos agregan ruido, no senal.
- **OVERKILL:** Real-time streaming analytics (Kafka, Flink). Nuestras ventas no ocurren a 1M eventos/segundo. Un refresh batch cada 30-60 minutos es suficiente para cualquier decision operativa.
- **OVERKILL:** Recommendation engine con matrix factorization (estilo Target CORE). Con miles de clientes y un catalogo de cientos de productos, la segmentacion manual por zona/ruta y comportamiento de pago supera en precision a cualquier ML con tan pocos datos. [A MEDIR NOSOTROS: cuantos clientes activos y cuantos SKUs distintos vendemos — si son <5,000 clientes y <500 SKUs, cualquier modelo de ML es overfitting puro.]
- **Valido:** Guest ID / perfil unificado por cliente. Asignar un ID persistente y acumular historial de transacciones, pagos y comportamiento de cobro ES el fundamento. Ya lo tenemos en Firebird.

### Fuentes

- [Walmart Data Cafe — Retail IT Insights](https://www.retailitinsights.com/doc/walmart-turns-to-data-caf-analytics-hub-to-make-sense-of-data-0001)
- [Walmart Big Data — Bernard Marr](https://bernardmarr.com/walmart-big-data-analytics-at-the-worlds-biggest-retailer/)
- [Target CORE — tech.target.com](https://tech.target.com/blog/contextual-offer-recommendation-engine)
- [Mercado Libre + BigQuery — dev.to/cloudnomics](https://dev.to/cloudnomics/mercado-libre-goes-bigquery-moneyball-goes-cloud-plus-plugging-a-300b-retail-search-hole-ecb)
- [Shopify Analytics 2024 — badger.blue](https://badger.blue/blogs/ecommerce-unpacked/shopify-analytics-2024-update)
- [Coppel vs Elektra — Espresso Matutino](https://www.espressomatutino.com/p/2025-12-18)
- [Retail y finanzas en Mexico — Americas Market Intelligence](https://americasmi.com/insights/digitization-mexico-retailers-finance/)

---

## 2. Modelado dimensional: star schema, grain, hechos y dimensiones

### Patron / como lo hacen

**El problema que resuelve el modelo dimensional**

Las bases de datos OLTP (como Firebird con Microsip) estan normalizadas para minimizar redundancia y optimizar escrituras concurrentes. Una venta en Microsip involucra joins entre `DOCTOS_VE`, `DOCTOS_VE_DET`, `CLIENTES`, `INVENTARIOS`, `VENDEDORES`, y posiblemente otras tablas. Una consulta analitica — "cuanto vendimos por ruta en los ultimos 90 dias, desglosado por categoria de producto" — requiere un join de 5+ tablas sobre cientos de miles de filas. En OLTP, esto es lento y compite con las escrituras del dia. En OLAP, es la operacion basica.

El modelado dimensional (Kimball, 1996) resuelve esto con dos tipos de tablas: [[9]](https://www.holistics.io/books/setup-analytics/kimball-s-dimensional-data-modeling/) [[10]](https://www.owox.com/blog/articles/star-schema-explained)

- **Fact table (tabla de hechos):** registra medidas cuantificables de un evento de negocio. Columnas: claves foraneas a dimensiones + medidas numericas (importe, cantidad, descuento).
- **Dimension tables (tablas de dimension):** contexto descriptivo del hecho. Columnas: atributos como nombre del cliente, categoria del producto, dia de la semana, nombre del vendedor.

**El concepto de grain**

El grain es el nivel mas atomico de detalle que almacena la fact table. La regla de Kimball: elegir el grain mas atomico posible. Para ventas en Microsip, el grain correcto es la **linea de detalle de la venta** (un renglón de `DOCTOS_VE_DET`), no el encabezado del documento. Esto permite responder tanto "cuanto vendio el vendedor X" como "cuantas unidades del producto Y se vendieron en la ruta Z".

**Ejemplo concreto: Sales Fact para nuestra muebleria**

```
FACT_VENTAS (grain: una linea de detalle de venta)
  -- Claves foraneas
  fecha_id          → DIM_FECHA
  cliente_id        → DIM_CLIENTE
  producto_id       → DIM_PRODUCTO
  vendedor_id       → DIM_VENDEDOR
  ruta_id           → DIM_RUTA
  -- Medidas
  importe_bruto     NUMERIC(18,2)   -- precio * cantidad
  descuento         NUMERIC(18,2)
  importe_neto      NUMERIC(18,2)   -- importe_bruto - descuento
  cantidad          INTEGER
  costo_unitario    NUMERIC(18,2)   -- para margen
  es_credito        SMALLINT        -- 0/1, bandera para separar contado vs credito

DIM_FECHA
  fecha_id, fecha, anio, mes, semana_del_anio, dia_semana,
  es_fin_de_semana, nombre_mes, trimestre

DIM_CLIENTE
  cliente_id, nombre, zona, colonia, municipio,
  tipo_cliente (nuevo/recurrente), fecha_primer_compra,
  limite_credito_vigente, estatus_cartera (al_dia/vencido/...)

DIM_PRODUCTO
  producto_id, descripcion, categoria, subcategoria, proveedor,
  costo_promedio, precio_lista

DIM_VENDEDOR
  vendedor_id, nombre, tipo (propio/comisionista)

DIM_RUTA
  ruta_id, nombre_ruta, cobrador_id, zona_geografica
```

**OLTP vs OLAP: la diferencia clave**

| Dimension       | OLTP (Firebird/Microsip)        | OLAP (modelo dimensional)          |
|-----------------|----------------------------------|------------------------------------|
| Optimizado para | Escrituras concurrentes rapidas  | Lecturas agregadas sobre millones  |
| Esquema         | Normalizado (muchos joins)       | Desnormalizado (star schema)       |
| Actualizacion   | En tiempo real, por transaccion  | Batch (cada N minutos/horas)       |
| Consulta tipica | "dame el documento #12345"       | "ventas por ruta, mes, categoria"  |
| Indices         | Por PK y FK                      | Por columnas de filtro/agrupacion  |

La observacion central de Kimball: "haz el trabajo dificil ahora para que sea facil de consultar despues." El modelo dimensional paga el costo en el momento de la ingesta/refresh, no en el momento de la consulta. [[9]](https://www.holistics.io/books/setup-analytics/kimball-s-dimensional-data-modeling/)

### Como aterrizarlo a nuestra escala

No necesitamos un data warehouse separado. El modelo dimensional puede vivir como tablas regulares en Firebird, con un prefijo `MSP_AN_` (analytics) para distinguirlas de las tablas operacionales:

- `MSP_AN_FACT_VENTAS` — grain: linea de detalle
- `MSP_AN_FACT_COBROS` — grain: un pago recibido
- `MSP_AN_DIM_FECHA` — estatica, generada una vez
- `MSP_AN_DIM_CLIENTE` — se actualiza cuando cambia un cliente
- `MSP_AN_DIM_PRODUCTO`, `MSP_AN_DIM_RUTA` — idem

El job de Go que refresca estas tablas lee de Microsip/MSP, transforma al modelo dimensional, y hace upserts. Las consultas analiticas van contra estas tablas, no contra las tablas operacionales. Separacion limpia, sin riesgo de contension.

**Dimension de fecha:** generar una vez, de 2015 a 2035. Una tabla de ~7,300 filas. Permite queries como `WHERE d.nombre_mes = 'Enero' AND d.anio = 2025` sin funciones SQL que rompan indices.

### Pitfalls / overkill

- **OVERKILL:** Snowflake schema (normalizar las dimensiones). Solo agrega joins sin beneficio para nuestra escala.
- **OVERKILL:** Slowly Changing Dimensions tipo 2 (historial de cambios en dimensiones). Para nosotros, si un cliente cambia de zona, nos interesa la zona actual, no la historia completa. SCD tipo 1 (sobreescribir) es suficiente.
- **OVERKILL:** Fact tables de eventos granulares para navegacion web, clicks, etc. Nuestros eventos relevantes son ventas y cobros — granulares si, pero acotados.
- **VALIDO:** Conformed dimensions (dimensiones compartidas entre facts). `DIM_CLIENTE` usada tanto por `FACT_VENTAS` como por `FACT_COBROS` permite cruzar "el cliente X compro en enero y pago en febrero" sin joins complicados.
- **CUIDADO [A MEDIR NOSOTROS]:** El volumen de `FACT_VENTAS` depende de cuantas lineas de detalle tenemos en historial. Si son <500,000 filas, incluso una query ad-hoc sobre las tablas normalizadas de Microsip puede ser aceptable. La justificacion de un modelo dimensional aumenta con el volumen y la frecuencia de las consultas.

### Fuentes

- [Kimball Dimensional Modeling — holistics.io](https://www.holistics.io/books/setup-analytics/kimball-s-dimensional-data-modeling/)
- [Star Schema explicado — OWOX](https://www.owox.com/blog/articles/star-schema-explained)
- [Kimball para principiantes — Medium/QuarkAndCode](https://medium.com/@QuarkAndCode/kimball-data-modeling-explained-star-schema-for-beginners-5298c31943bb)
- [OLTP vs OLAP — AWS](https://aws.amazon.com/compare/the-difference-between-olap-and-oltp/)
- [dbt + Kimball — dbt Developer Blog](https://docs.getdbt.com/blog/kimball-dimensional-model)

---

## 3. La capa semantica / de metricas: el gran giro de los 2020s

### Patron / como lo hacen

**El problema que resuelve**

En cualquier equipo con mas de un dashboard, la misma metrica se define de formas distintas en distintos lugares. "Ventas del mes" puede significar `SUM(importe_bruto)` en un reporte, `SUM(importe_neto)` en otro, y `SUM(importe_neto WHERE es_credito=0)` en un tercero. Cuando el gerente ve tres numeros distintos en tres reportes distintos, deja de confiar en los datos. La capa semantica resuelve esto definiendo cada metrica una sola vez, en un lugar neutral, y sirviendo esa definicion a todos los consumidores. [[11]](https://airbyte.com/blog/the-rise-of-the-semantic-layer-metrics-on-the-fly) [[12]](https://cube.dev/articles/best-semantic-layer-for-ai-and-bi-2026)

**Tres arquitecturas dominantes en 2025-2026**

1. **Warehouse-native** (Snowflake Semantic Views, Databricks Metric Views): la logica semantica vive como objetos de base de datos. Conveniente dentro de una plataforma; limitante si hay multiples consumidores o apps embebidas.

2. **Transformation-layer** (dbt MetricFlow, GA octubre 2024): las definiciones de metricas viven como YAML junto a los modelos dbt, con control de versiones en Git. Sin cache propio; delega la ejecucion al warehouse.

3. **Decoupled semantic layer** (Cube.dev, AtScale, GoodData): una capa independiente entre el warehouse y todos los consumidores — dashboards, apps embebidas, agentes de IA. Expone SQL, REST, GraphQL. Agrega cache y control de acceso propios. [[13]](https://www.typedef.ai/resources/semantic-layer-architectures-explained-warehouse-native-vs-dbt-vs-cube)

**Cuando NO necesitas una capa semantica de terceros**

Cube.dev lo dice explicitamente: equipos con un solo warehouse y un solo consumidor de BI, sin planes de embeber analytics ni de usar agentes de IA, pueden no necesitar una capa semantica separada todavia. La recomendacion es revisarlo cuando aparezca un segundo consumidor. [[12]](https://cube.dev/articles/best-semantic-layer-for-ai-and-bi-2026)

**"Nearly headless BI": el beneficio sin el stack completo**

David Jayatillake articulo el patron para equipos chicos: codificar las definiciones de negocio una sola vez como codigo (structs, YAML embebido, constantes SQL nombradas), hacer que todos los consumidores lean de esas definiciones, y dejar que el motor de base de datos ejecute. El beneficio — consistencia de metricas — se obtiene sin desplegar Cube ni conectarse a dbt Cloud. [[14]](https://davidsj.substack.com/p/nearly-headless-bi) [[11]](https://airbyte.com/blog/the-rise-of-the-semantic-layer-metrics-on-the-fly)

### Como aterrizarlo a nuestra escala

**La implementacion Go-native de una thin metrics layer:**

En lugar de dbt o Cube, creamos un paquete `internal/analytics/metrics/` con definiciones de metricas como constantes SQL nombradas o structs Go:

```go
// internal/analytics/metrics/ventas.go

// VentasNetas define la metrica canonica de ventas netas.
// Todos los handlers, exports y jobs usan esta query, no SQL ad-hoc.
const VentasNetas = `
    SELECT
        SUM(f.importe_neto)  AS ventas_netas,
        SUM(f.importe_bruto) AS ventas_brutas,
        COUNT(DISTINCT f.cliente_id) AS clientes_unicos,
        SUM(f.cantidad)      AS unidades
    FROM MSP_AN_FACT_VENTAS f
    WHERE f.fecha_id BETWEEN :desde AND :hasta
`

// VentasNetasPorRuta extiende la metrica base con desglose.
const VentasNetasPorRuta = VentasNetas + `
    JOIN MSP_AN_DIM_RUTA r ON r.ruta_id = f.ruta_id
    GROUP BY r.ruta_id, r.nombre_ruta
`
```

Reglas para que funcione:
1. **Ningun handler construye SQL de metricas directamente.** Todo SQL analitico viene del paquete `metrics/`.
2. **Si la definicion cambia** (ej. "las notas de credito no cuentan como venta"), se cambia en un solo lugar y todos los reportes se actualizan automaticamente en el siguiente refresh.
3. **Las metricas tienen tests.** Un test de integracion verifica que `VentasNetas` retorna el valor correcto contra datos de fixture — exactamente igual que testear un endpoint HTTP.

Esto es "semantica como codigo" sin ninguna herramienta externa. El costo es disciplina de equipo: nadie escribe SQL analitico ad-hoc en los handlers.

**Analogia con el problema original de la capa semantica:** en lugar de tener tres dashboards con tres queries distintas para "ventas netas", tenemos una constante `metrics.VentasNetas` que los tres handlers usan. El numero siempre coincide.

### Pitfalls / overkill

- **OVERKILL:** dbt Semantic Layer / MetricFlow. Requiere dbt Cloud, una arquitectura de transformacion separada, y un equipo de data engineers. No aplica para un sistema Go monolitico sobre Firebird.
- **OVERKILL:** Cube.dev. Es una pieza de infraestructura adicional (servidor, cache Redis, config YAML compleja) que resuelve el problema de multi-tenant y multi-warehouse. Nosotros tenemos un tenant y un warehouse.
- **OVERKILL:** LookML / Looker. Es una herramienta de BI completa con su propio lenguaje de modelado. Para nuestro caso, un dashboard custom en React/Svelte consume directamente nuestra API Go.
- **VALIDO:** El patron "metrics as code" descrito arriba. Zero dependencias externas, 100% compatible con Go + Firebird, y resuelve exactamente el problema de inconsistencia de metricas.
- **CUIDADO [A MEDIR NOSOTROS]:** La necesidad de una capa semantica formal crece con el numero de consumidores. Si solo hay un dashboard interno y un report Excel exportado, una o dos constantes SQL nombradas son suficientes. Si hay un dashboard de gerencia, un reporte para el dueno, una app movil del cobrador y un export para contabilidad, la disciplina del paquete `metrics/` se vuelve critica.

### Fuentes

- [Rise of the Semantic Layer — Airbyte](https://airbyte.com/blog/the-rise-of-the-semantic-layer-metrics-on-the-fly)
- [Best Semantic Layer 2026 — Cube.dev](https://cube.dev/articles/best-semantic-layer-for-ai-and-bi-2026)
- [Arquitecturas de Semantic Layer — typedef.ai](https://www.typedef.ai/resources/semantic-layer-architectures-explained-warehouse-native-vs-dbt-vs-cube)
- [Nearly Headless BI — David Jayatillake / Substack](https://davidsj.substack.com/p/nearly-headless-bi)
- [dbt Semantic Layer vale la pena? — Medium](https://medium.com/@reliabledataengineering/the-dbt-semantic-layer-is-it-worth-migrating-from-your-metrics-store-e7aa4bec2e52)

---

## 4. Arquitectura para nuestras restricciones: Go, Firebird, Windows Server

### Patron / como lo hacen los que trabajan con stacks similares

**Batch rollups vs on-demand: el debate resuelto**

En sistemas OLTP donde el motor de base de datos es el cuello de botella, el consenso de la industria es claro: las consultas analiticas complejas no deben competir con las transacciones operacionales. El patron dominante para equipos sin data warehouse dedicado es: **tablas de resumen refrescadas en batch por un worker**, no queries on-demand contra las tablas normalizadas. [[15]](https://clickhouse.com/resources/engineering/real-time-analytics-postgres) [[16]](https://codelit.io/blog/database-materialized-view-refresh)

Los cuatro patrones de refresh documentados (de mas simple a mas complejo):

| Patron        | Latencia de datos | Impacto en escrituras | Complejidad |
|---------------|-------------------|----------------------|-------------|
| Lazy (cron)   | Minutos-horas     | Ninguno              | Baja        |
| Incremental   | Segundos-minutos  | Bajo                 | Alta        |
| Eager (sync)  | Ninguna           | Alto (bloquea writes)| Baja        |
| Streaming     | Sub-segundo       | Medio                | Muy alta    |

Para analytics de negocio (dashboards, reportes, perfiles de cliente), **lazy refresh via cron** es el patron correcto. Los datos de "ventas de esta semana" no necesitan actualizarse mas rapido que cada 30-60 minutos. [[16]](https://codelit.io/blog/database-materialized-view-refresh)

**El problema especifico de Firebird: no tiene materialized views nativas**

A diferencia de PostgreSQL (que tiene `REFRESH MATERIALIZED VIEW`), Firebird no implementa materialized views como objeto de base de datos nativo. El feature request lleva abierto desde 2005 (CORE822). [[17]](https://github.com/FirebirdSQL/firebird/issues/1208)

Esto es una limitacion real, pero no bloqueante. El workaround estandar: **tablas regulares que actuan como materialized views**, refrescadas por un proceso externo (un job de Go). La tabla `MSP_AN_FACT_VENTAS` no es una view — es una tabla ordinaria. Un goroutine con ticker, o un endpoint `/admin/analytics/refresh` llamado por Windows Task Scheduler, ejecuta el `DELETE + INSERT` periodicamente.

**DuckDB como motor OLAP embebido: el caso**

DuckDB emergió como "el SQLite del OLAP" — un motor columnar embebido, sin servidor, que puede leer archivos Parquet, CSV, y conectarse a bases de datos externas. Para analytics complejos sobre datos ya extraidos, DuckDB puede dar una aceleracion de ~500x sobre queries equivalentes en bases de datos relacionales orientadas a filas. [[18]](https://motherduck.com/learn/select-olap-solution-postgres/) [[19]](https://medium.com/@connect.hashblock/materialized-views-in-duckdb-fast-analytics-without-warehouses-981189a26f05)

Sin embargo, DuckDB en produccion sobre Windows Server con Go tiene fricciones: el driver CGO para Go requiere CGO_ENABLED=1, que rompe la compilacion cruzada CGO_ENABLED=0 que usamos. Hasta que DuckDB tenga un driver Go puro sin CGO estable, **esta opcion es overkill para nosotros**.

**La arquitectura correcta para Go + Firebird + Windows Server**

```
[Firebird: tablas operacionales Microsip + MSP_*]
              |
              | (lectura batch, cada 30-60 min)
              v
[Go analytics worker: lee, transforma, hace upserts]
              |
              v
[Firebird: tablas MSP_AN_* (modelo dimensional)]
              |
    +---------+----------+
    |                    |
    v                    v
[API Go: endpoints    [API Go: endpoints
 /analytics/ventas     /clientes/:id/perfil
 /analytics/rutas      /clientes/:id/historial]
 /analytics/productos]
              |
              v
[Frontend: dashboard Go/React/Svelte via JSON]
```

**Componentes concretos:**

1. **`internal/analytics/worker/`** — goroutine o job externo que refresca las tablas `MSP_AN_*`. Logica: `DELETE FROM MSP_AN_FACT_VENTAS WHERE fecha_id >= :watermark` + `INSERT INTO MSP_AN_FACT_VENTAS SELECT ... FROM DOCTOS_VE_DET ...`. Guarda un watermark de ultimo refresh en `MSP_CFG` o en archivo de estado.

2. **`internal/analytics/repo/`** — repositorio Firebird que lee de `MSP_AN_*`. Implementa queries como `VentasPorRuta(desde, hasta time.Time) ([]RutaMetrica, error)`. No construye SQL en los handlers.

3. **`internal/analytics/metrics/`** — el paquete de definiciones de metricas descrito en la seccion 3. SQL nombrado, no ad-hoc.

4. **`internal/analytics/infra/http/`** — handlers HTTP que consumen el repo. Endpoints: `GET /analytics/ventas`, `GET /analytics/rutas`, `GET /analytics/productos`, `GET /clientes/:id/perfil-analitico`.

**Perfil de cliente desde la capa analitica**

La misma `FACT_VENTAS` + `FACT_COBROS` que sirven los dashboards agregados sirven los perfiles individuales de cliente:

```sql
-- Perfil analitico de un cliente especifico
SELECT
    COUNT(DISTINCT v.fecha_id)  AS dias_con_compra,
    SUM(v.importe_neto)         AS gasto_historico_total,
    AVG(v.importe_neto)         AS ticket_promedio,
    MAX(f.fecha)                AS ultima_compra,
    (SELECT SUM(c.monto) FROM MSP_AN_FACT_COBROS c
     WHERE c.cliente_id = :cid
       AND c.dias_mora > 0)     AS monto_pagado_con_mora
FROM MSP_AN_FACT_VENTAS v
JOIN MSP_AN_DIM_FECHA f ON f.fecha_id = v.fecha_id
WHERE v.cliente_id = :cid
```

Esto demuestra la ganancia del modelo dimensional: un query de perfil de cliente y un query de dashboard de ventas por ruta usan exactamente la misma fact table, con filtros distintos. Una sola capa materializada sirve ambos casos de uso.

**Estrategia de refresh incremental sin materialized views nativas**

```
MSP_AN_REFRESH_LOG (tabla de control)
  refresh_id   CHAR(36)
  tipo         VARCHAR(50)   -- 'fact_ventas', 'fact_cobros', etc.
  watermark    TIMESTAMP     -- hasta donde se proceso
  started_at   TIMESTAMP
  finished_at  TIMESTAMP
  filas_procesadas INTEGER
  estatus      VARCHAR(20)   -- 'ok', 'error'
```

El worker Go: al arrancar, lee el watermark de la ultima corrida exitosa y solo procesa registros nuevos desde ese punto. Si falla, el registro queda en estatus 'error' y la proxima corrida reintenta desde el watermark anterior. Logica simple, sin dependencias externas, compatible con Windows Task Scheduler o un goroutine con ticker.

### Como aterrizarlo a nuestra escala

Pasos en orden de implementacion:

1. **Generar `MSP_AN_DIM_FECHA`** una sola vez (migration). 7,300 filas para 20 anios.
2. **Crear `MSP_AN_DIM_CLIENTE`, `MSP_AN_DIM_PRODUCTO`, `MSP_AN_DIM_RUTA`** — refrescadas por el worker cuando hay cambios.
3. **Crear `MSP_AN_FACT_VENTAS`** y `MSP_AN_FACT_COBROS` — el nucleo del modelo.
4. **Implementar el worker de refresh** en `internal/analytics/worker/` con watermark y log.
5. **Implementar el repo analitico** en `internal/analytics/repo/` usando el paquete `metrics/`.
6. **Exponer endpoints HTTP** de dashboards y perfiles de cliente.

**[A MEDIR NOSOTROS]:**
- Cuantas filas tiene `DOCTOS_VE_DET` en historial — determina si el refresh inicial es un problema de rendimiento.
- Cuanto tarda un `SELECT COUNT(*) FROM DOCTOS_VE_DET` en Firebird — baseline de velocidad.
- Cuantos clientes distintos tienen mas de una compra — determina si los perfiles de cliente tienen suficiente historial para ser utiles.
- Frecuencia de consultas al dashboard — si es <10 veces al dia, incluso una query on-demand lenta es aceptable; si es continua, el refresh batch se justifica.

### Pitfalls / overkill

- **OVERKILL:** DuckDB embebido en Go (CGO requerido, rompe compilacion cruzada Windows).
- **OVERKILL:** ClickHouse o cualquier motor columnar externo. Agregan un servidor adicional en Windows, operacion compleja, sin beneficio claro para nuestra escala.
- **OVERKILL:** Streaming analytics (Kafka + Flink). Las ventas de una muebleria no requieren latencia sub-segundo. El gerente no necesita ver el dashboard actualizado en tiempo real mientras el vendedor esta llenando el contrato.
- **OVERKILL:** ETL con Airflow o herramientas de orquestacion. Un goroutine de Go con un ticker de 30 minutos o una tarea de Windows Task Scheduler hacen el mismo trabajo con cero dependencias adicionales.
- **VALIDO:** Tablas regulares en Firebird como "materialized views manuales" — el workaround documentado por la comunidad Firebird. Sin fricciones de stack, compatible con el resto del sistema.
- **VALIDO:** Un unico worker de refresh que construye toda la capa analitica — no necesitamos pipelines de ETL separados para cada tabla.
- **CUIDADO:** La contension de recursos entre el worker de refresh y las transacciones operacionales. El worker debe programarse en horas de baja actividad (madrugada, hora de comida) y usar transacciones de solo lectura sobre las tablas Microsip para no bloquear escrituras.

### Fuentes

- [Real-time analytics sobre Postgres — ClickHouse Blog](https://clickhouse.com/resources/engineering/real-time-analytics-postgres)
- [Estrategias de refresh para materialized views — codelit.io](https://codelit.io/blog/database-materialized-view-refresh)
- [Materialized views en Firebird — GitHub Issue CORE822](https://github.com/FirebirdSQL/firebird/issues/1208)
- [Seleccionar solucion OLAP — MotherDuck](https://motherduck.com/learn/select-olap-solution-postgres/)
- [DuckDB materialized views — Medium/HashBlock](https://medium.com/@connect.hashblock/materialized-views-in-duckdb-fast-analytics-without-warehouses-981189a26f05)
- [Rollup tables — QuestDB](https://questdb.com/glossary/rollup-table/)
- [OLAP en Postgres: retos y estrategias — epsio.io](https://www.epsio.io/blog/olap-in-postgres-features-challenges-and-optimization-strategies)
