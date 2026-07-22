# Diseño — Reactivación R7 · Fase 3c: Bandeja del operador

> Continúa [`2026-07-21-reactivacion-r7-fase3-design.md`](2026-07-21-reactivacion-r7-fase3-design.md) (§12
> dirección visual) y consume la API de la **Fase 3a** (copiloto backend, mergeada a `main` local). La 3a
> construyó el cerebro + los 7 endpoints; la **3c** construye la interfaz del operador para operar el **modo
> sombra** (la IA redacta, el humano confirma). Fuera de alcance: 3b (canal real WhatsApp).

## 1. Objetivo

Una **bandeja de 3 paneles con lógica de cola de revisión** (inbox SOTA 2026) para que un operador revise las
conversaciones del copiloto, apruebe/edite/dicte borradores y atienda escaladas — todo en modo sombra. Los
mockups aprobados son la fuente de verdad visual: `.superpowers/brainstorm/23612-1784662562/content/
bandeja-sota.html` (inbox) y `bandeja-escalada.html` (briefing). Principio: **la IA recomienda; el humano
confirma**; reactivación y cobranza estrictamente separadas (el operador nunca ve cifras de deuda en el bot).

Se construye **profesional y robusto, con tests en todo** (backend Go + frontend vitest), a través de agentes,
cada uno en su **worktree** (backend + frontend) para no tocar `main`.

## 2. Alcance (decidido con el usuario)

- **Bandeja SOTA completa** en un solo entregable: cola (Te necesitan / Al día) + hilo de conversación +
  compositor de borrador IA (violeta: aprobar/editar/dictar) + briefing de escalada (ámbar: escalar/tomar) +
  ficha (identidad + nota destilada + banderas + "La IA recomienda").
- **Control de "simular mensaje entrante"** (gateado a admin) — como el inbound es simulado en 3a, la bandeja
  incluye un afford. para inyectar un entrante y así demostrar el flujo escalar/borrador de punta a punta sin
  chip real. Es la única forma de ejercitar el loop hasta 3b.
- **Ambos temas (oscuro + claro)** desde el inicio, siguiendo el `ThemeToggle` global de la app.
- **Enriquecimiento del backend** (necesario): la 3c requiere que los DTOs de 3a lleven `nombre`/`segmento`/
  `telefono`/último-mensaje (§4).

**Fuera de alcance (3c):** el encabezado "La IA habría acertado N%" (requiere un agregado nuevo de precisión
sombra sobre el log de decisiones); identidad Microsip profunda en la ficha (compras/zona/"cliente desde" — la
ficha usa sólo los datos que enriquece el copiloto); canal real WhatsApp (3b); captura de voz para Dictar
(intención escrita por ahora).

## 3. Arquitectura (dos repos, dos worktrees)

```
msp-api (worktree fase3c-api, branch feat/reactivacion-r7-fase3c)
  └─ internal/reactivacion/  → enriquecer DTOs + app query (§4)
sistema-cobro-web (worktree fase3c-web, branch feat/reactivacion-r7-fase3c-bandeja)
  └─ src/modules/bandeja/    → módulo hexagonal nuevo (§5)
```

## 4. Enriquecimiento del backend (msp-api)

Los DTOs de 3a (`copiloto_dto.go`) no cargan lo que la bandeja renderiza. Los datos ya existen vía
`ClienteFactsReader` (nombre/segmento/telefono) + los turnos. Cambios mínimos:

- **Cola** (`GET /v2/reactivacion/conversaciones`, `ConversacionResumenDTO`): agregar
  - `nombre` (string) — de `ClienteFactsReader.GetFacts`.
  - `segmento` (string, `recien_liquidado`|`por_liquidar_hueco`) — para el chip.
  - `ultimo_mensaje` (string, truncado ~120 chars) — el último turno **entrante** (cuerpo), para la línea de
    vista previa. Vacío si no hay entrante.
