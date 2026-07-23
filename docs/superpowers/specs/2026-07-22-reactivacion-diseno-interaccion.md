# Diseño de interacción del copiloto de reactivación — evidencia, imágenes y elección de modelo

> **Fecha:** 2026-07-22
> **Estado:** documento de referencia (research + hallazgos de datos). Alimenta el system prompt del agente y el plan de imágenes. NO es código.
> **Alcance:** el TRATO CON EL CLIENTE en la conversación de venta por mensajería (WhatsApp) para reactivar clientes tibios de la mueblería. Es la parte que decide las ventas.
> **Relacionados:** `2026-07-20-reactivacion-r7-design.md` (spec padre), `2026-07-21-reactivacion-r7-fase3-design.md` (copiloto), `ventas-ai-chatbot-research-2026.md` (research previo). Memorias: `project_reactivacion_r7_fase3a_copiloto`, `project_reactivacion_imagenes_producto`.

---

## 0. Cómo leer este documento

Todo aquí está clasificado en tres niveles de certeza. **Respeta la clasificación** — es lo que evita vender humo:

- 🟢 **REGLA DURA (evidencia primaria):** respaldada por estudio académico top o meta-análisis. No se toca sin una razón muy fuerte.
- 🔵 **DECIDIDO (diseño de producto):** decisión deliberada del proyecto, documentada en los specs previos. Puede tener o no evidencia, pero ya es el terreno.
- 🟡 **DEFAULT DE PILOTO (sin evidencia dura):** no hay evidencia de frontera que lo resuelva. Se arranca con un default razonable y **se valida por A/B en modo sombra** ("la IA habría acertado el N%"). Cualquiera que afirme certeza aquí está citando marketing.

---

## 1. Reglas duras — respaldadas por evidencia primaria 🟢

La investigación profunda (2026, verificación adversarial: 34 claims extraídos → 25 verificados → 21 confirmados → 5 tras síntesis) mató casi todos los "benchmarks de plataforma" y dejó en pie **solo lo que tiene fuente primaria top**. Estas son las reglas que el agente debe encarnar:

### 1.1 "Vende la parcialidad, no el precio" — probado científicamente
- **Hallazgo:** el reencuadre temporal *pennies-a-day* (presentar un costo agregado como pagos pequeños recurrentes) **aumenta la aprobación de la compra sin cambiar el precio real**. Mecanismo: cambia el punto de comparación (lo comparas con un gasto chico y cotidiano, no con un gasto grande).
- **Fuente:** Gourville (1998), *Pennies-a-Day: The Effect of Temporal Reframing on Transaction Evaluation*, Journal of Consumer Research 24(4):395-408 (+1,000 citas). https://www.hbs.edu/faculty/Pages/item.aspx?num=8875
- **Regla para el agente:** la **parcialidad ("desde $X a la semana") va SIEMPRE como cifra protagonista.** El total NUNCA lidera. El enganche se dice como el paso de entrada, no como precio.
- **Nota:** un sub-claim sobre el modelo experimental multi-categoría fue REFUTADO; se afirma solo el efecto principal, no el detalle de dos pasos.

