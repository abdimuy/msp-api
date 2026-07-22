# Análisis de sourcing de imágenes por ventas — reactivación

> **Fecha:** 2026-07-22 · **Fuente:** BD dev `MUEBLERA.FDB`, ventas `DOCTOS_PV` + `DOCTOS_PV_DET`, periodo **2025-01-01 en adelante**.
> **Pregunta:** ¿qué tan complicado es tener imágenes vendibles del catálogo, dado que (a) no tenemos acceso físico a los muebles —están en bodega—, (b) la ruta realista es **fotos que tenga el papá + retoque con IA**, y (c) mientras más salga de **internet**, mejor?
> **Relacionado:** `docs/superpowers/specs/2026-07-22-reactivacion-diseno-interaccion.md` §5. Memoria `project_reactivacion_imagenes_producto`.

---

## 1. Veredicto ejecutivo

- **~53% del INGRESO se resuelve desde internet** (electrónica/línea blanca + posiblemente colchones de marca), con **retoque IA de imágenes descargadas** y **cero dependencia del papá.**
- **~43% del ingreso ($58.4M) es mueble local/genérico que SOLO el papá puede fotografiar.** No existe en internet (fabricantes locales: Leos, GIM, Rivermen, Lerisol, Samy). Este es el cuello de botella real.
- **CORRECCIÓN (2026-07-22):** las fotos del papá **NO son las de Microsip** — el papá las maneja en un **grupo de WhatsApp aparte** donde se mandan fotos de producto. Por tanto el 27% de cobertura de Microsip (abajo) **NO es proxy de lo que el papá tiene**; es una fuente separada y peor (miniaturas viejas). La fuente real de fotos de mueble es el **grupo de WhatsApp**, cuya cobertura real es probablemente **mayor** que ese 27%.
- **El cuello de botella se mueve:** ya no es "¿existe la foto?" sino **"¿cómo saco fotos del grupo de WhatsApp y las EMPAREJO con el producto correcto?"** (las fotos del grupo no traen `ARTICULO_ID`). La dificultad depende de si el grupo trae el nombre/modelo en el texto (→ semi-automatizable por *fuzzy match*) o son puras imágenes (→ emparejado manual).
- **Traducción:** la mitad del catálogo (por dinero) es fácil (internet + IA). La otra mitad (mueble local) depende del acervo del grupo de WhatsApp del papá + retoque IA; el trabajo real es **emparejar foto↔producto**, no fotografiar. Para el demo (15-25 piezas) no hay bloqueo: se escogen a mano del grupo.

---

## 2. Metodología

Clasificación por **línea de artículo** en 4 tiers de sourcing, cruzada con **unidades**, **ingreso** (`PRECIO_TOTAL_NETO`) y con las **imágenes ya existentes** en `IMAGENES_ARTICULOS` (BLOB; se mide presencia y si supera 100 KB = "buena", ya que 64% del acervo son miniaturas <100 KB).

- **Tier A — Internet (electrónica/línea blanca):** líneas de aparatos con marca+modelo en el nombre → descargable de fabricante/retailer.
- **Tier B — Colchones de marca:** marcas nacionales de colchón (Lester, América, Beder, Restonic) → imagen de catálogo *a veces* existe; semi-internet.
- **Tier C — Foto (mueble local/genérico):** roperos, bases, comedores, alacenas, etc. de fabricantes locales → **no existe en internet, requiere foto.**
- **Tier D — Mixto:** plásticos, blancos (cobertores), varios, metálicos → genéricos o de marca; mayormente fáciles.

---

## 3. Resultados globales (2025+)

Totales del periodo: **29,528 unidades**, **~$136.3M** de ingreso.

| Tier | SKUs | Unidades | % uds | Ingreso | % ing | $/unidad | Ya con img | Con img BUENA |
|---|---|---|---|---|---|---|---|---|
| **A — Internet electrónica** | 452 | 10,382 | 35.2% | $59.5M | **43.7%** | $5,734 | 82% | 26% |
| **B — Colchones de marca** | 105 | 4,457 | 15.1% | $12.9M | 9.5% | $2,894 | 14% | 3% |
| **C — Foto mueble (local)** | 918 | 11,461 | 38.8% | $58.4M | **42.8%** | $5,096 | 27% | 8% |
| **D — Mixto** | 159 | 3,228 | 10.9% | $5.5M | 4.0% | $1,697 | 61% | 10% |