- **Detalle** (`GET /v2/reactivacion/conversaciones/{cliente_id}`, `ConversacionDTO`): agregar `nombre`,
  `segmento`, `telefono` al bloque `conversacion` — para el encabezado de la ficha.
- **App**: extender `ListarConversaciones` para hidratar nombre/segmento/último-entrante por cliente (batch:
  reusar `ClienteFactsReader` + un lookup del último turno entrante, o un método de repo acotado). Extender
  `ObtenerConversacion` para incluir los facts en el detalle. Mantener CLAUDE.md (nada nuevo en DB; los facts
  vienen de `MSP_RX_COHORTE` UTF8).
- **Tests**: extender los handler tests (`copiloto_test.go`) para los campos nuevos; extender los tests de app
  con fakes. Sin regresión en los 7 endpoints existentes.
- **Degradación**: si un cliente no está en la cohorte (facts nil), los campos van vacíos — la bandeja lo
  tolera (no rompe).

## 5. Frontend — módulo `bandeja` (sistema-cobro-web)

Hexagonal, calcando `configuracion` (el template del rediseño SOTA):
`domain/ · application/{ports,usecases} · infrastructure/{http,mappers} · presentation/{context,composition,
hooks} · components/`.

- **domain**: tipos (`ConversacionResumen`, `ConversacionDetalle`, `Turno`, `Decision`, `DecisionFinalKind`),
  `DomainError`, y helpers puros de UI (bucket de cola `teNecesitan|alDia`, confianza binaria alta/baja por
  umbral 65, mapear estado→etiqueta, elegir borrador-vs-briefing desde la última decisión).
- **infrastructure/http**: `apiClient.ts` (copiar de `rutas`, con `authStateReady`), `HttpBandejaAdapter`
  (GET cola, GET detalle, POST mensaje-entrante, POST aprobar/editar/dictar/escalar) + `dtos.ts` snake_case
  espejando los DTO Go + `mappers` (dto→domain, lanzando `DomainError`).
- **application**: puertos (interfaz del adapter) + usecases finos (listarCola, obtenerDetalle, aprobar,
  editar, dictar, escalar, simularEntrante).
- **presentation**: `BandejaContainer` (composición) → `BandejaContext` → hooks (`useCola` con polling ~20s +
  refetch tras acción; `useConversacion`; `useAccionesBorrador` máquina idle|enviando|hecho). Pantalla
  `BandejaScreen` = 3 columnas.
- **components**:
  - **Cola** (`ColaPanel`): secciones `Te necesitan` (escaladas/señal-compra/confianza-baja arriba) y `Al día`;
    `QueueItem` (chip segmento, nombre, vista previa `ultimo_mensaje`, tiempo relativo, punto de confianza
    binario). Orden ya viene del backend; el front respeta y agrupa.
  - **Conversación** (`ConversacionPanel`): `Hilo` (burbujas: cliente izq. neutro; ia/humano der.), luego:
    - **Compositor IA** (`BorradorComposer`, violeta) cuando la última decisión propuso borrador: texto +
      confianza binaria (%, en hover) + "Por qué" (razón) + chips de evidencia + acciones **Aprobar y enviar**
      (primaria) · **Editar** (inline) · **Dictar** (input de intención → `Dictar` → nuevo borrador) · **Escalar**.
    - **Briefing** (`BriefingEscalada`, ámbar) cuando está escalada: campos estructurados (intención,
      sentimiento/estado, por qué escaló, confianza, entidades, qué hizo la IA) + siguiente paso sugerido +
      acciones **Tomar** · **Dictar** · **Escalar/asignar**. (Los campos salen de la última decisión +
      razon_escalamiento; el "sentimiento/entidades/siguiente paso" que no exponga la API se derivan de la
      decisión o se omiten con gracia — sin inventar.)
  - **Ficha** (`FichaPanel`): identidad (nombre/segmento/telefono/chips de estado), tarjeta **nota destilada**
    (`contexto_nota`), tarjeta **banderas** (danger), y **"La IA recomienda"** (acción/confianza/razón de la
    última decisión). NUNCA muestra texto crudo de la nota ni cifras de deuda.
  - **Simular entrante** (`SimularEntranteControl`, sólo admin): input + enviar → POST mensaje-entrante →
    refetch. Visible únicamente para el rol admin/dev.
  - **comunes**: primitivas compartidas (dialog de confirmación = `ConfirmActionDialog` existente; toasts =
    `sonner`).