### 1.2 Una sugerencia dirigida, no un menú — y por la razón correcta
- **Hallazgo:** el "choice overload" **NO es universal** (efecto promedio ≈ cero). Solo daña bajo moderadores: **decisión difícil, conjunto complejo, baja pericia del que decide, incertidumbre de preferencia.** La reactivación asíncrona por mensajería —donde el cliente decide solo, sin vendedor— **activa esos moderadores**.
- **Fuentes:** Scheibehenne, Greifeneder & Todd (2010), JCR 37(3), 50 experimentos, N=5,036 (https://www.jstor.org/stable/10.1086/651235); Chernev, Böckenholt & Goodman (2015), JCP 25(2), 99 observaciones, N=7,202 (https://doi.org/10.1016/j.jcps.2014.08.002).
- **Regla para el agente:** abrir con **UN next-best-product** dirigido (del historial), a lo más un mini-set de **2-3**. **Nunca un catálogo/menú largo** en el primer mensaje. El cliente siempre puede pedir alternativas.
- **Corolario:** esto responde la duda "¿mostrar productos desde el primer mensaje?" → sí, pero **uno dirigido, no un menú.** (El "1 vs 2-3" exacto es 🟡 default de piloto, ver §4.)

### 1.3 No anunciar que es IA al inicio
- **Hallazgo:** revelar la identidad de IA **antes** de conversar reduce las compras **79.7%**. Los bots no revelados venden como agentes humanos expertos (~4x más que humanos inexpertos). El daño es un **sesgo subjetivo** (percibir a la máquina como menos capaz/empática), no una brecha real de competencia. Se **mitiga** con revelación tardía y con experiencia previa del cliente con IA.
- **Fuente:** Luo, Tong, Fang & Qu (2019), *Machines vs. Humans: The Impact of AI Chatbot Disclosure on Customer Purchases*, Marketing Science 38(6), experimento de campo >6,200 clientes. https://pubsonline.informs.org/doi/10.1287/mksc.2019.1192
- **Regla para el agente:** la IA **conduce y redacta sin presentarse como bot** al inicio. Si se revela, **tarde y natural**. El handoff invertido al humano cerrador encaja aquí (el humano entra con autoridad en el momento de intención de compra).
- **⚠️ Asterisco de alcance:** estudio de 2019, llamadas de VOZ, servicios financieros en China, pre-ChatGPT. La magnitud pudo atenuarse con la familiaridad con IA de 2026. Trasladarlo a texto asíncrono y al umbral ético/legal de transparencia **requiere criterio** (ver §4, pregunta abierta).

### 1.4 Persona cálida y humana — pero bájala ante enojo
- **Hallazgo:** la persona antropomórfica (con diseño humano) ayuda en **estados neutrales/positivos, búsqueda de producto e interacciones de promoción**, pero **es contraproducente con clientes enojados**: baja satisfacción, evaluación de la empresa e intención de compra. Mecanismo: **violación de expectativas** (el diseño humano infla la expectativa de eficacia; al decepcionar, el cliente enojado castiga a la empresa). Robusto en datos reales (461,689 sesiones: alta ira β=−.350, p=.02). Se neutraliza **rebajando/aclarando las capacidades limitadas** del bot.
- **Fuente:** Crolic, Thomaz, Hadi & Stephen (2022), *Blame the Bot: Anthropomorphism and Anger in Customer–Chatbot Interactions*, Journal of Marketing 86(1), 5 estudios (FT50). https://journals.sagepub.com/doi/10.1177/00222429211045687
- **Regla para el agente:** cálido y humano en **apertura y descubrimiento** (reactivación = promoción, cliente neutral/positivo). PERO ante **objeción con enojo, reclamo, o cualquier cosa adyacente a deuda/cobranza → baja el antropomorfismo, no sobrevendas capacidades, y escala.** Refuerza el rail "reactivación ≠ cobranza".

### 1.5 Velocidad de respuesta = palanca de conversión (confianza media)
- **Hallazgo:** casi todas las empresas responden lentísimo a los leads (promedio ~42h; solo ~37% en la primera hora). La velocidad de primer contacto es palanca de conversión.
- **Fuente:** Oldroyd, McElheran & Elkington (2011), HBR *The Short Life of Online Sales Leads*, 2,241 empresas. https://hbr.org/2011/03/the-short-life-of-online-sales-leads
- **Confianza media:** principio general (2011) transferido por analogía al canal de mensajería, no dato directo de reactivación por WhatsApp.
- **Regla para el agente/sistema:** minimizar latencia de la IA en cada respuesta; el escalamiento invertido debe disparar al humano cerrador **rápido** cuando aparece intención real, sin dejar enfriar el momentum.

---

## 2. Diseño de producto ya decidido (el terreno) 🔵

De los specs previos (`2026-07-20-reactivacion-r7-design.md`, Fase 3). No re-litigar sin razón fuerte:

- **Escalamiento invertido:** se escala en el momento BUENO (intención real de compra → humano cierra same-day) y en lo sensible (deuda), no solo en lo malo. Meta: escalar **10-20%**, umbral **alto** ("en duda, escala" — lección Klarna). **Matiz crítico (ver §6):** "intención de compra" que escala = *listo para cerrar*, NO cualquier pregunta de producto/interés (eso lo maneja la IA).
- **La IA es siempre la pluma:** el humano nunca teclea el mensaje final crudo, ni en el cierre. Aporta decisión/autoridad (aprobar precio, confirmar existencia); dicta la intención y la IA redacta.
- **Modo sombra (demo):** `auto_send = off`, human-gate; la IA decide y registra lo que *habría* hecho autónomo → segundo número para el dueño: "la IA habría acertado el N%".
- **Precio determinista en Go — la IA NO hace aritmética:** enganche = `redondear_50(0.10 × precio)`, parcialidad = `redondear_50((precio − enganche)/periodos)`, plazo 1 año, piso ~$3,000 `[POR CONFIRMAR]`, todo en múltiplos de $50. La IA solo **enuncia** montos pre-calculados; nunca multiplica ni deriva un "total al mes". (Esto previene por diseño el bug del §6.)
- **Rails de seguridad:** nunca una cifra de deuda (solo afirmar en positivo "ya terminó de pagar" — es el gancho); no inventar productos/montos/promos; reactivación ≠ cobranza; no citar la nota del cobrador (solo contexto destilado); allowlist `[BORRADOR, requiere números del dueño]`.
- **Segmentos:** `recien_liquidado` (felicitar + siguiente compra con beneficio) y `por_liquidar_hueco` (completar el juego con tacto, no liderar con "compra más"). Solo Tehuacán (LADA 238) para el arranque.
- **Next-best-product:** una sugerencia dirigida derivada del historial + canasta de mercado. Algoritmo investigado en `docs/research/_parts/A3-nbp-propension.md`.
- **Anti-baneo / cadencia demo:** máx **2 toques** (opener + 1 follow-up suave ~día 3); 3+ solo tras migrar a API oficial + opt-in.

---

## 3. Lo REFUTADO — no citar a nadie ❌

La verificación adversarial tumbó estos "datos" por ser de blogs/marketing, no fuentes primarias:

- ❌ "WhatsApp tiene 98% de open rate."
- ❌ "JioMart: 3x tráfico / +68% recompra con WhatsApp commerce."
- ❌ "LivePerson: 4x conversiones con conversational commerce."
- ❌ El modelo experimental "de dos pasos multi-categoría" del efecto pennies-a-day (solo se sostiene el efecto principal).

**Confirma de forma independiente la postura de los docs previos:** *se copian las técnicas, NO los porcentajes.* Los números de conversión reales de ESTE negocio solo salen del piloto.

---

## 4. Preguntas de piloto — sin evidencia dura, validar en modo sombra 🟡

**Hallazgo honesto del research:** para estas preguntas **no sobrevivió evidencia de frontera** — ni siquiera existe en la literatura top. Se arranca con un default y se aprende con A/B en sombra. Marcadas 🟡.

| # | Pregunta abierta | Default de arranque propuesto | Cómo se valida |
|---|---|---|---|
| A | Forma exacta de la **apertura** (¿saludo+NBP+CTA?, ¿referencia al pasado?) | Saludo personalizado + referencia a comportamiento pasado + UNA sugerencia (NBP) con beneficio + CTA suave (1 pregunta) | A/B de variantes de opener; medir tasa de respuesta |
| B | **¿1 vs 2-3 productos** en el primer tiro? | 1 (NBP dirigido); ofrecer alternativas solo si el cliente titubea | A/B 1 vs 3; medir avance de conversación |
| C | **Descubrir vs presentar** primero (¿preguntar necesidad antes de ofrecer?) | Presentar el NBP y ajustar; discovery mínimo en asíncrono (el canal no tolera interrogatorio) | A/B present-first vs question-first |
| D | **Diseño de mensaje:** longitud, un-solo-CTA, emojis, formato | Corto (2-4 líneas), UN CTA por mensaje, emojis con mesura, negritas para montos | A/B longitud y CTA |
| E | **Imágenes en la conversación:** cuándo, cuántas, ambientada vs fondo blanco (ver §5) | Enviar foto del NBP junto con la parcialidad; 1-3 fotos; muebles = ambientada, línea blanca = fondo blanco | A/B con-foto vs sin-foto; #fotos |
| F | **Notas de voz** (¿suben rapport o incomodan?) | No en demo; evaluar después | Piloto posterior |
| G | **Cadencia de follow-up** exacta (más allá de "máx 2 toques") | Opener + 1 follow-up suave a ~72h si no responde | A/B timing del 2º toque |
| H | **Handoff IA→humano sin romper momentum** (§1.3/§1.5) | Puente redactado por la IA que anuncia que "un asesor" continúa; transición cálida, sin cambio brusco de voz | Medir caída de conversión en el handoff |
| I | **Umbral de revelación de IA** (ético/legal vs §1.3) | No anunciarse al inicio; si preguntan directo "¿eres un bot?", no mentir | Decisión de política + legal |

---

## 5. Manejo de imágenes de producto — core, y su estado real

**Sin foto no hay venta de muebles por WhatsApp.** La imagen ES el producto en el canal. Datos verificados en la BD dev (venta 2025+):

### 5.1 El estado real (medido)
- Microsip guarda fotos en `IMAGENES_ARTICULOS` (BLOB embebido `IMAGEN`, ~162 KB promedio).
- **1,686 imágenes / 1,685 productos = UNA foto por producto** (solo 1 artículo tiene 2+). Sin ángulos.
- **64% pesan <100 KB** (miniaturas/baja resolución). No vendibles tal cual.
- Cobertura por unidades: 14,246 / 29,528 = **48%** — pero es **piso de respaldo**, no activo.
- **Patrón por línea:** línea blanca/electrónica BIEN cubierta (lavadoras 84%, refris 91%, estufas/licuadoras/ventiladores 93-100% — stock del fabricante, fondo blanco, usable). Mueble de MADERA mal cubierto: colchones 14%, roperos 19%, comedores 18%, KIT CAMAS 0%, tocadores 17%, recámaras 22%.
- **Concentración: top 300 SKUs = 81% de las unidades** (1,634 distintos/año). El nombre trae marca+modelo para línea blanca.

### 5.2 Los productos que más se venden (contexto de dificultad)
Top por unidades: colchones (Lester Facility #1), bases de cama (Leos Venecia), roperos (labrados, Samy, San Bernardo), sillas, lavadoras (Easy, Koblenz, Midea, Winia), pantallas (JVC), refris (Hisense, Winia), celulares (Samsung A16), estufas (Mabe, Acros), licuadoras (Oster). → **Bimodal:** electrónica/línea blanca = imagen fácil (marca+modelo online); mueble de madera genérico/local = SIN imagen online, hay que fotografiar.

### 5.3 Plan de imágenes
- **Nosotros creamos las fotos buenas** (2-4 ángulos, buena luz, ambientada para muebles / fondo blanco para línea blanca). Microsip = fallback solo para línea blanca.
- **Acotado:** set héroe (~15-25) para el demo primero; tanda ~100-200 SKUs de madera por prioridad de venta. No 1,634.
- **El proyecto aporta el pipeline:** almacén de imágenes propio (filesystem local, ADR-0003) con **múltiples fotos por producto**; orden de resolución **foto nuestra → BLOB Microsip usable → sin foto** (en demo nunca sugerir mueble sin foto decente); herramienta de captura móvil (buscar producto → 2-4 fotos → auto-etiqueta ARTICULO_ID → sube); tablero "faltan fotos" ordenado por volumen, marcando las <100 KB como "re-tomar".
- **IA sí:** limpieza/upscale/quitar fondo/encuadre; rellenar línea blanca por marca+modelo. **IA NUNCA:** generar imágenes de muebles (publicidad falsa → devoluciones → confianza rota; choca con el rail "no inventar"). Solo LIMPIA fotos reales.
- **Cómo convencer al dueño:** el demo es el argumento — misma conversación con foto real bien tomada vs miniatura fea de Microsip. Número: "la mitad de tu catálogo tiene una sola foto en baja; tus mayores vendedores —colchones, camas, roperos, comedores— son los peor cubiertos, y son >50% de tu volumen."

---

## 6. Elección de modelo LLM — hallazgos del bake-off

Banco de prueba desechable (`internal/reactivacion/infra/reactivacionllm/bakeoff_test.go`, env-gated) contra **Claude Haiku 4.5** vs **OpenAI GPT-5.6-Luna** sobre el prompt real de `Analizar`.

### 6.1 Resultados
- **JSON válido: 100% ambos.** Seguridad de deuda: **ambos escalan y ninguno soltó una cifra.**
- **Tono:** Claude más cálido/natural en español MX; OpenAI más seco y a veces intención en snake_case.
- **Configuración:** GPT-5.6 **no acepta `temperature=0`** (400) → hay que omitir temperatura. Claude corre determinista a temp 0.
- **En conversación con contexto:** las decisiones finales son **casi idénticas** (las dos escalan lo mismo por señales). La divergencia real: **cómo etiquetan señales** (OpenAI más conservador/literal; Claude más suelto).
- **Riesgo de Claude:** por fluido, **improvisó un monto no dado** ("$1,400 al mes" calculando 4×$350) — viola el rail "no inventar montos". `triar` NO lo atrapa (solo caza cifras de deuda). → El precio-en-Go (§2) mitiga esto por diseño.
- **A favor de OpenAI:** siguió la política de escalamiento más literal y redactó un puente de handoff al cliente.

### 6.2 Dos hallazgos duros (más importantes que el modelo)
1. **La escala vive en `triar` (determinista), no en el LLM.** `triar` **ignora `out.Accion`** del LLM y decide por **señales + confianza + texto del borrador**. Su regla actual escala ante *cualquier* `senal_compra` → colapsa "pregunta de producto/interés" (que la IA debe manejar) con "listo para cerrar" (que sí escala). **Ese es el bug de calibración real** para lograr "la IA conduce, el humano cierra" (ver §2 matiz).
2. **Bug de parser de `confianza`:** ambos modelos a veces devuelven `confianza` como decimal 0-1 (0.99) o escala rara (Claude "7"/"8"). El `Generator` la parsea como `int` 0-100 → **tronaría en producción**, y peor: `triar` escala por "confianza baja" si lee 7 como 7/100. **Blindar el parser** pase lo que pase con el modelo.

### 6.3 Conclusión (honesta)
Para el objetivo "la IA conduce la venta", **el modelo pesa MENOS que la política + el prompt + el catálogo/imágenes.** Con el prompt "IA conduce / humano cierra" los dos venden bien. Claude gana en calidez; OpenAI en disciplina de reglas. **La decisión de modelo se difiere** hasta tener el diseño de interacción final y re-correr el bake-off alineado (opener NBP, precio pre-calculado, escalar solo cierre/sensibles). Ambos necesitan endurecimiento de prompt: Claude contra invención de montos + sobre-escalamiento; OpenAI contra sequedad + drift de escala de confianza.

---

## 7. Metodología del research (para auditar)
- Deep research con verificación adversarial (3 votos/claim, 2/3 para matar). 5 ángulos, 20 fuentes fetch, 34 claims → 25 verificados → 21 confirmados → 5 tras síntesis. 102 agentes.
- **Instrucción clave de fuentes:** frontier mundial (China/WeChat, WhatsApp India/Brasil, BDC automotriz EE.UU., ciencia conductual top), México solo como capa de adaptación. Resultado: la ciencia conductual sobrevivió; los benchmarks de plataforma no.

### Fuentes primarias que sobrevivieron
- Gourville (1998) JCR — pennies-a-day. https://www.hbs.edu/faculty/Pages/item.aspx?num=8875
- Scheibehenne et al. (2010) JCR — meta-análisis choice overload. https://www.jstor.org/stable/10.1086/651235
- Chernev et al. (2015) JCP — meta-análisis choice overload moderado. https://doi.org/10.1016/j.jcps.2014.08.002
- Luo et al. (2019) Marketing Science — disclosure de IA. https://pubsonline.informs.org/doi/10.1287/mksc.2019.1192
- Crolic et al. (2022) Journal of Marketing — antropomorfismo y enojo. https://journals.sagepub.com/doi/10.1177/00222429211045687
- Oldroyd et al. (2011) HBR — speed-to-lead. https://hbr.org/2011/03/the-short-life-of-online-sales-leads

---

## 8. Decisión de modelo LLM + modelo de costos (2026-07-23)

### 8.1 Modelo elegido: **Claude Haiku 4.5** (`claude-haiku-4-5`)
Decidido tras el scorecard sobre escenarios realistas (prompt de producción con B). Gana en las tres dimensiones que importan:
- **Seguridad:** empate perfecto con OpenAI GPT-5.6-Luna (0 fugas de cifra de deuda, 0 montos inventados, 0 emojis, 100% JSON). El único "miss" de Claude (no etiquetar `confianza_baja` en "???") lo ataja `triar` de forma determinista (dio confianza=0 → escala igual).
- **Tono:** con el mismo prompt profesional aplicado a ambos, Claude sale más cálido/humano — y la evidencia (Crolic 2022, Luo 2019) **premia la calidez** en contexto de promoción/cliente neutral. Es determinista a temp 0 (OpenAI GPT-5.x no acepta temp 0).
- **Precio:** input idéntico ($1/1M), caché idéntico ($0.10/1M), **output más barato** ($5 vs $6/1M).

### 8.2 Gotcha de cableado (RESUELTO)
El endpoint OpenAI-compat de Anthropic **rechaza `response_format: "json_object"`** (400: "Input should be 'json_schema'"). El `Generator.chatJSON` lo mandaba siempre. **Fix:** se quitó `response_format`; el contrato JSON lo garantizan el system prompt ("responde ÚNICAMENTE con un objeto JSON") + `extractJSON` (100% fiable en el bakeoff). El Generator quedó agnóstico del endpoint. Verificado con `TestClaudeRutaProduccion` (ruta real de producción contra Anthropic, verde).

### 8.3 Cableado (`.env`, dev)
`LLM_ENABLED=true`, `LLM_BASE_URL=https://api.anthropic.com/v1`, `LLM_MODEL=claude-haiku-4-5`, `LLM_TIMEOUT=60s`, `LLM_API_KEY=<llave Anthropic>`. En Docker requiere `docker compose up -d --force-recreate api` (el `.env` se evalúa al crear; ver [[reference_dev_env_change_requires_recreate]]).

### 8.4 Modelo de costos (Claude Haiku 4.5, $1 input / $5 output por 1M)
Puntos de llamada por cliente: **opener** (~1K in + 150 out), **destilar nota** (~1.5K in + 150 out, 1 vez/cliente), **cada respuesta del cliente** (`Analizar`, ~2K in + 300 out).

| Escenario | Volumen | Costo estimado |
|---|---|---|
| **Demo** | ~20-30 conversaciones simuladas (~200 llamadas) | **~$1-2** (centavos) |
| **Piloto** | 500 clientes | **~$4-5** |
| **Campaña completa** | ~6,700 clientes (universo Tehuacán c/tel), ~20% responde (~1,340 conversaciones ×5 msgs), 2 toques | **~$57** (≈ **$35-45 con prompt caching**) |
| **Ongoing** | ~2,000 clientes nuevos/mes | **~$15-20/mes** |

**Unidad:** ~4 centavos USD por conversación real; ~1 centavo por cliente contactado. Supuestos (response rate 20%, ~5 mensajes/conversación) los afina el piloto. Palancas para bajarlo: templatear el opener (−$19), prompt caching (−40%), destilar nota solo de quien responde (−$12) → campaña completa a ~$20-25.

**Perspectiva:** la campaña completa cuesta menos que una comida; una sola venta de mueble (~$5,000) paga ~100× toda la operación de IA. El costo de LLM **nunca es el cuello de botella** — lo son las imágenes y la conversión.

---

## 9. Estado y próximos pasos
Este documento es la referencia consolidada. **HECHO:** taxonomía B + parser de confianza + prompt de venta + opener (validados con modelos reales); modelo elegido (Claude Haiku 4.5) y **cableado en dev** (fix de response_format incluido). Pendientes: (1) opener a producción (Generator+puerto+endpoint); (2) pipeline de imágenes + set héroe (tras actualizar la DB); (3) smoke end-to-end en dev (Fase 2); (4) defaults de piloto §4 y A/B en sombra.