**Lecturas clave:**
- El mueble (Tier C) genera **casi tanto ingreso como toda la línea blanca** ($58.4M vs $59.5M) y tiene alto ticket ($5,096/unidad). No es cola larga barata: **es la mitad del negocio.**
- La línea blanca (A) ya tiene 82% de cobertura de imagen (stock de fabricante) — pero solo 26% en buena calidad; da igual, **se re-descarga por marca+modelo.**
- El mueble (C) es donde la cobertura se desploma: 27% con cualquier foto, 8% buena.

---

## 4. Tier A — Internet (electrónica/línea blanca) 🌐

**452 SKUs, 35% de unidades, 44% del ingreso.** Cero dependencia del papá.

- El nombre trae **marca + modelo**: "LAVADORA REDONDA EASY 15 KG **MOD. LRE-15M**", "PANTALLA JVC **MOD. SI43FRF**", "REFRIGERADOR HISENSE **MOD. RR63D6WGX**", "ESTUFA MABE **MOD. EM7654BFIS**", "CELULAR SAMSUNG GALAXY A16".
- **Fuente:** sitio del fabricante o retailers (Coppel, Elektra, Mercado Libre, Liverpool) buscando el modelo.
- **Automatizable:** el nombre estructurado (marca+modelo) permite un asistente que arme la query y proponga imágenes; luego una pasada humana rápida de validación. Sin script, es una tarde de descargas para los ~100-150 que importan.
- **Retoque IA:** normalmente ya vienen en fondo blanco; solo recorte/encuadre uniforme.

## 5. Tier B — Colchones de marca 🌐/📸

**105 SKUs, 15% de unidades, 9.5% del ingreso.** Incluye el #1 de la tienda (Colchón Lester Facility Matrimonial, 1,858 uds).

- Marcas: **Lester, América (Tivoli/Sofía), Beder (Eclipse/Hydra/Leo), Restonic.**
- Un colchón se ve genérico → la imagen de catálogo del fabricante suele servir, aunque el modelo exacto no siempre coincide. **Semi-internet.**
- Solo 14% tiene foto hoy. **Recomendación:** intentar descarga por marca; si no, es de los pocos "muebles" fáciles de fotografiar (planos, en piso). El Lester #1 amerita foto propia buena.

## 6. Tier C — Foto de mueble local 📸 (el cuello de botella)

**918 SKUs, 39% de unidades, 43% del ingreso ($58.4M).** Aquí está el problema.

- Fabricantes **locales/genéricos**: roperos (Labrado, Samy, San Bernardo, Luanda, California), bases de cama (Leos Venecia, GIM Finisterre, madera chocolate, Antoni Rivermen), sillas tubulares (Sao Paulo, Tajin GIM), alacenas labradas, cómodas Venecia, tocadores, cabeceras Aitana Rivermen. **Ninguno existe en internet.**
- **Concentración (bueno):** los **150 muebles top = 9,409 / 11,461 = 82%** del volumen de mueble. No son 918, son ~150 los que mueven la aguja.
- **Cobertura en Microsip (NO es la fuente del papá):** de los 150 top, solo 52 tienen foto en Microsip; 189 de 918 SKUs (21%) en total. **Esto ya NO se usa como proxy de lo que el papá tiene** (ver corrección §1) — el papá maneja las fotos en un **grupo de WhatsApp aparte**.
- **La fuente real: grupo de WhatsApp del papá.** Cobertura real desconocida (probablemente > 27%). **El trabajo no es fotografiar, es emparejar** cada foto del grupo con su `ARTICULO_ID`. Preguntas abiertas que dimensionan el esfuerzo: (1) ¿las fotos traen nombre/modelo en el texto? (2) ¿un grupo o varios? (3) ¿qué tan completo/desde cuándo? (4) ¿volumen aprox? Si traen texto → *fuzzy match* semi-automático; si no → emparejado manual. Todas las fotos (grupo o Microsip) → **retoque IA** (centrar/limpiar).