## 6. Flujo de datos y frescura

- Lectura: `GET /conversaciones` (cola) → al seleccionar, `GET /conversaciones/{id}` (hilo + decisiones +
  ficha). Tras cualquier acción (aprobar/editar/dictar/escalar/simular) → refetch de la conversación afectada
  + de la cola. La cola además hace **polling ~20s** para que nuevas escaladas floten arriba (modelo de cola de
  revisión; sin websockets — fuera de alcance).
- Estados de acción: máquina de estados discriminada (idle|enviando|hecho|error) por acción; deshabilitar
  botones mientras envía; toast de éxito/error; `Aprobar` es idempotente en backend (2º clic = no-op).

## 7. Tema (oscuro + claro)

Los tokens del mockup (`--bg #0B0D12 … --ai #A78BFA/#7C3AED`, Inter, `font-variant-numeric: tabular-nums`)
se definen como **custom properties CSS** con un set **oscuro** y uno **claro**, conmutados por la clase de
tema global de la app (`darkMode: ["class"]` + `ThemeContext`/`ThemeToggle`). El look SOTA a la medida es
Tailwind/CSS propio (no shadcn de fábrica; se usa la skill `frontend-design`); las primitivas compartidas
(dialog, toast) siguen viniendo del kit existente. El acento **violeta es exclusivo de la IA**; azul para la
acción humana; 3 semánticos (ok/warn/danger).

## 8. Gating y seguridad

- Módulo nuevo `BANDEJA` en `src/constants/modules.ts` (`requiredRole: [SUPER_ADMIN, ADMIN]`) + ruta en
  `App.tsx` con `<ProtectedRoute requiredModule="BANDEJA" requiredRole={[SUPER_ADMIN, ADMIN]}>`; el sidebar la
  renderiza automáticamente. El backend YA exige `reactivacion:leer`/`administrar` (seguridad real =
  server-side). El control de simular-entrante se gatea además por rol admin en el front.

## 9. Pruebas

- **Backend** (Go): handler tests extendidos para los DTOs enriquecidos (nombre/segmento/telefono/último
  mensaje presentes); app tests con fakes para la hidratación; sin regresión en los 7 endpoints; gates de
  cobertura de siempre.
- **Frontend** (vitest): adapters (assert URL/params/DTO→dominio, `DomainError`); hooks (puertos fake +
  renderHook: polling, refetch tras acción, máquina de estados); helpers puros (bucket de cola, confianza
  binaria, borrador-vs-briefing); un screen test del flujo (seleccionar → aprobar → refetch) con MSW. Regla:
  **todo lo nuevo lleva tests** (BE y FE).

## 10. Entrega

- Dos worktrees, dos ramas: `feat/reactivacion-r7-fase3c` (msp-api) y `feat/reactivacion-r7-fase3c-bandeja`
  (sistema-cobro-web). Construido con agentes (subagent-driven), review por tarea + review de rama completa,
  verificación final (build/lint/tests/cobertura + verificación visual en vivo de la bandeja). No se toca
  `main`; el usuario decide el merge al final.

## 11. Riesgos / decisiones abiertas menores

- La API 3a no expone `sentimiento`/`entidades`/`siguiente_paso` estructurados del briefing; se derivan de la
  decisión (razón + señales + intención) o se omiten con gracia — **nunca se inventan**.
- Identidad profunda de la ficha (compras/zona) diferida; si el usuario la quiere, es un enriquecimiento
  posterior (lee Microsip o reusa el módulo `clientes`).
- El orden de la cola lo decide el backend (`ListarConversaciones`); el front sólo agrupa/renderiza.