### Top muebles a conseguir/retocar (por unidades)
Colchón Lester Facility Matrimonial (1,858) · Base Leos Venecia Choc (795) · Ropero Labrado Pasador Vino (570) · Silla Tubular Sao Paulo (436) · Base Madera Matrimonial Choc (399) · Kit Base Leos Venecia (388) · Base Madera Individual Choc (382) · Base GIM Finisterre Mat (357) · Ropero Samy MDF (297) · Ropero Labrado California Vino (225) · Alacena Labrada Vino (205) · Ropero Ovalos San Bernardo (185) · Cabecera Aitana Rivermen (113) · Cómoda Venecia 7 cajones (87) · Tocador Colonial Javier (83)…

## 7. Tier D — Mixto

**159 SKUs, 11% de unidades, 4% del ingreso.** Plásticos (bancos, organizadores), blancos (cobertores Providencia — marca, descargable), varios. 61% ya con imagen. Baja prioridad; mezcla de descarga y genéricos.

---

## 8. Feasibility: la ruta realista (papá + IA, maximizar internet)

| Fuente | Cobertura | Esfuerzo | Dependencia |
|---|---|---|---|
| **Internet (Tier A)** | 35% uds / 44% ing | Descargar ~100-150 por marca+modelo + recorte | Ninguna (tú/proyecto) |
| **Internet colchones (B)** | +15% uds / +9.5% ing | Descargar por marca; fallback foto | Baja |
| **Grupo de WhatsApp del papá** | mueble (cobertura real desconocida, prob. alta) | **Emparejar foto↔producto** + retoque IA | Media (el papá comparte el acervo; el emparejado es nuestro) |
| **Microsip (fallback)** | ~189 SKUs mueble | Extraer BLOB + retoque IA | Baja (respaldo si el grupo no la tiene) |
| **Hueco real** | lo que ni el grupo ni Microsip tengan | Papá lo consigue/toma | Alta, pero **tamaño por confirmar** (depende del grupo) |

**Conclusión de factibilidad:**
1. **Lo fácil (≈53% del ingreso) no depende del papá:** descargas de internet + retoque IA. Empezar por aquí.
2. **El acervo de mueble está en el grupo de WhatsApp del papá** (fuente principal), con Microsip como respaldo. El trabajo real es **emparejar cada foto con su producto** (semi-auto si hay texto; manual si no) + **retoque IA**. No es fotografiar.
3. **El hueco real** (lo que ni el grupo ni Microsip tengan) **está por dimensionar** — depende de qué tan completo esté el grupo. Primer paso: que el papá comparta/exporte el grupo para medir cobertura real.
4. **Para el DEMO:** el set héroe (15-25 piezas) se arma escogiendo a mano del grupo + colchones/electrónica de internet. Sin bloqueo.

---

## 9. Recomendación de pipeline (proyecto)

1. **Extractor de BLOBs Microsip** → materializa las imágenes existentes a archivo (línea blanca usable directa; mueble = candidatos a retoque).
2. **Descarga asistida por marca+modelo** para Tier A/B (el nombre ya está estructurado).
3. **Retoque IA por lote:** centrar, quitar/limpiar fondo, uniformar encuadre, upscale. **Nunca generar el mueble; solo limpiar la foto real** (una foto inventada que no coincide con lo entregado = publicidad falsa + devoluciones).
4. **Lista priorizada para el papá:** los ~100 muebles top sin foto, ordenados por unidades vendidas, como checklist.
5. **Almacén propio multi-foto** (filesystem, ADR-0003) con orden de resolución: foto retocada nuestra → BLOB Microsip usable → sin foto (no sugerir mueble sin foto en el copiloto).

---

## 10. Caveats

- Clasificación por **línea**, no por inspección producto-a-producto; hay ruido en los bordes (algún genérico en Tier A y viceversa), pero el patrón marca/local es robusto.
- "Ya con imagen" = existe en Microsip; **no** implica calidad (64% del acervo <100 KB). Es un proxy de "el papá probablemente tiene esa foto".
- El papá podría tener fotos **fuera** de Microsip (teléfono/WhatsApp); el 27% de cobertura de mueble es **piso**, no techo. Confirmar con él cuántas tiene sueltas cambiaría el tamaño del hueco duro.
- Ingreso = `PRECIO_TOTAL_NETO` sumado; no descuenta cancelaciones/devoluciones (no había flag limpio en `DOCTOS_PV`). Órdenes de magnitud, no contabilidad exacta.
