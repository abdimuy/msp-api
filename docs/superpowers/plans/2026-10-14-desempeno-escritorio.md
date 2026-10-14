# Desempeño de cobradores — escritorio (carril D) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reemplazar el Inicio del escritorio por la pantalla "Desempeño de cobradores" (tablero por usuario, ficha con Jornada / Actividad / Cuentas, popovers y modales con fotos), construida contra un puerto falso y luego conectada al API real `/v2/desempeno/*`.

**Architecture:** Módulo hexagonal nuevo `src/modules/desempeno` (domain → application → infrastructure / presentation → components), con la forma de `src/modules/rutas`. Todo número, pausa, tramo, conteo por hora, estado por venta y señal viene del API (D10): el escritorio solo pinta y ordena lo que ya llegó. Una **Tarea 0** corta deja la base común (dominio, puerto, puerto falso, formato y primitivos); después **3 carriles en paralelo** en worktrees separados con archivos disjuntos (tablero · ficha · modales+fotos+HTTP) construyen contra `FakeDesempenoPort`; los carriles se conectan por *huecos* (`renderLinea`, `controlesLinea`, `renderFotos`, callbacks de modales) y una **tarea final de integración** los une, cuelga la pantalla de `/` detrás de `DESEMPENO_COBRADORES` y retira el Home viejo. **Fuera de este plan (dueño, 2026-10-14):** exportación a Excel (Apéndice A).

**Tech Stack:** Tauri 2, React 18, Vite 5, TypeScript 5 (strict), Tailwind 3 + shadcn/Radix (se agrega `@radix-ui/react-hover-card`), axios, lucide-react, `@react-google-maps/api`, Vitest 2 + Testing Library + user-event + MSW 2 (jsdom, `TZ=UTC`).

**Spec:** `/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/specs/2026-10-14-desempeno-cobradores-design.md` (§3.3, §5, §6, §7, §8 carril D) y **contratos (LEY)** `/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/specs/2026-10-14-desempeno-contratos.md`. Referencia visual e interacción: mock estilo B "Audaz 2026" `/private/tmp/claude-501/-Volumes-M2-1TB-Developer-msp-api/763f9984-4f54-48d0-aac4-12b9cfa885ce/scratchpad/desempeno/desempeno-cobradores.html` (el mock calcula en JS; el producto no).

**Repo de trabajo:** `/Volumes/M2-1TB/Developer/sistema-cobro-web` (todas las rutas de este plan son relativas a esa raíz salvo que digan otra cosa).

## Global Constraints

- Contratos = ley: el DTO usa los nombres JSON del archivo de contratos **byte a byte** (`snake_case`, dinero como string `"1250.00"`, horas RFC3339 UTC, `null` explícito). Cambiar un nombre exige cambiar primero `2026-10-14-desempeno-contratos.md`.
- D10 "toda la lógica en el servidor": el escritorio **no** suma, resta ni promedia dinero o conteos, **no** deriva pausas, tramos, conteos por hora, estados por venta, cobertura ni señales. `Number(...)` sobre un decimal solo se permite para formatear o para ordenar filas ya recibidas.
- D14: el tablero **no muestra totales sumados de varios usuarios** (ni banda de resumen ni pie de tabla).
- Formas de pago: efectivo, transferencia y cheque (más condonación aparte). **No existe "Depósito". No se muestra modelo de teléfono** (spec §10).
- Fechas: se pintan en `America/Mexico_City` con `src/utils/tiempoDeNegocio.ts` (`enRelojDeNegocio`, `rangoDeDiasDeNegocio`). Prohibido `dayjs(...)`/`new Date().getHours()` para pintar horas del módulo nuevo. La suite corre con `TZ=UTC` (vitest.config.ts): los fixtures usan horas que caen en días/horas distintos en UTC y en CDMX.
- Arquitectura (`src/modules/ventasLocales/README.md`): `domain/` sin imports externos; `application/` solo importa `domain/`; `infrastructure/` es la única capa con axios; `components/` consume hooks de `presentation/` (y tipos de `domain/`). Sin React Query, sin Zod, sin react-hook-form.
- Popovers (D17, spec §10): los de la línea de actividad se abren con el ratón (sin `tabIndex`); los del nombre y de las celdas también con teclado (`tabIndex={0}`). Contenido solo del API.
- Selector "Vista" (D16): Puntos, Bloques, Por hora, Bloques + hora; predeterminado **Bloques**; se recuerda por computadora en `localStorage` (clave `desempeno.vista-linea`). Todo acceso a `localStorage` va en `try/catch`.
- Tablero: abre en **Hoy** (D8), umbral de pausa 15/30/45/60/90 con 45 por defecto, horario opcional, se actualiza **cada 60 s**.
- Copy en español, corto (2–4 palabras en botones), nunca "ciclo" (se dice "semana"); una ubicación es "dónde se registró", nunca "Casa".
- Colores como tokens HSL en `src/index.css` (`--dsm-*`, solo canales) consumidos con `hsl(var(--dsm-*))` desde `tailwind.config.js`; modo oscuro por clase `.dark`.
- Commits conventional en español, **sin atribución de Claude**, nunca `--no-verify`. El árbol tiene cambios ajenos sin commitear en `src/modules/failedIntents/**`: **nunca** `git add -A`/`git add .`; siempre `git add <rutas de la tarea>`.
- Comandos del repo: prueba puntual `npx vitest run <ruta>`; suite `npm test`; tipos `npx tsc --noEmit`; build `npm run build`. `npm run lint` **ya falla hoy en main** (88 problemas preexistentes); la compuerta de lint de este plan es `npx eslint <archivos de la tarea> --max-warnings 0` para archivos nuevos, y "no más problemas que antes" para archivos existentes tocados (se mide antes y después).
- Archivos `.tsx` del módulo exportan **solo componentes** (y tipos/constantes primitivas) para no disparar `react-refresh/only-export-components`; hooks, contextos y funciones van en `.ts`.

## Review Focus

1. **Falla de la actualización de cada minuto o cambio de Hoy/Semana:** la tabla no se vacía si una recarga falla (se muestra el aviso "No se pudo actualizar" con la hora de los datos), y al cambiar de periodo o de día nunca se pintan los datos del periodo anterior bajo la etiqueta nueva. Pruebas: Tarea 0 Parte B (`useCargaRemota` borra datos al cambiar de consulta y los conserva en `refresh` fallido) y Tarea 1.2 (aviso en el tablero).
2. **Horas cerca de medianoche y zona horaria:** una actividad a las `05:30Z` es del día de negocio **anterior** y se pinta "23:30", y en la línea queda al borde derecho, no a la izquierda. Pruebas: Tarea 0 Parte C (`format.test.ts`, `escala.test.ts`).
3. **Pago que aún no llega a Microsip (`pago.docto_cc_id = null`):** no existe detalle de Clientes que abrir; al hacer clic debe abrirse el modal propio con su estado ("Sin aplicar"), no tronar ni abrir un `PagoModal` vacío. Pruebas: Tarea 3.3 (`ModalActividadDetalle` y `ModalesFicha`).
4. **API anterior sin `pago_recibido_id`/`num_fotos`** (el escritorio puede llegar antes que el API a alguna máquina, o el detalle viene de un pago de Microsip): `PagoModal` no truena y muestra "Sin comprobante". Prueba: Tarea 3.2.
5. **Rango de fechas invertido en Actividad (Del > al):** `rangoDeDiasDeNegocio` lanza; el filtro no debe tumbar la pantalla: se muestra "El rango está invertido" y se conserva el filtro anterior. Prueba: Tarea 2.2.

---

## Mapa de archivos por dueño

```
TAREA 0 (feat/desempeno, repo principal)
  package.json/package-lock.json (+@radix-ui/react-hover-card) · src/components/ui/hover-card.tsx
  src/index.css · tailwind.config.js (tokens --dsm-*)
  src/modules/desempeno/domain/**            (errors, values/{esUno,estadoCuenta,catalogoTipoVisita}, entities/*)
  src/modules/desempeno/application/**       (ports/DesempenoPort, usecases/consultas, __tests__/fakeDesempenoPort)
  src/modules/desempeno/presentation/{preferencias.ts, context/*, hooks/lib/{toDomainError,almacenLocal,useCargaRemota,useCadaMinuto,useEsPantallaAngosta}}
  src/modules/desempeno/test/renderConDesempeno.tsx
  src/modules/desempeno/components/lib/{format,escala,tratoMeta,tipoActividadMeta}.ts
  src/modules/desempeno/components/comunes/{InfoRapida,ChipTrato,PuntoEstado,IconoTipoActividad,MontoActividad,TarjetaActividad,Estados,SegmentoPeriodo}.tsx

CARRIL 1 — tablero (feat/desempeno-tablero)
  src/modules/desempeno/components/linea/**  ·  src/modules/desempeno/components/tablero/**
  src/modules/desempeno/presentation/hooks/useTablero.ts (+ __tests__/useTablero.test.tsx)

CARRIL 2 — ficha (feat/desempeno-ficha)
  src/modules/desempeno/components/{jornada,actividad,cuentas,ficha}/**
  src/modules/desempeno/presentation/actividadPreferencias.ts
  src/modules/desempeno/presentation/hooks/{useJornada,useActividad,useVistasGuardadas,useColumnasActividad,useCuentas}.ts (+ sus pruebas)

CARRIL 3 — modales, fotos y HTTP (feat/desempeno-modales)
  src/modules/ventasLocales/components/detalle/{fuenteImagen,AuthenticatedImage,VentaImagenLightbox,VentaImagenesGrid,index}.* (+ pruebas)
  src/modules/ventasLocales/components/EditarVentaModal/{piezas/ImagenTileEditable,tabs/ImagenesTab}.tsx
  src/modules/clientes/** (PagoDetalle, ImagenPago, puerto, adapter, fake, useImagenesPago, PagoFotos, PagoModal, index.ts)
  src/modules/desempeno/components/modales/** · src/modules/desempeno/presentation/hooks/useImagenes.ts
  src/modules/desempeno/infrastructure/** · src/test/msw/{handlers,fixtures}/desempeno.ts

INTEGRACIÓN (feat/desempeno, al final)
  src/modules/desempeno/{Desempeno.tsx,index.ts,components/DesempenoScreen.tsx,components/__tests__/,presentation/composition/}
  src/modules/inicio/** · src/constants/modules.ts · src/utils/permissions.ts(+test) · src/components/AppSidebar.tsx · src/App.tsx
  borra: src/modules/home/Home.tsx · src/services/api/getPagosAndVisitas.ts
```

Ningún archivo tiene dos dueños. Los carriles **no se importan entre sí**: lo que uno necesita de otro entra por un hueco (`renderLinea`, `controlesLinea`, `renderFotos`, `onAbrirActividad`, `onAbrirCuenta`) que llena la integración.

## Ramas, worktrees y orden de merge

| Paso | Quién | Rama | Contenido |
|---|---|---|---|
| 1 | 1 agente, repo principal | `feat/desempeno` (desde `main`) | Tarea 0 (Partes A, B, C) |
| 2 | 3 agentes **en paralelo**, cada uno en su worktree | `feat/desempeno-tablero` · `feat/desempeno-ficha` · `feat/desempeno-modales` (las tres desde `feat/desempeno` con Tarea 0) | Carril 1 (Tareas 1.1–1.2) · Carril 2 (Tareas 2.1–2.4) · Carril 3 (Tareas 3.1–3.4) |
| 3 | 1 agente, repo principal | `feat/desempeno` | merge `--no-ff` en este orden: **modales → tablero → ficha**; luego Tarea 4 (integración y verificación) |
| 4 | dueño | PR `feat/desempeno` → `main` | revisión; se despliega después de C4 (spec §8) |

Crear las ramas y worktrees (después del commit de Tarea 0):

```bash
cd /Volumes/M2-1TB/Developer/sistema-cobro-web
git checkout -b feat/desempeno main          # solo si aún no existe; Tarea 0 se hace aquí
# … Tarea 0 …
git worktree add ../sistema-cobro-web-tablero -b feat/desempeno-tablero feat/desempeno
git worktree add ../sistema-cobro-web-ficha   -b feat/desempeno-ficha   feat/desempeno
git worktree add ../sistema-cobro-web-modales -b feat/desempeno-modales feat/desempeno
(cd ../sistema-cobro-web-tablero && npm ci)
(cd ../sistema-cobro-web-ficha   && npm ci)
(cd ../sistema-cobro-web-modales && npm ci)
```

Cada agente trabaja y corre comandos **dentro de su worktree** (las rutas de archivo del plan son relativas a la raíz de ese worktree). El orden de merge pone primero el carril que toca módulos existentes (Ventas Locales, Clientes) para que la regresión de esos módulos se mida antes de sumar pantallas nuevas; como los archivos son disjuntos, cualquier orden debe unir sin conflictos.

Dependencias duras del spec (§8) que este plan NO puede saltar: el adapter HTTP (Tarea 3.4) se prueba contra fixtures, pero **conectarlo a producción** requiere C3 y B del API; la integración (Tarea 4) **se despliega** después de C4 (medición de cifras). La pantalla queda detrás del interruptor `DESEMPENO_COBRADORES`, que nadie tiene encendido salvo `SUPER_ADMIN` (anti-bloqueo de `canUserAccessModule`): ver Apéndice B.

---

## TAREA 0 — base común (un agente, rama `feat/desempeno`, va primero)

### Tarea 0 — Parte A: dominio, puerto, casos de uso y puerto falso

**Rama `feat/desempeno` (repo principal), antes de abrir los carriles.**

**Files:**
- Create: `src/modules/desempeno/domain/errors.ts`
- Create: `src/modules/desempeno/domain/values/esUno.ts`
- Create: `src/modules/desempeno/domain/values/estadoCuenta.ts`
- Create: `src/modules/desempeno/domain/values/catalogoTipoVisita.ts`
- Create: `src/modules/desempeno/domain/entities/Conteo.ts`
- Create: `src/modules/desempeno/domain/entities/Actividad.ts`
- Create: `src/modules/desempeno/domain/entities/Linea.ts`
- Create: `src/modules/desempeno/domain/entities/Senal.ts`
- Create: `src/modules/desempeno/domain/entities/Tablero.ts`
- Create: `src/modules/desempeno/domain/entities/Jornada.ts`
- Create: `src/modules/desempeno/domain/entities/ActividadZona.ts`
- Create: `src/modules/desempeno/domain/entities/Cuentas.ts`
- Create: `src/modules/desempeno/domain/entities/Imagen.ts`
- Create: `src/modules/desempeno/domain/entities/index.ts`
- Create: `src/modules/desempeno/application/ports/DesempenoPort.ts`
- Create: `src/modules/desempeno/application/usecases/consultas.ts`
- Create: `src/modules/desempeno/application/__tests__/fakeDesempenoPort.ts`
- Test: `src/modules/desempeno/domain/__tests__/dominio.test.ts`
- Test: `src/modules/desempeno/application/usecases/consultas.test.ts`

**Interfaces:**
- Consumes: nada.
- Produces (todas las tareas siguientes dependen de estos nombres exactos):
  - `DomainError(code: string, message: string)` en `domain/errors.ts`.
  - Catálogos + guardas: `ESTADOS_CUENTA`/`EstadoCuenta`/`esEstadoCuenta`, `TRATOS`/`Trato`/`esTrato`, `SEGMENTOS`/`Segmento`/`esSegmento` (`values/estadoCuenta.ts`); `TIPOS_VISITA` (`values/catalogoTipoVisita.ts`); `esUno<T>(catalogo, valor)`.
  - Entidades (re-exportadas por `domain/entities/index.ts`): `Conteo`, `TipoActividad`, `TIPOS_ACTIVIDAD`, `FormaCobro`, `FORMAS_COBRO`, `OrigenRegistro`, `RegistradoPor`, `PagoDeActividad`, `VisitaDeActividad`, `Actividad`, `RefImagenes`, `refImagenesDeActividad(a): RefImagenes | null`, `PuntoLinea`, `Pausa`, `Tramo`, `HoraDelDia`, `LineaDelDia`, `ResumenPausas`, `TipoSenal`, `TIPOS_SENAL`, `Senal`, `Periodo`, `PERIODOS`, `EstadoAhora`, `ESTADOS_AHORA`, `MotivoNoConfigurado`, `MOTIVOS_NO_CONFIGURADO`, `UltimaActividadUsuario`, `PagosPorForma`, `CoberturaPorSegmento`, `UsuarioTablero`, `Tablero`, `ResumenJornada`, `Jornada`, `OrdenActividad`, `ORDENES_ACTIVIDAD`, `Direccion`, `FiltrosActividad`, `FILTROS_ACTIVIDAD_INICIALES`, `REGISTRADO_POR_OFICINA`, `LIMITE_PAGINA_ACTIVIDAD`, `TotalesActividad`, `PaginaActividad`, `UltimaActividadCuenta`, `Cuenta`, `ConteosCuentas`, `CuentasSemana`, `SegmentoFiltro`, `OrdenCuentas`, `ORDENES_CUENTAS`, `ImagenAdjunta`.
  - Puerto: `Horario`, `ConsultaJornada`, `ConsultaTablero`, `ConsultaCuentas`, `PaginaSolicitada`, `interface DesempenoPort` (6 métodos, firmas abajo; la exportación a Excel queda fuera, ver Apéndice).
  - Casos de uso (`application/usecases/consultas.ts`): `obtenerTablero`, `obtenerJornada`, `listarActividad`, `obtenerCuentas`, `listarImagenes`, `descargarImagen`.
  - Fake y builders (`application/__tests__/fakeDesempenoPort.ts`): `FakeDesempenoPort`, `makeRegistradoPor`, `makeActividadPago`, `makeActividadVisita`, `makeActividadCondonacion`, `makePausa`, `makeTramo`, `makePorHora`, `makeLinea`, `makeUsuarioTablero`, `makeUsuarioSinConfigurar`, `makeTablero`, `makeSenal`, `makeJornada`, `makePaginaActividad`, `makeCuenta`, `makeCuentaSinTocar`, `makeCuentasSemana`, `makeImagen`.

- [ ] **Step 1: Escribir la prueba de dominio (falla: no existen los módulos)**

`src/modules/desempeno/domain/__tests__/dominio.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  esEstadoCuenta,
  esSegmento,
  esTrato,
  ESTADOS_CUENTA,
  TRATOS,
} from "../values/estadoCuenta";
import { TIPOS_VISITA } from "../values/catalogoTipoVisita";
import { refImagenesDeActividad, FILTROS_ACTIVIDAD_INICIALES } from "../entities";
import {
  makeActividadCondonacion,
  makeActividadPago,
  makeActividadVisita,
} from "../../application/__tests__/fakeDesempenoPort";

describe("catálogos del contrato", () => {
  it("tiene los 8 estados y los 8 tratos del teléfono, con los códigos del contrato", () => {
    expect(ESTADOS_CUENTA).toEqual([
      "pago", "abono_parcial", "visite_vuelvo", "prometio_proxima",
      "se_nego", "cita_a_una_hora", "no_estaba", "sin_tocar",
    ]);
    expect(TRATOS).toEqual([
      "pagado", "parcial", "regresas", "diferido",
      "escalar", "cita", "nadie", "sin_trabajar",
    ]);
  });

  it("las guardas aceptan solo códigos conocidos", () => {
    expect(esEstadoCuenta("no_estaba")).toBe(true);
    expect(esEstadoCuenta("NO_ESTABA")).toBe(false);
    expect(esEstadoCuenta(3)).toBe(false);
    expect(esTrato("sin_trabajar")).toBe(true);
    expect(esTrato("falta")).toBe(false);
    expect(esSegmento("sin_visitar")).toBe(true);
    // "sin_nada" es un filtro de la pestaña, no un segmento del contrato.
    expect(esSegmento("sin_nada")).toBe(false);
  });

  it("el catálogo de tipo de visita trae los 14 literales del servidor", () => {
    expect(TIPOS_VISITA).toHaveLength(14);
    expect(TIPOS_VISITA).toContain("No se encontraba");
    expect(TIPOS_VISITA).toContain("Dijo que no va a pagar");
  });
});

describe("refImagenesDeActividad", () => {
  it("un pago con pago_recibido_id y fotos apunta a las fotos del pago", () => {
    const ref = refImagenesDeActividad(makeActividadPago());
    expect(ref).toEqual({
      tipo: "pago",
      pagoRecibidoId: "7a1f39e4-2b6c-4d1e-9f0a-3c5b8e2d1a47",
    });
  });

  it("una visita con fotos apunta a las fotos de la visita", () => {
    expect(refImagenesDeActividad(makeActividadVisita())).toEqual({
      tipo: "visita",
      visitaId: "412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e",
    });
  });

  it("sin fotos no hay a dónde ir", () => {
    expect(refImagenesDeActividad(makeActividadPago({ numFotos: 0 }))).toBeNull();
  });

  it("un pago sin pago_recibido_id (Microsip, legado) no tiene fotos que pedir", () => {
    const pago = makeActividadPago();
    const sinId = makeActividadPago({
      pago: { ...pago.pago!, pagoRecibidoId: null },
    });
    expect(refImagenesDeActividad(sinId)).toBeNull();
    expect(refImagenesDeActividad(makeActividadCondonacion())).toBeNull();
  });
});

describe("FILTROS_ACTIVIDAD_INICIALES", () => {
  it("ordena por hora descendente y no filtra nada (default del contrato §4)", () => {
    expect(FILTROS_ACTIVIDAD_INICIALES.orden).toBe("hora");
    expect(FILTROS_ACTIVIDAD_INICIALES.dir).toBe("desc");
    expect(FILTROS_ACTIVIDAD_INICIALES.tipos).toEqual([]);
    expect(FILTROS_ACTIVIDAD_INICIALES.desde).toBeNull();
  });
});
```

- [ ] **Step 2: Correr la prueba y verla fallar**

Run: `npx vitest run src/modules/desempeno/domain/__tests__/dominio.test.ts`
Expected: FAIL con `Failed to resolve import "../values/estadoCuenta"`.

- [ ] **Step 3: Escribir dominio**

`src/modules/desempeno/domain/errors.ts`:

```ts
// DomainError sigue la convención apperror del backend: `code` estable en
// inglés snake_case y `message` en español para la persona.
export class DomainError extends Error {
  constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "DomainError";
  }
}
```

`src/modules/desempeno/domain/values/esUno.ts`:

```ts
// esUno comprueba que un valor desconocido pertenezca a un catálogo cerrado
// de cadenas. Lo usan los mappers para rechazar códigos que el contrato no
// conoce, en vez de dejarlos pasar como si fueran válidos.
export function esUno<T extends string>(
  catalogo: readonly T[],
  valor: unknown,
): valor is T {
  return typeof valor === "string" && (catalogo as readonly string[]).includes(valor);
}
```

`src/modules/desempeno/domain/values/estadoCuenta.ts`:

```ts
import { esUno } from "./esUno";

// Códigos fijos del contrato §1. La etiqueta y el detalle en español los arma
// el servidor; el escritorio no deriva ninguno de estos valores (D10).
export const ESTADOS_CUENTA = [
  "pago",
  "abono_parcial",
  "visite_vuelvo",
  "prometio_proxima",
  "se_nego",
  "cita_a_una_hora",
  "no_estaba",
  "sin_tocar",
] as const;
export type EstadoCuenta = (typeof ESTADOS_CUENTA)[number];

export const TRATOS = [
  "pagado",
  "parcial",
  "regresas",
  "diferido",
  "escalar",
  "cita",
  "nadie",
  "sin_trabajar",
] as const;
export type Trato = (typeof TRATOS)[number];

export const SEGMENTOS = ["sin_visitar", "volver", "despues", "pagados"] as const;
export type Segmento = (typeof SEGMENTOS)[number];

export const esEstadoCuenta = (v: unknown): v is EstadoCuenta => esUno(ESTADOS_CUENTA, v);
export const esTrato = (v: unknown): v is Trato => esUno(TRATOS, v);
export const esSegmento = (v: unknown): v is Segmento => esUno(SEGMENTOS, v);
```

`src/modules/desempeno/domain/values/catalogoTipoVisita.ts`:

```ts
// Literales exactos de TIPO_VISITA que acepta el filtro `tipo_visita` del API
// (contrato §4: "literal exacto"). Fuente: msp-api
// internal/visitas/domain/tipo_visita.go, catálogo cerrado de 14 (11 de la app
// actual + 3 retirados que siguen llegando de teléfonos viejos).
// El contrato no expone un endpoint de catálogo; esta copia existe solo para
// poblar el filtro y se quita el día que el servidor lo exponga.
export const TIPOS_VISITA = [
  "No se encontraba",
  "Casa cerrada con candado",
  "Solo había menores",
  "No pagará esta ocasión",
  "Pidió que regrese otro día",
  "Tiene dinero pero no quiso pagar",
  "Fue grosero o agresivo",
  "Se asomó pero no salió",
  "No responde aunque está",
  "Se escuchan ruidos pero no abre",
  "Pidió reagendar visita",
  "No va a dar pago",
  "Dijo que no va a pagar",
  "Se esconde y no sale",
] as const;
```

`src/modules/desempeno/domain/entities/Conteo.ts`:

```ts
// Conteo: cuántos y cuánto. `importe` es decimal en string ("8600.00"):
// nunca se convierte a número salvo para formatear u ordenar.
export type Conteo = { readonly n: number; readonly importe: string };
```

`src/modules/desempeno/domain/entities/Actividad.ts`:

```ts
import type { EstadoCuenta } from "../values/estadoCuenta";

export const TIPOS_ACTIVIDAD = ["pago", "visita", "condonacion"] as const;
export type TipoActividad = (typeof TIPOS_ACTIVIDAD)[number];

export const FORMAS_COBRO = [
  "efectivo",
  "transferencia",
  "cheque",
  "condonacion",
  "otra",
] as const;
export type FormaCobro = (typeof FORMAS_COBRO)[number];

export type OrigenRegistro = "app" | "oficina";

export type RegistradoPor = {
  readonly usuarioUid: string | null;
  readonly cobradorId: number | null;
  readonly nombre: string;
  readonly origen: OrigenRegistro;
};

export type PagoDeActividad = {
  readonly importe: string;
  readonly formaCobro: FormaCobro;
  readonly formaCobroId: number;
  readonly aplicado: boolean;
  readonly rechazado: boolean;
  readonly doctoCcId: number | null;
  readonly pagoRecibidoId: string | null;
};

export type VisitaDeActividad = {
  readonly visitaId: string;
  readonly tipoVisita: string;
  readonly nota: string | null;
  readonly estado: EstadoCuenta;
};

// Actividad: un pago, una visita o una condonación (contrato §1). `hora` es la
// que registró el teléfono (o Microsip), RFC3339 UTC.
export type Actividad = {
  readonly id: string;
  readonly tipo: TipoActividad;
  readonly hora: string;
  readonly recibidoAt: string | null;
  readonly clienteId: number;
  readonly clienteNombre: string;
  readonly ventaId: number | null;
  readonly folio: string | null;
  readonly registradoPor: RegistradoPor;
  readonly lat: number | null;
  readonly lng: number | null;
  readonly conGps: boolean;
  readonly numFotos: number;
  readonly pago: PagoDeActividad | null;
  readonly visita: VisitaDeActividad | null;
};

// RefImagenes: a qué recurso del API se le piden las fotos.
export type RefImagenes =
  | { readonly tipo: "pago"; readonly pagoRecibidoId: string }
  | { readonly tipo: "visita"; readonly visitaId: string };

// refImagenesDeActividad decide de dónde salen las fotos de una actividad.
// Un pago sin pago_recibido_id (anterior a 2.18.0, Microsip o legado) no tiene
// fotos que pedir: la pantalla dice "Sin comprobante" (spec §6).
export function refImagenesDeActividad(a: Actividad): RefImagenes | null {
  if (a.numFotos === 0) return null;
  if (a.visita) return { tipo: "visita", visitaId: a.visita.visitaId };
  if (a.pago?.pagoRecibidoId) {
    return { tipo: "pago", pagoRecibidoId: a.pago.pagoRecibidoId };
  }
  return null;
}
```

`src/modules/desempeno/domain/entities/Linea.ts`:

```ts
import type { TipoActividad } from "./Actividad";
import type { Conteo } from "./Conteo";

export type PuntoLinea = {
  readonly id: string;
  readonly tipo: TipoActividad;
  readonly hora: string;
};

// Pausa: hueco ≥ pausa_min entre dos actividades (lo calcula el servidor).
export type Pausa = {
  readonly desde: string;
  readonly hasta: string;
  readonly minutos: number;
  readonly anteriorId: string | null;
  readonly siguienteId: string | null;
  readonly enCurso: boolean;
};

// Tramo: bloque continuo de trabajo (vista Bloques).
export type Tramo = {
  readonly desde: string;
  readonly hasta: string;
  readonly pagos: Conteo;
  readonly visitas: number;
  readonly condonaciones: number;
  readonly clientes: number;
};

// HoraDelDia: una entrada por hora 7..19, intervalo [h, h+1) (vista Por hora).
export type HoraDelDia = {
  readonly hora: number;
  readonly pagos: number;
  readonly visitas: number;
  readonly condonaciones: number;
  readonly importe: string;
};

export type LineaDelDia = {
  readonly dia: string;
  readonly puntos: readonly PuntoLinea[];
  readonly tramos: readonly Tramo[];
  readonly porHora: readonly HoraDelDia[];
  readonly pausas: readonly Pausa[];
};

export type ResumenPausas = {
  readonly n: number;
  readonly minutos: number;
  readonly enCurso: boolean;
};
```

`src/modules/desempeno/domain/entities/Senal.ts`:

```ts
import { esUno } from "../values/esUno";

export const TIPOS_SENAL = [
  "pausa_larga",
  "inicio_tarde",
  "fin_temprano",
  "mismo_punto",
  "captura_tardia",
  "sin_gps",
] as const;
export type TipoSenal = (typeof TIPOS_SENAL)[number];
export const esTipoSenal = (v: unknown): v is TipoSenal => esUno(TIPOS_SENAL, v);

// Senal: algo "para revisar". `detalle` y `regla` vienen en español del servidor.
export type Senal = {
  readonly tipo: TipoSenal;
  readonly actividadIds: readonly string[];
  readonly desde: string | null;
  readonly hasta: string | null;
  readonly detalle: string;
  readonly regla: string;
};
```

`src/modules/desempeno/domain/entities/Tablero.ts`:

```ts
import { esUno } from "../values/esUno";
import type { TipoActividad } from "./Actividad";
import type { Conteo } from "./Conteo";
import type { LineaDelDia, ResumenPausas } from "./Linea";

export const PERIODOS = ["hoy", "semana"] as const;
export type Periodo = (typeof PERIODOS)[number];
export const esPeriodo = (v: unknown): v is Periodo => esUno(PERIODOS, v);

export const ESTADOS_AHORA = ["activo", "sin_actividad", "no_ha_empezado"] as const;
export type EstadoAhora = (typeof ESTADOS_AHORA)[number];
export const esEstadoAhora = (v: unknown): v is EstadoAhora => esUno(ESTADOS_AHORA, v);

export const MOTIVOS_NO_CONFIGURADO = ["sin_zona", "sin_inicio_semana"] as const;
export type MotivoNoConfigurado = (typeof MOTIVOS_NO_CONFIGURADO)[number];
export const esMotivoNoConfigurado = (v: unknown): v is MotivoNoConfigurado =>
  esUno(MOTIVOS_NO_CONFIGURADO, v);

export type UltimaActividadUsuario = {
  readonly id: string;
  readonly tipo: TipoActividad;
  readonly hora: string;
  readonly clienteNombre: string;
};

export type PagosPorForma = {
  readonly efectivo: Conteo;
  readonly transferencia: Conteo;
  readonly cheque: Conteo;
};

export type CoberturaPorSegmento = {
  readonly sinVisitar: number;
  readonly volver: number;
  readonly despues: number;
  readonly pagados: number;
  readonly sinNada: number;
};

// UsuarioTablero: una fila del tablero = un usuario de Firestore (D1). Sin
// totales del equipo (D14). `cobertura`, `coberturaPorSegmento` y `cobro` son
// siempre de la semana del usuario aunque el periodo sea "hoy" (contrato §2).
export type UsuarioTablero = {
  readonly uid: string;
  readonly nombre: string;
  readonly email: string;
  readonly zonaId: number | null;
  readonly zonaNombre: string | null;
  readonly configurado: boolean;
  readonly motivoNoConfigurado: MotivoNoConfigurado | null;
  readonly inicioSemana: string | null;
  readonly ventasUniverso: number;
  readonly estadoAhora: EstadoAhora;
  readonly estadoDesde: string | null;
  readonly ultima: UltimaActividadUsuario | null;
  readonly primeraActividad: string | null;
  readonly ultimaActividad: string | null;
  readonly minutosActivos: number;
  readonly pausas: ResumenPausas;
  readonly pagos: Conteo;
  readonly pagosPorForma: PagosPorForma;
  readonly condonaciones: Conteo;
  readonly noAplicados: Conteo;
  readonly visitas: number;
  readonly cobertura: { readonly conAlgo: number; readonly total: number };
  readonly coberturaPorSegmento: CoberturaPorSegmento;
  readonly cobro: {
    readonly cobrado: string;
    readonly esperado: string;
    readonly pct: string | null;
  };
  readonly lineas: readonly LineaDelDia[];
};

export type Tablero = {
  readonly generadoAt: string;
  readonly periodo: Periodo;
  readonly pausaMin: number;
  readonly usuarios: readonly UsuarioTablero[];
};
```

`src/modules/desempeno/domain/entities/Jornada.ts`:

```ts
import type { Actividad } from "./Actividad";
import type { Conteo } from "./Conteo";
import type { LineaDelDia, ResumenPausas } from "./Linea";
import type { Senal } from "./Senal";

export type ResumenJornada = {
  readonly primeraActividad: string | null;
  readonly ultimaActividad: string | null;
  readonly minutosActivos: number;
  readonly pausas: ResumenPausas;
  readonly pagos: Conteo;
  readonly visitas: number;
  readonly condonaciones: number;
  readonly clientes: number;
};

// Jornada: lo que registró ESTE usuario en un día (contrato §3).
export type Jornada = {
  readonly uid: string;
  readonly fecha: string;
  readonly pausaMin: number;
  readonly actividades: readonly Actividad[];
  readonly linea: LineaDelDia;
  readonly senales: readonly Senal[];
  readonly resumen: ResumenJornada;
};
```

`src/modules/desempeno/domain/entities/ActividadZona.ts`:

```ts
import type { Actividad, FormaCobro, TipoActividad } from "./Actividad";

export const ORDENES_ACTIVIDAD = [
  "hora",
  "cliente",
  "monto",
  "tipo",
  "registrado_por",
] as const;
export type OrdenActividad = (typeof ORDENES_ACTIVIDAD)[number];
export type Direccion = "asc" | "desc";

// Valor de `registrado_por` que pide lo capturado directo en Microsip.
export const REGISTRADO_POR_OFICINA = "oficina";

export const LIMITE_PAGINA_ACTIVIDAD = 100;

// FiltrosActividad: espejo de los parámetros de GET .../actividad (contrato §4).
// `desde`/`hasta` son instantes RFC3339 (los arma la UI con tiempoDeNegocio).
export type FiltrosActividad = {
  readonly desde: string | null;
  readonly hasta: string | null;
  readonly tipos: readonly TipoActividad[];
  readonly formasCobro: readonly FormaCobro[];
  readonly tiposVisita: readonly string[];
  readonly registradoPor: readonly string[];
  readonly montoMin: string | null;
  readonly montoMax: string | null;
  readonly conFotos: boolean | null;
  readonly conGps: boolean | null;
  readonly q: string;
  readonly orden: OrdenActividad;
  readonly dir: Direccion;
};

export const FILTROS_ACTIVIDAD_INICIALES: FiltrosActividad = {
  desde: null,
  hasta: null,
  tipos: [],
  formasCobro: [],
  tiposVisita: [],
  registradoPor: [],
  montoMin: null,
  montoMax: null,
  conFotos: null,
  conGps: null,
  q: "",
  orden: "hora",
  dir: "desc",
};

export type TotalesActividad = {
  readonly n: number;
  readonly importe: string;
  readonly porTipo: {
    readonly pago: number;
    readonly visita: number;
    readonly condonacion: number;
  };
};

export type PaginaActividad = {
  readonly items: readonly Actividad[];
  readonly siguienteCursor: string | null;
  readonly totales: TotalesActividad;
};
```

`src/modules/desempeno/domain/entities/Cuentas.ts`:

```ts
import type { EstadoCuenta, Segmento, Trato } from "../values/estadoCuenta";
import type { Direccion } from "./ActividadZona";
import type { TipoActividad } from "./Actividad";

export type UltimaActividadCuenta = {
  readonly id: string;
  readonly tipo: TipoActividad;
  readonly hora: string;
};

// Cuenta: una venta del universo de la semana del usuario (contrato §5).
export type Cuenta = {
  readonly ventaId: number;
  readonly doctoPvId: number | null;
  readonly folio: string | null;
  readonly clienteId: number;
  readonly clienteNombre: string;
  readonly producto: string | null;
  readonly estado: EstadoCuenta;
  readonly trato: Trato;
  readonly etiqueta: string;
  readonly detalle: string;
  readonly segmento: Segmento | null;
  readonly conAlgo: boolean;
  readonly determinadoPor: string | null;
  readonly ultimaActividad: UltimaActividadCuenta | null;
  readonly saldo: string;
  readonly parcialidad: string;
  readonly abonoSemana: string;
  readonly numAtrasos: number;
  readonly nueva: boolean;
};

export type ConteosCuentas = {
  readonly sinVisitar: number;
  readonly volver: number;
  readonly despues: number;
  readonly pagados: number;
  readonly sinNada: number;
  readonly total: number;
};

export type CuentasSemana = {
  readonly inicioSemana: string;
  readonly conteos: ConteosCuentas;
  readonly cuentas: readonly Cuenta[];
};

export type SegmentoFiltro = Segmento | "sin_nada";

export const ORDENES_CUENTAS = ["cliente", "saldo", "atrasos", "ultima_actividad"] as const;
export type OrdenCuentas = (typeof ORDENES_CUENTAS)[number];

export type { Direccion };
```

`src/modules/desempeno/domain/entities/Imagen.ts`:

```ts
// ImagenAdjunta: foto de un pago o de una visita (contrato §6, misma forma que
// ImagenPagoDTO del API).
export type ImagenAdjunta = {
  readonly id: string;
  readonly mime: string;
  readonly sizeBytes: number;
  readonly descripcion: string | null;
  readonly createdAt: string;
  readonly updatedAt: string;
};
```

`src/modules/desempeno/domain/entities/index.ts`:

```ts
export type { Conteo } from "./Conteo";
export {
  TIPOS_ACTIVIDAD,
  FORMAS_COBRO,
  refImagenesDeActividad,
} from "./Actividad";
export type {
  TipoActividad,
  FormaCobro,
  OrigenRegistro,
  RegistradoPor,
  PagoDeActividad,
  VisitaDeActividad,
  Actividad,
  RefImagenes,
} from "./Actividad";
export type {
  PuntoLinea,
  Pausa,
  Tramo,
  HoraDelDia,
  LineaDelDia,
  ResumenPausas,
} from "./Linea";
export { TIPOS_SENAL, esTipoSenal } from "./Senal";
export type { TipoSenal, Senal } from "./Senal";
export {
  PERIODOS,
  esPeriodo,
  ESTADOS_AHORA,
  esEstadoAhora,
  MOTIVOS_NO_CONFIGURADO,
  esMotivoNoConfigurado,
} from "./Tablero";
export type {
  Periodo,
  EstadoAhora,
  MotivoNoConfigurado,
  UltimaActividadUsuario,
  PagosPorForma,
  CoberturaPorSegmento,
  UsuarioTablero,
  Tablero,
} from "./Tablero";
export type { ResumenJornada, Jornada } from "./Jornada";
export {
  ORDENES_ACTIVIDAD,
  REGISTRADO_POR_OFICINA,
  LIMITE_PAGINA_ACTIVIDAD,
  FILTROS_ACTIVIDAD_INICIALES,
} from "./ActividadZona";
export type {
  OrdenActividad,
  Direccion,
  FiltrosActividad,
  TotalesActividad,
  PaginaActividad,
} from "./ActividadZona";
export { ORDENES_CUENTAS } from "./Cuentas";
export type {
  UltimaActividadCuenta,
  Cuenta,
  ConteosCuentas,
  CuentasSemana,
  SegmentoFiltro,
  OrdenCuentas,
} from "./Cuentas";
export type { ImagenAdjunta } from "./Imagen";
```

- [ ] **Step 4: Escribir el puerto**

`src/modules/desempeno/application/ports/DesempenoPort.ts`:

```ts
import type {
  CuentasSemana,
  Direccion,
  FiltrosActividad,
  ImagenAdjunta,
  Jornada,
  OrdenCuentas,
  PaginaActividad,
  Periodo,
  RefImagenes,
  SegmentoFiltro,
  Tablero,
} from "../../domain/entities";

// Horario opcional de la consulta (D7): "HH:MM", ambos o ninguno.
export type Horario = { readonly desde: string; readonly hasta: string };

export type ConsultaJornada = {
  readonly pausaMin: number;
  readonly horario: Horario | null;
};

export type ConsultaTablero = ConsultaJornada & { readonly periodo: Periodo };

export type ConsultaCuentas = {
  readonly segmento: SegmentoFiltro | null;
  readonly q: string;
  readonly orden: OrdenCuentas;
  readonly dir: Direccion;
};

export type PaginaSolicitada = {
  readonly cursor: string | null;
  readonly limit: number;
};

// DesempenoPort: un método por endpoint de los contratos. El adapter HTTP lo
// cumple en producción; FakeDesempenoPort en pruebas y mientras no hay API.
export interface DesempenoPort {
  obtenerTablero(consulta: ConsultaTablero, signal?: AbortSignal): Promise<Tablero>;
  obtenerJornada(
    uid: string,
    fecha: string,
    consulta: ConsultaJornada,
    signal?: AbortSignal,
  ): Promise<Jornada>;
  listarActividad(
    uid: string,
    filtros: FiltrosActividad,
    pagina: PaginaSolicitada,
    signal?: AbortSignal,
  ): Promise<PaginaActividad>;
  obtenerCuentas(
    uid: string,
    consulta: ConsultaCuentas,
    signal?: AbortSignal,
  ): Promise<CuentasSemana>;
  listarImagenes(ref: RefImagenes, signal?: AbortSignal): Promise<ImagenAdjunta[]>;
  descargarImagen(
    ref: RefImagenes,
    imagenId: string,
    signal?: AbortSignal,
  ): Promise<Blob>;
}
```

- [ ] **Step 5: Escribir el puerto falso y los builders**

`src/modules/desempeno/application/__tests__/fakeDesempenoPort.ts`:

```ts
import type {
  Actividad,
  Cuenta,
  CuentasSemana,
  FiltrosActividad,
  HoraDelDia,
  ImagenAdjunta,
  Jornada,
  LineaDelDia,
  PaginaActividad,
  Pausa,
  RefImagenes,
  RegistradoPor,
  Senal,
  Tablero,
  Tramo,
  UsuarioTablero,
} from "../../domain/entities";
import type {
  ConsultaCuentas,
  ConsultaJornada,
  ConsultaTablero,
  DesempenoPort,
  PaginaSolicitada,
} from "../ports/DesempenoPort";

// Datos de ejemplo del 14-oct-2026. CDMX es UTC-6 todo el año (sin horario de
// verano desde 2022): 14:20Z = 08:20 en la oficina. La suite corre en UTC, así
// que una pantalla que pinte la hora UTC cruda falla estas pruebas.

export function makeRegistradoPor(overrides: Partial<RegistradoPor> = {}): RegistradoPor {
  return {
    usuarioUid: "uid-juan",
    cobradorId: 11502,
    nombre: "JUAN CARLOS MENDEZ",
    origen: "app",
    ...overrides,
  };
}

export function makeActividadPago(overrides: Partial<Actividad> = {}): Actividad {
  return {
    id: "pago:15082369",
    tipo: "pago",
    hora: "2026-10-14T14:20:00Z",
    recibidoAt: "2026-10-14T14:20:05Z",
    clienteId: 1544855,
    clienteNombre: "JUANA ANDRADE LECHUGA",
    ventaId: 13785217,
    folio: "Z1677035",
    registradoPor: makeRegistradoPor(),
    lat: 18.4617,
    lng: -97.3928,
    conGps: true,
    numFotos: 1,
    pago: {
      importe: "150.00",
      formaCobro: "efectivo",
      formaCobroId: 157,
      aplicado: true,
      rechazado: false,
      doctoCcId: 15082369,
      pagoRecibidoId: "7a1f39e4-2b6c-4d1e-9f0a-3c5b8e2d1a47",
    },
    visita: null,
    ...overrides,
  };
}

export function makeActividadVisita(overrides: Partial<Actividad> = {}): Actividad {
  return {
    id: "visita:412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e",
    tipo: "visita",
    hora: "2026-10-14T14:45:00Z",
    recibidoAt: "2026-10-14T16:58:00Z",
    clienteId: 1602231,
    clienteNombre: "MARÍA ELENA CUAUTLE TLAPA",
    ventaId: 13790011,
    folio: "Z1680412",
    registradoPor: makeRegistradoPor(),
    lat: null,
    lng: null,
    conGps: false,
    numFotos: 2,
    pago: null,
    visita: {
      visitaId: "412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e",
      tipoVisita: "No se encontraba",
      nota: "Vecina dice que regresa el lunes",
      estado: "no_estaba",
    },
    ...overrides,
  };
}

export function makeActividadCondonacion(overrides: Partial<Actividad> = {}): Actividad {
  return {
    id: "pago:15082401",
    tipo: "condonacion",
    hora: "2026-10-14T16:40:00Z",
    recibidoAt: "2026-10-14T16:40:09Z",
    clienteId: 1577310,
    clienteNombre: "ROSA ISELA PACHECO NAVA",
    ventaId: 13788420,
    folio: "Z1679003",
    registradoPor: makeRegistradoPor(),
    lat: 18.4655,
    lng: -97.3981,
    conGps: true,
    numFotos: 0,
    pago: {
      importe: "50.00",
      formaCobro: "condonacion",
      formaCobroId: 137026,
      aplicado: true,
      rechazado: false,
      doctoCcId: 15082401,
      pagoRecibidoId: null,
    },
    visita: null,
    ...overrides,
  };
}

export function makePausa(overrides: Partial<Pausa> = {}): Pausa {
  return {
    desde: "2026-10-14T15:10:00Z",
    hasta: "2026-10-14T16:22:00Z",
    minutos: 72,
    anteriorId: "pago:15082370",
    siguienteId: "pago:15082390",
    enCurso: false,
    ...overrides,
  };
}

export function makeTramo(overrides: Partial<Tramo> = {}): Tramo {
  return {
    desde: "2026-10-14T14:20:00Z",
    hasta: "2026-10-14T15:10:00Z",
    pagos: { n: 2, importe: "300.00" },
    visitas: 1,
    condonaciones: 0,
    clientes: 3,
    ...overrides,
  };
}

const POR_HORA_EJEMPLO: Record<number, Partial<HoraDelDia>> = {
  8: { pagos: 1, visitas: 1, importe: "150.00" },
  9: { pagos: 1, importe: "150.00" },
  10: { pagos: 1, condonaciones: 1, importe: "150.00" },
};

// makePorHora devuelve las 13 entradas 7..19 del contrato.
export function makePorHora(
  conteos: Record<number, Partial<HoraDelDia>> = POR_HORA_EJEMPLO,
): HoraDelDia[] {
  const horas: HoraDelDia[] = [];
  for (let hora = 7; hora <= 19; hora++) {
    horas.push({
      hora,
      pagos: 0,
      visitas: 0,
      condonaciones: 0,
      importe: "0.00",
      ...conteos[hora],
    });
  }
  return horas;
}

export function makeLinea(overrides: Partial<LineaDelDia> = {}): LineaDelDia {
  return {
    dia: "2026-10-14",
    puntos: [
      { id: "pago:15082369", tipo: "pago", hora: "2026-10-14T14:20:00Z" },
      { id: "visita:412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e", tipo: "visita", hora: "2026-10-14T14:45:00Z" },
      { id: "pago:15082370", tipo: "pago", hora: "2026-10-14T15:10:00Z" },
      { id: "pago:15082390", tipo: "pago", hora: "2026-10-14T16:22:00Z" },
      { id: "pago:15082401", tipo: "condonacion", hora: "2026-10-14T16:40:00Z" },
    ],
    tramos: [
      makeTramo(),
      makeTramo({
        desde: "2026-10-14T16:22:00Z",
        hasta: "2026-10-14T16:40:00Z",
        pagos: { n: 1, importe: "150.00" },
        visitas: 0,
        condonaciones: 1,
        clientes: 2,
      }),
    ],
    porHora: makePorHora(),
    pausas: [makePausa()],
    ...overrides,
  };
}

export function makeUsuarioTablero(overrides: Partial<UsuarioTablero> = {}): UsuarioTablero {
  return {
    uid: "uid-juan",
    nombre: "Juan Carlos Méndez",
    email: "juan.mendez@muebleriamsp.mx",
    zonaId: 21563,
    zonaNombre: "RUTA 21",
    configurado: true,
    motivoNoConfigurado: null,
    inicioSemana: "2026-10-09T13:46:00Z",
    ventasUniverso: 232,
    estadoAhora: "activo",
    estadoDesde: "2026-10-14T16:40:00Z",
    ultima: {
      id: "pago:15082401",
      tipo: "condonacion",
      hora: "2026-10-14T16:40:00Z",
      clienteNombre: "ROSA ISELA PACHECO NAVA",
    },
    primeraActividad: "2026-10-14T14:20:00Z",
    ultimaActividad: "2026-10-14T16:40:00Z",
    minutosActivos: 346,
    pausas: { n: 1, minutos: 72, enCurso: false },
    pagos: { n: 31, importe: "8600.00" },
    pagosPorForma: {
      efectivo: { n: 28, importe: "7800.00" },
      transferencia: { n: 3, importe: "800.00" },
      cheque: { n: 0, importe: "0.00" },
    },
    condonaciones: { n: 0, importe: "0.00" },
    noAplicados: { n: 0, importe: "0.00" },
    visitas: 13,
    cobertura: { conAlgo: 186, total: 232 },
    coberturaPorSegmento: { sinVisitar: 46, volver: 20, despues: 3, pagados: 140, sinNada: 46 },
    cobro: { cobrado: "8600.00", esperado: "13050.00", pct: "65.90" },
    lineas: [makeLinea()],
    ...overrides,
  };
}

export function makeUsuarioSinConfigurar(
  overrides: Partial<UsuarioTablero> = {},
): UsuarioTablero {
  return makeUsuarioTablero({
    uid: "uid-norma",
    nombre: "Norma Velázquez Rojas",
    email: "norma.velazquez@muebleriamsp.mx",
    zonaId: 21572,
    zonaNombre: "RUTA 30",
    configurado: false,
    motivoNoConfigurado: "sin_inicio_semana",
    inicioSemana: null,
    ventasUniverso: 0,
    estadoAhora: "no_ha_empezado",
    estadoDesde: null,
    ultima: null,
    primeraActividad: null,
    ultimaActividad: null,
    minutosActivos: 0,
    pausas: { n: 0, minutos: 0, enCurso: false },
    pagos: { n: 0, importe: "0.00" },
    pagosPorForma: {
      efectivo: { n: 0, importe: "0.00" },
      transferencia: { n: 0, importe: "0.00" },
      cheque: { n: 0, importe: "0.00" },
    },
    condonaciones: { n: 0, importe: "0.00" },
    noAplicados: { n: 0, importe: "0.00" },
    visitas: 0,
    cobertura: { conAlgo: 0, total: 0 },
    coberturaPorSegmento: { sinVisitar: 0, volver: 0, despues: 0, pagados: 0, sinNada: 0 },
    cobro: { cobrado: "0.00", esperado: "0.00", pct: null },
    lineas: [],
    ...overrides,
  });
}

export function makeTablero(overrides: Partial<Tablero> = {}): Tablero {
  return {
    generadoAt: "2026-10-14T22:40:00Z",
    periodo: "hoy",
    pausaMin: 45,
    usuarios: [
      makeUsuarioTablero(),
      makeUsuarioTablero({
        uid: "uid-rosa",
        nombre: "Rosa Isela Pacheco",
        email: "rosa.pacheco@muebleriamsp.mx",
        zonaId: 21564,
        zonaNombre: "RUTA 22",
        estadoAhora: "sin_actividad",
        estadoDesde: "2026-10-14T21:05:00Z",
        pagos: { n: 24, importe: "5100.00" },
        cobertura: { conAlgo: 185, total: 246 },
        coberturaPorSegmento: { sinVisitar: 61, volver: 18, despues: 4, pagados: 163, sinNada: 61 },
        cobro: { cobrado: "5100.00", esperado: "11800.00", pct: "43.22" },
        ventasUniverso: 246,
      }),
      makeUsuarioSinConfigurar(),
    ],
    ...overrides,
  };
}

export function makeSenal(overrides: Partial<Senal> = {}): Senal {
  return {
    tipo: "pausa_larga",
    actividadIds: ["pago:15082370", "pago:15082390"],
    desde: "2026-10-14T15:10:00Z",
    hasta: "2026-10-14T16:22:00Z",
    detalle: "Pausa de 1 h 12 min",
    regla: "hueco de 45 min o más entre dos movimientos",
    ...overrides,
  };
}

export function makeJornada(overrides: Partial<Jornada> = {}): Jornada {
  return {
    uid: "uid-juan",
    fecha: "2026-10-14",
    pausaMin: 45,
    actividades: [
      makeActividadPago(),
      makeActividadVisita(),
      makeActividadPago({
        id: "pago:15082370",
        hora: "2026-10-14T15:10:00Z",
        clienteId: 1590077,
        clienteNombre: "PEDRO TEHUITZIL ZEPEDA",
        folio: "Z1678820",
      }),
      makeActividadPago({
        id: "pago:15082390",
        hora: "2026-10-14T16:22:00Z",
        clienteId: 1561200,
        clienteNombre: "ARACELI CORTÉS LUNA",
        folio: "Z1676110",
        numFotos: 0,
      }),
      makeActividadCondonacion(),
    ],
    linea: makeLinea(),
    senales: [
      makeSenal(),
      makeSenal({
        tipo: "sin_gps",
        actividadIds: ["visita:412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e"],
        desde: null,
        hasta: null,
        detalle: "Visita sin ubicación",
        regla: "el teléfono no mandó coordenadas",
      }),
    ],
    resumen: {
      primeraActividad: "2026-10-14T14:20:00Z",
      ultimaActividad: "2026-10-14T16:40:00Z",
      minutosActivos: 68,
      pausas: { n: 1, minutos: 72, enCurso: false },
      pagos: { n: 3, importe: "450.00" },
      visitas: 1,
      condonaciones: 1,
      clientes: 5,
    },
    ...overrides,
  };
}

export function makePaginaActividad(overrides: Partial<PaginaActividad> = {}): PaginaActividad {
  return {
    items: makeJornada().actividades,
    siguienteCursor: null,
    totales: { n: 5, importe: "500.00", porTipo: { pago: 3, visita: 1, condonacion: 1 } },
    ...overrides,
  };
}

export function makeCuenta(overrides: Partial<Cuenta> = {}): Cuenta {
  return {
    ventaId: 13785217,
    doctoPvId: 9921,
    folio: "Z1677035",
    clienteId: 1544855,
    clienteNombre: "JUANA ANDRADE LECHUGA",
    producto: "ROPERO LABRADO CALIFORNIA VINO",
    estado: "pago",
    trato: "pagado",
    etiqueta: "Pagó esta semana",
    detalle: "No hace falta volver",
    segmento: "pagados",
    conAlgo: true,
    determinadoPor: "pago:15082369",
    ultimaActividad: { id: "pago:15082369", tipo: "pago", hora: "2026-10-14T14:20:00Z" },
    saldo: "16650.00",
    parcialidad: "150.00",
    abonoSemana: "150.00",
    numAtrasos: 0,
    nueva: false,
    ...overrides,
  };
}

export function makeCuentaSinTocar(overrides: Partial<Cuenta> = {}): Cuenta {
  return makeCuenta({
    ventaId: 13791100,
    doctoPvId: 9950,
    folio: "Z1681007",
    clienteId: 1610045,
    clienteNombre: "GERARDO XICALE ANALCO",
    producto: "COMEDOR 6 SILLAS TOSCANA",
    estado: "sin_tocar",
    trato: "sin_trabajar",
    etiqueta: "Falta pasar",
    detalle: "Todavía no vas",
    segmento: "sin_visitar",
    conAlgo: false,
    determinadoPor: null,
    ultimaActividad: null,
    saldo: "8400.00",
    parcialidad: "200.00",
    abonoSemana: "0.00",
    numAtrasos: 6,
    ...overrides,
  });
}

export function makeCuentasSemana(overrides: Partial<CuentasSemana> = {}): CuentasSemana {
  return {
    inicioSemana: "2026-10-09T13:46:00Z",
    conteos: { sinVisitar: 46, volver: 20, despues: 3, pagados: 140, sinNada: 46, total: 232 },
    cuentas: [makeCuenta(), makeCuentaSinTocar()],
    ...overrides,
  };
}

export function makeImagen(overrides: Partial<ImagenAdjunta> = {}): ImagenAdjunta {
  return {
    id: "0b6f1c2e-5a7d-4e3b-8c9f-1d2e3f4a5b6c",
    mime: "image/jpeg",
    sizeBytes: 184320,
    descripcion: "Recibo firmado",
    createdAt: "2026-10-14T14:20:10Z",
    updatedAt: "2026-10-14T14:20:10Z",
    ...overrides,
  };
}

type Respuesta<T> = T | (() => T | Promise<T>);

async function resolver<T>(r: Respuesta<T>): Promise<T> {
  return typeof r === "function" ? (r as () => T | Promise<T>)() : r;
}

// FakeDesempenoPort: implementación en memoria que registra cada llamada. Las
// pruebas de hooks y componentes la usan en vez de MSW.
export class FakeDesempenoPort implements DesempenoPort {
  tableroCalls: Array<{ consulta: ConsultaTablero }> = [];
  jornadaCalls: Array<{ uid: string; fecha: string; consulta: ConsultaJornada }> = [];
  actividadCalls: Array<{ uid: string; filtros: FiltrosActividad; pagina: PaginaSolicitada }> = [];
  cuentasCalls: Array<{ uid: string; consulta: ConsultaCuentas }> = [];
  imagenesCalls: Array<{ ref: RefImagenes }> = [];
  descargarCalls: Array<{ ref: RefImagenes; imagenId: string }> = [];

  tableroResponse: Respuesta<Tablero> = makeTablero();
  jornadaResponse: Respuesta<Jornada> = makeJornada();
  actividadResponse:
    | PaginaActividad
    | ((pagina: PaginaSolicitada) => PaginaActividad | Promise<PaginaActividad>) =
    makePaginaActividad();
  cuentasResponse: Respuesta<CuentasSemana> = makeCuentasSemana();
  imagenesResponse: Respuesta<ImagenAdjunta[]> = [makeImagen()];
  descargarResponse: Respuesta<Blob> = new Blob(["jpg"], { type: "image/jpeg" });

  // Si se llena, la siguiente llamada a ese método lanza este error.
  throwOnNext: Partial<Record<keyof DesempenoPort, Error>> = {};

  async obtenerTablero(consulta: ConsultaTablero): Promise<Tablero> {
    this.tableroCalls.push({ consulta });
    this.lanzarSiToca("obtenerTablero");
    return resolver(this.tableroResponse);
  }

  async obtenerJornada(uid: string, fecha: string, consulta: ConsultaJornada): Promise<Jornada> {
    this.jornadaCalls.push({ uid, fecha, consulta });
    this.lanzarSiToca("obtenerJornada");
    return resolver(this.jornadaResponse);
  }

  async listarActividad(
    uid: string,
    filtros: FiltrosActividad,
    pagina: PaginaSolicitada,
  ): Promise<PaginaActividad> {
    this.actividadCalls.push({ uid, filtros, pagina });
    this.lanzarSiToca("listarActividad");
    const r = this.actividadResponse;
    return typeof r === "function" ? r(pagina) : r;
  }

  async obtenerCuentas(uid: string, consulta: ConsultaCuentas): Promise<CuentasSemana> {
    this.cuentasCalls.push({ uid, consulta });
    this.lanzarSiToca("obtenerCuentas");
    return resolver(this.cuentasResponse);
  }

  async listarImagenes(ref: RefImagenes): Promise<ImagenAdjunta[]> {
    this.imagenesCalls.push({ ref });
    this.lanzarSiToca("listarImagenes");
    return resolver(this.imagenesResponse);
  }

  async descargarImagen(ref: RefImagenes, imagenId: string): Promise<Blob> {
    this.descargarCalls.push({ ref, imagenId });
    this.lanzarSiToca("descargarImagen");
    return resolver(this.descargarResponse);
  }

  private lanzarSiToca(metodo: keyof DesempenoPort): void {
    const e = this.throwOnNext[metodo];
    if (e) {
      delete this.throwOnNext[metodo];
      throw e;
    }
  }
}
```

- [ ] **Step 6: Correr la prueba de dominio y verla pasar**

Run: `npx vitest run src/modules/desempeno/domain/__tests__/dominio.test.ts`
Expected: PASS (8 pruebas).

- [ ] **Step 7: Escribir la prueba de los casos de uso (falla: no existe `consultas.ts`)**

`src/modules/desempeno/application/usecases/consultas.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  descargarImagen,
  listarActividad,
  listarImagenes,
  obtenerCuentas,
  obtenerJornada,
  obtenerTablero,
} from "./consultas";
import { FakeDesempenoPort, makeTablero } from "../__tests__/fakeDesempenoPort";
import { FILTROS_ACTIVIDAD_INICIALES } from "../../domain/entities";

describe("casos de uso de desempeño (pass-through al puerto)", () => {
  it("obtenerTablero reenvía la consulta completa", async () => {
    const port = new FakeDesempenoPort();
    port.tableroResponse = makeTablero({ periodo: "semana" });
    const consulta = { periodo: "semana" as const, pausaMin: 30, horario: { desde: "08:00", hasta: "18:00" } };

    const t = await obtenerTablero(port, consulta);

    expect(port.tableroCalls).toEqual([{ consulta }]);
    expect(t.periodo).toBe("semana");
  });

  it("obtenerJornada reenvía uid, fecha y consulta", async () => {
    const port = new FakeDesempenoPort();
    await obtenerJornada(port, "uid-juan", "2026-10-13", { pausaMin: 45, horario: null });
    expect(port.jornadaCalls).toEqual([
      { uid: "uid-juan", fecha: "2026-10-13", consulta: { pausaMin: 45, horario: null } },
    ]);
  });

  it("listarActividad reenvía los filtros sin tocarlos", async () => {
    const port = new FakeDesempenoPort();
    const filtros = { ...FILTROS_ACTIVIDAD_INICIALES, tipos: ["pago" as const], q: "andrade" };
    await listarActividad(port, "uid-juan", filtros, { cursor: "c1", limit: 100 });
    expect(port.actividadCalls[0]).toEqual({ uid: "uid-juan", filtros, pagina: { cursor: "c1", limit: 100 } });
  });

  it("obtenerCuentas, listarImagenes y descargarImagen reenvían sus argumentos", async () => {
    const port = new FakeDesempenoPort();
    const consulta = { segmento: "sin_nada" as const, q: "", orden: "atrasos" as const, dir: "desc" as const };
    const ref = { tipo: "visita" as const, visitaId: "v-1" };
    await obtenerCuentas(port, "uid-juan", consulta);
    await listarImagenes(port, ref);
    await descargarImagen(port, ref, "img-1");
    expect(port.cuentasCalls[0]).toEqual({ uid: "uid-juan", consulta });
    expect(port.imagenesCalls[0]).toEqual({ ref });
    expect(port.descargarCalls[0]).toEqual({ ref, imagenId: "img-1" });
  });

  it("propaga el error del puerto tal cual", async () => {
    const port = new FakeDesempenoPort();
    port.throwOnNext.obtenerTablero = new Error("se cayó");
    await expect(obtenerTablero(port, { periodo: "hoy", pausaMin: 45, horario: null })).rejects.toThrow("se cayó");
  });
});
```

- [ ] **Step 8: Correr y verla fallar**

Run: `npx vitest run src/modules/desempeno/application/usecases/consultas.test.ts`
Expected: FAIL con `Failed to resolve import "./consultas"`.

- [ ] **Step 9: Escribir los casos de uso**

`src/modules/desempeno/application/usecases/consultas.ts`:

```ts
import type {
  CuentasSemana,
  FiltrosActividad,
  ImagenAdjunta,
  Jornada,
  PaginaActividad,
  RefImagenes,
  Tablero,
} from "../../domain/entities";
import type {
  ConsultaCuentas,
  ConsultaJornada,
  ConsultaTablero,
  DesempenoPort,
  PaginaSolicitada,
} from "../ports/DesempenoPort";

// Casos de uso de solo lectura. Son pass-through a propósito (README de
// ventasLocales): la indirección deja meter telemetría o caché sin que la
// presentación toque infraestructura. Ninguno calcula nada (D10).

export function obtenerTablero(
  port: DesempenoPort,
  consulta: ConsultaTablero,
  signal?: AbortSignal,
): Promise<Tablero> {
  return port.obtenerTablero(consulta, signal);
}

export function obtenerJornada(
  port: DesempenoPort,
  uid: string,
  fecha: string,
  consulta: ConsultaJornada,
  signal?: AbortSignal,
): Promise<Jornada> {
  return port.obtenerJornada(uid, fecha, consulta, signal);
}

export function listarActividad(
  port: DesempenoPort,
  uid: string,
  filtros: FiltrosActividad,
  pagina: PaginaSolicitada,
  signal?: AbortSignal,
): Promise<PaginaActividad> {
  return port.listarActividad(uid, filtros, pagina, signal);
}

export function obtenerCuentas(
  port: DesempenoPort,
  uid: string,
  consulta: ConsultaCuentas,
  signal?: AbortSignal,
): Promise<CuentasSemana> {
  return port.obtenerCuentas(uid, consulta, signal);
}

export function listarImagenes(
  port: DesempenoPort,
  ref: RefImagenes,
  signal?: AbortSignal,
): Promise<ImagenAdjunta[]> {
  return port.listarImagenes(ref, signal);
}

export function descargarImagen(
  port: DesempenoPort,
  ref: RefImagenes,
  imagenId: string,
  signal?: AbortSignal,
): Promise<Blob> {
  return port.descargarImagen(ref, imagenId, signal);
}
```

- [ ] **Step 10: Correr las pruebas de la tarea, tipos y lint**

Run: `npx vitest run src/modules/desempeno && npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: PASS (13 pruebas), `tsc` sin salida, eslint sin salida.

- [ ] **Step 11: Commit**

```bash
git add src/modules/desempeno/domain src/modules/desempeno/application
git commit -m "feat(desempeno): dominio, puerto y puerto falso según los contratos del API"
```

---

### Tarea 0 — Parte B: base de presentación (hover card, tokens, contexto, preferencias, carga remota)

**Rama `feat/desempeno`, después de la Parte A.**

**Files:**
- Modify: `package.json`, `package-lock.json` (dependencia `@radix-ui/react-hover-card`)
- Create: `src/components/ui/hover-card.tsx`
- Modify: `src/index.css` (tokens `--dsm-*` en `:root` y `.dark`, clases `.dsm-pausa` y `.dsm-fuera-horario`)
- Modify: `tailwind.config.js` (`theme.extend.colors.dsm`)
- Create: `src/modules/desempeno/presentation/preferencias.ts`
- Create: `src/modules/desempeno/presentation/context/contextos.ts`
- Create: `src/modules/desempeno/presentation/context/PreferenciasProvider.tsx`
- Create: `src/modules/desempeno/presentation/context/DesempenoProvider.tsx`
- Create: `src/modules/desempeno/presentation/context/useDesempenoPort.ts`
- Create: `src/modules/desempeno/presentation/context/usePreferencias.ts`
- Create: `src/modules/desempeno/presentation/hooks/lib/toDomainError.ts`
- Create: `src/modules/desempeno/presentation/hooks/lib/almacenLocal.ts`
- Create: `src/modules/desempeno/presentation/hooks/lib/useCargaRemota.ts`
- Create: `src/modules/desempeno/presentation/hooks/lib/useCadaMinuto.ts`
- Create: `src/modules/desempeno/presentation/hooks/lib/useEsPantallaAngosta.ts`
- Create: `src/modules/desempeno/test/renderConDesempeno.tsx`
- Test: `src/modules/desempeno/presentation/hooks/lib/__tests__/almacenLocal.test.ts`
- Test: `src/modules/desempeno/presentation/hooks/lib/__tests__/useCargaRemota.test.tsx`
- Test: `src/modules/desempeno/presentation/hooks/lib/__tests__/useCadaMinuto.test.tsx`
- Test: `src/modules/desempeno/presentation/context/__tests__/preferencias.test.tsx`

**Interfaces:**
- Consumes (Parte A): `DesempenoPort`, `ConsultaJornada`, `ConsultaTablero`, `Horario`, `Periodo`, `DomainError`, `FakeDesempenoPort`.
- Produces:
  - `src/components/ui/hover-card.tsx`: `HoverCard`, `HoverCardTrigger`, `HoverCardContent`, `HoverCardPortal`.
  - Clases Tailwind `bg-dsm-canvas`, `bg-dsm-tarjeta`, `{bg,text}-dsm-{marca,ok,teal,ambar,rojo,violeta,gris,visita}`, `bg-dsm-{marca,ok,teal,ambar,rojo,violeta,gris}-fondo`; clases CSS `dsm-pausa`, `dsm-fuera-horario`.
  - `presentation/preferencias.ts`: `VISTAS_LINEA`, `type VistaLinea`, `VISTA_LINEA_PREDETERMINADA`, `CLAVE_VISTA_LINEA = "desempeno.vista-linea"`, `UMBRALES_PAUSA`, `PAUSA_MIN_PREDETERMINADA`, `HORARIO_PREDETERMINADO`, `esVistaLinea`, `type PreferenciasDesempeno`, `type ValorPreferencias`.
  - `DesempenoProvider({ port, preferenciasIniciales?, children })`, `useDesempenoPort(): DesempenoPort`, `usePreferencias(): ValorPreferencias` (`periodo, pausaMin, horario, vista, consultaJornada, consultaTablero, setPeriodo, setPausaMin, setHorario, setVista`).
  - `useCargaRemota<T>(cargar: ((s: AbortSignal) => Promise<T>) | null): CargaRemota<T>` con `{ datos, isLoading, error, refresh }`.
  - `useCadaMinuto(alTocar: () => void, activo: boolean)`, `UN_MINUTO_MS`.
  - `useEsPantallaAngosta(): boolean`.
  - `leerLocal<T>(clave, defecto, valido)`, `escribirLocal(clave, valor)`.
  - `toDomainError(e: unknown): DomainError`.
  - `renderConDesempeno(ui, { port?, ruta?, preferencias? })` → `RenderResult & { port: FakeDesempenoPort }`.

- [ ] **Step 1: Instalar la dependencia del hover card**

Run: `npm install @radix-ui/react-hover-card@^1.1.15`
Expected: `package.json` gana `"@radix-ui/react-hover-card": "^1.1.15"` en `dependencies` y `package-lock.json` se actualiza. (Equivale a lo que instala `npx shadcn@latest add hover-card`; se hace a mano para no depender del asistente interactivo.)

- [ ] **Step 2: Crear el componente shadcn `hover-card`**

`src/components/ui/hover-card.tsx` (estilo new-york de `components.json`, con `Portal` exportado para que el contenido no quede recortado dentro de tablas con `overflow`):

```tsx
import * as React from "react"
import * as HoverCardPrimitive from "@radix-ui/react-hover-card"

import { cn } from "@/lib/utils"

const HoverCard = HoverCardPrimitive.Root

const HoverCardTrigger = HoverCardPrimitive.Trigger

const HoverCardPortal = HoverCardPrimitive.Portal

const HoverCardContent = React.forwardRef<
  React.ElementRef<typeof HoverCardPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof HoverCardPrimitive.Content>
>(({ className, align = "center", sideOffset = 4, ...props }, ref) => (
  <HoverCardPrimitive.Content
    ref={ref}
    align={align}
    sideOffset={sideOffset}
    className={cn(
      "z-50 w-64 rounded-md border bg-popover p-4 text-popover-foreground shadow-md outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 origin-[--radix-hover-card-content-transform-origin]",
      className
    )}
    {...props}
  />
))
HoverCardContent.displayName = HoverCardPrimitive.Content.displayName

export { HoverCard, HoverCardTrigger, HoverCardContent, HoverCardPortal }
```

- [ ] **Step 3: Agregar los tokens de color del estilo "Audaz 2026"**

En `src/index.css`, dentro de `@layer base { :root { ... } }`, inmediatamente después de la línea `    --sidebar-ring: 217.2 91.2% 59.8%;` del bloque `:root` (la primera aparición), insertar:

```css
    /* Desempeño de cobradores — estilo B "Audaz 2026" (mock aprobado 2026-10-14).
       Solo canales HSL, como el resto de los tokens: se usan con hsl(var(--x)). */
    --dsm-canvas: 220 14% 96%;
    --dsm-tarjeta: 0 0% 100%;
    --dsm-marca: 221 83% 53%;
    --dsm-marca-fondo: 221 83% 95%;
    --dsm-ok: 150 66% 27%;
    --dsm-ok-fondo: 143 32% 92%;
    --dsm-teal: 187 82% 30%;
    --dsm-teal-fondo: 186 42% 91%;
    --dsm-ambar: 35 100% 30%;
    --dsm-ambar-fondo: 35 79% 92%;
    --dsm-rojo: 343 80% 35%;
    --dsm-rojo-fondo: 344 70% 95%;
    --dsm-violeta: 252 92% 66%;
    --dsm-violeta-fondo: 253 83% 95%;
    --dsm-gris: 155 6% 38%;
    --dsm-gris-fondo: 160 9% 94%;
    --dsm-visita: 153 6% 62%;
```

Dentro de `.dark { ... }`, inmediatamente después de su línea `    --sidebar-ring: 217.2 91.2% 59.8%;` (la segunda aparición), insertar:

```css
    --dsm-canvas: 240 9% 2%;
    --dsm-tarjeta: 220 8% 7%;
    --dsm-marca: 221 100% 72%;
    --dsm-marca-fondo: 221 83% 53% / 0.22;
    --dsm-ok: 148 47% 56%;
    --dsm-ok-fondo: 150 66% 27% / 0.28;
    --dsm-teal: 186 59% 56%;
    --dsm-teal-fondo: 187 82% 30% / 0.3;
    --dsm-ambar: 36 78% 60%;
    --dsm-ambar-fondo: 35 100% 30% / 0.32;
    --dsm-rojo: 345 82% 71%;
    --dsm-rojo-fondo: 343 80% 35% / 0.34;
    --dsm-violeta: 252 100% 80%;
    --dsm-violeta-fondo: 252 92% 66% / 0.28;
    --dsm-gris: 153 6% 66%;
    --dsm-gris-fondo: 155 6% 38% / 0.32;
    --dsm-visita: 155 5% 45%;
```

Al final de `src/index.css` agregar:

```css
/* Desempeño: pausa sombreada y horas fuera del horario elegido (mock B). */
.dsm-pausa {
  background: repeating-linear-gradient(
    135deg,
    hsl(var(--dsm-ambar) / 0.17) 0 3px,
    hsl(var(--dsm-ambar) / 0.06) 3px 6px
  );
}

.dsm-fuera-horario {
  background: repeating-linear-gradient(
    135deg,
    hsl(var(--foreground) / 0.055) 0 2px,
    transparent 2px 5px
  );
}
```

En `tailwind.config.js`, dentro de `theme.extend.colors`, después del objeto `sidebar: { ... }`, agregar:

```js
  			dsm: {
  				canvas: 'hsl(var(--dsm-canvas))',
  				tarjeta: 'hsl(var(--dsm-tarjeta))',
  				marca: 'hsl(var(--dsm-marca))',
  				'marca-fondo': 'hsl(var(--dsm-marca-fondo))',
  				ok: 'hsl(var(--dsm-ok))',
  				'ok-fondo': 'hsl(var(--dsm-ok-fondo))',
  				teal: 'hsl(var(--dsm-teal))',
  				'teal-fondo': 'hsl(var(--dsm-teal-fondo))',
  				ambar: 'hsl(var(--dsm-ambar))',
  				'ambar-fondo': 'hsl(var(--dsm-ambar-fondo))',
  				rojo: 'hsl(var(--dsm-rojo))',
  				'rojo-fondo': 'hsl(var(--dsm-rojo-fondo))',
  				violeta: 'hsl(var(--dsm-violeta))',
  				'violeta-fondo': 'hsl(var(--dsm-violeta-fondo))',
  				gris: 'hsl(var(--dsm-gris))',
  				'gris-fondo': 'hsl(var(--dsm-gris-fondo))',
  				visita: 'hsl(var(--dsm-visita))'
  			}
```

(recuerda la coma después del `}` de `sidebar`).

- [ ] **Step 4: Escribir las pruebas de almacenamiento local y carga remota (fallan: no existen)**

`src/modules/desempeno/presentation/hooks/lib/__tests__/almacenLocal.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { escribirLocal, leerLocal } from "../almacenLocal";

const esTexto = (v: unknown): v is string => typeof v === "string";

describe("almacenLocal", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("devuelve el defecto si no hay nada guardado", () => {
    expect(leerLocal("k", "bloques", esTexto)).toBe("bloques");
  });

  it("lee lo que escribió", () => {
    escribirLocal("k", "por_hora");
    expect(leerLocal("k", "bloques", esTexto)).toBe("por_hora");
  });

  it("JSON roto o forma inválida → defecto", () => {
    localStorage.setItem("k", "{roto");
    expect(leerLocal("k", "bloques", esTexto)).toBe("bloques");
    localStorage.setItem("k", "42");
    expect(leerLocal("k", "bloques", esTexto)).toBe("bloques");
  });

  it("almacenamiento bloqueado (ventana privada) no tumba la pantalla", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("bloqueado");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("bloqueado");
    });
    expect(leerLocal("k", "bloques", esTexto)).toBe("bloques");
    expect(() => escribirLocal("k", "puntos")).not.toThrow();
  });
});
```

`src/modules/desempeno/presentation/hooks/lib/__tests__/useCargaRemota.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { useCargaRemota } from "../useCargaRemota";
import { DomainError } from "../../../../domain/errors";

type Cargar = ((s: AbortSignal) => Promise<string>) | null;

function diferida() {
  let resolver: (v: string) => void = () => {};
  let rechazar: (e: unknown) => void = () => {};
  const promesa = new Promise<string>((res, rej) => {
    resolver = res;
    rechazar = rej;
  });
  return { promesa, resolver, rechazar };
}

describe("useCargaRemota", () => {
  it("carga y expone los datos", async () => {
    const cargar: Cargar = async () => "hoy";
    const { result } = renderHook(() => useCargaRemota(cargar));
    expect(result.current.isLoading).toBe(true);
    await waitFor(() => expect(result.current.datos).toBe("hoy"));
    expect(result.current.isLoading).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it("con cargar=null no hace nada", () => {
    const { result } = renderHook(() => useCargaRemota<string>(null));
    expect(result.current.isLoading).toBe(false);
    expect(result.current.datos).toBeNull();
  });

  it("una recarga fallida CONSERVA los datos y expone el error", async () => {
    let llamada = 0;
    const cargar: Cargar = async () => {
      llamada++;
      if (llamada === 2) throw new DomainError("network_error", "error de red");
      return "datos de las 16:40";
    };
    const { result } = renderHook(() => useCargaRemota(cargar));
    await waitFor(() => expect(result.current.datos).toBe("datos de las 16:40"));

    act(() => result.current.refresh());

    await waitFor(() => expect(result.current.error?.code).toBe("network_error"));
    expect(result.current.datos).toBe("datos de las 16:40");
  });

  it("una consulta NUEVA borra los datos anteriores (no se pinta 'semana' con datos de 'hoy')", async () => {
    const semana = diferida();
    const porHoy: Cargar = async () => "datos de hoy";
    const porSemana: Cargar = () => semana.promesa;
    const { result, rerender } = renderHook(({ c }: { c: Cargar }) => useCargaRemota(c), {
      initialProps: { c: porHoy },
    });
    await waitFor(() => expect(result.current.datos).toBe("datos de hoy"));

    rerender({ c: porSemana });

    expect(result.current.datos).toBeNull();
    expect(result.current.isLoading).toBe(true);
    await act(async () => semana.resolver("datos de semana"));
    expect(result.current.datos).toBe("datos de semana");
  });

  it("la respuesta tardía de una consulta vieja no pisa a la nueva", async () => {
    const vieja = diferida();
    const cargarVieja: Cargar = () => vieja.promesa;
    const cargarNueva: Cargar = async () => "nueva";
    const { result, rerender } = renderHook(({ c }: { c: Cargar }) => useCargaRemota(c), {
      initialProps: { c: cargarVieja },
    });
    rerender({ c: cargarNueva });
    await waitFor(() => expect(result.current.datos).toBe("nueva"));

    await act(async () => vieja.resolver("vieja"));

    expect(result.current.datos).toBe("nueva");
  });
});
```

`src/modules/desempeno/presentation/hooks/lib/__tests__/useCadaMinuto.test.tsx`:

```tsx
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { UN_MINUTO_MS, useCadaMinuto } from "../useCadaMinuto";

describe("useCadaMinuto", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("llama cada 60 s mientras está activo", () => {
    const fn = vi.fn();
    renderHook(() => useCadaMinuto(fn, true));
    vi.advanceTimersByTime(UN_MINUTO_MS - 1);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(fn).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(UN_MINUTO_MS);
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it("inactivo no llama, y al desmontar se detiene", () => {
    const inactivo = vi.fn();
    renderHook(() => useCadaMinuto(inactivo, false));
    vi.advanceTimersByTime(3 * UN_MINUTO_MS);
    expect(inactivo).not.toHaveBeenCalled();

    const activo = vi.fn();
    const { unmount } = renderHook(() => useCadaMinuto(activo, true));
    unmount();
    vi.advanceTimersByTime(3 * UN_MINUTO_MS);
    expect(activo).not.toHaveBeenCalled();
  });
});
```

`src/modules/desempeno/presentation/context/__tests__/preferencias.test.tsx`:

```tsx
import { beforeEach, describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { DesempenoProvider } from "../DesempenoProvider";
import { usePreferencias } from "../usePreferencias";
import { useDesempenoPort } from "../useDesempenoPort";
import { CLAVE_VISTA_LINEA } from "../../preferencias";
import { FakeDesempenoPort } from "../../../application/__tests__/fakeDesempenoPort";

const port = new FakeDesempenoPort();
function envoltura({ children }: { children: ReactNode }) {
  return <DesempenoProvider port={port}>{children}</DesempenoProvider>;
}

describe("preferencias de la pantalla", () => {
  beforeEach(() => localStorage.clear());

  it("abre en Hoy, pausa de 45 min, sin horario y vista Bloques (D8, D16)", () => {
    const { result } = renderHook(() => usePreferencias(), { wrapper: envoltura });
    expect(result.current.periodo).toBe("hoy");
    expect(result.current.pausaMin).toBe(45);
    expect(result.current.horario).toBeNull();
    expect(result.current.vista).toBe("bloques");
  });

  it("la vista se recuerda por computadora", () => {
    localStorage.setItem(CLAVE_VISTA_LINEA, JSON.stringify("por_hora"));
    const { result } = renderHook(() => usePreferencias(), { wrapper: envoltura });
    expect(result.current.vista).toBe("por_hora");

    act(() => result.current.setVista("puntos"));

    expect(localStorage.getItem(CLAVE_VISTA_LINEA)).toBe(JSON.stringify("puntos"));
  });

  it("una vista guardada que ya no existe vuelve a Bloques", () => {
    localStorage.setItem(CLAVE_VISTA_LINEA, JSON.stringify("barras"));
    const { result } = renderHook(() => usePreferencias(), { wrapper: envoltura });
    expect(result.current.vista).toBe("bloques");
  });

  it("consultaTablero es estable entre renders y cambia con el periodo", () => {
    const { result, rerender } = renderHook(() => usePreferencias(), { wrapper: envoltura });
    const antes = result.current.consultaTablero;
    rerender();
    expect(result.current.consultaTablero).toBe(antes);

    act(() => result.current.setPeriodo("semana"));

    expect(result.current.consultaTablero).toEqual({ periodo: "semana", pausaMin: 45, horario: null });
  });

  it("useDesempenoPort entrega el puerto del provider", () => {
    const { result } = renderHook(() => useDesempenoPort(), { wrapper: envoltura });
    expect(result.current).toBe(port);
  });
});
```

- [ ] **Step 5: Correrlas y verlas fallar**

Run: `npx vitest run src/modules/desempeno/presentation`
Expected: FAIL con `Failed to resolve import "../almacenLocal"` (y equivalentes).

- [ ] **Step 6: Implementar**

`src/modules/desempeno/presentation/hooks/lib/toDomainError.ts`:

```ts
import { DomainError } from "../../../domain/errors";

export function toDomainError(e: unknown): DomainError {
  if (e instanceof DomainError) return e;
  if (e instanceof Error) return new DomainError("error_inesperado", e.message);
  return new DomainError("error_inesperado", String(e));
}
```

`src/modules/desempeno/presentation/hooks/lib/almacenLocal.ts`:

```ts
// Preferencias por computadora (vista de la línea, columnas, vistas guardadas).
// localStorage puede no existir o lanzar (ventana privada, datos bloqueados):
// todo acceso va en try/catch y la pantalla funciona igual sin él.

export function leerLocal<T>(
  clave: string,
  defecto: T,
  valido: (v: unknown) => v is T,
): T {
  try {
    const crudo = window.localStorage.getItem(clave);
    if (crudo === null) return defecto;
    const valor: unknown = JSON.parse(crudo);
    return valido(valor) ? valor : defecto;
  } catch {
    return defecto;
  }
}

export function escribirLocal(clave: string, valor: unknown): void {
  try {
    window.localStorage.setItem(clave, JSON.stringify(valor));
  } catch {
    // Sin almacenamiento: la preferencia vive solo en memoria esta sesión.
  }
}
```

`src/modules/desempeno/presentation/hooks/lib/useCargaRemota.ts`:

```ts
import { useCallback, useEffect, useRef, useState } from "react";
import type { DomainError } from "../../../domain/errors";
import { toDomainError } from "./toDomainError";

export type CargaRemota<T> = {
  datos: T | null;
  isLoading: boolean;
  error: DomainError | null;
  refresh: () => void;
};

type Cargar<T> = (signal: AbortSignal) => Promise<T>;

// useCargaRemota ejecuta `cargar` y expone {datos, isLoading, error, refresh}.
//
// - Un `cargar` NUEVO (otra consulta: otro periodo, otro día, otro filtro)
//   borra los datos anteriores. Nunca se pintan datos de "hoy" bajo la etiqueta
//   "semana" mientras llega la respuesta.
// - `refresh` (la MISMA consulta, p. ej. la de cada minuto) conserva los datos
//   mientras recarga y también si la recarga falla: la falla queda en `error`
//   y la pantalla avisa sin vaciar la tabla.
// - La respuesta de una consulta vieja nunca pisa a la nueva (AbortController).
//
// El llamador debe memoizar `cargar` con useCallback.
export function useCargaRemota<T>(cargar: Cargar<T> | null): CargaRemota<T> {
  const [datos, setDatos] = useState<T | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(cargar !== null);
  const [error, setError] = useState<DomainError | null>(null);
  const [tick, setTick] = useState(0);
  const cargarAnterior = useRef<Cargar<T> | null>(null);

  useEffect(() => {
    if (cargarAnterior.current !== cargar) {
      cargarAnterior.current = cargar;
      setDatos(null);
    }
    if (cargar === null) {
      setIsLoading(false);
      setError(null);
      return undefined;
    }
    const ctrl = new AbortController();
    setIsLoading(true);
    setError(null);
    cargar(ctrl.signal)
      .then((r) => {
        if (!ctrl.signal.aborted) setDatos(r);
      })
      .catch((e: unknown) => {
        if (!ctrl.signal.aborted) setError(toDomainError(e));
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setIsLoading(false);
      });
    return () => ctrl.abort();
  }, [cargar, tick]);

  const refresh = useCallback(() => setTick((t) => t + 1), []);
  return { datos, isLoading, error, refresh };
}
```

`src/modules/desempeno/presentation/hooks/lib/useCadaMinuto.ts`:

```ts
import { useEffect, useRef } from "react";

export const UN_MINUTO_MS = 60_000;

// useCadaMinuto llama `alTocar` cada 60 s mientras `activo` (spec §5: el
// tablero se actualiza cada minuto). Siempre llama a la versión más reciente.
export function useCadaMinuto(alTocar: () => void, activo: boolean): void {
  const ultimo = useRef(alTocar);
  useEffect(() => {
    ultimo.current = alTocar;
  }, [alTocar]);
  useEffect(() => {
    if (!activo) return undefined;
    const id = window.setInterval(() => ultimo.current(), UN_MINUTO_MS);
    return () => window.clearInterval(id);
  }, [activo]);
}
```

`src/modules/desempeno/presentation/hooks/lib/useEsPantallaAngosta.ts`:

```ts
import { useEffect, useState } from "react";

// Por debajo de este ancho la línea 7–20 h deja de leerse (spec §5: aviso en
// pantalla angosta).
export const CONSULTA_PANTALLA_ANGOSTA = "(max-width: 1100px)";

export function useEsPantallaAngosta(): boolean {
  const [angosta, setAngosta] = useState(
    () => window.matchMedia(CONSULTA_PANTALLA_ANGOSTA).matches,
  );
  useEffect(() => {
    const mq = window.matchMedia(CONSULTA_PANTALLA_ANGOSTA);
    const alCambiar = (e: MediaQueryListEvent) => setAngosta(e.matches);
    mq.addEventListener("change", alCambiar);
    return () => mq.removeEventListener("change", alCambiar);
  }, []);
  return angosta;
}
```

`src/modules/desempeno/presentation/preferencias.ts`:

```ts
import type { Periodo } from "../domain/entities";
import type {
  ConsultaJornada,
  ConsultaTablero,
  Horario,
} from "../application/ports/DesempenoPort";

// D16: cada quien elige cómo ver la línea; se recuerda por computadora.
export const VISTAS_LINEA = ["puntos", "bloques", "por_hora", "bloques_hora"] as const;
export type VistaLinea = (typeof VISTAS_LINEA)[number];
export const VISTA_LINEA_PREDETERMINADA: VistaLinea = "bloques";
export const CLAVE_VISTA_LINEA = "desempeno.vista-linea";

// D7: el umbral y el horario van en la consulta, no se guardan como configuración.
export const UMBRALES_PAUSA = [15, 30, 45, 60, 90] as const;
export const PAUSA_MIN_PREDETERMINADA = 45;
export const HORARIO_PREDETERMINADO: Horario = { desde: "08:00", hasta: "18:00" };

export function esVistaLinea(v: unknown): v is VistaLinea {
  return typeof v === "string" && (VISTAS_LINEA as readonly string[]).includes(v);
}

export type PreferenciasDesempeno = {
  periodo: Periodo;
  pausaMin: number;
  horario: Horario | null;
  vista: VistaLinea;
};

export type ValorPreferencias = PreferenciasDesempeno & {
  consultaJornada: ConsultaJornada;
  consultaTablero: ConsultaTablero;
  setPeriodo: (p: Periodo) => void;
  setPausaMin: (m: number) => void;
  setHorario: (h: Horario | null) => void;
  setVista: (v: VistaLinea) => void;
};
```

`src/modules/desempeno/presentation/context/contextos.ts`:

```ts
import { createContext } from "react";
import type { DesempenoPort } from "../../application/ports/DesempenoPort";
import type { ValorPreferencias } from "../preferencias";

export const DesempenoContext = createContext<{ port: DesempenoPort } | null>(null);
export const PreferenciasContext = createContext<ValorPreferencias | null>(null);
```

`src/modules/desempeno/presentation/context/PreferenciasProvider.tsx`:

```tsx
import { useCallback, useMemo, useState, type ReactNode } from "react";
import type { Periodo } from "../../domain/entities";
import type {
  ConsultaJornada,
  ConsultaTablero,
  Horario,
} from "../../application/ports/DesempenoPort";
import {
  CLAVE_VISTA_LINEA,
  esVistaLinea,
  PAUSA_MIN_PREDETERMINADA,
  VISTA_LINEA_PREDETERMINADA,
  type PreferenciasDesempeno,
  type ValorPreferencias,
  type VistaLinea,
} from "../preferencias";
import { escribirLocal, leerLocal } from "../hooks/lib/almacenLocal";
import { PreferenciasContext } from "./contextos";

export function PreferenciasProvider({
  iniciales,
  children,
}: {
  iniciales?: Partial<PreferenciasDesempeno>;
  children: ReactNode;
}) {
  const [periodo, setPeriodo] = useState<Periodo>(iniciales?.periodo ?? "hoy");
  const [pausaMin, setPausaMin] = useState<number>(
    iniciales?.pausaMin ?? PAUSA_MIN_PREDETERMINADA,
  );
  const [horario, setHorario] = useState<Horario | null>(iniciales?.horario ?? null);
  const [vista, setVistaEstado] = useState<VistaLinea>(
    () =>
      iniciales?.vista ??
      leerLocal(CLAVE_VISTA_LINEA, VISTA_LINEA_PREDETERMINADA, esVistaLinea),
  );

  const setVista = useCallback((v: VistaLinea) => {
    setVistaEstado(v);
    escribirLocal(CLAVE_VISTA_LINEA, v);
  }, []);

  const consultaJornada = useMemo<ConsultaJornada>(
    () => ({ pausaMin, horario }),
    [pausaMin, horario],
  );
  const consultaTablero = useMemo<ConsultaTablero>(
    () => ({ periodo, pausaMin, horario }),
    [periodo, pausaMin, horario],
  );

  const valor = useMemo<ValorPreferencias>(
    () => ({
      periodo,
      pausaMin,
      horario,
      vista,
      consultaJornada,
      consultaTablero,
      setPeriodo,
      setPausaMin,
      setHorario,
      setVista,
    }),
    [periodo, pausaMin, horario, vista, consultaJornada, consultaTablero, setVista],
  );

  return <PreferenciasContext.Provider value={valor}>{children}</PreferenciasContext.Provider>;
}
```

`src/modules/desempeno/presentation/context/DesempenoProvider.tsx`:

```tsx
import { useMemo, type ReactNode } from "react";
import type { DesempenoPort } from "../../application/ports/DesempenoPort";
import type { PreferenciasDesempeno } from "../preferencias";
import { DesempenoContext } from "./contextos";
import { PreferenciasProvider } from "./PreferenciasProvider";

// DesempenoProvider entrega el puerto y las preferencias de la pantalla. El
// container de producción le pasa el adapter HTTP; las pruebas, el fake.
export function DesempenoProvider({
  port,
  preferenciasIniciales,
  children,
}: {
  port: DesempenoPort;
  preferenciasIniciales?: Partial<PreferenciasDesempeno>;
  children: ReactNode;
}) {
  const valor = useMemo(() => ({ port }), [port]);
  return (
    <DesempenoContext.Provider value={valor}>
      <PreferenciasProvider iniciales={preferenciasIniciales}>{children}</PreferenciasProvider>
    </DesempenoContext.Provider>
  );
}
```

`src/modules/desempeno/presentation/context/useDesempenoPort.ts`:

```ts
import { useContext } from "react";
import type { DesempenoPort } from "../../application/ports/DesempenoPort";
import { DesempenoContext } from "./contextos";

export function useDesempenoPort(): DesempenoPort {
  const ctx = useContext(DesempenoContext);
  if (!ctx) {
    throw new Error(
      "useDesempenoPort: falta <DesempenoProvider> — envuelve el árbol en la entrada del módulo",
    );
  }
  return ctx.port;
}
```

`src/modules/desempeno/presentation/context/usePreferencias.ts`:

```ts
import { useContext } from "react";
import type { ValorPreferencias } from "../preferencias";
import { PreferenciasContext } from "./contextos";

export function usePreferencias(): ValorPreferencias {
  const v = useContext(PreferenciasContext);
  if (!v) {
    throw new Error("usePreferencias: falta <PreferenciasProvider> (lo pone <DesempenoProvider>)");
  }
  return v;
}
```

`src/modules/desempeno/test/renderConDesempeno.tsx`:

```tsx
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { DesempenoProvider } from "../presentation/context/DesempenoProvider";
import type { PreferenciasDesempeno } from "../presentation/preferencias";
import { FakeDesempenoPort } from "../application/__tests__/fakeDesempenoPort";

// renderConDesempeno monta `ui` con router en memoria y el provider del
// módulo sobre un FakeDesempenoPort. Devuelve el puerto para inspeccionarlo.
export function renderConDesempeno(
  ui: ReactElement,
  opciones: {
    port?: FakeDesempenoPort;
    ruta?: string;
    preferencias?: Partial<PreferenciasDesempeno>;
  } = {},
) {
  const port = opciones.port ?? new FakeDesempenoPort();
  const resultado = render(
    <MemoryRouter initialEntries={[opciones.ruta ?? "/"]}>
      <DesempenoProvider port={port} preferenciasIniciales={opciones.preferencias}>
        {ui}
      </DesempenoProvider>
    </MemoryRouter>,
  );
  return { ...resultado, port };
}
```

- [ ] **Step 7: Correr las pruebas de la tarea y verlas pasar**

Run: `npx vitest run src/modules/desempeno/presentation`
Expected: PASS (16 pruebas: 4 + 5 + 2 + 5).

- [ ] **Step 8: Tipos, lint y build de estilos**

Run: `npx tsc --noEmit && npx eslint src/modules/desempeno src/components/ui/hover-card.tsx --max-warnings 0 && npm run build`
Expected: sin errores; `npm run build` termina en `✓ built in` (comprueba que el `tailwind.config.js` editado es válido).

- [ ] **Step 9: Commit**

```bash
git add package.json package-lock.json src/components/ui/hover-card.tsx src/index.css tailwind.config.js src/modules/desempeno/presentation src/modules/desempeno/test
git commit -m "feat(desempeno): base de presentación, preferencias por computadora y tokens del estilo Audaz"
```

---

### Tarea 0 — Parte C: primitivos de UI (formato, escala del eje, popover, chips, tarjeta de actividad, estados, selector de periodo)

**Rama `feat/desempeno`, después de la Parte B. Al terminar, se abren los 3 carriles.**

**Files:**
- Create: `src/modules/desempeno/components/lib/format.ts`
- Create: `src/modules/desempeno/components/lib/escala.ts`
- Create: `src/modules/desempeno/components/lib/tratoMeta.ts`
- Create: `src/modules/desempeno/components/lib/tipoActividadMeta.ts`
- Create: `src/modules/desempeno/components/comunes/InfoRapida.tsx`
- Create: `src/modules/desempeno/components/comunes/ChipTrato.tsx`
- Create: `src/modules/desempeno/components/comunes/PuntoEstado.tsx`
- Create: `src/modules/desempeno/components/comunes/IconoTipoActividad.tsx`
- Create: `src/modules/desempeno/components/comunes/MontoActividad.tsx`
- Create: `src/modules/desempeno/components/comunes/TarjetaActividad.tsx`
- Create: `src/modules/desempeno/components/comunes/Estados.tsx`
- Create: `src/modules/desempeno/components/comunes/SegmentoPeriodo.tsx`
- Test: `src/modules/desempeno/components/lib/__tests__/format.test.ts`
- Test: `src/modules/desempeno/components/lib/__tests__/escala.test.ts`
- Test: `src/modules/desempeno/components/comunes/__tests__/InfoRapida.test.tsx`
- Test: `src/modules/desempeno/components/comunes/__tests__/comunes.test.tsx`

**Interfaces:**
- Consumes: Parte A (`Actividad`, `TipoActividad`, `FormaCobro`, `Trato`, `EstadoAhora`, `DomainError`), Parte B (`HoverCard*`, tokens `dsm-*`, `useEsPantallaAngosta`), `@/utils/tiempoDeNegocio` (`enRelojDeNegocio`).
- Produces:
  - `format.ts`: `horaCorta(iso)`, `diaDeNegocioDe(iso)`, `hoyDeNegocio(ahora?)`, `fechaLarga(dia)`, `fechaCorta(dia)`, `sumarDias(dia, n)`, `diasEntre(desde, hasta)`, `duracion(min)`, `duracionCorta(min)`, `dinero(raw)`, `dinero2(raw)`, `porcentaje(raw|null)`, `entero(n)`, `plural(n, uno, varios)`, `sinAcentos(s)`, `primerNombre(nombre)`.
  - `escala.ts`: `HORA_INICIO_EJE=7`, `HORA_FIN_EJE=20`, `MARCAS_EJE`, `minutosDelDia(iso)`, `posicionMinutos(min)`, `posicion(iso)`, `ancho(desde, hasta)`, `posicionHorario("HH:MM")`.
  - `tratoMeta.ts`: `type Tono`, `TRATO_META: Record<Trato, {icono, tono, relleno}>`, `CLASE_TONO: Record<Tono,string>`, `CLASE_RELLENO: Record<Tono,string>`.
  - `tipoActividadMeta.ts`: `TIPO_ACTIVIDAD_META: Record<TipoActividad, {etiqueta, icono, claseIcono, clasePunto}>`, `FORMA_COBRO_ETIQUETA: Record<FormaCobro,string>`, `etiquetaDeIdActividad(id)`.
  - Componentes: `InfoRapida({children, contenido, lado?, className?})`, `TarjetaTitulo`, `TarjetaSub`, `TarjetaFilas({filas})`, `TarjetaNota`, `TarjetaSeparador`, `TarjetaLista({filas})`, `TarjetaBarras({filas})`; `ChipTrato({trato, etiqueta})`; `PuntoEstado({estado})`; `IconoTipoActividad({tipo})`; `MontoActividad({actividad})`; `TarjetaActividad({actividad})`; `ErrorConReintento({titulo, mensaje, onReintentar})`, `EstadoVacio({titulo, detalle?})`, `AvisoPantallaAngosta()`, `FilasCargando({filas, columnas})`, `SegmentoPeriodo()` (Hoy/Semana, lee y escribe `usePreferencias().periodo`; lo usan el tablero y la ficha).

- [ ] **Step 1: Escribir las pruebas de formato y escala (fallan)**

`src/modules/desempeno/components/lib/__tests__/format.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  diaDeNegocioDe,
  diasEntre,
  dinero,
  dinero2,
  duracion,
  duracionCorta,
  entero,
  fechaCorta,
  fechaLarga,
  horaCorta,
  hoyDeNegocio,
  plural,
  porcentaje,
  primerNombre,
  sinAcentos,
  sumarDias,
} from "../format";

// La suite corre con TZ=UTC: si estas funciones usaran el reloj del
// navegador, "16:31Z" saldría "16:31" y no "10:31".
describe("horas en el reloj del negocio (America/Mexico_City)", () => {
  it("convierte UTC a la hora de la oficina", () => {
    expect(horaCorta("2026-10-14T16:31:00Z")).toBe("10:31");
  });

  it("de madrugada en UTC sigue siendo la noche del día anterior en CDMX", () => {
    expect(horaCorta("2026-10-15T05:30:00Z")).toBe("23:30");
    expect(diaDeNegocioDe("2026-10-15T05:30:00Z")).toBe("2026-10-14");
    expect(hoyDeNegocio(new Date("2026-10-15T03:00:00Z"))).toBe("2026-10-14");
  });
});

describe("días de calendario", () => {
  it("nombres en español sin depender de Intl", () => {
    expect(fechaLarga("2026-10-14")).toBe("miércoles 14 de octubre");
    expect(fechaCorta("2026-10-09")).toBe("vie 9 oct");
  });

  it("suma días sobre el calendario y enumera rangos inclusivos", () => {
    expect(sumarDias("2026-10-31", 1)).toBe("2026-11-01");
    expect(sumarDias("2026-10-01", -1)).toBe("2026-09-30");
    expect(diasEntre("2026-10-09", "2026-10-14")).toEqual([
      "2026-10-09", "2026-10-10", "2026-10-11", "2026-10-12", "2026-10-13", "2026-10-14",
    ]);
    expect(diasEntre("2026-10-14", "2026-10-13")).toEqual([]);
  });
});

describe("duraciones, dinero y textos", () => {
  it("duraciones legibles", () => {
    expect(duracion(72)).toBe("1 h 12 min");
    expect(duracion(45)).toBe("45 min");
    expect(duracion(120)).toBe("2 h");
    expect(duracionCorta(65)).toBe("1 h 05");
    expect(duracionCorta(30)).toBe("30 min");
  });

  it("dinero desde el decimal en string, sin tocar el valor", () => {
    expect(dinero("8600.00")).toBe("$8,600");
    expect(dinero2("150.00")).toBe("$150.00");
    expect(dinero("no-numero")).toBe("no-numero");
  });

  it("porcentajes, enteros y plurales", () => {
    expect(porcentaje("65.90")).toBe("65.9%");
    expect(porcentaje(null)).toBe("—");
    expect(entero(1188)).toBe("1,188");
    expect(plural(1, "pago", "pagos")).toBe("1 pago");
    expect(plural(3, "pago", "pagos")).toBe("3 pagos");
  });

  it("búsqueda sin acentos y nombre corto", () => {
    expect(sinAcentos("Méndez ÁVILA")).toBe("mendez avila");
    expect(primerNombre("Juan Carlos Méndez")).toBe("Juan Carlos");
  });
});
```

`src/modules/desempeno/components/lib/__tests__/escala.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { ancho, minutosDelDia, posicion, posicionHorario } from "../escala";

describe("eje 7:00–20:00 en hora de la oficina", () => {
  it("minutos del día en CDMX, no en UTC", () => {
    expect(minutosDelDia("2026-10-14T14:20:00Z")).toBe(8 * 60 + 20);
  });

  it("posición en porcentaje del eje", () => {
    // 08:20 → (500 − 420) / 780 = 10.256 %
    expect(posicion("2026-10-14T14:20:00Z")).toBeCloseTo(10.256, 2);
    expect(posicionHorario("08:00")).toBeCloseTo(7.692, 2);
  });

  it("lo que cae fuera del eje se pega al borde (nunca se sale ni se voltea)", () => {
    expect(posicion("2026-10-14T12:00:00Z")).toBe(0); // 06:00
    expect(posicion("2026-10-15T03:00:00Z")).toBe(100); // 21:00
    expect(posicion("2026-10-15T05:30:00Z")).toBe(100); // 23:30, no 05:30
  });

  it("ancho de un tramo", () => {
    // 09:10 → 10:22 = 72 min de 780
    expect(ancho("2026-10-14T15:10:00Z", "2026-10-14T16:22:00Z")).toBeCloseTo(9.231, 2);
  });
});
```

- [ ] **Step 2: Correrlas y verlas fallar**

Run: `npx vitest run src/modules/desempeno/components/lib`
Expected: FAIL con `Failed to resolve import "../format"`.

- [ ] **Step 3: Implementar formato y escala**

`src/modules/desempeno/components/lib/format.ts`:

```ts
import { enRelojDeNegocio } from "@/utils/tiempoDeNegocio";

// Formato de presentación. Ninguna función calcula cifras del negocio: solo
// convierte lo que llegó del API a texto (D10). Las horas SIEMPRE se leen en
// la zona del negocio (tiempoDeNegocio), nunca en la del navegador.

const DIAS = ["domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"];
const DIAS_CORTOS = ["dom", "lun", "mar", "mié", "jue", "vie", "sáb"];
const MESES = [
  "enero", "febrero", "marzo", "abril", "mayo", "junio",
  "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre",
];
const MESES_CORTOS = ["ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"];

/** "2026-10-14T16:31:00Z" → "10:31" (reloj de la oficina). */
export function horaCorta(iso: string): string {
  return enRelojDeNegocio(iso).slice(11, 16);
}

/** Día de calendario del negocio de un instante: "2026-10-14". */
export function diaDeNegocioDe(iso: string): string {
  return enRelojDeNegocio(iso).slice(0, 10);
}

export function hoyDeNegocio(ahora: Date = new Date()): string {
  return diaDeNegocioDe(ahora.toISOString());
}

function aFecha(dia: string): Date {
  const [anio, mes, d] = dia.split("-").map(Number);
  return new Date(Date.UTC(anio, mes - 1, d));
}

/** "2026-10-14" → "miércoles 14 de octubre". */
export function fechaLarga(dia: string): string {
  const f = aFecha(dia);
  return `${DIAS[f.getUTCDay()]} ${f.getUTCDate()} de ${MESES[f.getUTCMonth()]}`;
}

/** "2026-10-09" → "vie 9 oct". */
export function fechaCorta(dia: string): string {
  const f = aFecha(dia);
  return `${DIAS_CORTOS[f.getUTCDay()]} ${f.getUTCDate()} ${MESES_CORTOS[f.getUTCMonth()]}`;
}

/** Avanza sobre el calendario, no sumando 24 h a un instante. */
export function sumarDias(dia: string, n: number): string {
  const f = aFecha(dia);
  f.setUTCDate(f.getUTCDate() + n);
  return f.toISOString().slice(0, 10);
}

/** Días de calendario de `desde` a `hasta`, ambos incluidos. */
export function diasEntre(desde: string, hasta: string): string[] {
  const dias: string[] = [];
  for (let d = desde; d <= hasta; d = sumarDias(d, 1)) dias.push(d);
  return dias;
}

/** 72 → "1 h 12 min"; 45 → "45 min"; 120 → "2 h". */
export function duracion(min: number): string {
  const m = Math.round(min);
  const h = Math.floor(m / 60);
  const r = m % 60;
  if (h === 0) return `${r} min`;
  return r === 0 ? `${h} h` : `${h} h ${r} min`;
}

/** 65 → "1 h 05"; 30 → "30 min" (celdas angostas). */
export function duracionCorta(min: number): string {
  const m = Math.round(min);
  const h = Math.floor(m / 60);
  const r = m % 60;
  return h === 0 ? `${r} min` : `${h} h ${String(r).padStart(2, "0")}`;
}

const MXN_SIN_CENTAVOS = new Intl.NumberFormat("es-MX", {
  style: "currency",
  currency: "MXN",
  minimumFractionDigits: 0,
  maximumFractionDigits: 0,
});
const MXN = new Intl.NumberFormat("es-MX", {
  style: "currency",
  currency: "MXN",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

/** "8600.00" → "$8,600". Si no es número, se muestra tal cual. */
export function dinero(raw: string): string {
  const n = Number(raw);
  return Number.isFinite(n) ? MXN_SIN_CENTAVOS.format(n) : raw;
}

/** "150.00" → "$150.00". */
export function dinero2(raw: string): string {
  const n = Number(raw);
  return Number.isFinite(n) ? MXN.format(n) : raw;
}

/** "65.90" → "65.9%"; null → "—". */
export function porcentaje(raw: string | null): string {
  if (raw === null) return "—";
  const n = Number(raw);
  return Number.isFinite(n) ? `${n.toFixed(1)}%` : raw;
}

export function entero(n: number): string {
  return n.toLocaleString("es-MX");
}

export function plural(n: number, uno: string, varios: string): string {
  return `${n} ${n === 1 ? uno : varios}`;
}

export function sinAcentos(s: string): string {
  return s.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();
}

/** "Juan Carlos Méndez" → "Juan Carlos". */
export function primerNombre(nombre: string): string {
  return nombre.split(" ").slice(0, 2).join(" ");
}
```

`src/modules/desempeno/components/lib/escala.ts`:

```ts
import { enRelojDeNegocio } from "@/utils/tiempoDeNegocio";

// Eje horizontal de la línea de actividad: 7:00–20:00 en hora de la oficina
// (spec §5). Solo posiciona en pantalla lo que llegó del API.
export const HORA_INICIO_EJE = 7;
export const HORA_FIN_EJE = 20;
export const MARCAS_EJE = [7, 10, 13, 16, 19] as const;

const INICIO = HORA_INICIO_EJE * 60;
const FIN = HORA_FIN_EJE * 60;
const SPAN = FIN - INICIO;

export function minutosDelDia(iso: string): number {
  const reloj = enRelojDeNegocio(iso);
  return Number(reloj.slice(11, 13)) * 60 + Number(reloj.slice(14, 16));
}

/** Minutos del día → % del eje, pegado a los bordes. */
export function posicionMinutos(min: number): number {
  const c = Math.min(FIN, Math.max(INICIO, min));
  return ((c - INICIO) / SPAN) * 100;
}

export function posicion(iso: string): number {
  return posicionMinutos(minutosDelDia(iso));
}

export function ancho(desde: string, hasta: string): number {
  return Math.max(0, posicion(hasta) - posicion(desde));
}

/** "08:00" → % del eje. */
export function posicionHorario(hhmm: string): number {
  const [h, m] = hhmm.split(":").map(Number);
  return posicionMinutos(h * 60 + m);
}
```

- [ ] **Step 4: Correr y ver pasar**

Run: `npx vitest run src/modules/desempeno/components/lib`
Expected: PASS (12 pruebas).

- [ ] **Step 5: Escribir las pruebas de los componentes comunes (fallan)**

`src/modules/desempeno/components/comunes/__tests__/InfoRapida.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { InfoRapida, TarjetaFilas, TarjetaTitulo } from "../InfoRapida";

function Ejemplo() {
  return (
    <InfoRapida
      contenido={
        <>
          <TarjetaTitulo>Pagos de hoy</TarjetaTitulo>
          <TarjetaFilas filas={[["Efectivo", "28 · $7,800"]]} />
        </>
      }
    >
      <span tabIndex={0}>$8,600</span>
    </InfoRapida>
  );
}

describe("InfoRapida (popover de información rápida, D17)", () => {
  it("no monta el contenido hasta que se pide", () => {
    render(<Ejemplo />);
    expect(screen.queryByText("Pagos de hoy")).not.toBeInTheDocument();
  });

  it("se abre con el ratón", async () => {
    const user = userEvent.setup();
    render(<Ejemplo />);
    await user.hover(screen.getByText("$8,600"));
    expect(await screen.findByText("Pagos de hoy")).toBeInTheDocument();
    expect(screen.getByText("28 · $7,800")).toBeInTheDocument();
  });

  it("se abre con el teclado cuando el disparador es enfocable", async () => {
    const user = userEvent.setup();
    render(<Ejemplo />);
    await user.tab();
    expect(screen.getByText("$8,600")).toHaveFocus();
    expect(await screen.findByText("Pagos de hoy")).toBeInTheDocument();
  });
});
```

`src/modules/desempeno/components/comunes/__tests__/comunes.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ChipTrato } from "../ChipTrato";
import { MontoActividad } from "../MontoActividad";
import { TarjetaActividad } from "../TarjetaActividad";
import { ErrorConReintento, EstadoVacio } from "../Estados";
import { SegmentoPeriodo } from "../SegmentoPeriodo";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import {
  makeActividadCondonacion,
  makeActividadPago,
  makeActividadVisita,
} from "../../../application/__tests__/fakeDesempenoPort";

describe("ChipTrato", () => {
  it("pinta la etiqueta del servidor con ícono (nunca color solo)", () => {
    const { container } = render(<ChipTrato trato="escalar" etiqueta="Se negó" />);
    expect(screen.getByText("Se negó")).toBeInTheDocument();
    expect(container.querySelector("svg")).not.toBeNull();
    expect(container.querySelector("[data-trato='escalar']")).not.toBeNull();
  });
});

describe("MontoActividad", () => {
  it("pago, condonación y visita", () => {
    const { rerender } = render(<MontoActividad actividad={makeActividadPago()} />);
    expect(screen.getByText("$150")).toBeInTheDocument();

    rerender(<MontoActividad actividad={makeActividadCondonacion()} />);
    expect(screen.getByText("−$50")).toBeInTheDocument();

    rerender(<MontoActividad actividad={makeActividadVisita()} />);
    expect(screen.getByText("No se encontraba")).toBeInTheDocument();
  });

  it("un pago rechazado o sin aplicar se muestra con su estado real (spec §6)", () => {
    const base = makeActividadPago();
    const { rerender } = render(
      <MontoActividad actividad={{ ...base, pago: { ...base.pago!, rechazado: true } }} />,
    );
    expect(screen.getByText("Rechazado")).toBeInTheDocument();

    rerender(<MontoActividad actividad={{ ...base, pago: { ...base.pago!, aplicado: false } }} />);
    expect(screen.getByText("Sin aplicar")).toBeInTheDocument();
  });
});

describe("TarjetaActividad", () => {
  it("resume un pago con hora de la oficina y sin 'Depósito'", () => {
    render(<TarjetaActividad actividad={makeActividadPago()} />);
    expect(screen.getByText("Pago · $150")).toBeInTheDocument();
    expect(screen.getByText("mié 14 oct 08:20")).toBeInTheDocument();
    expect(screen.getByText("Efectivo")).toBeInTheDocument();
    expect(screen.getByText("JUAN CARLOS MENDEZ")).toBeInTheDocument();
    expect(screen.queryByText(/Depósito/)).not.toBeInTheDocument();
  });

  it("una visita muestra resultado, nota y que no trae ubicación", () => {
    render(<TarjetaActividad actividad={makeActividadVisita()} />);
    expect(screen.getByText("Visita · No se encontraba")).toBeInTheDocument();
    expect(screen.getByText("sin ubicación")).toBeInTheDocument();
    expect(screen.getByText("“Vecina dice que regresa el lunes”")).toBeInTheDocument();
  });
});

describe("SegmentoPeriodo", () => {
  it("abre en Hoy y cambia a Semana", async () => {
    const user = userEvent.setup();
    renderConDesempeno(<SegmentoPeriodo />);
    expect(screen.getByRole("button", { name: "Hoy" })).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "Semana" }));
    expect(screen.getByRole("button", { name: "Semana" })).toHaveAttribute("aria-pressed", "true");
  });
});

describe("Estados", () => {
  it("error con Reintentar", async () => {
    const user = userEvent.setup();
    const onReintentar = vi.fn();
    render(
      <ErrorConReintento titulo="No se pudo cargar" mensaje="error de red" onReintentar={onReintentar} />,
    );
    await user.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(onReintentar).toHaveBeenCalledTimes(1);
  });

  it("vacío", () => {
    render(<EstadoVacio titulo="Sin actividad hoy" detalle="Su último movimiento fue ayer." />);
    expect(screen.getByText("Sin actividad hoy")).toBeInTheDocument();
  });
});
```

- [ ] **Step 6: Correrlas y verlas fallar**

Run: `npx vitest run src/modules/desempeno/components/comunes`
Expected: FAIL con `Failed to resolve import "../InfoRapida"`.

- [ ] **Step 7: Implementar metas y componentes comunes**

`src/modules/desempeno/components/lib/tratoMeta.ts`:

```ts
import {
  Ban,
  CalendarClock,
  Circle,
  CircleCheck,
  CircleDashed,
  DoorClosed,
  RotateCcw,
  type LucideIcon,
} from "lucide-react";
import type { Trato } from "../../domain/values/estadoCuenta";

export type Tono = "ok" | "teal" | "ambar" | "rojo" | "violeta" | "gris" | "ninguno";

// Cómo se pinta cada trato: color + ícono + texto, nunca color solo. Espejo de
// msp-app-kt feature/pagos/ui/EstadoCuentaUi.kt (tratoDe → contenidoDe/iconoDe);
// "escalar" es el único en sólido invertido, como en el teléfono. El trato y la
// etiqueta los manda el servidor; aquí solo se elige el pincel.
export const TRATO_META: Record<Trato, { icono: LucideIcon; tono: Tono; relleno: boolean }> = {
  pagado: { icono: CircleCheck, tono: "ok", relleno: false },
  parcial: { icono: CircleDashed, tono: "teal", relleno: false },
  regresas: { icono: RotateCcw, tono: "ambar", relleno: false },
  diferido: { icono: CalendarClock, tono: "rojo", relleno: false },
  escalar: { icono: Ban, tono: "rojo", relleno: true },
  cita: { icono: CalendarClock, tono: "violeta", relleno: false },
  nadie: { icono: DoorClosed, tono: "gris", relleno: false },
  sin_trabajar: { icono: Circle, tono: "ninguno", relleno: false },
};

export const CLASE_TONO: Record<Tono, string> = {
  ok: "bg-dsm-ok-fondo text-dsm-ok",
  teal: "bg-dsm-teal-fondo text-dsm-teal",
  ambar: "bg-dsm-ambar-fondo text-dsm-ambar",
  rojo: "bg-dsm-rojo-fondo text-dsm-rojo",
  violeta: "bg-dsm-violeta-fondo text-dsm-violeta",
  gris: "bg-dsm-gris-fondo text-dsm-gris",
  ninguno: "border border-dashed border-border bg-transparent text-foreground/80",
};

export const CLASE_RELLENO: Record<Tono, string> = {
  ok: "bg-dsm-ok text-white",
  teal: "bg-dsm-teal text-white",
  ambar: "bg-dsm-ambar text-white",
  rojo: "bg-dsm-rojo text-white",
  violeta: "bg-dsm-violeta text-white",
  gris: "bg-dsm-gris text-white",
  ninguno: "bg-foreground text-background",
};
```

`src/modules/desempeno/components/lib/tipoActividadMeta.ts`:

```ts
import { Banknote, DoorOpen, Percent, type LucideIcon } from "lucide-react";
import type { FormaCobro, TipoActividad } from "../../domain/entities";

export const TIPO_ACTIVIDAD_META: Record<
  TipoActividad,
  { etiqueta: string; icono: LucideIcon; claseIcono: string; clasePunto: string }
> = {
  pago: {
    etiqueta: "Pago",
    icono: Banknote,
    claseIcono: "bg-dsm-ok-fondo text-dsm-ok",
    clasePunto: "bg-dsm-ok",
  },
  visita: {
    etiqueta: "Visita",
    icono: DoorOpen,
    claseIcono: "bg-dsm-gris-fondo text-dsm-gris",
    clasePunto: "bg-dsm-visita",
  },
  condonacion: {
    etiqueta: "Condonación",
    icono: Percent,
    claseIcono: "bg-dsm-violeta-fondo text-dsm-violeta",
    clasePunto: "bg-dsm-violeta",
  },
};

// Solo las formas del contrato. No existe "Depósito" (spec §10).
export const FORMA_COBRO_ETIQUETA: Record<FormaCobro, string> = {
  efectivo: "Efectivo",
  transferencia: "Transferencia",
  cheque: "Cheque",
  condonacion: "Condonación",
  otra: "Otra",
};

// Los ids del contrato son "pago:<n>" o "visita:<uuid>" (contrato §1).
export function etiquetaDeIdActividad(id: string): string {
  if (id.startsWith("pago:")) return "Pago";
  if (id.startsWith("visita:")) return "Visita";
  return "Movimiento";
}
```

`src/modules/desempeno/components/comunes/InfoRapida.tsx`:

```tsx
import { Fragment, type ReactElement, type ReactNode } from "react";
import {
  HoverCard,
  HoverCardContent,
  HoverCardPortal,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { cn } from "@/lib/utils";

export const RETRASO_APERTURA_MS = 300;

// InfoRapida: popover de información rápida (D17). El disparador es el hijo
// (un elemento DOM). Para abrirse también con teclado, el hijo lleva
// tabIndex={0}; los elementos de la línea de actividad no lo llevan (spec §10:
// esos se abren con el ratón). El contenido solo se monta al abrirse.
export function InfoRapida({
  children,
  contenido,
  lado = "bottom",
  className,
}: {
  children: ReactElement;
  contenido: ReactNode;
  lado?: "top" | "bottom";
  className?: string;
}) {
  return (
    <HoverCard openDelay={RETRASO_APERTURA_MS} closeDelay={120}>
      <HoverCardTrigger asChild>{children}</HoverCardTrigger>
      <HoverCardPortal>
        <HoverCardContent
          side={lado}
          sideOffset={8}
          className={cn(
            "w-auto min-w-[220px] max-w-[320px] rounded-[10px] bg-dsm-tarjeta px-3.5 py-3 text-[12.5px] leading-snug shadow-lg",
            className,
          )}
        >
          {contenido}
        </HoverCardContent>
      </HoverCardPortal>
    </HoverCard>
  );
}

export function TarjetaTitulo({ children }: { children: ReactNode }) {
  return <div className="text-[13px] font-medium">{children}</div>;
}

export function TarjetaSub({ children }: { children: ReactNode }) {
  return <div className="mt-px font-mono text-[10.5px] text-muted-foreground">{children}</div>;
}

export function TarjetaFilas({ filas }: { filas: ReadonlyArray<readonly [string, ReactNode]> }) {
  return (
    <dl className="mt-2.5 grid grid-cols-[auto_auto] items-baseline gap-x-4 gap-y-1">
      {filas.map(([clave, valor]) => (
        <Fragment key={clave}>
          <dt className="whitespace-nowrap font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
            {clave}
          </dt>
          <dd className="m-0 text-right tabular">{valor}</dd>
        </Fragment>
      ))}
    </dl>
  );
}

export function TarjetaNota({ children }: { children: ReactNode }) {
  return <p className="mt-2 text-foreground/80">{children}</p>;
}

export function TarjetaSeparador() {
  return <div className="my-2.5 h-px bg-border" />;
}

export function TarjetaLista({ filas }: { filas: ReadonlyArray<readonly [string, string]> }) {
  return (
    <ul className="mt-2 flex flex-col gap-1 tabular">
      {filas.map(([izq, der], i) => (
        <li key={`${izq}-${i}`} className="flex justify-between gap-4">
          <span className="font-mono text-[11.5px]">{izq}</span>
          <span>{der}</span>
        </li>
      ))}
    </ul>
  );
}

// TarjetaBarras: barras de proporción. `n` y `total` llegan del API; el ancho
// es solo dibujo.
export function TarjetaBarras({
  filas,
}: {
  filas: ReadonlyArray<{ etiqueta: string; n: number; total: number; clase: string }>;
}) {
  return (
    <div className="mt-2.5 flex flex-col gap-1.5">
      {filas.map((f) => (
        <div key={f.etiqueta} className="grid grid-cols-[78px_1fr_34px] items-center gap-2 text-xs">
          <span>{f.etiqueta}</span>
          <i className="block h-1.5 overflow-hidden rounded-full bg-muted">
            <b
              className={cn("block h-full", f.clase)}
              style={{ width: `${f.total > 0 ? (f.n / f.total) * 100 : 0}%` }}
            />
          </i>
          <span className="text-right tabular">{f.n}</span>
        </div>
      ))}
    </div>
  );
}
```

`src/modules/desempeno/components/comunes/ChipTrato.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { Trato } from "../../domain/values/estadoCuenta";
import { CLASE_RELLENO, CLASE_TONO, TRATO_META } from "../lib/tratoMeta";

export function ChipTrato({ trato, etiqueta }: { trato: Trato; etiqueta: string }) {
  const meta = TRATO_META[trato];
  const Icono = meta.icono;
  return (
    <span
      data-trato={trato}
      className={cn(
        "inline-flex h-[22px] items-center gap-[5px] whitespace-nowrap rounded-full px-2 text-[11.5px] font-medium",
        meta.relleno ? CLASE_RELLENO[meta.tono] : CLASE_TONO[meta.tono],
      )}
    >
      <Icono aria-hidden className="h-3 w-3" />
      {etiqueta}
    </span>
  );
}
```

`src/modules/desempeno/components/comunes/PuntoEstado.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { EstadoAhora } from "../../domain/entities";

const CLASE: Record<EstadoAhora, string> = {
  activo: "bg-dsm-ok ring-[3px] ring-[hsl(var(--dsm-ok-fondo))]",
  sin_actividad: "bg-dsm-ambar ring-[3px] ring-[hsl(var(--dsm-ambar-fondo))]",
  no_ha_empezado: "bg-muted-foreground/50",
};

export function PuntoEstado({ estado }: { estado: EstadoAhora }) {
  return (
    <span
      aria-hidden
      data-estado={estado}
      className={cn("inline-block h-[7px] w-[7px] flex-none rounded-full", CLASE[estado])}
    />
  );
}
```

`src/modules/desempeno/components/comunes/IconoTipoActividad.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { TipoActividad } from "../../domain/entities";
import { TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";

export function IconoTipoActividad({ tipo }: { tipo: TipoActividad }) {
  const meta = TIPO_ACTIVIDAD_META[tipo];
  const Icono = meta.icono;
  return (
    <span
      aria-label={meta.etiqueta}
      className={cn("inline-flex h-7 w-7 flex-none items-center justify-center rounded-md", meta.claseIcono)}
    >
      <Icono aria-hidden className="h-3.5 w-3.5" />
    </span>
  );
}
```

`src/modules/desempeno/components/comunes/MontoActividad.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { Actividad } from "../../domain/entities";
import { dinero } from "../lib/format";

// Monto o resultado de una actividad. Una visita muestra el literal que
// capturó el teléfono; el contrato no trae su trato (ver huecos del plan), así
// que no se le pone color de estado.
export function MontoActividad({ actividad }: { actividad: Actividad }) {
  if (actividad.visita) {
    return (
      <span className="inline-flex h-[22px] items-center whitespace-nowrap rounded-full bg-dsm-gris-fondo px-2 text-[11.5px] text-dsm-gris">
        {actividad.visita.tipoVisita}
      </span>
    );
  }
  const pago = actividad.pago;
  if (!pago) return <span className="text-muted-foreground">—</span>;
  if (actividad.tipo === "condonacion") {
    return <span className="tabular text-dsm-violeta">−{dinero(pago.importe)}</span>;
  }
  return (
    <span className="inline-flex items-baseline gap-1.5">
      <span className={cn("tabular", pago.rechazado && "text-muted-foreground line-through")}>
        {dinero(pago.importe)}
      </span>
      {pago.rechazado ? (
        <span className="text-[11px] text-dsm-rojo">Rechazado</span>
      ) : !pago.aplicado ? (
        <span className="text-[11px] text-dsm-ambar">Sin aplicar</span>
      ) : null}
    </span>
  );
}
```

`src/modules/desempeno/components/comunes/TarjetaActividad.tsx`:

```tsx
import type { ReactNode } from "react";
import type { Actividad } from "../../domain/entities";
import { diaDeNegocioDe, dinero, fechaCorta, horaCorta } from "../lib/format";
import { FORMA_COBRO_ETIQUETA, TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";
import { TarjetaFilas, TarjetaNota, TarjetaSub, TarjetaTitulo } from "./InfoRapida";

const LARGO_NOTA = 70;

function cuando(iso: string): string {
  return `${fechaCorta(diaDeNegocioDe(iso))} ${horaCorta(iso)}`;
}

// TarjetaActividad: contenido del popover de una actividad (renglón, punto del
// eje, ícono). Todo sale del objeto Actividad del API.
export function TarjetaActividad({ actividad: a }: { actividad: Actividad }) {
  const meta = TIPO_ACTIVIDAD_META[a.tipo];
  const resumen = a.visita
    ? a.visita.tipoVisita
    : a.pago
      ? `${a.tipo === "condonacion" ? "−" : ""}${dinero(a.pago.importe)}`
      : "";
  const filas: Array<readonly [string, ReactNode]> = [
    ["Cliente", a.clienteNombre],
    ["Venta", a.folio ?? "—"],
  ];
  if (a.pago && a.tipo === "pago") filas.push(["Forma", FORMA_COBRO_ETIQUETA[a.pago.formaCobro]]);
  filas.push(["Registró", a.registradoPor.nombre]);
  filas.push(["GPS", a.conGps ? "con ubicación" : "sin ubicación"]);
  filas.push(["Fotos", a.numFotos > 0 ? String(a.numFotos) : "—"]);
  if (a.recibidoAt) filas.push(["Llegó", cuando(a.recibidoAt)]);

  const nota = a.visita?.nota ?? null;
  const notaCorta =
    nota && nota.length > LARGO_NOTA ? `${nota.slice(0, LARGO_NOTA - 2)}…` : nota;

  return (
    <>
      <TarjetaTitulo>{`${meta.etiqueta} · ${resumen}`}</TarjetaTitulo>
      <TarjetaSub>{cuando(a.hora)}</TarjetaSub>
      <TarjetaFilas filas={filas} />
      {notaCorta && (
        <TarjetaNota>
          <i>{`“${notaCorta}”`}</i>
        </TarjetaNota>
      )}
    </>
  );
}
```

`src/modules/desempeno/components/comunes/Estados.tsx`:

```tsx
import { MonitorSmartphone } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useEsPantallaAngosta } from "../../presentation/hooks/lib/useEsPantallaAngosta";

export function ErrorConReintento({
  titulo,
  mensaje,
  onReintentar,
}: {
  titulo: string;
  mensaje: string;
  onReintentar: () => void;
}) {
  return (
    <div role="alert" className="flex flex-col items-center gap-3 rounded-[14px] bg-dsm-tarjeta px-6 py-12 text-center">
      <p className="font-serif text-xl">{titulo}</p>
      <p className="max-w-sm text-sm text-muted-foreground">{mensaje}</p>
      <Button variant="outline" size="sm" className="rounded-full" onClick={onReintentar}>
        Reintentar
      </Button>
    </div>
  );
}

export function EstadoVacio({ titulo, detalle }: { titulo: string; detalle?: string }) {
  return (
    <div className="flex flex-col items-center gap-1 px-6 py-12 text-center">
      <p className="font-serif text-xl">{titulo}</p>
      {detalle && <p className="text-sm text-muted-foreground">{detalle}</p>}
    </div>
  );
}

export function AvisoPantallaAngosta() {
  const angosta = useEsPantallaAngosta();
  if (!angosta) return null;
  return (
    <div role="status" className="flex items-center gap-2 rounded-full bg-dsm-ambar-fondo px-4 py-2 text-xs text-dsm-ambar">
      <MonitorSmartphone aria-hidden className="h-3.5 w-3.5" />
      Ventana angosta: ensánchala para leer la línea del día completa
    </div>
  );
}

export function FilasCargando({ filas, columnas }: { filas: number; columnas: number }) {
  return (
    <>
      {Array.from({ length: filas }).map((_, i) => (
        <tr key={i} data-testid="fila-cargando">
          {Array.from({ length: columnas }).map((__, j) => (
            <td key={j} className="px-2 py-4">
              <Skeleton className="h-4 w-full" />
            </td>
          ))}
        </tr>
      ))}
    </>
  );
}
```


`src/modules/desempeno/components/comunes/SegmentoPeriodo.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { Periodo } from "../../domain/entities";
import { usePreferencias } from "../../presentation/context/usePreferencias";

const OPCIONES: ReadonlyArray<readonly [Periodo, string]> = [
  ["hoy", "Hoy"],
  ["semana", "Semana"],
];

// Hoy/Semana (D8). Vive en Tarea 0 porque lo usan el tablero (carril 1) y la
// ficha (carril 2).
export function SegmentoPeriodo() {
  const { periodo, setPeriodo } = usePreferencias();
  return (
    <div role="group" aria-label="Periodo" className="inline-flex rounded-full bg-dsm-tarjeta p-[3px] shadow-sm ring-1 ring-border">
      {OPCIONES.map(([valor, texto]) => (
        <button
          key={valor}
          type="button"
          aria-pressed={periodo === valor}
          onClick={() => setPeriodo(valor)}
          className={cn(
            "rounded-full px-4 py-1.5 text-[13px] transition-colors",
            periodo === valor ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground",
          )}
        >
          {texto}
        </button>
      ))}
    </div>
  );
}
```

- [ ] **Step 8: Correr las pruebas de la tarea**

Run: `npx vitest run src/modules/desempeno/components`
Expected: PASS (12 + 3 + 8 = 23 pruebas). Si `InfoRapida` "se abre con el teclado" falla, confirmar en `node_modules/@radix-ui/react-hover-card/dist/index.mjs` que `HoverCardTrigger` compone `onFocus: context.onOpen` (versión ≥1.1); no cambiar a `Tooltip`.

- [ ] **Step 9: Tipos y lint**

Run: `npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: sin salida.

- [ ] **Step 10: Commit**

```bash
git add src/modules/desempeno/components/lib src/modules/desempeno/components/comunes
git commit -m "feat(desempeno): primitivos de UI — horas de la oficina, eje 7–20 h, popover, chips y estados"
```

---

## CARRIL 1 — tablero (rama `feat/desempeno-tablero`, worktree `../sistema-cobro-web-tablero`)

### Tarea 1.1 (Carril 1): línea de actividad en 4 vistas con popovers, selector "Vista" y leyenda

**Carril 1, rama `feat/desempeno-tablero`, worktree propio.** Depende de Tarea 0.

**Files:**
- Create: `src/modules/desempeno/components/linea/TarjetasLinea.tsx`
- Create: `src/modules/desempeno/components/linea/LineaActividad.tsx`
- Create: `src/modules/desempeno/components/linea/SelectorVista.tsx`
- Create: `src/modules/desempeno/components/linea/Leyenda.tsx`
- Test: `src/modules/desempeno/components/linea/__tests__/LineaActividad.test.tsx`
- Test: `src/modules/desempeno/components/linea/__tests__/SelectorVista.test.tsx`

**Interfaces:**
- Consumes: Tarea 0 (`LineaDelDia`, `Pausa`, `Tramo`, `HoraDelDia`, `PuntoLinea`, `Horario`), `VistaLinea`, `usePreferencias`, `renderConDesempeno`, `InfoRapida`, `Tarjeta*`, `escala.ts`, `format.ts`, `TIPO_ACTIVIDAD_META`).
- Produces:
  - `LineaActividad(props: LineaActividadProps)` con `LineaActividadProps = { linea: LineaDelDia | null; vista: VistaLinea; pausaMin: number; horario: Horario | null; ahora: string | null; tamano?: "mini" | "grande"; describir?: (actividadId: string) => string | null; etiquetaDia?: string }`. `data-testid`: `linea-actividad` (raíz, con `data-vista`), `punto`, `tramo`, `barra-hora`, `pausa`, `fuera-horario`, `marca-ahora`.
  - `ESCALA_BARRA_HORA = 12` (movimientos por hora que llenan la barra; solo dibujo).
  - `TarjetaPausa`, `TarjetaTramo`, `TarjetaHora`, `TarjetaPunto` (contenido de popover).
  - `SelectorVista()` (lee/escribe `usePreferencias().vista`; botones con `aria-label` "Puntos", "Bloques de trabajo", "Por hora", "Bloques + hora").
  - `Leyenda({ vista })`.

- [ ] **Step 1: Escribir las pruebas de la línea (fallan)**

`src/modules/desempeno/components/linea/__tests__/LineaActividad.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { LineaActividad } from "../LineaActividad";
import { makeLinea, makePausa } from "../../../application/__tests__/fakeDesempenoPort";

const base = {
  linea: makeLinea(),
  pausaMin: 45,
  horario: null,
  ahora: null,
};

describe("LineaActividad — Puntos", () => {
  it("un punto por actividad y la pausa sombreada", () => {
    render(<LineaActividad {...base} vista="puntos" />);
    expect(screen.getAllByTestId("punto")).toHaveLength(5);
    expect(screen.getAllByTestId("pausa")).toHaveLength(1);
    expect(screen.queryByTestId("tramo")).not.toBeInTheDocument();
  });

  it("el punto se ubica en la hora de la oficina (08:20 → 10.26 % del eje 7–20 h)", () => {
    render(<LineaActividad {...base} vista="puntos" />);
    const primero = screen
      .getAllByTestId("punto")
      .find((p) => p.getAttribute("data-id") === "pago:15082369")!;
    expect(parseFloat(primero.style.left)).toBeCloseTo(10.256, 2);
  });

  it("popover del punto con ratón", async () => {
    const user = userEvent.setup();
    render(<LineaActividad {...base} vista="puntos" />);
    const primero = screen
      .getAllByTestId("punto")
      .find((p) => p.getAttribute("data-id") === "pago:15082369")!;
    await user.hover(primero);
    expect(await screen.findByText("Pago · 08:20")).toBeInTheDocument();
  });
});

describe("LineaActividad — Bloques", () => {
  it("tramos del API y su popover sin cifras inventadas", async () => {
    const user = userEvent.setup();
    render(<LineaActividad {...base} vista="bloques" />);
    const tramos = screen.getAllByTestId("tramo");
    expect(tramos).toHaveLength(2);

    await user.hover(tramos[0]);

    expect(await screen.findByText("08:20–09:10")).toBeInTheDocument();
    expect(screen.getByText("2 · $300")).toBeInTheDocument();
    expect(screen.getByText("Clientes atendidos")).toBeInTheDocument();
  });

  it("popover de la pausa: duración, horas y qué hubo antes y después", async () => {
    const user = userEvent.setup();
    render(<LineaActividad {...base} vista="bloques" />);
    await user.hover(screen.getByTestId("pausa"));
    expect(await screen.findByText("Pausa · 1 h 12 min")).toBeInTheDocument();
    expect(screen.getByText("09:10–10:22")).toBeInTheDocument();
    expect(screen.getByText("09:10 · pago")).toBeInTheDocument();
    expect(screen.getByText("10:22 · pago")).toBeInTheDocument();
    expect(screen.getByText("Hueco de 45 min o más sin movimientos.")).toBeInTheDocument();
  });

  it("pausa en curso dice 'ahora' y 'sigue sin actividad'", async () => {
    const user = userEvent.setup();
    const linea = makeLinea({
      pausas: [makePausa({ hasta: "2026-10-14T22:40:00Z", minutos: 378, siguienteId: null, enCurso: true })],
    });
    render(<LineaActividad {...base} linea={linea} vista="bloques" />);
    await user.hover(screen.getByTestId("pausa"));
    expect(await screen.findByText("09:10–ahora")).toBeInTheDocument();
    expect(screen.getByText("sigue sin actividad")).toBeInTheDocument();
  });
});

describe("LineaActividad — Por hora y Bloques + hora", () => {
  it("13 barras (7..19) y popover de una hora con su importe del API", async () => {
    const user = userEvent.setup();
    render(<LineaActividad {...base} vista="por_hora" />);
    const barras = screen.getAllByTestId("barra-hora");
    expect(barras).toHaveLength(13);

    await user.hover(barras[3]); // 10:00

    expect(await screen.findByText("10:00–11:00")).toBeInTheDocument();
    expect(screen.getByText("1 · $150")).toBeInTheDocument();
  });

  it("una hora vacía lo dice", async () => {
    const user = userEvent.setup();
    render(<LineaActividad {...base} vista="por_hora" />);
    await user.hover(screen.getAllByTestId("barra-hora")[0]); // 07:00
    expect(await screen.findByText("Sin movimientos en esta hora.")).toBeInTheDocument();
  });

  it("Bloques + hora pinta tramos, barras y la tira de pausas", () => {
    render(<LineaActividad {...base} vista="bloques_hora" />);
    expect(screen.getAllByTestId("tramo")).toHaveLength(2);
    expect(screen.getAllByTestId("barra-hora")).toHaveLength(13);
    expect(screen.getAllByTestId("pausa")).toHaveLength(1);
  });
});

describe("LineaActividad — bordes", () => {
  it("sin línea no pinta actividad pero sí la marca de ahora si la hay", () => {
    render(<LineaActividad {...base} linea={null} vista="bloques" ahora="2026-10-14T22:40:00Z" />);
    expect(screen.queryByTestId("tramo")).not.toBeInTheDocument();
    expect(screen.getByTestId("marca-ahora")).toBeInTheDocument();
  });

  it("la marca de ahora solo aparece si se pasa `ahora` (días pasados no)", () => {
    render(<LineaActividad {...base} vista="bloques" />);
    expect(screen.queryByTestId("marca-ahora")).not.toBeInTheDocument();
  });

  it("con horario, sombrea antes y después", () => {
    render(<LineaActividad {...base} vista="bloques" horario={{ desde: "08:00", hasta: "18:00" }} />);
    expect(screen.getAllByTestId("fuera-horario")).toHaveLength(2);
  });

  it("describir permite al llamador nombrar mejor lo de antes y después (jornada)", async () => {
    const user = userEvent.setup();
    render(
      <LineaActividad
        {...base}
        vista="bloques"
        describir={(id) => (id === "pago:15082370" ? "09:10 · pago · PEDRO TEHUITZIL ZEPEDA" : null)}
      />,
    );
    await user.hover(screen.getByTestId("pausa"));
    expect(await screen.findByText("09:10 · pago · PEDRO TEHUITZIL ZEPEDA")).toBeInTheDocument();
  });
});
```

`src/modules/desempeno/components/linea/__tests__/SelectorVista.test.tsx`:

```tsx
import { beforeEach, describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SelectorVista } from "../SelectorVista";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import { CLAVE_VISTA_LINEA } from "../../../presentation/preferencias";

describe("SelectorVista (D16)", () => {
  beforeEach(() => localStorage.clear());

  it("Bloques viene marcado por defecto", () => {
    renderConDesempeno(<SelectorVista />);
    expect(screen.getByRole("button", { name: "Bloques de trabajo" })).toHaveAttribute("aria-pressed", "true");
  });

  it("elegir otra vista la marca y la recuerda en esta computadora", async () => {
    const user = userEvent.setup();
    renderConDesempeno(<SelectorVista />);
    await user.click(screen.getByRole("button", { name: "Por hora" }));
    expect(screen.getByRole("button", { name: "Por hora" })).toHaveAttribute("aria-pressed", "true");
    expect(localStorage.getItem(CLAVE_VISTA_LINEA)).toBe(JSON.stringify("por_hora"));
  });

  it("cada botón explica su vista al pasar el ratón", async () => {
    const user = userEvent.setup();
    renderConDesempeno(<SelectorVista />);
    await user.hover(screen.getByRole("button", { name: "Bloques + hora" }));
    expect(await screen.findByText("Tramos de trabajo con barras por hora encima")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Correrlas y verlas fallar**

Run: `npx vitest run src/modules/desempeno/components/linea`
Expected: FAIL con `Failed to resolve import "../LineaActividad"`.

- [ ] **Step 3: Implementar las tarjetas de la línea**

`src/modules/desempeno/components/linea/TarjetasLinea.tsx`:

```tsx
import type { HoraDelDia, Pausa, PuntoLinea, Tramo } from "../../domain/entities";
import { dinero, duracion, horaCorta } from "../lib/format";
import { TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";
import { TarjetaFilas, TarjetaNota, TarjetaSub, TarjetaTitulo } from "../comunes/InfoRapida";

function sub(texto: string, etiquetaDia?: string): string {
  return etiquetaDia ? `${texto} · ${etiquetaDia}` : texto;
}

export function TarjetaPausa({
  pausa,
  pausaMin,
  describir,
  etiquetaDia,
}: {
  pausa: Pausa;
  pausaMin: number;
  describir: (id: string) => string | null;
  etiquetaDia?: string;
}) {
  const antes = pausa.anteriorId ? describir(pausa.anteriorId) : null;
  const despues = pausa.enCurso
    ? "sigue sin actividad"
    : pausa.siguienteId
      ? describir(pausa.siguienteId)
      : null;
  return (
    <>
      <TarjetaTitulo>{`Pausa · ${duracion(pausa.minutos)}`}</TarjetaTitulo>
      <TarjetaSub>
        {sub(`${horaCorta(pausa.desde)}–${pausa.enCurso ? "ahora" : horaCorta(pausa.hasta)}`, etiquetaDia)}
      </TarjetaSub>
      <TarjetaFilas
        filas={[
          ["Antes", antes ?? "—"],
          ["Después", despues ?? "—"],
        ]}
      />
      <TarjetaNota>{`Hueco de ${pausaMin} min o más sin movimientos.`}</TarjetaNota>
    </>
  );
}

export function TarjetaTramo({ tramo, etiquetaDia }: { tramo: Tramo; etiquetaDia?: string }) {
  return (
    <>
      <TarjetaTitulo>{`${horaCorta(tramo.desde)}–${horaCorta(tramo.hasta)}`}</TarjetaTitulo>
      <TarjetaSub>{sub("Tramo de trabajo", etiquetaDia)}</TarjetaSub>
      <TarjetaFilas
        filas={[
          ["Pagos", `${tramo.pagos.n} · ${dinero(tramo.pagos.importe)}`],
          ["Visitas", String(tramo.visitas)],
          ["Condonaciones", String(tramo.condonaciones)],
          ["Clientes atendidos", String(tramo.clientes)],
        ]}
      />
    </>
  );
}

export function TarjetaHora({ hora, etiquetaDia }: { hora: HoraDelDia; etiquetaDia?: string }) {
  const vacia = hora.pagos === 0 && hora.visitas === 0 && hora.condonaciones === 0;
  return (
    <>
      <TarjetaTitulo>{`${hora.hora}:00–${hora.hora + 1}:00`}</TarjetaTitulo>
      {etiquetaDia && <TarjetaSub>{etiquetaDia}</TarjetaSub>}
      {vacia ? (
        <TarjetaNota>Sin movimientos en esta hora.</TarjetaNota>
      ) : (
        <TarjetaFilas
          filas={[
            ["Pagos", `${hora.pagos} · ${dinero(hora.importe)}`],
            ["Visitas", String(hora.visitas)],
            ["Condonaciones", String(hora.condonaciones)],
          ]}
        />
      )}
    </>
  );
}

export function TarjetaPunto({ punto, detalle }: { punto: PuntoLinea; detalle: string | null }) {
  return (
    <>
      <TarjetaTitulo>{`${TIPO_ACTIVIDAD_META[punto.tipo].etiqueta} · ${horaCorta(punto.hora)}`}</TarjetaTitulo>
      {detalle && <TarjetaNota>{detalle}</TarjetaNota>}
    </>
  );
}
```

(Nota: `TarjetaHora` usa `hora.hora + 1` solo para rotular el intervalo `[h, h+1)` que define el contrato; no es un cálculo de negocio.)

- [ ] **Step 4: Implementar la línea**

`src/modules/desempeno/components/linea/LineaActividad.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { LineaDelDia, Pausa, PuntoLinea } from "../../domain/entities";
import type { Horario } from "../../application/ports/DesempenoPort";
import type { VistaLinea } from "../../presentation/preferencias";
import { ancho, MARCAS_EJE, posicion, posicionHorario, posicionMinutos } from "../lib/escala";
import { horaCorta } from "../lib/format";
import { TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";
import { InfoRapida } from "../comunes/InfoRapida";
import { TarjetaHora, TarjetaPausa, TarjetaPunto, TarjetaTramo } from "./TarjetasLinea";

// Cuántos movimientos en una hora llenan la barra. Es solo la escala del dibujo.
export const ESCALA_BARRA_HORA = 12;
const HORAS_EN_EJE = 13;
const ORDEN_DIBUJO = { visita: 0, condonacion: 1, pago: 2 } as const;

export type LineaActividadProps = {
  linea: LineaDelDia | null;
  vista: VistaLinea;
  pausaMin: number;
  horario: Horario | null;
  /** Instante "ahora" del servidor (generado_at); solo si la línea es de hoy. */
  ahora: string | null;
  tamano?: "mini" | "grande";
  /** Cómo nombrar una actividad por su id en "Antes/Después" de una pausa. */
  describir?: (actividadId: string) => string | null;
  etiquetaDia?: string;
};

function describirConPuntos(linea: LineaDelDia | null) {
  return (id: string): string | null => {
    const p = linea?.puntos.find((x) => x.id === id);
    return p ? `${horaCorta(p.hora)} · ${TIPO_ACTIVIDAD_META[p.tipo].etiqueta.toLowerCase()}` : null;
  };
}

// LineaActividad pinta la línea del día en una de las 4 vistas (D16). Puntos,
// tramos, conteos por hora y pausas vienen del API (D10); aquí solo se ubican
// en el eje 7–20 h. Los elementos no son enfocables: sus popovers se abren con
// el ratón (spec §10).
export function LineaActividad({
  linea,
  vista,
  pausaMin,
  horario,
  ahora,
  tamano = "mini",
  describir,
  etiquetaDia,
}: LineaActividadProps) {
  const nombrar = describir ?? describirConPuntos(linea);
  const conActividad = linea !== null && linea.puntos.length > 0;
  return (
    <div
      data-testid="linea-actividad"
      data-vista={vista}
      className={cn(
        "relative w-full overflow-hidden rounded-[7px] bg-muted/60",
        tamano === "mini" ? "h-8" : "h-10",
      )}
    >
      {MARCAS_EJE.map((h) => (
        <i
          key={h}
          aria-hidden
          className="absolute inset-y-0 w-px bg-border"
          style={{ left: `${posicionMinutos(h * 60)}%` }}
        />
      ))}
      {horario && (
        <>
          <span
            data-testid="fuera-horario"
            className="dsm-fuera-horario absolute inset-y-0 left-0"
            style={{ width: `${posicionHorario(horario.desde)}%` }}
          />
          <span
            data-testid="fuera-horario"
            className="dsm-fuera-horario absolute inset-y-0 right-0"
            style={{ left: `${posicionHorario(horario.hasta)}%` }}
          />
        </>
      )}
      {conActividad && linea && vista === "puntos" && (
        <VistaPuntos linea={linea} ahora={ahora} pausaMin={pausaMin} nombrar={nombrar} etiquetaDia={etiquetaDia} />
      )}
      {conActividad && linea && vista === "bloques" && (
        <>
          <Pausas pausas={linea.pausas} alto="completa" pausaMin={pausaMin} nombrar={nombrar} etiquetaDia={etiquetaDia} />
          <Tramos linea={linea} mitad={false} etiquetaDia={etiquetaDia} />
        </>
      )}
      {conActividad && linea && vista === "por_hora" && (
        <>
          <BarrasHora linea={linea} etiquetaDia={etiquetaDia} />
          <Pausas pausas={linea.pausas} alto="tira" pausaMin={pausaMin} nombrar={nombrar} etiquetaDia={etiquetaDia} />
        </>
      )}
      {conActividad && linea && vista === "bloques_hora" && (
        <>
          <Tramos linea={linea} mitad etiquetaDia={etiquetaDia} />
          <BarrasHora linea={linea} etiquetaDia={etiquetaDia} />
          <Pausas pausas={linea.pausas} alto="tira" pausaMin={pausaMin} nombrar={nombrar} etiquetaDia={etiquetaDia} />
        </>
      )}
      {ahora && (
        <span
          data-testid="marca-ahora"
          aria-hidden
          className="absolute inset-y-0 w-px bg-dsm-marca"
          style={{ left: `${posicion(ahora)}%` }}
        />
      )}
    </div>
  );
}

function Pausas({
  pausas,
  alto,
  pausaMin,
  nombrar,
  etiquetaDia,
}: {
  pausas: readonly Pausa[];
  alto: "completa" | "tira";
  pausaMin: number;
  nombrar: (id: string) => string | null;
  etiquetaDia?: string;
}) {
  return (
    <>
      {pausas.map((p) => (
        <InfoRapida
          key={`${p.desde}-${p.hasta}`}
          contenido={<TarjetaPausa pausa={p} pausaMin={pausaMin} describir={nombrar} etiquetaDia={etiquetaDia} />}
        >
          <span
            data-testid="pausa"
            className={cn(
              "dsm-pausa absolute rounded-[3px]",
              alto === "completa" ? "inset-y-0" : "bottom-0 h-1.5",
              p.enCurso && "ring-1 ring-inset ring-[hsl(var(--dsm-ambar))]",
            )}
            style={{ left: `${posicion(p.desde)}%`, width: `${ancho(p.desde, p.hasta)}%` }}
          />
        </InfoRapida>
      ))}
    </>
  );
}

function Tramos({ linea, mitad, etiquetaDia }: { linea: LineaDelDia; mitad: boolean; etiquetaDia?: string }) {
  return (
    <>
      {linea.tramos.map((t) => (
        <InfoRapida key={`${t.desde}-${t.hasta}`} contenido={<TarjetaTramo tramo={t} etiquetaDia={etiquetaDia} />}>
          <span
            data-testid="tramo"
            className={cn(
              "absolute rounded-[5px]",
              t.pagos.n > 0 ? "bg-dsm-ok" : "bg-dsm-visita",
              mitad ? "bottom-2 top-1/2" : "inset-y-1.5",
            )}
            style={{ left: `${posicion(t.desde)}%`, width: `${Math.max(0.6, ancho(t.desde, t.hasta))}%` }}
          />
        </InfoRapida>
      ))}
    </>
  );
}

function altura(n: number): string {
  return `${Math.min(100, (n / ESCALA_BARRA_HORA) * 100)}%`;
}

function BarrasHora({ linea, etiquetaDia }: { linea: LineaDelDia; etiquetaDia?: string }) {
  return (
    <>
      {linea.porHora.map((h) => (
        <InfoRapida key={h.hora} contenido={<TarjetaHora hora={h} etiquetaDia={etiquetaDia} />}>
          <span
            data-testid="barra-hora"
            className="absolute bottom-1.5 top-0 flex flex-col-reverse px-px"
            style={{ left: `${posicionMinutos(h.hora * 60)}%`, width: `${100 / HORAS_EN_EJE}%` }}
          >
            {h.pagos > 0 && <i className="block bg-dsm-ok" style={{ height: altura(h.pagos) }} />}
            {h.visitas > 0 && <i className="block bg-dsm-visita" style={{ height: altura(h.visitas) }} />}
            {h.condonaciones > 0 && (
              <i className="block bg-dsm-violeta" style={{ height: altura(h.condonaciones) }} />
            )}
          </span>
        </InfoRapida>
      ))}
    </>
  );
}

function VistaPuntos({
  linea,
  ahora,
  pausaMin,
  nombrar,
  etiquetaDia,
}: {
  linea: LineaDelDia;
  ahora: string | null;
  pausaMin: number;
  nombrar: (id: string) => string | null;
  etiquetaDia?: string;
}) {
  const primero = linea.puntos[0];
  const ultimo = ahora ?? linea.puntos[linea.puntos.length - 1].hora;
  const ordenados: PuntoLinea[] = [...linea.puntos].sort(
    (a, b) => ORDEN_DIBUJO[a.tipo] - ORDEN_DIBUJO[b.tipo],
  );
  return (
    <>
      <span
        aria-hidden
        className="absolute inset-y-1 rounded-[4px] bg-foreground/[0.06]"
        style={{ left: `${posicion(primero.hora)}%`, width: `${ancho(primero.hora, ultimo)}%` }}
      />
      <Pausas pausas={linea.pausas} alto="completa" pausaMin={pausaMin} nombrar={nombrar} etiquetaDia={etiquetaDia} />
      {ordenados.map((p) => (
        <InfoRapida key={p.id} contenido={<TarjetaPunto punto={p} detalle={null} />}>
          <span
            data-testid="punto"
            data-id={p.id}
            data-tipo={p.tipo}
            className={cn(
              "absolute -ml-[3.5px] -mt-[3.5px] h-[7px] w-[7px] rounded-full",
              TIPO_ACTIVIDAD_META[p.tipo].clasePunto,
              p.tipo === "visita" ? "top-[70%]" : "top-[32%]",
            )}
            style={{ left: `${posicion(p.hora)}%` }}
          />
        </InfoRapida>
      ))}
    </>
  );
}
```

(El `sort` de `VistaPuntos` solo decide qué punto se dibuja encima; no altera datos.)

- [ ] **Step 5: Implementar selector y leyenda**

`src/modules/desempeno/components/linea/SelectorVista.tsx`:

```tsx
import { ChartColumnStacked, ChartGantt, Ellipsis, Layers, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import type { VistaLinea } from "../../presentation/preferencias";
import { usePreferencias } from "../../presentation/context/usePreferencias";
import { InfoRapida, TarjetaNota, TarjetaTitulo } from "../comunes/InfoRapida";

const OPCIONES: ReadonlyArray<{ vista: VistaLinea; icono: LucideIcon; titulo: string; detalle: string }> = [
  { vista: "puntos", icono: Ellipsis, titulo: "Puntos", detalle: "Cada movimiento como un punto" },
  {
    vista: "bloques",
    icono: ChartGantt,
    titulo: "Bloques de trabajo",
    detalle: "Tramos continuos de trabajo y huecos de pausa",
  },
  { vista: "por_hora", icono: ChartColumnStacked, titulo: "Por hora", detalle: "Movimientos por hora, apilados por tipo" },
  {
    vista: "bloques_hora",
    icono: Layers,
    titulo: "Bloques + hora",
    detalle: "Tramos de trabajo con barras por hora encima",
  },
];

export function SelectorVista() {
  const { vista, setVista } = usePreferencias();
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-muted-foreground">Vista</span>
      <div
        role="group"
        aria-label="Línea de actividad"
        className="inline-flex rounded-full bg-dsm-tarjeta p-[3px] shadow-sm ring-1 ring-border"
      >
        {OPCIONES.map((o) => {
          const Icono = o.icono;
          const activa = vista === o.vista;
          return (
            <InfoRapida
              key={o.vista}
              contenido={
                <>
                  <TarjetaTitulo>{o.titulo}</TarjetaTitulo>
                  <TarjetaNota>{o.detalle}</TarjetaNota>
                </>
              }
            >
              <button
                type="button"
                aria-label={o.titulo}
                aria-pressed={activa}
                onClick={() => setVista(o.vista)}
                className={cn(
                  "flex h-8 w-9 items-center justify-center rounded-full transition-colors",
                  activa ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground",
                )}
              >
                <Icono aria-hidden className="h-4 w-4" />
              </button>
            </InfoRapida>
          );
        })}
      </div>
    </div>
  );
}
```

`src/modules/desempeno/components/linea/Leyenda.tsx`:

```tsx
import type { VistaLinea } from "../../presentation/preferencias";

export function Leyenda({ vista }: { vista: VistaLinea }) {
  const conBloques = vista === "bloques" || vista === "bloques_hora";
  const conHoras = vista === "por_hora" || vista === "bloques_hora";
  return (
    <div className="flex flex-wrap items-center gap-3 text-[11.5px] text-muted-foreground">
      <span className="inline-flex items-center gap-1.5"><i className="h-2 w-2 rounded-full bg-dsm-ok" />Pago</span>
      <span className="inline-flex items-center gap-1.5"><i className="h-2 w-2 rounded-full bg-dsm-visita" />Visita</span>
      <span className="inline-flex items-center gap-1.5"><i className="h-2 w-2 rounded-full bg-dsm-violeta" />Condonación</span>
      {conBloques && (
        <span className="inline-flex items-center gap-1.5" title="Verde con pagos, gris solo visitas">
          <i className="h-2 w-4 rounded-sm bg-dsm-ok" />Trabajando
        </span>
      )}
      {conHoras && (
        <span className="inline-flex items-center gap-1.5">
          <i className="flex h-2.5 items-end gap-px"><b className="block h-2.5 w-1 bg-dsm-ok" /><b className="block h-1.5 w-1 bg-dsm-visita" /></i>
          Por hora
        </span>
      )}
      <span className="inline-flex items-center gap-1.5"><i className="dsm-pausa h-2 w-4 rounded-sm" />Pausa</span>
    </div>
  );
}
```

- [ ] **Step 6: Correr las pruebas de la tarea**

Run: `npx vitest run src/modules/desempeno/components/linea`
Expected: PASS (13 + 3 = 16 pruebas).

- [ ] **Step 7: Tipos y lint**

Run: `npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: sin salida.

- [ ] **Step 8: Commit**

```bash
git add src/modules/desempeno/components/linea
git commit -m "feat(desempeno): línea de actividad en cuatro vistas con popovers de punto, tramo, hora y pausa"
```

---

### Tarea 1.2 (Carril 1): tablero del equipo (Inicio) con popovers en nombre, estado y celdas

**Carril 1, rama `feat/desempeno-tablero`, después de la Tarea 1.1.**

**Files:**
- Create: `src/modules/desempeno/presentation/hooks/useTablero.ts`
- Create: `src/modules/desempeno/components/tablero/ordenTablero.ts`
- Create: `src/modules/desempeno/components/tablero/TarjetasTablero.tsx`
- Create: `src/modules/desempeno/components/tablero/TableroFila.tsx`
- Create: `src/modules/desempeno/components/tablero/FilaSinConfigurar.tsx`
- Create: `src/modules/desempeno/components/tablero/TableroToolbar.tsx`
- Create: `src/modules/desempeno/components/tablero/TableroScreen.tsx`
- Test: `src/modules/desempeno/presentation/hooks/__tests__/useTablero.test.tsx`
- Test: `src/modules/desempeno/components/tablero/__tests__/ordenTablero.test.ts`
- Test: `src/modules/desempeno/components/tablero/__tests__/TableroScreen.test.tsx`

**Interfaces:**
- Consumes: Tarea 0 (`Tablero`, `UsuarioTablero`, `Periodo`, builders, `useCargaRemota`, `useCadaMinuto`, `usePreferencias`, `useDesempenoPort`, `UMBRALES_PAUSA`, `HORARIO_PREDETERMINADO`, `renderConDesempeno`, formato, `InfoRapida`, tarjetas, `PuntoEstado`, `Estados`, `SegmentoPeriodo`), Tarea 1.1 (`LineaActividad`, `SelectorVista`, `Leyenda`).
- Produces:
  - `useTablero(): { tablero: Tablero | null; isLoading: boolean; error: DomainError | null; refresh: () => void }` — usa `usePreferencias().consultaTablero` y recarga cada 60 s. **La integración la usa también para la ficha.**
  - `TableroScreen({ onAbrirUsuario: (uid: string) => void; puedeConfigurar: boolean })`.
  - `ordenarUsuarios(usuarios, orden)`, `filtrarUsuarios(usuarios, q)`, `siguienteOrden(actual, clave)`, `ORDEN_TABLERO_INICIAL`, tipos `ClaveOrdenTablero`, `OrdenTablero`.
  - `FilaSinConfigurar({ usuario, puedeConfigurar, columnas })`.
  - Tarjetas: `TarjetaUsuario`, `TarjetaAhora`, `TarjetaPausas`, `TarjetaPagos`, `TarjetaCuentas`, `TarjetaCobro` (props `{ usuario: UsuarioTablero; periodo: Periodo; pausaMin: number }` donde aplique).

- [ ] **Step 1: Pruebas del hook y del orden (fallan)**

`src/modules/desempeno/presentation/hooks/__tests__/useTablero.test.tsx`:

```tsx
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { useTablero } from "../useTablero";
import { usePreferencias } from "../../context/usePreferencias";
import { DesempenoProvider } from "../../context/DesempenoProvider";
import { FakeDesempenoPort, makeTablero } from "../../../application/__tests__/fakeDesempenoPort";
import { DomainError } from "../../../domain/errors";
import { UN_MINUTO_MS } from "../lib/useCadaMinuto";

function envolver(port: FakeDesempenoPort) {
  return ({ children }: { children: ReactNode }) => (
    <DesempenoProvider port={port}>{children}</DesempenoProvider>
  );
}

describe("useTablero", () => {
  afterEach(() => vi.useRealTimers());

  it("pide Hoy con pausa de 45 min y sin horario al abrir", async () => {
    const port = new FakeDesempenoPort();
    const { result } = renderHook(() => useTablero(), { wrapper: envolver(port) });
    await waitFor(() => expect(result.current.tablero).not.toBeNull());
    expect(port.tableroCalls[0].consulta).toEqual({ periodo: "hoy", pausaMin: 45, horario: null });
  });

  it("cambiar a Semana vuelve a pedir y no muestra los datos de Hoy mientras llega", async () => {
    const port = new FakeDesempenoPort();
    const { result } = renderHook(() => ({ t: useTablero(), p: usePreferencias() }), {
      wrapper: envolver(port),
    });
    await waitFor(() => expect(result.current.t.tablero?.periodo).toBe("hoy"));
    let liberar: () => void = () => {};
    port.tableroResponse = () =>
      new Promise((res) => {
        liberar = () => res(makeTablero({ periodo: "semana" }));
      });

    act(() => result.current.p.setPeriodo("semana"));

    expect(result.current.t.tablero).toBeNull();
    await act(async () => liberar());
    expect(result.current.t.tablero?.periodo).toBe("semana");
    expect(port.tableroCalls.at(-1)?.consulta.periodo).toBe("semana");
  });

  it("se actualiza cada minuto y conserva los datos si la actualización falla", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const port = new FakeDesempenoPort();
    const { result } = renderHook(() => useTablero(), { wrapper: envolver(port) });
    await waitFor(() => expect(result.current.tablero).not.toBeNull());

    port.throwOnNext.obtenerTablero = new DomainError("network_error", "error de red");
    act(() => {
      vi.advanceTimersByTime(UN_MINUTO_MS);
    });

    await waitFor(() => expect(result.current.error?.code).toBe("network_error"));
    expect(port.tableroCalls).toHaveLength(2);
    expect(result.current.tablero).not.toBeNull();
  });
});
```

`src/modules/desempeno/components/tablero/__tests__/ordenTablero.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { filtrarUsuarios, ordenarUsuarios, siguienteOrden } from "../ordenTablero";
import { makeUsuarioTablero } from "../../../application/__tests__/fakeDesempenoPort";

const juan = makeUsuarioTablero();
const rosa = makeUsuarioTablero({
  uid: "uid-rosa",
  nombre: "Rosa Isela Pacheco",
  zonaNombre: "RUTA 22",
  pagos: { n: 24, importe: "5100.00" },
  estadoAhora: "sin_actividad",
  cobro: { cobrado: "5100.00", esperado: "11800.00", pct: null },
});

describe("ordenTablero", () => {
  it("ordena por zona por defecto y por pagos (importe del API) cuando se pide", () => {
    expect(ordenarUsuarios([rosa, juan], { clave: "zona", dir: 1 }).map((u) => u.uid)).toEqual(["uid-juan", "uid-rosa"]);
    expect(ordenarUsuarios([rosa, juan], { clave: "pagos", dir: -1 }).map((u) => u.uid)).toEqual(["uid-juan", "uid-rosa"]);
    expect(ordenarUsuarios([juan, rosa], { clave: "pagos", dir: 1 }).map((u) => u.uid)).toEqual(["uid-rosa", "uid-juan"]);
  });

  it("estado ahora: activo antes que sin actividad; % nulo al final", () => {
    expect(ordenarUsuarios([rosa, juan], { clave: "ahora", dir: 1 })[0].uid).toBe("uid-juan");
    expect(ordenarUsuarios([rosa, juan], { clave: "cobro", dir: -1 })[1].uid).toBe("uid-rosa");
  });

  it("buscar no distingue acentos ni mayúsculas y mira nombre y zona", () => {
    expect(filtrarUsuarios([juan, rosa], "mendez").map((u) => u.uid)).toEqual(["uid-juan"]);
    expect(filtrarUsuarios([juan, rosa], "ruta 22").map((u) => u.uid)).toEqual(["uid-rosa"]);
    expect(filtrarUsuarios([juan, rosa], "  ")).toHaveLength(2);
  });

  it("clic en la misma columna invierte; en otra, empieza en su sentido natural", () => {
    expect(siguienteOrden({ clave: "pagos", dir: -1 }, "pagos")).toEqual({ clave: "pagos", dir: 1 });
    expect(siguienteOrden({ clave: "zona", dir: 1 }, "pagos")).toEqual({ clave: "pagos", dir: -1 });
    expect(siguienteOrden({ clave: "pagos", dir: -1 }, "nombre")).toEqual({ clave: "nombre", dir: 1 });
  });
});
```

- [ ] **Step 2: Correrlas y verlas fallar**

Run: `npx vitest run src/modules/desempeno/presentation/hooks/__tests__/useTablero.test.tsx src/modules/desempeno/components/tablero/__tests__/ordenTablero.test.ts`
Expected: FAIL con `Failed to resolve import "../useTablero"`.

- [ ] **Step 3: Implementar hook y orden**

`src/modules/desempeno/presentation/hooks/useTablero.ts`:

```ts
import { useCallback } from "react";
import type { Tablero } from "../../domain/entities";
import type { DomainError } from "../../domain/errors";
import { obtenerTablero } from "../../application/usecases/consultas";
import { useDesempenoPort } from "../context/useDesempenoPort";
import { usePreferencias } from "../context/usePreferencias";
import { useCargaRemota } from "./lib/useCargaRemota";
import { useCadaMinuto } from "./lib/useCadaMinuto";

export type UseTableroReturn = {
  tablero: Tablero | null;
  isLoading: boolean;
  error: DomainError | null;
  refresh: () => void;
};

// useTablero: GET /v2/desempeno/usuarios con el periodo, umbral y horario de
// la pantalla. Se actualiza cada minuto (spec §5).
export function useTablero(): UseTableroReturn {
  const port = useDesempenoPort();
  const { consultaTablero } = usePreferencias();
  const cargar = useCallback(
    (signal: AbortSignal) => obtenerTablero(port, consultaTablero, signal),
    [port, consultaTablero],
  );
  const { datos, isLoading, error, refresh } = useCargaRemota(cargar);
  useCadaMinuto(refresh, true);
  return { tablero: datos, isLoading, error, refresh };
}
```

`src/modules/desempeno/components/tablero/ordenTablero.ts`:

```ts
import type { UsuarioTablero } from "../../domain/entities";
import { sinAcentos } from "../lib/format";

// Orden y búsqueda de filas YA recibidas. Number() sobre los decimales del API
// solo sirve para comparar; ninguna cifra se calcula aquí.

export type ClaveOrdenTablero =
  | "nombre"
  | "zona"
  | "ahora"
  | "horas"
  | "pausas"
  | "pagos"
  | "visitas"
  | "faltan"
  | "cobro";

export type OrdenTablero = { clave: ClaveOrdenTablero; dir: 1 | -1 };

export const ORDEN_TABLERO_INICIAL: OrdenTablero = { clave: "zona", dir: 1 };

const ASCENDENTES: readonly ClaveOrdenTablero[] = ["nombre", "zona", "ahora", "faltan"];
const RANGO_AHORA = { activo: 0, sin_actividad: 1, no_ha_empezado: 2 } as const;

function valor(u: UsuarioTablero, clave: ClaveOrdenTablero): string | number {
  switch (clave) {
    case "nombre":
      return u.nombre;
    case "zona":
      return u.zonaNombre ?? "";
    case "ahora":
      return RANGO_AHORA[u.estadoAhora];
    case "horas":
      return u.minutosActivos;
    case "pausas":
      return u.pausas.minutos;
    case "pagos":
      return Number(u.pagos.importe);
    case "visitas":
      return u.visitas;
    case "faltan":
      return u.coberturaPorSegmento.sinNada;
    case "cobro":
      return u.cobro.pct === null ? Number.NEGATIVE_INFINITY : Number(u.cobro.pct);
  }
}

export function ordenarUsuarios(
  usuarios: readonly UsuarioTablero[],
  orden: OrdenTablero,
): UsuarioTablero[] {
  return [...usuarios].sort((a, b) => {
    const x = valor(a, orden.clave);
    const y = valor(b, orden.clave);
    const c =
      typeof x === "string" && typeof y === "string"
        ? x.localeCompare(y, "es", { numeric: true })
        : (x as number) - (y as number);
    return c * orden.dir;
  });
}

export function filtrarUsuarios(usuarios: readonly UsuarioTablero[], q: string): UsuarioTablero[] {
  const buscado = sinAcentos(q.trim());
  if (buscado === "") return [...usuarios];
  return usuarios.filter((u) =>
    sinAcentos(`${u.nombre} ${u.zonaNombre ?? ""} ${u.email}`).includes(buscado),
  );
}

export function siguienteOrden(actual: OrdenTablero, clave: ClaveOrdenTablero): OrdenTablero {
  if (actual.clave === clave) return { clave, dir: actual.dir === 1 ? -1 : 1 };
  return { clave, dir: ASCENDENTES.includes(clave) ? 1 : -1 };
}
```

- [ ] **Step 4: Correr y ver pasar**

Run: `npx vitest run src/modules/desempeno/presentation/hooks/__tests__/useTablero.test.tsx src/modules/desempeno/components/tablero/__tests__/ordenTablero.test.ts`
Expected: PASS (3 + 4 = 7 pruebas).

- [ ] **Step 5: Prueba de la pantalla (falla)**

`src/modules/desempeno/components/tablero/__tests__/TableroScreen.test.tsx`:

```tsx
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TableroScreen } from "../TableroScreen";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import {
  FakeDesempenoPort,
  makeLinea,
  makeTablero,
  makeUsuarioTablero,
} from "../../../application/__tests__/fakeDesempenoPort";
import { DomainError } from "../../../domain/errors";

function montar(port = new FakeDesempenoPort(), onAbrirUsuario = vi.fn()) {
  renderConDesempeno(<TableroScreen onAbrirUsuario={onAbrirUsuario} puedeConfigurar />, { port });
  return { port, onAbrirUsuario };
}

describe("TableroScreen", () => {
  beforeEach(() => localStorage.clear());

  it("una fila por usuario configurado y la fila 'Sin configurar' con enlace", async () => {
    montar();
    expect(await screen.findByText("Juan Carlos Méndez")).toBeInTheDocument();
    expect(screen.getByText("Rosa Isela Pacheco")).toBeInTheDocument();
    const norma = screen.getByText("Norma Velázquez Rojas").closest("tr")!;
    expect(within(norma).getByText("Sin configurar")).toBeInTheDocument();
    expect(within(norma).getByText(/No tiene inicio de semana/)).toBeInTheDocument();
    expect(within(norma).getByRole("link", { name: "Configurar" })).toHaveAttribute("href", "/settings");
  });

  it("muestra cifras por usuario y NINGÚN total del equipo (D14)", async () => {
    montar();
    const fila = (await screen.findByText("Juan Carlos Méndez")).closest("tr")!;
    expect(within(fila).getByText("$8,600")).toBeInTheDocument();
    expect(within(fila).getByText("Faltan")).toBeInTheDocument();
    expect(within(fila).getByText("46")).toBeInTheDocument();
    expect(within(fila).getByText("65.9%")).toBeInTheDocument();
    expect(screen.queryByText(/total/i)).not.toBeInTheDocument();
    expect(document.querySelector("tfoot")).toBeNull();
  });

  it("la hora del encabezado es la del servidor en la zona de la oficina", async () => {
    montar();
    expect(await screen.findByText("Actualizado 16:40")).toBeInTheDocument();
    expect(screen.getByText("miércoles 14 de octubre · 16:40")).toBeInTheDocument();
  });

  it("Semana, umbral y horario viajan en la consulta (D7)", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("Juan Carlos Méndez");

    await user.click(screen.getByRole("button", { name: "Semana" }));
    await waitFor(() => expect(port.tableroCalls.at(-1)?.consulta.periodo).toBe("semana"));

    await user.selectOptions(screen.getByLabelText("Pausa desde"), "30");
    await waitFor(() => expect(port.tableroCalls.at(-1)?.consulta.pausaMin).toBe(30));

    await user.click(screen.getByRole("button", { name: "Horario" }));
    await waitFor(() =>
      expect(port.tableroCalls.at(-1)?.consulta.horario).toEqual({ desde: "08:00", hasta: "18:00" }),
    );
    await user.selectOptions(screen.getByLabelText("Fin de horario"), "19:00");
    await waitFor(() =>
      expect(port.tableroCalls.at(-1)?.consulta.horario).toEqual({ desde: "08:00", hasta: "19:00" }),
    );
  });

  it("en Semana la línea se pinta por día con su etiqueta", async () => {
    const port = new FakeDesempenoPort();
    port.tableroResponse = makeTablero({
      periodo: "semana",
      usuarios: [
        makeUsuarioTablero({ lineas: [makeLinea({ dia: "2026-10-13" }), makeLinea({ dia: "2026-10-14" })] }),
      ],
    });
    montar(port);
    const fila = (await screen.findByText("Juan Carlos Méndez")).closest("tr")!;
    expect(within(fila).getAllByTestId("linea-actividad")).toHaveLength(2);
    expect(within(fila).getByText("mar 13 oct")).toBeInTheDocument();
  });

  it("popover de pagos: solo efectivo, transferencia y cheque; condonación aparte; sin Depósito", async () => {
    const user = userEvent.setup();
    montar();
    const fila = (await screen.findByText("Juan Carlos Méndez")).closest("tr")!;
    await user.hover(within(fila).getByText("$8,600"));
    expect(await screen.findByText("Pagos de hoy")).toBeInTheDocument();
    expect(screen.getByText("Transferencia")).toBeInTheDocument();
    expect(screen.getByText("3 · $800")).toBeInTheDocument();
    expect(screen.getByText("Condonaciones (aparte)")).toBeInTheDocument();
    expect(screen.queryByText(/Depósito/)).not.toBeInTheDocument();
  });

  it("popover del nombre con teclado, sin modelo de teléfono", async () => {
    montar();
    const nombre = await screen.findByText("Juan Carlos Méndez");
    nombre.focus();
    expect(await screen.findByText("Ventas en su universo")).toBeInTheDocument();
    expect(screen.getByText("232")).toBeInTheDocument();
    expect(screen.queryByText(/Teléfono/)).not.toBeInTheDocument();
  });

  it("popover de cuentas con los segmentos del API", async () => {
    const user = userEvent.setup();
    montar();
    const fila = (await screen.findByText("Juan Carlos Méndez")).closest("tr")!;
    await user.hover(within(fila).getByText("Faltan"));
    expect(await screen.findByText("Cuentas de la semana")).toBeInTheDocument();
    expect(screen.getByText("Sin visitar")).toBeInTheDocument();
    expect(screen.getByText("140")).toBeInTheDocument();
  });

  it("ordenar por Pagos y buscar sin acentos", async () => {
    const user = userEvent.setup();
    montar();
    await screen.findByText("Juan Carlos Méndez");
    // Primer clic: mayor a menor ($8,600 antes que $5,100); segundo: al revés.
    await user.click(screen.getByRole("button", { name: /Pagos/ }));
    expect(screen.getAllByRole("row", { name: /Abrir ficha de/ })[0]).toHaveAccessibleName(
      "Abrir ficha de Juan Carlos Méndez",
    );
    await user.click(screen.getByRole("button", { name: /Pagos/ }));
    expect(screen.getAllByRole("row", { name: /Abrir ficha de/ })[0]).toHaveAccessibleName(
      "Abrir ficha de Rosa Isela Pacheco",
    );

    await user.type(screen.getByPlaceholderText("Buscar cobrador o zona"), "pacheco");
    expect(screen.queryByText("Juan Carlos Méndez")).not.toBeInTheDocument();
    expect(screen.getByText("Rosa Isela Pacheco")).toBeInTheDocument();

    await user.clear(screen.getByPlaceholderText("Buscar cobrador o zona"));
    await user.type(screen.getByPlaceholderText("Buscar cobrador o zona"), "zzz");
    expect(screen.getByText("Nadie coincide con “zzz”")).toBeInTheDocument();
  });

  it("clic o Enter en la fila abre la ficha", async () => {
    const user = userEvent.setup();
    const { onAbrirUsuario } = montar();
    const fila = (await screen.findByText("Rosa Isela Pacheco")).closest("tr")!;
    await user.click(within(fila).getByText("$5,100"));
    expect(onAbrirUsuario).toHaveBeenCalledWith("uid-rosa");
    fila.focus();
    await user.keyboard("{Enter}");
    expect(onAbrirUsuario).toHaveBeenCalledTimes(2);
  });

  it("error al cargar: mensaje y Reintentar", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    port.throwOnNext.obtenerTablero = new DomainError(
      "desempeno_firestore_unavailable",
      "no se pudo consultar a los usuarios",
    );
    montar(port);
    expect(await screen.findByText("No se pudo cargar el tablero")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(await screen.findByText("Juan Carlos Méndez")).toBeInTheDocument();
  });

  it("falla la actualización: se quedan las filas y aparece el aviso", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    montar(port);
    await screen.findByText("Juan Carlos Méndez");
    port.throwOnNext.obtenerTablero = new DomainError("network_error", "error de red");
    await user.click(screen.getByRole("button", { name: "Actualizar" }));
    expect(await screen.findByText("No se pudo actualizar. Se muestran los datos de las 16:40.")).toBeInTheDocument();
    expect(screen.getByText("Juan Carlos Méndez")).toBeInTheDocument();
  });

  it("sin usuarios: estado vacío", async () => {
    const port = new FakeDesempenoPort();
    port.tableroResponse = makeTablero({ usuarios: [] });
    montar(port);
    expect(await screen.findByText("No hay usuarios para mostrar")).toBeInTheDocument();
  });

  it("la vista elegida cambia la línea de todas las filas", async () => {
    const user = userEvent.setup();
    montar();
    await screen.findByText("Juan Carlos Méndez");
    expect(screen.getAllByTestId("linea-actividad")[0]).toHaveAttribute("data-vista", "bloques");
    await user.click(screen.getByRole("button", { name: "Puntos" }));
    expect(screen.getAllByTestId("linea-actividad")[0]).toHaveAttribute("data-vista", "puntos");
  });
});
```

- [ ] **Step 6: Correrla y verla fallar**

Run: `npx vitest run src/modules/desempeno/components/tablero/__tests__/TableroScreen.test.tsx`
Expected: FAIL con `Failed to resolve import "../TableroScreen"`.

- [ ] **Step 7: Implementar tarjetas, filas, barra y pantalla**

`src/modules/desempeno/components/tablero/TarjetasTablero.tsx`:

```tsx
import type { Periodo, UsuarioTablero } from "../../domain/entities";
import {
  diaDeNegocioDe,
  dinero,
  duracionCorta,
  fechaCorta,
  horaCorta,
  plural,
  porcentaje,
} from "../lib/format";
import { TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";
import {
  TarjetaBarras,
  TarjetaFilas,
  TarjetaLista,
  TarjetaNota,
  TarjetaSeparador,
  TarjetaSub,
  TarjetaTitulo,
} from "../comunes/InfoRapida";

function cuando(iso: string | null): string {
  return iso ? `${fechaCorta(diaDeNegocioDe(iso))} ${horaCorta(iso)}` : "—";
}

const DEL_PERIODO: Record<Periodo, string> = { hoy: "de hoy", semana: "de la semana" };

export function TarjetaUsuario({ usuario: u }: { usuario: UsuarioTablero }) {
  return (
    <>
      <TarjetaTitulo>{u.nombre}</TarjetaTitulo>
      <TarjetaSub>{u.email}</TarjetaSub>
      <TarjetaFilas
        filas={[
          ["Zona", u.zonaNombre ?? "—"],
          ["Inicio de semana", cuando(u.inicioSemana)],
          ["Ventas en su universo", String(u.ventasUniverso)],
          ["Primera actividad", cuando(u.primeraActividad)],
          ["Última actividad", cuando(u.ultimaActividad)],
        ]}
      />
    </>
  );
}

export function TarjetaAhora({ usuario: u, pausaMin }: { usuario: UsuarioTablero; pausaMin: number }) {
  if (u.estadoAhora === "no_ha_empezado" || !u.ultima) {
    return (
      <>
        <TarjetaTitulo>No ha empezado</TarjetaTitulo>
        <TarjetaSub>{`última actividad: ${cuando(u.ultimaActividad)}`}</TarjetaSub>
      </>
    );
  }
  const titulo =
    u.estadoAhora === "activo"
      ? "Activo"
      : `Sin actividad desde ${u.estadoDesde ? horaCorta(u.estadoDesde) : "—"}`;
  return (
    <>
      <TarjetaTitulo>{titulo}</TarjetaTitulo>
      <TarjetaFilas
        filas={[
          ["Último", TIPO_ACTIVIDAD_META[u.ultima.tipo].etiqueta],
          ["Cliente", u.ultima.clienteNombre],
          ["Hora", horaCorta(u.ultima.hora)],
        ]}
      />
      {u.estadoAhora === "sin_actividad" && (
        <TarjetaNota>{`Cuenta como pausa: pasaron ${pausaMin} min o más sin movimientos.`}</TarjetaNota>
      )}
    </>
  );
}

export function TarjetaPausas({
  usuario: u,
  periodo,
  pausaMin,
}: {
  usuario: UsuarioTablero;
  periodo: Periodo;
  pausaMin: number;
}) {
  const filas: Array<readonly [string, string]> = u.lineas.flatMap((l) =>
    l.pausas.map(
      (p) =>
        [
          `${periodo === "semana" ? `${fechaCorta(l.dia)} · ` : ""}${horaCorta(p.desde)}–${p.enCurso ? "ahora" : horaCorta(p.hasta)}`,
          `${duracionCorta(p.minutos)}${p.enCurso ? " · en curso" : ""}`,
        ] as const,
    ),
  );
  return (
    <>
      <TarjetaTitulo>{`Pausas ${DEL_PERIODO[periodo]}`}</TarjetaTitulo>
      <TarjetaSub>{`huecos de ${pausaMin} min o más`}</TarjetaSub>
      <TarjetaLista filas={filas} />
    </>
  );
}

export function TarjetaPagos({ usuario: u, periodo }: { usuario: UsuarioTablero; periodo: Periodo }) {
  const c = (n: number, importe: string) => `${n} · ${dinero(importe)}`;
  return (
    <>
      <TarjetaTitulo>{`Pagos ${DEL_PERIODO[periodo]}`}</TarjetaTitulo>
      <TarjetaSub>{`${plural(u.pagos.n, "pago", "pagos")} · ${dinero(u.pagos.importe)}`}</TarjetaSub>
      <TarjetaFilas
        filas={[
          ["Efectivo", c(u.pagosPorForma.efectivo.n, u.pagosPorForma.efectivo.importe)],
          ["Transferencia", c(u.pagosPorForma.transferencia.n, u.pagosPorForma.transferencia.importe)],
          ["Cheque", c(u.pagosPorForma.cheque.n, u.pagosPorForma.cheque.importe)],
        ]}
      />
      <TarjetaSeparador />
      <TarjetaFilas
        filas={[
          ["Condonaciones (aparte)", c(u.condonaciones.n, u.condonaciones.importe)],
          ["Sin aplicar", c(u.noAplicados.n, u.noAplicados.importe)],
        ]}
      />
    </>
  );
}

export function TarjetaCuentas({ usuario: u }: { usuario: UsuarioTablero }) {
  const s = u.coberturaPorSegmento;
  const total = u.cobertura.total;
  return (
    <>
      <TarjetaTitulo>Cuentas de la semana</TarjetaTitulo>
      <TarjetaSub>{`${total} ventas en su universo`}</TarjetaSub>
      <TarjetaBarras
        filas={[
          { etiqueta: "Sin visitar", n: s.sinVisitar, total, clase: "bg-foreground/70" },
          { etiqueta: "Volver", n: s.volver, total, clase: "bg-dsm-ambar" },
          { etiqueta: "Después", n: s.despues, total, clase: "bg-dsm-violeta" },
          { etiqueta: "Pagados", n: s.pagados, total, clase: "bg-dsm-ok" },
          { etiqueta: "Sin nada", n: s.sinNada, total, clase: "bg-dsm-rojo" },
        ]}
      />
      <TarjetaNota>Semana del usuario, desde su inicio de semana.</TarjetaNota>
    </>
  );
}

export function TarjetaCobro({ usuario: u }: { usuario: UsuarioTablero }) {
  return (
    <>
      <TarjetaTitulo>{`Cobro de la semana · ${porcentaje(u.cobro.pct)}`}</TarjetaTitulo>
      <TarjetaSub>{`desde ${cuando(u.inicioSemana)}`}</TarjetaSub>
      <TarjetaFilas
        filas={[
          ["Cobrado", dinero(u.cobro.cobrado)],
          ["Esperado", dinero(u.cobro.esperado)],
        ]}
      />
    </>
  );
}
```

`src/modules/desempeno/components/tablero/TableroFila.tsx`:

```tsx
import type { Periodo, UsuarioTablero } from "../../domain/entities";
import type { Horario } from "../../application/ports/DesempenoPort";
import type { VistaLinea } from "../../presentation/preferencias";
import { diaDeNegocioDe, dinero, duracionCorta, fechaCorta, horaCorta, plural, porcentaje } from "../lib/format";
import { InfoRapida } from "../comunes/InfoRapida";
import { PuntoEstado } from "../comunes/PuntoEstado";
import { LineaActividad } from "../linea/LineaActividad";
import {
  TarjetaAhora,
  TarjetaCobro,
  TarjetaCuentas,
  TarjetaPagos,
  TarjetaPausas,
  TarjetaUsuario,
} from "./TarjetasTablero";

const CELDA = "border-y border-border bg-dsm-tarjeta px-2 py-3.5 align-middle";
const DISPARADOR = "cursor-help underline decoration-dotted decoration-foreground/25 underline-offset-[3px]";

function textoAhora(u: UsuarioTablero): string {
  if (u.estadoAhora === "activo") return "Activo";
  if (u.estadoAhora === "sin_actividad") return `Desde ${u.estadoDesde ? horaCorta(u.estadoDesde) : "—"}`;
  return "No ha empezado";
}

export function TableroFila({
  usuario: u,
  periodo,
  pausaMin,
  horario,
  vista,
  generadoAt,
  onAbrir,
}: {
  usuario: UsuarioTablero;
  periodo: Periodo;
  pausaMin: number;
  horario: Horario | null;
  vista: VistaLinea;
  generadoAt: string;
  onAbrir: (uid: string) => void;
}) {
  const hoy = diaDeNegocioDe(generadoAt);
  const pct = u.cobertura.total > 0 ? (u.cobertura.conAlgo / u.cobertura.total) * 100 : 0;
  return (
    <tr
      tabIndex={0}
      aria-label={`Abrir ficha de ${u.nombre}`}
      onClick={() => onAbrir(u.uid)}
      onKeyDown={(e) => {
        if (e.key === "Enter" && e.target === e.currentTarget) onAbrir(u.uid);
      }}
      className="group cursor-pointer outline-none [&>td]:transition-colors hover:[&>td]:bg-dsm-marca-fondo focus-visible:[&>td]:bg-dsm-marca-fondo"
    >
      <td className={`${CELDA} max-w-[220px] rounded-l-[14px] border-l pl-[18px]`}>
        <InfoRapida contenido={<TarjetaUsuario usuario={u} />}>
          <span tabIndex={0} className="cursor-help font-serif text-[17px] leading-tight">
            {u.nombre}
          </span>
        </InfoRapida>
        <div className="mt-0.5 font-mono text-[11px] text-muted-foreground">{u.zonaNombre}</div>
      </td>
      <td className={CELDA}>
        <InfoRapida contenido={<TarjetaAhora usuario={u} pausaMin={pausaMin} />}>
          <span tabIndex={0} className="inline-flex cursor-help items-center gap-2 text-[13px]">
            <PuntoEstado estado={u.estadoAhora} />
            {textoAhora(u)}
          </span>
        </InfoRapida>
      </td>
      <td className={`${CELDA} w-[32%] min-w-[260px]`}>
        {periodo === "hoy" ? (
          <LineaActividad
            linea={u.lineas[0] ?? null}
            vista={vista}
            pausaMin={pausaMin}
            horario={horario}
            ahora={u.lineas[0]?.dia === hoy || u.lineas.length === 0 ? generadoAt : null}
          />
        ) : (
          <div className="flex gap-1">
            {u.lineas.map((l) => (
              <div key={l.dia} className="min-w-0 flex-1">
                <LineaActividad
                  linea={l}
                  vista={vista}
                  pausaMin={pausaMin}
                  horario={horario}
                  ahora={l.dia === hoy ? generadoAt : null}
                  etiquetaDia={fechaCorta(l.dia)}
                />
                <span className="mt-1 block text-center font-mono text-[10px] text-muted-foreground">
                  {fechaCorta(l.dia)}
                </span>
              </div>
            ))}
          </div>
        )}
      </td>
      <td className={`${CELDA} text-right tabular`}>
        {u.minutosActivos > 0 ? duracionCorta(u.minutosActivos) : "—"}
      </td>
      <td className={`${CELDA} text-right tabular`}>
        {u.pausas.n > 0 ? (
          <InfoRapida contenido={<TarjetaPausas usuario={u} periodo={periodo} pausaMin={pausaMin} />}>
            <span tabIndex={0} className="inline-flex flex-col items-end">
              <b className={`font-medium ${DISPARADOR}`}>{duracionCorta(u.pausas.minutos)}</b>
              <span className={`text-[11px] ${u.pausas.enCurso ? "text-dsm-ambar" : "text-muted-foreground"}`}>
                {u.pausas.enCurso ? `${u.pausas.n} · en curso` : plural(u.pausas.n, "pausa", "pausas")}
              </span>
            </span>
          </InfoRapida>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      <td className={`${CELDA} text-right tabular`}>
        <InfoRapida contenido={<TarjetaPagos usuario={u} periodo={periodo} />}>
          <span tabIndex={0} className="inline-flex flex-col items-end">
            <b className={`font-medium ${DISPARADOR}`}>{dinero(u.pagos.importe)}</b>
            <span className="text-[11px] text-muted-foreground">{plural(u.pagos.n, "pago", "pagos")}</span>
          </span>
        </InfoRapida>
      </td>
      <td className={`${CELDA} text-right tabular`}>{u.visitas}</td>
      <td className={`${CELDA} min-w-[150px]`}>
        <InfoRapida contenido={<TarjetaCuentas usuario={u} />}>
          <span tabIndex={0} className="block cursor-help">
            <span className="text-[13px]">
              <span className={DISPARADOR}>Faltan</span> <b className="font-medium">{u.coberturaPorSegmento.sinNada}</b>{" "}
              de {u.cobertura.total}
            </span>
            <span className="mt-1.5 block h-1.5 overflow-hidden rounded-full bg-muted">
              <span className="block h-full bg-dsm-marca" style={{ width: `${pct}%` }} />
            </span>
          </span>
        </InfoRapida>
      </td>
      <td className={`${CELDA} rounded-r-[14px] border-r pr-[18px] text-right tabular`}>
        <InfoRapida contenido={<TarjetaCobro usuario={u} />}>
          <b tabIndex={0} className={`font-medium ${DISPARADOR}`}>
            {porcentaje(u.cobro.pct)}
          </b>
        </InfoRapida>
      </td>
    </tr>
  );
}
```

(La barra de progreso usa `conAlgo/total` del API solo como ancho de dibujo; el número que se lee es `sin_nada` tal como llega — ver huecos.)

`src/modules/desempeno/components/tablero/FilaSinConfigurar.tsx`:

```tsx
import { Link } from "react-router-dom";
import type { UsuarioTablero } from "../../domain/entities";

const MENSAJE = {
  sin_zona: "No tiene zona asignada. Sin zona no se puede medir su jornada ni sus cuentas.",
  sin_inicio_semana: "No tiene inicio de semana. Sin él no se puede medir su jornada ni sus cuentas.",
} as const;

// Usuario sin zona o sin inicio de semana (spec §5). El enlace va a Usuarios
// (/settings), que es donde se edita FECHA_CARGA_INICIAL y la zona del usuario
// (src/components/settings/UserCard.tsx) — ver huecos del plan.
export function FilaSinConfigurar({
  usuario: u,
  puedeConfigurar,
  columnas,
}: {
  usuario: UsuarioTablero;
  puedeConfigurar: boolean;
  columnas: number;
}) {
  const motivo = u.motivoNoConfigurado ?? "sin_zona";
  return (
    <tr>
      <td className="rounded-l-[14px] border-y border-l border-dashed border-border px-2 py-3.5 pl-[18px]">
        <div className="font-serif text-[17px] leading-tight text-muted-foreground">{u.nombre}</div>
        <div className="mt-0.5 font-mono text-[11px] text-muted-foreground">{u.zonaNombre ?? u.email}</div>
      </td>
      <td className="border-y border-dashed border-border px-2 py-3.5">
        <span className="inline-flex items-center gap-2 text-[13px] text-muted-foreground">
          <span aria-hidden className="h-[7px] w-[7px] rounded-full bg-muted-foreground/50" />
          Sin configurar
        </span>
      </td>
      <td colSpan={columnas - 3} className="border-y border-dashed border-border px-2 py-3.5 text-[12.5px] text-muted-foreground">
        {MENSAJE[motivo]}
      </td>
      <td className="rounded-r-[14px] border-y border-r border-dashed border-border px-2 py-3.5 pr-[18px] text-right">
        {puedeConfigurar ? (
          <Link
            to="/settings"
            className="inline-flex h-9 items-center rounded-full bg-dsm-tarjeta px-4 text-[13px] ring-1 ring-border hover:bg-muted"
          >
            Configurar
          </Link>
        ) : (
          <span className="text-[12px] text-muted-foreground">Pide a un administrador</span>
        )}
      </td>
    </tr>
  );
}
```

`src/modules/desempeno/components/tablero/TableroToolbar.tsx`:

```tsx
import { Search } from "lucide-react";
import { cn } from "@/lib/utils";
import { HORARIO_PREDETERMINADO, UMBRALES_PAUSA } from "../../presentation/preferencias";
import { usePreferencias } from "../../presentation/context/usePreferencias";
import { SegmentoPeriodo } from "../comunes/SegmentoPeriodo";
import { SelectorVista } from "../linea/SelectorVista";
import { Leyenda } from "../linea/Leyenda";

const INICIOS = ["07:00", "07:30", "08:00", "08:30", "09:00"];
const FINES = ["17:00", "17:30", "18:00", "18:30", "19:00"];
const SELECT = "h-9 rounded-full bg-dsm-tarjeta pl-3.5 pr-7 text-[13px] ring-1 ring-border disabled:opacity-50";

export function TableroToolbar({ q, onQ }: { q: string; onQ: (q: string) => void }) {
  const { pausaMin, setPausaMin, horario, setHorario, vista } = usePreferencias();
  const conHorario = horario !== null;
  const actual = horario ?? HORARIO_PREDETERMINADO;
  return (
    <div className="flex flex-wrap items-center gap-2.5">
      <SegmentoPeriodo />
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        Pausa desde
        <select
          className={SELECT}
          value={pausaMin}
          onChange={(e) => setPausaMin(Number(e.target.value))}
        >
          {UMBRALES_PAUSA.map((m) => (
            <option key={m} value={m}>{`${m} min`}</option>
          ))}
        </select>
      </label>
      <SelectorVista />
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <button
          type="button"
          aria-pressed={conHorario}
          onClick={() => setHorario(conHorario ? null : HORARIO_PREDETERMINADO)}
          className={cn(
            "h-9 rounded-full px-3.5 text-[13px] ring-1 ring-border",
            conHorario ? "bg-foreground text-background" : "bg-dsm-tarjeta text-foreground",
          )}
        >
          Horario
        </button>
        <select
          aria-label="Inicio de horario"
          className={SELECT}
          disabled={!conHorario}
          value={actual.desde}
          onChange={(e) => setHorario({ desde: e.target.value, hasta: actual.hasta })}
        >
          {INICIOS.map((h) => <option key={h} value={h}>{h}</option>)}
        </select>
        –
        <select
          aria-label="Fin de horario"
          className={SELECT}
          disabled={!conHorario}
          value={actual.hasta}
          onChange={(e) => setHorario({ desde: actual.desde, hasta: e.target.value })}
        >
          {FINES.map((h) => <option key={h} value={h}>{h}</option>)}
        </select>
      </div>
      <label className="relative">
        <Search aria-hidden className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <input
          value={q}
          onChange={(e) => onQ(e.target.value)}
          placeholder="Buscar cobrador o zona"
          className="h-9 w-[260px] rounded-full bg-dsm-tarjeta pl-8 pr-3 text-[13px] ring-1 ring-border outline-none focus:ring-foreground/40"
        />
      </label>
      <Leyenda vista={vista} />
    </div>
  );
}
```

`src/modules/desempeno/components/tablero/TableroScreen.tsx`:

```tsx
import { useMemo, useState } from "react";
import { ArrowDown, ArrowUp, ChevronsUpDown, RefreshCw } from "lucide-react";
import { cn } from "@/lib/utils";
import { useTablero } from "../../presentation/hooks/useTablero";
import { usePreferencias } from "../../presentation/context/usePreferencias";
import { diaDeNegocioDe, fechaLarga, horaCorta } from "../lib/format";
import { MARCAS_EJE, posicionMinutos } from "../lib/escala";
import { AvisoPantallaAngosta, ErrorConReintento, EstadoVacio, FilasCargando } from "../comunes/Estados";
import { TableroToolbar } from "./TableroToolbar";
import { TableroFila } from "./TableroFila";
import { FilaSinConfigurar } from "./FilaSinConfigurar";
import {
  filtrarUsuarios,
  ORDEN_TABLERO_INICIAL,
  ordenarUsuarios,
  siguienteOrden,
  type ClaveOrdenTablero,
  type OrdenTablero,
} from "./ordenTablero";

const COLUMNAS = 9;

function Encabezado({
  clave,
  texto,
  orden,
  onOrdenar,
  derecha,
}: {
  clave: ClaveOrdenTablero;
  texto: string;
  orden: OrdenTablero;
  onOrdenar: (c: ClaveOrdenTablero) => void;
  derecha?: boolean;
}) {
  const activa = orden.clave === clave;
  const Icono = activa ? (orden.dir === 1 ? ArrowUp : ArrowDown) : ChevronsUpDown;
  return (
    <th className={cn("h-7 px-2 font-normal", derecha && "text-right")}>
      <button
        type="button"
        onClick={() => onOrdenar(clave)}
        className="inline-flex items-center gap-1 font-mono text-[10.5px] uppercase tracking-[0.08em] text-muted-foreground hover:text-foreground"
      >
        {texto}
        <Icono aria-hidden className={cn("h-3 w-3", !activa && "opacity-40")} />
      </button>
    </th>
  );
}

// TableroScreen: Inicio para quien tiene DESEMPENO_COBRADORES. Una fila por
// usuario, sin totales del equipo (D14).
export function TableroScreen({
  onAbrirUsuario,
  puedeConfigurar,
}: {
  onAbrirUsuario: (uid: string) => void;
  puedeConfigurar: boolean;
}) {
  const { tablero, isLoading, error, refresh } = useTablero();
  const { vista, horario } = usePreferencias();
  const [q, setQ] = useState("");
  const [orden, setOrden] = useState<OrdenTablero>(ORDEN_TABLERO_INICIAL);

  const configurados = useMemo(
    () => ordenarUsuarios(filtrarUsuarios(tablero?.usuarios.filter((u) => u.configurado) ?? [], q), orden),
    [tablero, q, orden],
  );
  const sinConfigurar = useMemo(
    () => filtrarUsuarios(tablero?.usuarios.filter((u) => !u.configurado) ?? [], q),
    [tablero, q],
  );
  const onOrdenar = (c: ClaveOrdenTablero) => setOrden((o) => siguienteOrden(o, c));

  return (
    <section className="flex min-h-full flex-col gap-6 bg-dsm-canvas px-10 pb-24 pt-9">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-serif text-[54px] font-light leading-none tracking-[-0.03em]">
            Desempeño de cobradores
          </h1>
          <p className="mt-2 text-[15px] text-muted-foreground">
            {tablero
              ? tablero.periodo === "hoy"
                ? `${fechaLarga(diaDeNegocioDe(tablero.generadoAt))} · ${horaCorta(tablero.generadoAt)}`
                : "Semana en curso · cada quien desde su inicio de semana"
              : " "}
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span aria-hidden className={cn("h-2 w-2 rounded-full bg-dsm-ok", isLoading && "animate-pulse")} />
          {tablero && <span>{`Actualizado ${horaCorta(tablero.generadoAt)}`}</span>}
          <button
            type="button"
            aria-label="Actualizar"
            onClick={refresh}
            className="flex h-8 w-8 items-center justify-center rounded-full ring-1 ring-border hover:bg-muted"
          >
            <RefreshCw aria-hidden className={cn("h-3.5 w-3.5", isLoading && "animate-spin")} />
          </button>
        </div>
      </header>

      <AvisoPantallaAngosta />
      <TableroToolbar q={q} onQ={setQ} />

      {error && tablero && (
        <div role="status" className="flex items-center justify-between rounded-full bg-dsm-ambar-fondo px-4 py-2 text-[13px] text-dsm-ambar">
          {`No se pudo actualizar. Se muestran los datos de las ${horaCorta(tablero.generadoAt)}.`}
          <button type="button" onClick={refresh} className="underline underline-offset-2">
            Reintentar
          </button>
        </div>
      )}

      {error && !tablero ? (
        <ErrorConReintento titulo="No se pudo cargar el tablero" mensaje={error.message} onReintentar={refresh} />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full border-separate border-spacing-y-2">
            <thead>
              <tr>
                <Encabezado clave="zona" texto="Cobrador" orden={orden} onOrdenar={onOrdenar} />
                <Encabezado clave="ahora" texto="Ahora" orden={orden} onOrdenar={onOrdenar} />
                <th className="relative h-7 px-2 text-left font-normal">
                  <span className="font-mono text-[10.5px] uppercase tracking-[0.08em] text-muted-foreground">
                    {tablero?.periodo === "semana" ? "Jornada por día" : "Jornada 7–20 h"}
                  </span>
                  {tablero?.periodo !== "semana" && (
                    <span aria-hidden className="absolute inset-x-2 bottom-0 h-3">
                      {MARCAS_EJE.map((h) => (
                        <span
                          key={h}
                          className="absolute -translate-x-1/2 font-mono text-[9.5px] text-muted-foreground"
                          style={{ left: `${posicionMinutos(h * 60)}%` }}
                        >
                          {h}
                        </span>
                      ))}
                    </span>
                  )}
                </th>
                <Encabezado clave="horas" texto="Horas activas" orden={orden} onOrdenar={onOrdenar} derecha />
                <Encabezado clave="pausas" texto="Pausas" orden={orden} onOrdenar={onOrdenar} derecha />
                <Encabezado clave="pagos" texto="Pagos" orden={orden} onOrdenar={onOrdenar} derecha />
                <Encabezado clave="visitas" texto="Visitas" orden={orden} onOrdenar={onOrdenar} derecha />
                <Encabezado clave="faltan" texto="Cuentas semana" orden={orden} onOrdenar={onOrdenar} />
                <Encabezado clave="cobro" texto="% cobro" orden={orden} onOrdenar={onOrdenar} derecha />
              </tr>
            </thead>
            <tbody>
              {isLoading && !tablero && <FilasCargando filas={5} columnas={COLUMNAS} />}
              {tablero &&
                configurados.map((u) => (
                  <TableroFila
                    key={u.uid}
                    usuario={u}
                    periodo={tablero.periodo}
                    pausaMin={tablero.pausaMin}
                    horario={horario}
                    vista={vista}
                    generadoAt={tablero.generadoAt}
                    onAbrir={onAbrirUsuario}
                  />
                ))}
              {tablero &&
                sinConfigurar.map((u) => (
                  <FilaSinConfigurar key={u.uid} usuario={u} puedeConfigurar={puedeConfigurar} columnas={COLUMNAS} />
                ))}
              {tablero && tablero.usuarios.length === 0 && (
                <tr>
                  <td colSpan={COLUMNAS}>
                    <EstadoVacio titulo="No hay usuarios para mostrar" />
                  </td>
                </tr>
              )}
              {tablero && tablero.usuarios.length > 0 && configurados.length + sinConfigurar.length === 0 && (
                <tr>
                  <td colSpan={COLUMNAS}>
                    <EstadoVacio titulo={`Nadie coincide con “${q}”`} detalle="Busca por nombre, zona o correo." />
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
```

- [ ] **Step 8: Correr las pruebas de la tarea**

Run: `npx vitest run src/modules/desempeno/components/tablero src/modules/desempeno/presentation/hooks/__tests__/useTablero.test.tsx`
Expected: PASS (4 + 14 + 3 = 21 pruebas).

- [ ] **Step 9: Tipos y lint**

Run: `npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: sin salida.

- [ ] **Step 10: Commit**

```bash
git add src/modules/desempeno/presentation/hooks/useTablero.ts src/modules/desempeno/presentation/hooks/__tests__/useTablero.test.tsx src/modules/desempeno/components/tablero
git commit -m "feat(desempeno): tablero por usuario sin totales del equipo, con popovers y actualización cada minuto"
```

---

## CARRIL 2 — ficha del usuario (rama `feat/desempeno-ficha`, worktree `../sistema-cobro-web-ficha`)

### Tarea 2.1 (Carril 2): pestaña Jornada (día con flechas, eje, lista con pausas intercaladas, mapa, Para revisar)

**Carril 2, rama `feat/desempeno-ficha`, worktree propio.** Depende de Tarea 0. La línea de actividad (carril 1) **no** se importa aquí: entra por el hueco `renderLinea` y `controlesLinea`, que conecta la Tarea de integración.

**Files:**
- Create: `src/modules/desempeno/presentation/hooks/useJornada.ts`
- Create: `src/modules/desempeno/components/jornada/senalMeta.ts`
- Create: `src/modules/desempeno/components/jornada/NavegadorDia.tsx`
- Create: `src/modules/desempeno/components/jornada/describirActividades.ts`
- Create: `src/modules/desempeno/components/jornada/EjeJornada.tsx`
- Create: `src/modules/desempeno/components/jornada/ListaJornada.tsx`
- Create: `src/modules/desempeno/components/jornada/MapaJornadaGoogle.tsx`
- Create: `src/modules/desempeno/components/jornada/MapaJornada.tsx`
- Create: `src/modules/desempeno/components/jornada/ParaRevisar.tsx`
- Create: `src/modules/desempeno/components/jornada/JornadaTab.tsx`
- Test: `src/modules/desempeno/presentation/hooks/__tests__/useJornada.test.tsx`
- Test: `src/modules/desempeno/components/jornada/__tests__/JornadaTab.test.tsx`

**Interfaces:**
- Consumes: Tarea 0 (`Jornada`, `Actividad`, `Senal`, `TipoSenal`, `UsuarioTablero`, builders, `useCargaRemota`, `useCadaMinuto`, `usePreferencias`, `useDesempenoPort`, `renderConDesempeno`, formato, escala, `InfoRapida`, `TarjetaActividad`, `IconoTipoActividad`, `MontoActividad`, `Estados`).
- Produces:
  - `useJornada(uid: string, fecha: string, esHoy: boolean): { jornada: Jornada | null; isLoading; error; refresh }`.
  - `JornadaTab(props: { usuario: UsuarioTablero; dia: string; hoy: string; ahora: string | null; onCambiarDia: (dia: string) => void; onAbrirActividad: (a: Actividad) => void; renderFotos?: (a: Actividad) => ReactNode; renderLinea?: (jornada: Jornada, ahora: string | null) => ReactNode; controlesLinea?: ReactNode })`.
  - `describirActividades(actividades): (id) => string | null` (la integración lo pasa a `LineaActividad.describir`).
  - `SENAL_META: Record<TipoSenal, { titulo: string; icono: LucideIcon; clase: string }>`.

- [ ] **Step 1: Pruebas (fallan)**

`src/modules/desempeno/presentation/hooks/__tests__/useJornada.test.tsx`:

```tsx
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { useJornada } from "../useJornada";
import { DesempenoProvider } from "../../context/DesempenoProvider";
import { FakeDesempenoPort } from "../../../application/__tests__/fakeDesempenoPort";
import { UN_MINUTO_MS } from "../lib/useCadaMinuto";

function envolver(port: FakeDesempenoPort) {
  return ({ children }: { children: ReactNode }) => (
    <DesempenoProvider port={port} preferenciasIniciales={{ pausaMin: 30 }}>
      {children}
    </DesempenoProvider>
  );
}

describe("useJornada", () => {
  afterEach(() => vi.useRealTimers());

  it("pide la jornada del usuario y día con el umbral de la pantalla", async () => {
    const port = new FakeDesempenoPort();
    const { result } = renderHook(() => useJornada("uid-juan", "2026-10-13", false), {
      wrapper: envolver(port),
    });
    await waitFor(() => expect(result.current.jornada).not.toBeNull());
    expect(port.jornadaCalls[0]).toEqual({
      uid: "uid-juan",
      fecha: "2026-10-13",
      consulta: { pausaMin: 30, horario: null },
    });
  });

  it("solo hoy se actualiza cada minuto", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const port = new FakeDesempenoPort();
    const pasado = renderHook(() => useJornada("uid-juan", "2026-10-13", false), { wrapper: envolver(port) });
    await waitFor(() => expect(pasado.result.current.jornada).not.toBeNull());
    act(() => {
      vi.advanceTimersByTime(UN_MINUTO_MS);
    });
    expect(port.jornadaCalls).toHaveLength(1);
    pasado.unmount();

    const hoy = renderHook(() => useJornada("uid-juan", "2026-10-14", true), { wrapper: envolver(port) });
    await waitFor(() => expect(hoy.result.current.jornada).not.toBeNull());
    act(() => {
      vi.advanceTimersByTime(UN_MINUTO_MS);
    });
    await waitFor(() => expect(port.jornadaCalls).toHaveLength(3));
  });
});
```

`src/modules/desempeno/components/jornada/__tests__/JornadaTab.test.tsx`:

```tsx
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { JornadaTab } from "../JornadaTab";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import {
  FakeDesempenoPort,
  makeJornada,
  makeUsuarioTablero,
} from "../../../application/__tests__/fakeDesempenoPort";
import { DomainError } from "../../../domain/errors";

vi.mock("../MapaJornadaGoogle", () => ({
  MapaJornadaGoogle: ({ puntos }: { puntos: Array<{ n: number }> }) => (
    <div data-testid="mapa-google">{puntos.map((p) => p.n).join(",")}</div>
  ),
}));

function montar(
  opciones: { port?: FakeDesempenoPort; dia?: string } = {},
) {
  const onCambiarDia = vi.fn();
  const onAbrirActividad = vi.fn();
  const port = opciones.port ?? new FakeDesempenoPort();
  renderConDesempeno(
    <JornadaTab
      usuario={makeUsuarioTablero()}
      dia={opciones.dia ?? "2026-10-14"}
      hoy="2026-10-14"
      ahora="2026-10-14T22:40:00Z"
      onCambiarDia={onCambiarDia}
      onAbrirActividad={onAbrirActividad}
    />,
    { port },
  );
  return { port, onCambiarDia, onAbrirActividad };
}

describe("JornadaTab", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.unstubAllEnvs());

  it("lista en orden con la hora de la oficina y la pausa intercalada después de su actividad", async () => {
    montar();
    const lista = await screen.findByRole("list", { name: "Movimientos en orden" });
    const renglones = within(lista).getAllByRole("listitem");
    const textos = renglones.map((r) => r.textContent ?? "");
    const iPago0910 = textos.findIndex((t) => t.includes("09:10") && t.includes("PEDRO TEHUITZIL ZEPEDA"));
    expect(textos[0]).toContain("08:20");
    expect(textos[iPago0910 + 1]).toContain("Pausa · 1 h 12 min (09:10–10:22)");
    expect(textos[iPago0910 + 2]).toContain("ARACELI CORTÉS LUNA");
  });

  it("clic en un renglón abre la actividad", async () => {
    const user = userEvent.setup();
    const { onAbrirActividad } = montar();
    await user.click(await screen.findByText("MARÍA ELENA CUAUTLE TLAPA"));
    expect(onAbrirActividad).toHaveBeenCalledWith(
      expect.objectContaining({ id: "visita:412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e" }),
    );
  });

  it("marca la actividad sin GPS", async () => {
    montar();
    const renglon = (await screen.findByText("MARÍA ELENA CUAUTLE TLAPA")).closest("li")!;
    expect(within(renglon).getByTitle("Sin GPS")).toBeInTheDocument();
  });

  it("Para revisar: señales del API, regla en el popover y salto a la actividad", async () => {
    const user = userEvent.setup();
    montar();
    const senal = await screen.findByRole("button", { name: /Pausa larga/ });
    expect(within(senal).getByText("Pausa de 1 h 12 min")).toBeInTheDocument();

    await user.hover(senal);
    expect(await screen.findByText("hueco de 45 min o más entre dos movimientos")).toBeInTheDocument();

    await user.click(senal);
    const renglon = screen.getByText("PEDRO TEHUITZIL ZEPEDA").closest("li")!;
    expect(renglon).toHaveAttribute("data-resaltada", "true");
  });

  it("eje: un punto por actividad, con popover y clic", async () => {
    const user = userEvent.setup();
    const { onAbrirActividad } = montar();
    const puntos = await screen.findAllByTestId("eje-punto");
    expect(puntos).toHaveLength(5);
    await user.hover(puntos[0]);
    expect(await screen.findByText("Pago · $150")).toBeInTheDocument();
    await user.click(puntos[0]);
    expect(onAbrirActividad).toHaveBeenCalled();
  });

  it("mapa con llave: solo las actividades con GPS, numeradas como en la lista", async () => {
    vi.stubEnv("VITE_GOOGLE_MAPS_API_KEY", "llave-de-prueba");
    montar();
    expect(await screen.findByTestId("mapa-google")).toHaveTextContent("1,3,4,5");
    expect(screen.getByText("1 sin GPS")).toBeInTheDocument();
  });

  it("mapa sin llave: aviso, no se rompe", async () => {
    vi.stubEnv("VITE_GOOGLE_MAPS_API_KEY", "");
    montar();
    expect(await screen.findByText("Mapa no disponible")).toBeInTheDocument();
  });

  it("navegar días: anterior, chips de la semana y no se pasa de hoy", async () => {
    const user = userEvent.setup();
    const { onCambiarDia } = montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");
    expect(screen.getByRole("button", { name: "Día siguiente" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Día anterior" }));
    expect(onCambiarDia).toHaveBeenCalledWith("2026-10-13");
    // Semana del usuario: desde vie 9 (inicio_semana 13:46Z = 07:46 CDMX) hasta hoy.
    await user.click(screen.getByRole("button", { name: "vie 9" }));
    expect(onCambiarDia).toHaveBeenCalledWith("2026-10-09");
  });

  it("día sin actividad", async () => {
    const port = new FakeDesempenoPort();
    port.jornadaResponse = makeJornada({ actividades: [], senales: [], linea: { ...makeJornada().linea, puntos: [], tramos: [], pausas: [] } });
    montar({ port, dia: "2026-10-12" });
    expect(await screen.findByText("Sin actividad este día")).toBeInTheDocument();
    expect(screen.getByText("Nada que revisar este día")).toBeInTheDocument();
  });

  it("los huecos de la línea muestran lo que pase la integración", async () => {
    renderConDesempeno(
      <JornadaTab
        usuario={makeUsuarioTablero()}
        dia="2026-10-14"
        hoy="2026-10-14"
        ahora="2026-10-14T22:40:00Z"
        onCambiarDia={vi.fn()}
        onAbrirActividad={vi.fn()}
        renderLinea={(j, ahora) => <div data-testid="hueco-linea">{`${j.linea.puntos.length}|${ahora}`}</div>}
        controlesLinea={<span>controles de vista</span>}
      />,
    );
    expect(await screen.findByTestId("hueco-linea")).toHaveTextContent("5|2026-10-14T22:40:00Z");
    expect(screen.getByText("controles de vista")).toBeInTheDocument();
  });

  it("error con Reintentar", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    port.throwOnNext.obtenerJornada = new DomainError("desempeno_usuario_sin_zona", "el usuario no tiene zona");
    montar({ port });
    expect(await screen.findByText("el usuario no tiene zona")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reintentar" }));
    await waitFor(() => expect(port.jornadaCalls).toHaveLength(2));
  });
});
```

- [ ] **Step 2: Correrlas y verlas fallar**

Run: `npx vitest run src/modules/desempeno/presentation/hooks/__tests__/useJornada.test.tsx src/modules/desempeno/components/jornada`
Expected: FAIL con `Failed to resolve import "../useJornada"`.

- [ ] **Step 3: Implementar hook y metas**

`src/modules/desempeno/presentation/hooks/useJornada.ts`:

```ts
import { useCallback } from "react";
import type { Jornada } from "../../domain/entities";
import type { DomainError } from "../../domain/errors";
import { obtenerJornada } from "../../application/usecases/consultas";
import { useDesempenoPort } from "../context/useDesempenoPort";
import { usePreferencias } from "../context/usePreferencias";
import { useCargaRemota } from "./lib/useCargaRemota";
import { useCadaMinuto } from "./lib/useCadaMinuto";

export function useJornada(
  uid: string,
  fecha: string,
  esHoy: boolean,
): { jornada: Jornada | null; isLoading: boolean; error: DomainError | null; refresh: () => void } {
  const port = useDesempenoPort();
  const { consultaJornada } = usePreferencias();
  const cargar = useCallback(
    (signal: AbortSignal) => obtenerJornada(port, uid, fecha, consultaJornada, signal),
    [port, uid, fecha, consultaJornada],
  );
  const { datos, isLoading, error, refresh } = useCargaRemota(cargar);
  useCadaMinuto(refresh, esHoy);
  return { jornada: datos, isLoading, error, refresh };
}
```

`src/modules/desempeno/components/jornada/senalMeta.ts`:

```ts
import { CloudOff, Crosshair, MapPinOff, Pause, Sunrise, Sunset, type LucideIcon } from "lucide-react";
import type { TipoSenal } from "../../domain/entities";

export const SENAL_META: Record<TipoSenal, { titulo: string; icono: LucideIcon; clase: string }> = {
  pausa_larga: { titulo: "Pausa larga", icono: Pause, clase: "bg-dsm-ambar-fondo text-dsm-ambar" },
  inicio_tarde: { titulo: "Empezó tarde", icono: Sunrise, clase: "bg-dsm-ambar-fondo text-dsm-ambar" },
  fin_temprano: { titulo: "Terminó temprano", icono: Sunset, clase: "bg-dsm-ambar-fondo text-dsm-ambar" },
  mismo_punto: { titulo: "Mismo punto", icono: Crosshair, clase: "bg-dsm-rojo-fondo text-dsm-rojo" },
  captura_tardia: { titulo: "Llegó tarde al servidor", icono: CloudOff, clase: "bg-dsm-gris-fondo text-dsm-gris" },
  sin_gps: { titulo: "Sin GPS", icono: MapPinOff, clase: "bg-dsm-gris-fondo text-dsm-gris" },
};
```

- [ ] **Step 4: Implementar componentes**

`src/modules/desempeno/components/jornada/NavegadorDia.tsx`:

```tsx
import { ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { diasEntre, fechaCorta, fechaLarga, sumarDias } from "../lib/format";

// Flechas para cualquier día pasado (la jornada sirve para cualquier fecha,
// spec §1) y chips para los días de la semana en curso del usuario.
export function NavegadorDia({
  dia,
  hoy,
  inicioSemanaDia,
  onCambiar,
}: {
  dia: string;
  hoy: string;
  inicioSemanaDia: string | null;
  onCambiar: (dia: string) => void;
}) {
  const semana = inicioSemanaDia ? diasEntre(inicioSemanaDia, hoy) : [];
  return (
    <div className="flex flex-wrap items-center gap-3">
      <div className="flex items-center gap-1">
        <button
          type="button"
          aria-label="Día anterior"
          onClick={() => onCambiar(sumarDias(dia, -1))}
          className="flex h-8 w-8 items-center justify-center rounded-full ring-1 ring-border hover:bg-muted"
        >
          <ChevronLeft aria-hidden className="h-4 w-4" />
        </button>
        <div className="min-w-[180px] text-center font-serif text-lg first-letter:uppercase">
          {fechaLarga(dia)}
          {dia === hoy && <small className="ml-2 font-mono text-[10px] uppercase text-dsm-marca">hoy</small>}
        </div>
        <button
          type="button"
          aria-label="Día siguiente"
          disabled={dia >= hoy}
          onClick={() => onCambiar(sumarDias(dia, 1))}
          className="flex h-8 w-8 items-center justify-center rounded-full ring-1 ring-border hover:bg-muted disabled:opacity-40"
        >
          <ChevronRight aria-hidden className="h-4 w-4" />
        </button>
      </div>
      <div className="flex gap-1">
        {semana.map((d) => {
          const etiqueta = fechaCorta(d).split(" ").slice(0, 2).join(" ");
          return (
            <button
              key={d}
              type="button"
              aria-label={etiqueta}
              aria-pressed={d === dia}
              onClick={() => onCambiar(d)}
              className={cn(
                "h-7 rounded-full px-2.5 font-mono text-[11px]",
                d === dia ? "bg-foreground text-background" : "bg-dsm-tarjeta ring-1 ring-border hover:bg-muted",
              )}
            >
              {etiqueta}
            </button>
          );
        })}
      </div>
    </div>
  );
}
```

`src/modules/desempeno/components/jornada/EjeJornada.tsx`:

```tsx
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import type { Actividad, Jornada, TipoActividad } from "../../domain/entities";
import { HORA_FIN_EJE, HORA_INICIO_EJE, posicion, posicionMinutos } from "../lib/escala";
import { horaCorta } from "../lib/format";
import { TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";
import { InfoRapida } from "../comunes/InfoRapida";
import { TarjetaActividad } from "../comunes/TarjetaActividad";

const CARRILES: ReadonlyArray<readonly [TipoActividad, string]> = [
  ["pago", "Pagos"],
  ["visita", "Visitas"],
  ["condonacion", "Condonaciones"],
];
const HORAS = Array.from({ length: HORA_FIN_EJE - HORA_INICIO_EJE + 1 }, (_, i) => HORA_INICIO_EJE + i);

// EjeJornada: carriles de pagos, visitas y condonaciones sobre el eje 7–20 h.
// `linea` es el hueco donde la integración pone la LineaActividad (carril 1).
export function EjeJornada({
  jornada,
  linea,
  onAbrirActividad,
}: {
  jornada: Jornada;
  linea?: ReactNode;
  onAbrirActividad: (a: Actividad) => void;
}) {
  return (
    <div className="rounded-[14px] bg-dsm-tarjeta px-6 pb-3 pt-5 shadow-sm ring-1 ring-border">
      <div className="grid grid-cols-[136px_1fr] items-center gap-y-2">
        {linea && (
          <>
            <span className="text-xs text-muted-foreground">Línea del día</span>
            {linea}
          </>
        )}
        {CARRILES.map(([tipo, etiqueta]) => {
          const delTipo = jornada.actividades.filter((a) => a.tipo === tipo);
          return (
            <div key={tipo} className="contents">
              <span className="flex items-center gap-2 text-xs text-muted-foreground">
                <i className={cn("h-2 w-2 rounded-full", TIPO_ACTIVIDAD_META[tipo].clasePunto)} />
                {etiqueta} <b className="font-medium text-foreground">{delTipo.length}</b>
              </span>
              <div className="relative h-10 border-b border-border">
                {jornada.linea.pausas.map((p) => (
                  <span
                    key={`${p.desde}-${p.hasta}`}
                    aria-hidden
                    className="dsm-pausa absolute inset-y-0"
                    style={{ left: `${posicion(p.desde)}%`, width: `${Math.max(0, posicion(p.hasta) - posicion(p.desde))}%` }}
                  />
                ))}
                {delTipo.map((a) => (
                  <InfoRapida key={a.id} contenido={<TarjetaActividad actividad={a} />}>
                    <button
                      type="button"
                      data-testid="eje-punto"
                      aria-label={`${horaCorta(a.hora)} · ${a.clienteNombre}`}
                      onClick={() => onAbrirActividad(a)}
                      className={cn(
                        "absolute top-1/2 -ml-1.5 -mt-1.5 h-3 w-3 rounded-full ring-2 ring-[hsl(var(--dsm-tarjeta))]",
                        TIPO_ACTIVIDAD_META[a.tipo].clasePunto,
                      )}
                      style={{ left: `${posicion(a.hora)}%` }}
                    />
                  </InfoRapida>
                ))}
              </div>
            </div>
          );
        })}
        <span />
        <div className="relative h-4">
          {HORAS.map((h) => (
            <span
              key={h}
              className="absolute -translate-x-1/2 font-mono text-[10px] text-muted-foreground"
              style={{ left: `${posicionMinutos(h * 60)}%` }}
            >
              {h}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}
```

`src/modules/desempeno/components/jornada/describirActividades.ts`:

```ts
import type { Actividad } from "../../domain/entities";
import { horaCorta } from "../lib/format";
import { TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";

// Nombra una actividad por su id para el "Antes/Después" del popover de pausa:
// en la jornada sí se conoce el cliente (en el tablero solo hora y tipo).
export function describirActividades(actividades: readonly Actividad[]) {
  return (id: string): string | null => {
    const a = actividades.find((x) => x.id === id);
    return a
      ? `${horaCorta(a.hora)} · ${TIPO_ACTIVIDAD_META[a.tipo].etiqueta.toLowerCase()} · ${a.clienteNombre}`
      : null;
  };
}
```

`src/modules/desempeno/components/jornada/ListaJornada.tsx`:

```tsx
import { useEffect, useRef, type MutableRefObject, type ReactNode } from "react";
import { Camera, MapPinOff, Pause } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Actividad, Pausa } from "../../domain/entities";
import { duracion, horaCorta } from "../lib/format";
import { InfoRapida } from "../comunes/InfoRapida";
import { IconoTipoActividad } from "../comunes/IconoTipoActividad";
import { MontoActividad } from "../comunes/MontoActividad";
import { TarjetaActividad } from "../comunes/TarjetaActividad";

function RenglonPausa({ pausa }: { pausa: Pausa }) {
  return (
    <li className="flex items-center gap-2 bg-dsm-ambar-fondo px-[18px] py-2 text-[12.5px] text-dsm-ambar">
      <Pause aria-hidden className="h-3.5 w-3.5" />
      {`Pausa · ${duracion(pausa.minutos)} (${horaCorta(pausa.desde)}–${pausa.enCurso ? "ahora" : horaCorta(pausa.hasta)})`}
      {pausa.enCurso && " · sigue sin actividad"}
    </li>
  );
}

function FotosPorDefecto({ actividad }: { actividad: Actividad }) {
  if (actividad.numFotos === 0) return null;
  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground">
      <Camera aria-hidden className="h-3.5 w-3.5" />
      {actividad.numFotos}
    </span>
  );
}

// Lista cronológica con las pausas intercaladas: cada pausa va después de la
// actividad `anterior_id` que la abrió (contrato §1); una pausa sin anterior
// (p. ej. antes del primer movimiento) va arriba.
export function ListaJornada({
  actividades,
  pausas,
  resaltada,
  onAbrir,
  renderFotos,
}: {
  actividades: readonly Actividad[];
  pausas: readonly Pausa[];
  resaltada: string | null;
  onAbrir: (a: Actividad) => void;
  renderFotos?: (a: Actividad) => ReactNode;
}) {
  const refResaltada = useRef<HTMLLIElement | null>(null);
  useEffect(() => {
    refResaltada.current?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [resaltada]);

  return (
    <ol aria-label="Movimientos en orden" className="divide-y divide-border">
      {pausas.filter((p) => p.anteriorId === null).map((p) => (
        <RenglonPausa key={`${p.desde}-inicio`} pausa={p} />
      ))}
      {actividades.map((a, i) => (
        <FragmentoActividad
          key={a.id}
          actividad={a}
          numero={i + 1}
          pausasDespues={pausas.filter((p) => p.anteriorId === a.id)}
          resaltada={resaltada === a.id}
          refResaltada={resaltada === a.id ? refResaltada : undefined}
          onAbrir={onAbrir}
          fotos={renderFotos ? renderFotos(a) : <FotosPorDefecto actividad={a} />}
        />
      ))}
    </ol>
  );
}

function FragmentoActividad({
  actividad: a,
  numero,
  pausasDespues,
  resaltada,
  refResaltada,
  onAbrir,
  fotos,
}: {
  actividad: Actividad;
  numero: number;
  pausasDespues: readonly Pausa[];
  resaltada: boolean;
  refResaltada?: MutableRefObject<HTMLLIElement | null>;
  onAbrir: (a: Actividad) => void;
  fotos: ReactNode;
}) {
  return (
    <>
      <li
        ref={refResaltada}
        data-resaltada={resaltada ? "true" : undefined}
        onClick={() => onAbrir(a)}
        className={cn(
          "grid cursor-pointer grid-cols-[22px_44px_28px_1fr_auto_auto] items-center gap-3 px-[18px] py-[11px] hover:bg-muted/50",
          resaltada && "bg-dsm-marca-fondo",
        )}
      >
        <span className="font-mono text-[11px] text-muted-foreground">{numero}</span>
        <time className="font-mono text-[12px]">{horaCorta(a.hora)}</time>
        <InfoRapida contenido={<TarjetaActividad actividad={a} />}>
          <span>
            <IconoTipoActividad tipo={a.tipo} />
          </span>
        </InfoRapida>
        <div className="min-w-0">
          <div className="truncate text-[14px]">{a.clienteNombre}</div>
          <div className="truncate font-mono text-[11px] text-muted-foreground">
            {a.folio ?? "Sin venta"}
            {a.registradoPor.origen === "oficina" && " · registró oficina"}
          </div>
        </div>
        <div className="text-right">
          <MontoActividad actividad={a} />
        </div>
        <div className="flex items-center gap-2 text-[12px]" onClick={(e) => e.stopPropagation()}>
          {fotos}
          {!a.conGps && (
            <span title="Sin GPS" className="text-dsm-ambar">
              <MapPinOff aria-hidden className="h-3.5 w-3.5" />
            </span>
          )}
        </div>
      </li>
      {pausasDespues.map((p) => (
        <RenglonPausa key={`${p.desde}-${p.hasta}`} pausa={p} />
      ))}
    </>
  );
}
```

`src/modules/desempeno/components/jornada/MapaJornadaGoogle.tsx`:

```tsx
import { GoogleMap, MarkerF, PolylineF, useLoadScript } from "@react-google-maps/api";

export type PuntoMapa = { id: string; n: number; lat: number; lng: number };

const CONTENEDOR = { width: "100%", height: "300px" };

// Cargado en diferido desde MapaJornada solo si hay llave de Maps (mismo
// patrón que clientes/components/ficha/FichaMapEmbed.tsx).
export function MapaJornadaGoogle({
  puntos,
  onSeleccionar,
}: {
  puntos: readonly PuntoMapa[];
  onSeleccionar: (id: string) => void;
}) {
  const { isLoaded, loadError } = useLoadScript({
    googleMapsApiKey: import.meta.env.VITE_GOOGLE_MAPS_API_KEY as string,
    preventGoogleFontsLoading: true,
  });
  if (loadError) return <p className="p-6 text-center font-mono text-[11px] text-muted-foreground">Mapa no disponible</p>;
  if (!isLoaded) return <p className="p-6 text-center font-mono text-[11px] text-muted-foreground">Cargando mapa…</p>;
  return (
    <GoogleMap
      mapContainerStyle={CONTENEDOR}
      onLoad={(mapa) => {
        const limites = new google.maps.LatLngBounds();
        puntos.forEach((p) => limites.extend({ lat: p.lat, lng: p.lng }));
        if (puntos.length > 0) mapa.fitBounds(limites, 32);
      }}
      options={{ disableDefaultUI: true, zoomControl: true, clickableIcons: false }}
    >
      <PolylineF
        path={puntos.map((p) => ({ lat: p.lat, lng: p.lng }))}
        options={{ strokeOpacity: 0.35, strokeWeight: 1.5 }}
      />
      {puntos.map((p) => (
        <MarkerF
          key={p.id}
          position={{ lat: p.lat, lng: p.lng }}
          label={{ text: String(p.n), fontSize: "10px", color: "#ffffff" }}
          onClick={() => onSeleccionar(p.id)}
        />
      ))}
    </GoogleMap>
  );
}
```

`src/modules/desempeno/components/jornada/MapaJornada.tsx`:

```tsx
import { lazy, Suspense } from "react";
import type { Actividad } from "../../domain/entities";
import type { PuntoMapa } from "./MapaJornadaGoogle";

const MapaJornadaGoogle = lazy(() =>
  import("./MapaJornadaGoogle").then((m) => ({ default: m.MapaJornadaGoogle })),
);

// Mapa con puntos numerados en orden de captura. Las actividades sin GPS van
// en la lista pero no en el mapa (spec §6). Los números son la posición en la
// lista, no un dato del negocio.
export function MapaJornada({
  actividades,
  etiquetaDia,
  onSeleccionar,
}: {
  actividades: readonly Actividad[];
  etiquetaDia: string;
  onSeleccionar: (id: string) => void;
}) {
  const puntos: PuntoMapa[] = [];
  actividades.forEach((a, i) => {
    if (a.conGps && a.lat !== null && a.lng !== null) puntos.push({ id: a.id, n: i + 1, lat: a.lat, lng: a.lng });
  });
  const sinGps = actividades.length - puntos.length;
  const hayLlave = Boolean(import.meta.env.VITE_GOOGLE_MAPS_API_KEY);
  return (
    <div className="overflow-hidden rounded-[14px] bg-dsm-tarjeta shadow-sm ring-1 ring-border">
      <div className="flex items-center justify-between px-[18px] py-3">
        <h3 className="text-[13px] font-medium">Recorrido</h3>
        <span className="font-mono text-[11px] text-muted-foreground">{etiquetaDia}</span>
      </div>
      {hayLlave ? (
        <Suspense fallback={<p className="p-6 text-center font-mono text-[11px] text-muted-foreground">Cargando mapa…</p>}>
          <MapaJornadaGoogle puntos={puntos} onSeleccionar={onSeleccionar} />
        </Suspense>
      ) : (
        <p className="p-6 text-center font-mono text-[11px] text-muted-foreground">Mapa no disponible</p>
      )}
      <div className="flex justify-between px-[18px] py-2 text-[11px] text-muted-foreground">
        <span>Números en orden de captura · ubicación donde se registró</span>
        <span>{sinGps > 0 ? `${sinGps} sin GPS` : "todos con GPS"}</span>
      </div>
    </div>
  );
}
```

`src/modules/desempeno/components/jornada/ParaRevisar.tsx`:

```tsx
import { Flag } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Senal } from "../../domain/entities";
import { InfoRapida, TarjetaNota, TarjetaTitulo } from "../comunes/InfoRapida";
import { SENAL_META } from "./senalMeta";

// "Para revisar" (spec §4.2): señales calculadas por el servidor, con su regla.
export function ParaRevisar({
  senales,
  onSeleccionar,
}: {
  senales: readonly Senal[];
  onSeleccionar: (actividadId: string) => void;
}) {
  return (
    <div className="rounded-[14px] bg-dsm-tarjeta shadow-sm ring-1 ring-border">
      <div className="flex items-center justify-between px-[18px] py-3">
        <h3 className="flex items-center gap-2 text-[13px] font-medium">
          <Flag aria-hidden className="h-3.5 w-3.5" />
          Para revisar
        </h3>
        <span className="font-mono text-[11px] text-muted-foreground">{senales.length} este día</span>
      </div>
      {senales.length === 0 ? (
        <p className="px-[18px] pb-5 text-center font-serif text-[15px]">Nada que revisar este día</p>
      ) : (
        senales.map((s, i) => {
          const meta = SENAL_META[s.tipo];
          const Icono = meta.icono;
          return (
            <InfoRapida
              key={`${s.tipo}-${i}`}
              contenido={
                <>
                  <TarjetaTitulo>Por qué aparece</TarjetaTitulo>
                  <TarjetaNota>{s.regla}</TarjetaNota>
                </>
              }
            >
              <button
                type="button"
                onClick={() => s.actividadIds[0] && onSeleccionar(s.actividadIds[0])}
                className="flex w-full items-start gap-3 border-t border-border px-[18px] py-2.5 text-left hover:bg-muted/50"
              >
                <span className={cn("mt-0.5 flex h-6 w-6 flex-none items-center justify-center rounded-md", meta.clase)}>
                  <Icono aria-hidden className="h-3.5 w-3.5" />
                </span>
                <span>
                  <span className="block text-[13px]">{meta.titulo}</span>
                  <span className="block text-[12px] text-muted-foreground">{s.detalle}</span>
                </span>
              </button>
            </InfoRapida>
          );
        })
      )}
    </div>
  );
}
```

`src/modules/desempeno/components/jornada/JornadaTab.tsx`:

```tsx
import { useEffect, useState, type ReactNode } from "react";
import { List } from "lucide-react";
import type { Actividad, Jornada, UsuarioTablero } from "../../domain/entities";
import { useJornada } from "../../presentation/hooks/useJornada";
import { diaDeNegocioDe, duracionCorta, fechaCorta, fechaLarga, horaCorta, sumarDias } from "../lib/format";
import { ErrorConReintento, EstadoVacio } from "../comunes/Estados";
import { NavegadorDia } from "./NavegadorDia";
import { EjeJornada } from "./EjeJornada";
import { ListaJornada } from "./ListaJornada";
import { MapaJornada } from "./MapaJornada";
import { ParaRevisar } from "./ParaRevisar";

export function JornadaTab({
  usuario,
  dia,
  hoy,
  ahora,
  onCambiarDia,
  onAbrirActividad,
  renderFotos,
  renderLinea,
  controlesLinea,
}: {
  usuario: UsuarioTablero;
  dia: string;
  hoy: string;
  ahora: string | null;
  onCambiarDia: (dia: string) => void;
  onAbrirActividad: (a: Actividad) => void;
  renderFotos?: (a: Actividad) => ReactNode;
  /** Hueco para la LineaActividad (carril 1); lo llena la integración. */
  renderLinea?: (jornada: Jornada, ahora: string | null) => ReactNode;
  /** Hueco para el selector "Vista" y la leyenda (carril 1). */
  controlesLinea?: ReactNode;
}) {
  const esHoy = dia === hoy;
  const { jornada, isLoading, error, refresh } = useJornada(usuario.uid, dia, esHoy);
  const [resaltada, setResaltada] = useState<string | null>(null);

  // ← → cambian de día (mock), salvo dentro de un campo o con un modal abierto.
  useEffect(() => {
    const alTeclado = (e: KeyboardEvent) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      const destino = e.target as HTMLElement | null;
      if (destino && /INPUT|SELECT|TEXTAREA/.test(destino.tagName)) return;
      if (document.querySelector("[role='dialog']")) return;
      if (e.key === "ArrowLeft") onCambiarDia(sumarDias(dia, -1));
      else if (dia < hoy) onCambiarDia(sumarDias(dia, 1));
    };
    window.addEventListener("keydown", alTeclado);
    return () => window.removeEventListener("keydown", alTeclado);
  }, [dia, hoy, onCambiarDia]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-4">
        <NavegadorDia
          dia={dia}
          hoy={hoy}
          inicioSemanaDia={usuario.inicioSemana ? diaDeNegocioDe(usuario.inicioSemana) : null}
          onCambiar={onCambiarDia}
        />
        {controlesLinea}
        {jornada && jornada.resumen.primeraActividad && (
          <span className="text-xs text-muted-foreground">
            {`${horaCorta(jornada.resumen.primeraActividad)} – ${esHoy || !jornada.resumen.ultimaActividad ? "ahora" : horaCorta(jornada.resumen.ultimaActividad)} · ${duracionCorta(jornada.resumen.minutosActivos)} activas · pausas de ${jornada.pausaMin} min o más`}
          </span>
        )}
      </div>

      {error && !jornada && (
        <ErrorConReintento titulo="No se pudo cargar la jornada" mensaje={error.message} onReintentar={refresh} />
      )}
      {isLoading && !jornada && <div className="h-48 animate-pulse rounded-[14px] bg-dsm-tarjeta" />}

      {jornada && (
        <>
          {jornada.actividades.length === 0 ? (
            <div className="rounded-[14px] bg-dsm-tarjeta ring-1 ring-border">
              <EstadoVacio
                titulo={esHoy ? "Sin actividad hoy" : "Sin actividad este día"}
                detalle={
                  usuario.ultimaActividad
                    ? `Su último movimiento fue el ${fechaCorta(diaDeNegocioDe(usuario.ultimaActividad))} a las ${horaCorta(usuario.ultimaActividad)}.`
                    : undefined
                }
              />
            </div>
          ) : (
            <EjeJornada
              jornada={jornada}
              linea={renderLinea?.(jornada, esHoy ? ahora : null)}
              onAbrirActividad={onAbrirActividad}
            />
          )}
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-[1fr_380px]">
            {jornada.actividades.length > 0 && (
              <div className="rounded-[14px] bg-dsm-tarjeta shadow-sm ring-1 ring-border">
                <div className="flex items-center justify-between px-[18px] py-3">
                  <h3 className="flex items-center gap-2 text-[13px] font-medium">
                    <List aria-hidden className="h-3.5 w-3.5" />
                    Movimientos en orden
                  </h3>
                  <span className="font-mono text-[11px] text-muted-foreground">
                    {`${jornada.resumen.pagos.n} pagos · ${jornada.resumen.visitas} visitas · ${jornada.resumen.condonaciones} condonaciones`}
                  </span>
                </div>
                <ListaJornada
                  actividades={jornada.actividades}
                  pausas={jornada.linea.pausas}
                  resaltada={resaltada}
                  onAbrir={onAbrirActividad}
                  renderFotos={renderFotos}
                />
              </div>
            )}
            <div className="flex flex-col gap-4">
              {jornada.actividades.length > 0 && (
                <MapaJornada actividades={jornada.actividades} etiquetaDia={fechaLarga(dia)} onSeleccionar={setResaltada} />
              )}
              <ParaRevisar senales={jornada.senales} onSeleccionar={setResaltada} />
            </div>
          </div>
        </>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Correr las pruebas de la tarea**

Run: `npx vitest run src/modules/desempeno/presentation/hooks/__tests__/useJornada.test.tsx src/modules/desempeno/components/jornada`
Expected: PASS (2 + 11 = 13 pruebas).

- [ ] **Step 6: Tipos y lint**

Run: `npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: sin salida.

- [ ] **Step 7: Commit**

```bash
git add src/modules/desempeno/presentation/hooks/useJornada.ts src/modules/desempeno/presentation/hooks/__tests__/useJornada.test.tsx src/modules/desempeno/components/jornada
git commit -m "feat(desempeno): pestaña Jornada con eje, pausas intercaladas, recorrido y Para revisar"
```

---

### Tarea 2.2 (Carril 2): pestaña Actividad de la zona (filtros, orden, agrupar, columnas, vistas guardadas, paginación)

**Carril 2, rama `feat/desempeno-ficha`, después de la Tarea 2.1.** La exportación a Excel queda fuera (Apéndice "Posterior").

**Files:**
- Create: `src/modules/desempeno/presentation/actividadPreferencias.ts`
- Create: `src/modules/desempeno/presentation/hooks/useActividad.ts`
- Create: `src/modules/desempeno/presentation/hooks/useVistasGuardadas.ts`
- Create: `src/modules/desempeno/presentation/hooks/useColumnasActividad.ts`
- Create: `src/modules/desempeno/components/actividad/columnas.ts`
- Create: `src/modules/desempeno/components/actividad/vistasPredefinidas.ts`
- Create: `src/modules/desempeno/components/actividad/agrupar.ts`
- Create: `src/modules/desempeno/components/actividad/FiltrosActividadBar.tsx`
- Create: `src/modules/desempeno/components/actividad/VistasActividad.tsx`
- Create: `src/modules/desempeno/components/actividad/MenuColumnas.tsx`
- Create: `src/modules/desempeno/components/actividad/TablaActividad.tsx`
- Create: `src/modules/desempeno/components/actividad/ActividadTab.tsx`
- Test: `src/modules/desempeno/presentation/hooks/__tests__/useActividad.test.tsx`
- Test: `src/modules/desempeno/presentation/hooks/__tests__/preferenciasActividad.test.tsx`
- Test: `src/modules/desempeno/components/actividad/__tests__/agrupar.test.ts`
- Test: `src/modules/desempeno/components/actividad/__tests__/ActividadTab.test.tsx`

**Interfaces:**
- Consumes: Tarea 0 (`FiltrosActividad`, `FILTROS_ACTIVIDAD_INICIALES`, `PaginaActividad`, `TotalesActividad`, `LIMITE_PAGINA_ACTIVIDAD`, `REGISTRADO_POR_OFICINA`, `TIPOS_VISITA`, `UsuarioTablero`, `Actividad`, builders, `useDesempenoPort`, `leerLocal`/`escribirLocal`, `toDomainError`, `renderConDesempeno`, formato, `InfoRapida`, `TarjetaActividad`, `IconoTipoActividad`, `MontoActividad`, `Estados`, `FORMA_COBRO_ETIQUETA`), `@/utils/tiempoDeNegocio` (`rangoDeDiasDeNegocio`), `@/components/ui/popover`.
- Produces:
  - `ActividadTab(props: { usuario: UsuarioTablero; opcionesRegistro: ReadonlyArray<{ valor: string; etiqueta: string }>; onAbrirActividad: (a: Actividad) => void; renderFotos?: (a: Actividad) => ReactNode })`.
  - `useActividad(uid, filtros): { items; totales; hayMas; isLoading; isLoadingMas; error; cargarMas; refresh }`.
  - Claves de `localStorage`: `desempeno.actividad.columnas.v1`, `desempeno.actividad.vistas.v1`.

- [ ] **Step 1: Pruebas de hooks y utilidades (fallan)**

`src/modules/desempeno/presentation/hooks/__tests__/useActividad.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { useActividad } from "../useActividad";
import { DesempenoProvider } from "../../context/DesempenoProvider";
import {
  FakeDesempenoPort,
  makeActividadPago,
  makePaginaActividad,
} from "../../../application/__tests__/fakeDesempenoPort";
import { FILTROS_ACTIVIDAD_INICIALES, type FiltrosActividad } from "../../../domain/entities";
import { DomainError } from "../../../domain/errors";

function envolver(port: FakeDesempenoPort) {
  return ({ children }: { children: ReactNode }) => (
    <DesempenoProvider port={port}>{children}</DesempenoProvider>
  );
}

describe("useActividad", () => {
  it("pide la primera página (cursor nulo, 100 por página) y guarda los totales del filtro", async () => {
    const port = new FakeDesempenoPort();
    const { result } = renderHook(() => useActividad("uid-juan", FILTROS_ACTIVIDAD_INICIALES), {
      wrapper: envolver(port),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(port.actividadCalls[0].pagina).toEqual({ cursor: null, limit: 100 });
    expect(result.current.items).toHaveLength(5);
    expect(result.current.totales?.n).toBe(5);
    expect(result.current.hayMas).toBe(false);
  });

  it("cargarMas sigue el cursor del servidor y agrega al final sin tocar los totales", async () => {
    const port = new FakeDesempenoPort();
    const extra = makeActividadPago({ id: "pago:15080001", hora: "2026-10-13T20:00:00Z" });
    port.actividadResponse = (pagina) =>
      pagina.cursor === null
        ? makePaginaActividad({ siguienteCursor: "c2" })
        : makePaginaActividad({ items: [extra], siguienteCursor: null, totales: { n: 999, importe: "0.00", porTipo: { pago: 0, visita: 0, condonacion: 0 } } });
    const { result } = renderHook(() => useActividad("uid-juan", FILTROS_ACTIVIDAD_INICIALES), {
      wrapper: envolver(port),
    });
    await waitFor(() => expect(result.current.hayMas).toBe(true));

    act(() => result.current.cargarMas());

    await waitFor(() => expect(result.current.items).toHaveLength(6));
    expect(port.actividadCalls[1].pagina).toEqual({ cursor: "c2", limit: 100 });
    expect(result.current.items[5].id).toBe("pago:15080001");
    expect(result.current.totales?.n).toBe(5);
    expect(result.current.hayMas).toBe(false);
  });

  it("filtros nuevos reinician la lista y una página vieja no se mezcla", async () => {
    const port = new FakeDesempenoPort();
    let liberarVieja: () => void = () => {};
    port.actividadResponse = () =>
      new Promise((res) => {
        liberarVieja = () => res(makePaginaActividad());
      });
    const { result, rerender } = renderHook(
      ({ f }: { f: FiltrosActividad }) => useActividad("uid-juan", f),
      { wrapper: envolver(port), initialProps: { f: FILTROS_ACTIVIDAD_INICIALES } },
    );
    port.actividadResponse = makePaginaActividad({ items: [makeActividadPago()] });
    rerender({ f: { ...FILTROS_ACTIVIDAD_INICIALES, tipos: ["pago"] } });
    await waitFor(() => expect(result.current.items).toHaveLength(1));

    await act(async () => liberarVieja());

    expect(result.current.items).toHaveLength(1);
  });

  it("expone el error del servidor", async () => {
    const port = new FakeDesempenoPort();
    port.throwOnNext.listarActividad = new DomainError("desempeno_parametro_invalido", "el rango máximo es de 92 días");
    const { result } = renderHook(() => useActividad("uid-juan", FILTROS_ACTIVIDAD_INICIALES), {
      wrapper: envolver(port),
    });
    await waitFor(() => expect(result.current.error?.message).toBe("el rango máximo es de 92 días"));
  });
});
```

`src/modules/desempeno/presentation/hooks/__tests__/preferenciasActividad.test.tsx`:

```tsx
import { beforeEach, describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useVistasGuardadas } from "../useVistasGuardadas";
import { useColumnasActividad } from "../useColumnasActividad";
import { CLAVE_COLUMNAS, CLAVE_VISTAS, COLUMNAS_POR_DEFECTO } from "../../actividadPreferencias";
import { FILTROS_ACTIVIDAD_INICIALES } from "../../../domain/entities";

describe("vistas guardadas y columnas (por computadora)", () => {
  beforeEach(() => localStorage.clear());

  it("guarda una vista y la recupera en otra sesión", () => {
    const filtros = { ...FILTROS_ACTIVIDAD_INICIALES, conGps: false };
    const primera = renderHook(() => useVistasGuardadas());
    act(() => {
      primera.result.current.guardar("Sin GPS de la tarde", filtros, ["tipo", "hora"]);
    });
    expect(JSON.parse(localStorage.getItem(CLAVE_VISTAS)!)).toHaveLength(1);

    const segunda = renderHook(() => useVistasGuardadas());
    expect(segunda.result.current.vistas[0].nombre).toBe("Sin GPS de la tarde");
    expect(segunda.result.current.vistas[0].filtros.conGps).toBe(false);

    act(() => segunda.result.current.eliminar(segunda.result.current.vistas[0].id));
    expect(segunda.result.current.vistas).toHaveLength(0);
  });

  it("basura en localStorage no tumba la pestaña", () => {
    localStorage.setItem(CLAVE_VISTAS, JSON.stringify([{ id: 3 }]));
    localStorage.setItem(CLAVE_COLUMNAS, JSON.stringify(["tipo", "inventada"]));
    expect(renderHook(() => useVistasGuardadas()).result.current.vistas).toEqual([]);
    expect(renderHook(() => useColumnasActividad()).result.current.columnas).toEqual(COLUMNAS_POR_DEFECTO);
  });

  it("alterna columnas, las recuerda y nunca deja la tabla sin columnas", () => {
    const { result } = renderHook(() => useColumnasActividad());
    act(() => result.current.alternar("forma"));
    expect(result.current.columnas).toContain("forma");
    expect(JSON.parse(localStorage.getItem(CLAVE_COLUMNAS)!)).toContain("forma");

    act(() => result.current.fijar(["tipo"]));
    act(() => result.current.alternar("tipo"));
    expect(result.current.columnas).toEqual(["tipo"]);
  });
});
```

`src/modules/desempeno/components/actividad/__tests__/agrupar.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { agruparConsecutivos } from "../agrupar";
import {
  makeActividadPago,
  makeActividadVisita,
} from "../../../application/__tests__/fakeDesempenoPort";

describe("agruparConsecutivos", () => {
  it("agrupa renglones contiguos por la clave elegida, sin sumar nada", () => {
    const items = [
      makeActividadPago({ id: "a", hora: "2026-10-14T16:00:00Z" }),
      makeActividadVisita({ id: "b", hora: "2026-10-14T15:00:00Z" }),
      makeActividadPago({ id: "c", hora: "2026-10-13T16:00:00Z" }),
    ];
    const grupos = agruparConsecutivos(items, "dia");
    expect(grupos.map((g) => g.clave)).toEqual(["miércoles 14 de octubre", "martes 13 de octubre"]);
    expect(grupos[0].items.map((a) => a.id)).toEqual(["a", "b"]);
    expect(Object.keys(grupos[0])).toEqual(["clave", "items"]);
  });

  it("por tipo usa la etiqueta en español", () => {
    const grupos = agruparConsecutivos([makeActividadVisita()], "tipo");
    expect(grupos[0].clave).toBe("Visita");
  });
});
```

- [ ] **Step 2: Correrlas y verlas fallar**

Run: `npx vitest run src/modules/desempeno/presentation/hooks src/modules/desempeno/components/actividad`
Expected: FAIL con `Failed to resolve import "../useActividad"` (y equivalentes).

- [ ] **Step 3: Implementar preferencias de actividad y hooks**

`src/modules/desempeno/presentation/actividadPreferencias.ts`:

```ts
import type { FiltrosActividad } from "../domain/entities";

export const CLAVES_COLUMNA = [
  "tipo",
  "hora",
  "cliente",
  "venta",
  "monto",
  "forma",
  "registro",
  "llegada",
  "fotos",
  "gps",
] as const;
export type ClaveColumna = (typeof CLAVES_COLUMNA)[number];

// Las del mock: forma de cobro y llegada al servidor arrancan ocultas.
export const COLUMNAS_POR_DEFECTO: ClaveColumna[] = [
  "tipo",
  "hora",
  "cliente",
  "venta",
  "monto",
  "registro",
  "fotos",
  "gps",
];
export const CLAVE_COLUMNAS = "desempeno.actividad.columnas.v1";
export const CLAVE_VISTAS = "desempeno.actividad.vistas.v1";

export type VistaGuardada = {
  id: string;
  nombre: string;
  filtros: FiltrosActividad;
  columnas: ClaveColumna[];
};

export function esListaDeColumnas(v: unknown): v is ClaveColumna[] {
  return (
    Array.isArray(v) &&
    v.length > 0 &&
    v.every((c) => typeof c === "string" && (CLAVES_COLUMNA as readonly string[]).includes(c))
  );
}

export function esListaDeVistas(v: unknown): v is VistaGuardada[] {
  return (
    Array.isArray(v) &&
    v.every(
      (x) =>
        typeof x === "object" &&
        x !== null &&
        typeof (x as VistaGuardada).id === "string" &&
        typeof (x as VistaGuardada).nombre === "string" &&
        typeof (x as VistaGuardada).filtros === "object" &&
        (x as VistaGuardada).filtros !== null &&
        esListaDeColumnas((x as VistaGuardada).columnas),
    )
  );
}
```

`src/modules/desempeno/presentation/hooks/useActividad.ts`:

```ts
import { useCallback, useEffect, useRef, useState } from "react";
import {
  LIMITE_PAGINA_ACTIVIDAD,
  type Actividad,
  type FiltrosActividad,
  type TotalesActividad,
} from "../../domain/entities";
import type { DomainError } from "../../domain/errors";
import { listarActividad } from "../../application/usecases/consultas";
import { useDesempenoPort } from "../context/useDesempenoPort";
import { toDomainError } from "./lib/toDomainError";

export type UseActividadReturn = {
  items: readonly Actividad[];
  totales: TotalesActividad | null;
  hayMas: boolean;
  isLoading: boolean;
  isLoadingMas: boolean;
  error: DomainError | null;
  cargarMas: () => void;
  refresh: () => void;
};

// useActividad pagina GET .../actividad por cursor (contrato §4). Los totales
// son del filtro completo y vienen con la primera página: no se recalculan al
// cargar más (D10). `generacion` descarta respuestas de filtros viejos.
export function useActividad(uid: string, filtros: FiltrosActividad): UseActividadReturn {
  const port = useDesempenoPort();
  const [items, setItems] = useState<readonly Actividad[]>([]);
  const [totales, setTotales] = useState<TotalesActividad | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingMas, setIsLoadingMas] = useState(false);
  const [error, setError] = useState<DomainError | null>(null);
  const [tick, setTick] = useState(0);
  const generacion = useRef(0);

  useEffect(() => {
    const gen = ++generacion.current;
    const ctrl = new AbortController();
    setItems([]);
    setTotales(null);
    setCursor(null);
    setIsLoading(true);
    setIsLoadingMas(false);
    setError(null);
    listarActividad(port, uid, filtros, { cursor: null, limit: LIMITE_PAGINA_ACTIVIDAD }, ctrl.signal)
      .then((p) => {
        if (gen !== generacion.current) return;
        setItems(p.items);
        setTotales(p.totales);
        setCursor(p.siguienteCursor);
      })
      .catch((e: unknown) => {
        if (gen === generacion.current) setError(toDomainError(e));
      })
      .finally(() => {
        if (gen === generacion.current) setIsLoading(false);
      });
    return () => ctrl.abort();
  }, [port, uid, filtros, tick]);

  const cargarMas = useCallback(() => {
    if (cursor === null || isLoadingMas) return;
    const gen = generacion.current;
    setIsLoadingMas(true);
    listarActividad(port, uid, filtros, { cursor, limit: LIMITE_PAGINA_ACTIVIDAD })
      .then((p) => {
        if (gen !== generacion.current) return;
        setItems((previos) => [...previos, ...p.items]);
        setCursor(p.siguienteCursor);
      })
      .catch((e: unknown) => {
        if (gen === generacion.current) setError(toDomainError(e));
      })
      .finally(() => {
        if (gen === generacion.current) setIsLoadingMas(false);
      });
  }, [port, uid, filtros, cursor, isLoadingMas]);

  const refresh = useCallback(() => setTick((t) => t + 1), []);

  return { items, totales, hayMas: cursor !== null, isLoading, isLoadingMas, error, cargarMas, refresh };
}
```

`src/modules/desempeno/presentation/hooks/useVistasGuardadas.ts`:

```ts
import { useCallback, useState } from "react";
import type { FiltrosActividad } from "../../domain/entities";
import {
  CLAVE_VISTAS,
  esListaDeVistas,
  type ClaveColumna,
  type VistaGuardada,
} from "../actividadPreferencias";
import { escribirLocal, leerLocal } from "./lib/almacenLocal";

// Vistas guardadas de la pestaña Actividad, por computadora (localStorage).
export function useVistasGuardadas() {
  const [vistas, setVistas] = useState<VistaGuardada[]>(() =>
    leerLocal<VistaGuardada[]>(CLAVE_VISTAS, [], esListaDeVistas),
  );

  const guardar = useCallback(
    (nombre: string, filtros: FiltrosActividad, columnas: ClaveColumna[]): VistaGuardada => {
      const vista: VistaGuardada = {
        id: `mi-${Date.now().toString(36)}`,
        nombre: nombre.trim() || "Mi vista",
        filtros,
        columnas,
      };
      setVistas((previas) => {
        const siguientes = [...previas, vista];
        escribirLocal(CLAVE_VISTAS, siguientes);
        return siguientes;
      });
      return vista;
    },
    [],
  );

  const eliminar = useCallback((id: string) => {
    setVistas((previas) => {
      const siguientes = previas.filter((v) => v.id !== id);
      escribirLocal(CLAVE_VISTAS, siguientes);
      return siguientes;
    });
  }, []);

  return { vistas, guardar, eliminar };
}
```

`src/modules/desempeno/presentation/hooks/useColumnasActividad.ts`:

```ts
import { useCallback, useState } from "react";
import {
  CLAVE_COLUMNAS,
  CLAVES_COLUMNA,
  COLUMNAS_POR_DEFECTO,
  esListaDeColumnas,
  type ClaveColumna,
} from "../actividadPreferencias";
import { escribirLocal, leerLocal } from "./lib/almacenLocal";

export function useColumnasActividad() {
  const [columnas, setColumnas] = useState<ClaveColumna[]>(() =>
    leerLocal(CLAVE_COLUMNAS, COLUMNAS_POR_DEFECTO, esListaDeColumnas),
  );

  const fijar = useCallback((nuevas: ClaveColumna[]) => {
    if (nuevas.length === 0) return;
    // Conserva el orden canónico de las columnas.
    const ordenadas = CLAVES_COLUMNA.filter((c) => nuevas.includes(c));
    setColumnas(ordenadas);
    escribirLocal(CLAVE_COLUMNAS, ordenadas);
  }, []);

  const alternar = useCallback(
    (clave: ClaveColumna) => {
      fijar(columnas.includes(clave) ? columnas.filter((c) => c !== clave) : [...columnas, clave]);
    },
    [columnas, fijar],
  );

  return { columnas, alternar, fijar };
}
```

- [ ] **Step 4: Implementar utilidades de la pestaña**

`src/modules/desempeno/components/actividad/columnas.ts`:

```ts
import type { OrdenActividad } from "../../domain/entities";
import type { ClaveColumna } from "../../presentation/actividadPreferencias";

// Solo las columnas que el contrato puede llenar. `orden` = valor de `orden`
// del API (contrato §4); null = no ordenable en el servidor.
export const COLUMNAS: ReadonlyArray<{
  clave: ClaveColumna;
  titulo: string;
  orden: OrdenActividad | null;
  derecha?: boolean;
}> = [
  { clave: "tipo", titulo: "Tipo", orden: "tipo" },
  { clave: "hora", titulo: "Fecha y hora", orden: "hora" },
  { clave: "cliente", titulo: "Cliente", orden: "cliente" },
  { clave: "venta", titulo: "Venta", orden: null },
  { clave: "monto", titulo: "Monto o resultado", orden: "monto", derecha: true },
  { clave: "forma", titulo: "Forma de cobro", orden: null },
  { clave: "registro", titulo: "Registró", orden: "registrado_por" },
  { clave: "llegada", titulo: "Llegó al servidor", orden: null },
  { clave: "fotos", titulo: "Fotos", orden: null },
  { clave: "gps", titulo: "GPS", orden: null },
];
```

`src/modules/desempeno/components/actividad/vistasPredefinidas.ts`:

```ts
import { FILTROS_ACTIVIDAD_INICIALES, type FiltrosActividad } from "../../domain/entities";
import { primerNombre } from "../lib/format";

export type VistaPredefinida = {
  id: string;
  nombre: (nombreUsuario: string) => string;
  filtros: (uid: string, actual: FiltrosActividad) => FiltrosActividad;
};

const limpio = (actual: FiltrosActividad): FiltrosActividad => ({
  ...FILTROS_ACTIVIDAD_INICIALES,
  orden: actual.orden,
  dir: actual.dir,
});

// Vistas del mock que el contrato puede servir. "Llegaron tarde" no está: el
// contrato §4 no tiene filtro por captura tardía (ver huecos).
export const VISTAS_PREDEFINIDAS: readonly VistaPredefinida[] = [
  { id: "todo", nombre: () => "Toda la zona", filtros: (_uid, a) => limpio(a) },
  {
    id: "usuario",
    nombre: (n) => `Solo ${primerNombre(n)}`,
    filtros: (uid, a) => ({ ...limpio(a), registradoPor: [uid] }),
  },
  { id: "sin_gps", nombre: () => "Sin GPS", filtros: (_uid, a) => ({ ...limpio(a), conGps: false }) },
  {
    id: "transferencias",
    nombre: () => "Transferencias",
    filtros: (_uid, a) => ({ ...limpio(a), tipos: ["pago"], formasCobro: ["transferencia"] }),
  },
  {
    id: "grandes",
    nombre: () => "Pagos de $400 o más",
    filtros: (_uid, a) => ({ ...limpio(a), tipos: ["pago"], montoMin: "400" }),
  },
];
```

`src/modules/desempeno/components/actividad/agrupar.ts`:

```ts
import type { Actividad, OrdenActividad } from "../../domain/entities";
import { diaDeNegocioDe, fechaLarga } from "../lib/format";
import { TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";

export type Agrupacion = "" | "dia" | "cliente" | "venta" | "tipo";

export const AGRUPACIONES: ReadonlyArray<readonly [Agrupacion, string]> = [
  ["", "Sin agrupar"],
  ["dia", "Por día"],
  ["cliente", "Por cliente"],
  ["venta", "Por venta"],
  ["tipo", "Por tipo"],
];

// Para agrupar se pide al servidor el orden que junta los renglones del mismo
// grupo. "venta" no tiene orden propio en el contrato: se pide por cliente
// (ver huecos).
export const ORDEN_PARA_AGRUPAR: Record<Exclude<Agrupacion, "">, OrdenActividad> = {
  dia: "hora",
  cliente: "cliente",
  venta: "cliente",
  tipo: "tipo",
};

export function claveDeGrupo(a: Actividad, g: Exclude<Agrupacion, "">): string {
  switch (g) {
    case "dia":
      return fechaLarga(diaDeNegocioDe(a.hora));
    case "cliente":
      return a.clienteNombre;
    case "venta":
      return `${a.folio ?? "Sin venta"} · ${a.clienteNombre}`;
    case "tipo":
      return TIPO_ACTIVIDAD_META[a.tipo].etiqueta;
  }
}

// Agrupa renglones CONSECUTIVOS en el orden que mandó el servidor. Solo
// etiqueta; no suma importes ni cuenta (D10).
export function agruparConsecutivos(
  items: readonly Actividad[],
  g: Exclude<Agrupacion, "">,
): Array<{ clave: string; items: Actividad[] }> {
  const grupos: Array<{ clave: string; items: Actividad[] }> = [];
  for (const a of items) {
    const clave = claveDeGrupo(a, g);
    const ultimo = grupos[grupos.length - 1];
    if (ultimo && ultimo.clave === clave) ultimo.items.push(a);
    else grupos.push({ clave, items: [a] });
  }
  return grupos;
}
```

- [ ] **Step 5: Prueba de la pestaña (falla)**

`src/modules/desempeno/components/actividad/__tests__/ActividadTab.test.tsx`:

```tsx
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ActividadTab } from "../ActividadTab";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import {
  FakeDesempenoPort,
  makeActividadPago,
  makePaginaActividad,
  makeUsuarioTablero,
} from "../../../application/__tests__/fakeDesempenoPort";
import { CLAVE_COLUMNAS } from "../../../presentation/actividadPreferencias";

const OPCIONES_REGISTRO = [
  { valor: "uid-juan", etiqueta: "Juan Carlos Méndez" },
  { valor: "uid-lupita", etiqueta: "Guadalupe Romero Nava" },
];

function montar(port = new FakeDesempenoPort(), onAbrirActividad = vi.fn()) {
  renderConDesempeno(
    <ActividadTab
      usuario={makeUsuarioTablero()}
      opcionesRegistro={OPCIONES_REGISTRO}
      onAbrirActividad={onAbrirActividad}
    />,
    { port },
  );
  return { port, onAbrirActividad };
}

const ultimaConsulta = (port: FakeDesempenoPort) => port.actividadCalls.at(-1)!.filtros;

describe("ActividadTab", () => {
  beforeEach(() => localStorage.clear());

  it("toda la zona: renglones y totales del filtro tal como llegan", async () => {
    const { port } = montar();
    expect(await screen.findByText("JUANA ANDRADE LECHUGA")).toBeInTheDocument();
    expect(port.actividadCalls[0].uid).toBe("uid-juan");
    expect(screen.getByText("5 movimientos")).toBeInTheDocument();
    expect(screen.getByText("importe $500")).toBeInTheDocument();
    expect(screen.getByText("3 pagos")).toBeInTheDocument();
  });

  it("filtros de tipo, forma, visita y quién registró viajan al API; no hay Depósito", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");

    await user.selectOptions(screen.getByLabelText("Tipo"), "pago");
    await waitFor(() => expect(ultimaConsulta(port).tipos).toEqual(["pago"]));

    const forma = screen.getByLabelText("Forma");
    expect(within(forma).queryByRole("option", { name: "Depósito" })).not.toBeInTheDocument();
    await user.selectOptions(forma, "transferencia");
    await waitFor(() => expect(ultimaConsulta(port).formasCobro).toEqual(["transferencia"]));

    expect(within(screen.getByLabelText("Visita")).getAllByRole("option")).toHaveLength(15);
    await user.selectOptions(screen.getByLabelText("Registró"), "oficina");
    await waitFor(() => expect(ultimaConsulta(port).registradoPor).toEqual(["oficina"]));
  });

  it("la búsqueda espera a que termines de escribir", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");
    await user.type(screen.getByPlaceholderText("Cliente o folio"), "andrade");
    await waitFor(() => expect(ultimaConsulta(port).q).toBe("andrade"));
    expect(port.actividadCalls.filter((c) => c.filtros.q === "andr")).toHaveLength(0);
  });

  it("fechas en días de la oficina; un rango invertido avisa y no se manda", async () => {
    const { port } = montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");

    fireEvent.change(screen.getByLabelText("Del"), { target: { value: "2026-10-13" } });
    await waitFor(() => expect(ultimaConsulta(port).desde).toBe("2026-10-13T06:00:00Z"));

    fireEvent.change(screen.getByLabelText("al"), { target: { value: "2026-10-12" } });
    expect(screen.getByText("El rango está invertido")).toBeInTheDocument();
    expect(ultimaConsulta(port).hasta).toBeNull();

    fireEvent.change(screen.getByLabelText("al"), { target: { value: "2026-10-14" } });
    await waitFor(() => expect(ultimaConsulta(port).hasta).toBe("2026-10-15T06:00:00Z"));
    expect(screen.queryByText("El rango está invertido")).not.toBeInTheDocument();
  });

  it("vistas predefinidas", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");
    await user.click(screen.getByRole("button", { name: "Solo Juan Carlos" }));
    await waitFor(() => expect(ultimaConsulta(port).registradoPor).toEqual(["uid-juan"]));
    await user.click(screen.getByRole("button", { name: "Transferencias" }));
    await waitFor(() => expect(ultimaConsulta(port).formasCobro).toEqual(["transferencia"]));
    expect(ultimaConsulta(port).registradoPor).toEqual([]);
    expect(screen.queryByRole("button", { name: "Llegaron tarde" })).not.toBeInTheDocument();
  });

  it("guardar una vista y volver a ella", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");
    await user.click(screen.getByRole("button", { name: "Sin GPS" }));
    await user.click(screen.getByRole("button", { name: "Guardar vista" }));
    await user.type(screen.getByLabelText("Nombre de la vista"), "Sin GPS de Juan");
    await user.click(screen.getByRole("button", { name: "Guardar" }));
    expect(screen.getByRole("button", { name: "Sin GPS de Juan" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Limpiar" }));
    await waitFor(() => expect(ultimaConsulta(port).conGps).toBeNull());
    await user.click(screen.getByRole("button", { name: "Sin GPS de Juan" }));
    await waitFor(() => expect(ultimaConsulta(port).conGps).toBe(false));
  });

  it("columnas: mostrar 'Forma de cobro' y recordarlo", async () => {
    const user = userEvent.setup();
    montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");
    expect(screen.queryByRole("columnheader", { name: "Forma de cobro" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Columnas" }));
    await user.click(await screen.findByRole("checkbox", { name: "Forma de cobro" }));
    expect(screen.getByRole("columnheader", { name: "Forma de cobro" })).toBeInTheDocument();
    expect(JSON.parse(localStorage.getItem(CLAVE_COLUMNAS)!)).toContain("forma");
  });

  it("ordenar por cliente lo pide al servidor y alterna la dirección", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");
    await user.click(screen.getByRole("button", { name: /^Cliente/ }));
    await waitFor(() => expect(ultimaConsulta(port)).toMatchObject({ orden: "cliente", dir: "asc" }));
    await user.click(screen.getByRole("button", { name: /^Cliente/ }));
    await waitFor(() => expect(ultimaConsulta(port)).toMatchObject({ orden: "cliente", dir: "desc" }));
  });

  it("agrupar por día pone encabezados sin sumas", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");
    await user.selectOptions(screen.getByLabelText("Agrupar"), "dia");
    await waitFor(() => expect(ultimaConsulta(port).orden).toBe("hora"));
    const grupos = await screen.findAllByTestId("grupo");
    expect(grupos[0]).toHaveTextContent("miércoles 14 de octubre");
    expect(grupos[0].textContent).not.toMatch(/\$/);
  });

  it("Mostrar 100 más sigue el cursor", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    port.actividadResponse = (pagina) =>
      pagina.cursor === null
        ? makePaginaActividad({ siguienteCursor: "c2" })
        : makePaginaActividad({
            items: [makeActividadPago({ id: "pago:1", clienteNombre: "HUGO NAVA CARRERA" })],
            siguienteCursor: null,
          });
    montar(port);
    await user.click(await screen.findByRole("button", { name: "Mostrar 100 más" }));
    expect(await screen.findByText("HUGO NAVA CARRERA")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mostrar 100 más" })).not.toBeInTheDocument();
  });

  it("sin exportación a Excel en esta fase", async () => {
    montar();
    await screen.findByText("JUANA ANDRADE LECHUGA");
    expect(screen.queryByRole("button", { name: "Excel" })).not.toBeInTheDocument();
  });

  it("clic en un renglón abre la actividad; sin resultados lo dice", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    const { onAbrirActividad } = montar(port);
    await user.click(await screen.findByText("JUANA ANDRADE LECHUGA"));
    expect(onAbrirActividad).toHaveBeenCalledWith(expect.objectContaining({ id: "pago:15082369" }));

    port.actividadResponse = makePaginaActividad({
      items: [],
      totales: { n: 0, importe: "0.00", porTipo: { pago: 0, visita: 0, condonacion: 0 } },
    });
    await user.selectOptions(screen.getByLabelText("Tipo"), "condonacion");
    expect(await screen.findByText("Ningún movimiento con estos filtros")).toBeInTheDocument();
  });
});
```

- [ ] **Step 6: Correrla y verla fallar**

Run: `npx vitest run src/modules/desempeno/components/actividad/__tests__/ActividadTab.test.tsx`
Expected: FAIL con `Failed to resolve import "../ActividadTab"`.

- [ ] **Step 7: Implementar los componentes de la pestaña**

`src/modules/desempeno/components/actividad/FiltrosActividadBar.tsx`:

```tsx
import { useEffect, useState } from "react";
import { Search, X } from "lucide-react";
import { rangoDeDiasDeNegocio } from "@/utils/tiempoDeNegocio";
import {
  REGISTRADO_POR_OFICINA,
  type FiltrosActividad,
  type FormaCobro,
  type TipoActividad,
} from "../../domain/entities";
import { TIPOS_VISITA } from "../../domain/values/catalogoTipoVisita";
import { diaDeNegocioDe, sumarDias } from "../lib/format";

const SELECT = "h-9 rounded-full bg-dsm-tarjeta pl-3 pr-7 text-[13px] ring-1 ring-border";
const CAMPO = "flex items-center gap-1.5 text-xs text-muted-foreground";
const ESPERA_BUSQUEDA_MS = 300;

type Props = {
  filtros: FiltrosActividad;
  opcionesRegistro: ReadonlyArray<{ valor: string; etiqueta: string }>;
  onCambiar: (parcial: Partial<FiltrosActividad>) => void;
  onLimpiar: () => void;
};

function diaDesde(f: FiltrosActividad): string {
  return f.desde ? diaDeNegocioDe(f.desde) : "";
}
// `hasta` es exclusivo (inicio del día siguiente): se muestra el día anterior.
function diaHasta(f: FiltrosActividad): string {
  return f.hasta ? sumarDias(diaDeNegocioDe(f.hasta), -1) : "";
}

function booleanoDe(v: string): boolean | null {
  return v === "con" ? true : v === "sin" ? false : null;
}
function textoDe(v: boolean | null): string {
  return v === true ? "con" : v === false ? "sin" : "";
}

export function FiltrosActividadBar({ filtros, opcionesRegistro, onCambiar, onLimpiar }: Props) {
  const [q, setQ] = useState(filtros.q);
  const [rango, setRango] = useState<{ desde: string; hasta: string } | null>(null);
  const [montoMin, setMontoMin] = useState(filtros.montoMin ?? "");
  const [montoMax, setMontoMax] = useState(filtros.montoMax ?? "");

  // Si los filtros cambian desde fuera (vista, Limpiar), los campos se alinean.
  useEffect(() => {
    setQ(filtros.q);
    setMontoMin(filtros.montoMin ?? "");
    setMontoMax(filtros.montoMax ?? "");
    setRango(null);
  }, [filtros.q, filtros.montoMin, filtros.montoMax, filtros.desde, filtros.hasta]);

  useEffect(() => {
    if (q === filtros.q) return undefined;
    const t = window.setTimeout(() => onCambiar({ q }), ESPERA_BUSQUEDA_MS);
    return () => window.clearTimeout(t);
  }, [q, filtros.q, onCambiar]);

  const desdeVisible = rango?.desde ?? diaDesde(filtros);
  const hastaVisible = rango?.hasta ?? diaHasta(filtros);
  const invertido = desdeVisible !== "" && hastaVisible !== "" && desdeVisible > hastaVisible;

  const cambiarRango = (desde: string, hasta: string) => {
    if (desde !== "" && hasta !== "" && desde > hasta) {
      // rangoDeDiasDeNegocio lanzaría: se avisa y se conserva el filtro anterior.
      setRango({ desde, hasta });
      return;
    }
    setRango(null);
    onCambiar({
      desde: desde ? rangoDeDiasDeNegocio(desde, desde).desde : null,
      hasta: hasta ? rangoDeDiasDeNegocio(hasta, hasta).hasta : null,
    });
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      <label className="relative">
        <Search aria-hidden className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Cliente o folio"
          className="h-9 w-[220px] rounded-full bg-dsm-tarjeta pl-8 pr-3 text-[13px] ring-1 ring-border outline-none"
        />
      </label>
      <label className={CAMPO}>
        Tipo
        <select
          className={SELECT}
          value={filtros.tipos[0] ?? ""}
          onChange={(e) => onCambiar({ tipos: e.target.value ? [e.target.value as TipoActividad] : [] })}
        >
          <option value="">Todos</option>
          <option value="pago">Pagos</option>
          <option value="visita">Visitas</option>
          <option value="condonacion">Condonaciones</option>
        </select>
      </label>
      <label className={CAMPO}>
        Forma
        <select
          className={SELECT}
          value={filtros.formasCobro[0] ?? ""}
          onChange={(e) => onCambiar({ formasCobro: e.target.value ? [e.target.value as FormaCobro] : [] })}
        >
          <option value="">Todas</option>
          <option value="efectivo">Efectivo</option>
          <option value="transferencia">Transferencia</option>
          <option value="cheque">Cheque</option>
          <option value="condonacion">Condonación</option>
        </select>
      </label>
      <label className={CAMPO}>
        Visita
        <select
          className={SELECT}
          value={filtros.tiposVisita[0] ?? ""}
          onChange={(e) => onCambiar({ tiposVisita: e.target.value ? [e.target.value] : [] })}
        >
          <option value="">Todas</option>
          {TIPOS_VISITA.map((t) => (
            <option key={t} value={t}>{t}</option>
          ))}
        </select>
      </label>
      <label className={CAMPO}>
        Registró
        <select
          className={SELECT}
          value={filtros.registradoPor[0] ?? ""}
          onChange={(e) => onCambiar({ registradoPor: e.target.value ? [e.target.value] : [] })}
        >
          <option value="">Todos</option>
          <option value={REGISTRADO_POR_OFICINA}>Oficina</option>
          {opcionesRegistro.map((o) => (
            <option key={o.valor} value={o.valor}>{o.etiqueta}</option>
          ))}
        </select>
      </label>
      <label className={CAMPO}>
        Del
        <input type="date" className={SELECT} value={desdeVisible} onChange={(e) => cambiarRango(e.target.value, hastaVisible)} />
      </label>
      <label className={CAMPO}>
        al
        <input type="date" className={SELECT} value={hastaVisible} onChange={(e) => cambiarRango(desdeVisible, e.target.value)} />
      </label>
      {invertido && <span role="alert" className="text-xs text-dsm-rojo">El rango está invertido</span>}
      <span className={CAMPO}>
        Monto
        <input
          aria-label="Monto mínimo"
          inputMode="decimal"
          placeholder="$ mín"
          className={`${SELECT} w-24`}
          value={montoMin}
          onChange={(e) => setMontoMin(e.target.value)}
          onBlur={() => onCambiar({ montoMin: montoMin.trim() === "" ? null : montoMin.trim() })}
          onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
        />
        –
        <input
          aria-label="Monto máximo"
          inputMode="decimal"
          placeholder="$ máx"
          className={`${SELECT} w-24`}
          value={montoMax}
          onChange={(e) => setMontoMax(e.target.value)}
          onBlur={() => onCambiar({ montoMax: montoMax.trim() === "" ? null : montoMax.trim() })}
          onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
        />
      </span>
      <label className={CAMPO}>
        Fotos
        <select className={SELECT} value={textoDe(filtros.conFotos)} onChange={(e) => onCambiar({ conFotos: booleanoDe(e.target.value) })}>
          <option value="">Todas</option>
          <option value="con">Con fotos</option>
          <option value="sin">Sin fotos</option>
        </select>
      </label>
      <label className={CAMPO}>
        GPS
        <select className={SELECT} value={textoDe(filtros.conGps)} onChange={(e) => onCambiar({ conGps: booleanoDe(e.target.value) })}>
          <option value="">Todos</option>
          <option value="con">Con GPS</option>
          <option value="sin">Sin GPS</option>
        </select>
      </label>
      <button type="button" onClick={onLimpiar} className="inline-flex h-9 items-center gap-1 rounded-full px-3 text-[13px] hover:bg-muted">
        <X aria-hidden className="h-3.5 w-3.5" />
        Limpiar
      </button>
    </div>
  );
}
```

`src/modules/desempeno/components/actividad/VistasActividad.tsx`:

```tsx
import { useState } from "react";
import { Bookmark, Plus, X } from "lucide-react";
import { cn } from "@/lib/utils";
import type { VistaGuardada } from "../../presentation/actividadPreferencias";

const PILDORA = "inline-flex h-8 items-center gap-1.5 rounded-full px-3.5 text-[12.5px] ring-1 ring-border";

export function VistasActividad({
  predefinidas,
  guardadas,
  activa,
  onAplicar,
  onEliminar,
  onGuardar,
}: {
  predefinidas: ReadonlyArray<{ id: string; nombre: string }>;
  guardadas: readonly VistaGuardada[];
  activa: string | null;
  onAplicar: (id: string) => void;
  onEliminar: (id: string) => void;
  onGuardar: (nombre: string) => void;
}) {
  const [nombrando, setNombrando] = useState(false);
  const [nombre, setNombre] = useState("");
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="mr-1 font-mono text-[10.5px] uppercase tracking-[0.08em] text-muted-foreground">Vistas</span>
      {predefinidas.map((v) => (
        <button
          key={v.id}
          type="button"
          aria-pressed={activa === v.id}
          onClick={() => onAplicar(v.id)}
          className={cn(PILDORA, activa === v.id ? "bg-foreground text-background" : "bg-dsm-tarjeta")}
        >
          {v.nombre}
        </button>
      ))}
      {guardadas.map((v) => (
        <span key={v.id} className={cn(PILDORA, "pr-1", activa === v.id ? "bg-foreground text-background" : "bg-dsm-tarjeta")}>
          <button type="button" aria-pressed={activa === v.id} onClick={() => onAplicar(v.id)} className="inline-flex items-center gap-1.5">
            <Bookmark aria-hidden className="h-3 w-3" />
            {v.nombre}
          </button>
          <button type="button" aria-label={`Quitar vista ${v.nombre}`} onClick={() => onEliminar(v.id)} className="rounded-full p-1 hover:bg-muted">
            <X aria-hidden className="h-3 w-3" />
          </button>
        </span>
      ))}
      {nombrando ? (
        <form
          className="inline-flex items-center gap-1.5"
          onSubmit={(e) => {
            e.preventDefault();
            onGuardar(nombre);
            setNombre("");
            setNombrando(false);
          }}
        >
          <input
            aria-label="Nombre de la vista"
            autoFocus
            value={nombre}
            onChange={(e) => setNombre(e.target.value)}
            placeholder={`Mi vista ${guardadas.length + 1}`}
            className="h-8 w-44 rounded-full bg-dsm-tarjeta px-3 text-[12.5px] ring-1 ring-border outline-none"
          />
          <button type="submit" className={cn(PILDORA, "bg-foreground text-background")}>Guardar</button>
        </form>
      ) : (
        <button type="button" onClick={() => setNombrando(true)} className={cn(PILDORA, "border-dashed bg-transparent")}>
          <Plus aria-hidden className="h-3 w-3" />
          Guardar vista
        </button>
      )}
    </div>
  );
}
```

`src/modules/desempeno/components/actividad/MenuColumnas.tsx`:

```tsx
import { Columns3 } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { ClaveColumna } from "../../presentation/actividadPreferencias";
import { COLUMNAS } from "./columnas";

export function MenuColumnas({
  columnas,
  onAlternar,
}: {
  columnas: readonly ClaveColumna[];
  onAlternar: (c: ClaveColumna) => void;
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" className="inline-flex h-9 items-center gap-1.5 rounded-full bg-dsm-tarjeta px-3.5 text-[13px] ring-1 ring-border">
          <Columns3 aria-hidden className="h-3.5 w-3.5" />
          Columnas
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-56 p-2">
        {COLUMNAS.map((c) => (
          <label key={c.clave} className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-[13px] hover:bg-muted">
            <input type="checkbox" checked={columnas.includes(c.clave)} onChange={() => onAlternar(c.clave)} />
            {c.titulo}
          </label>
        ))}
      </PopoverContent>
    </Popover>
  );
}
```

`src/modules/desempeno/components/actividad/TablaActividad.tsx`:

```tsx
import type { ReactNode } from "react";
import { ArrowDown, ArrowUp, Camera, MapPin, MapPinOff } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Actividad, FiltrosActividad, OrdenActividad } from "../../domain/entities";
import type { ClaveColumna } from "../../presentation/actividadPreferencias";
import { diaDeNegocioDe, fechaCorta, horaCorta } from "../lib/format";
import { FORMA_COBRO_ETIQUETA } from "../lib/tipoActividadMeta";
import { InfoRapida } from "../comunes/InfoRapida";
import { IconoTipoActividad } from "../comunes/IconoTipoActividad";
import { MontoActividad } from "../comunes/MontoActividad";
import { TarjetaActividad } from "../comunes/TarjetaActividad";
import { COLUMNAS } from "./columnas";
import { agruparConsecutivos, type Agrupacion } from "./agrupar";

function cuando(iso: string): string {
  return `${fechaCorta(diaDeNegocioDe(iso))} · ${horaCorta(iso)}`;
}

function Celda({
  clave,
  a,
  renderFotos,
}: {
  clave: ClaveColumna;
  a: Actividad;
  renderFotos?: (a: Actividad) => ReactNode;
}) {
  switch (clave) {
    case "tipo":
      return (
        <InfoRapida contenido={<TarjetaActividad actividad={a} />}>
          <span>
            <IconoTipoActividad tipo={a.tipo} />
          </span>
        </InfoRapida>
      );
    case "hora":
      return <span className="font-mono text-[12px]">{cuando(a.hora)}</span>;
    case "cliente":
      return (
        <InfoRapida contenido={<TarjetaActividad actividad={a} />}>
          <span className="cursor-help">{a.clienteNombre}</span>
        </InfoRapida>
      );
    case "venta":
      return <span className="font-mono text-[12px]">{a.folio ?? "—"}</span>;
    case "monto":
      return <MontoActividad actividad={a} />;
    case "forma":
      return <span>{a.pago && a.tipo === "pago" ? FORMA_COBRO_ETIQUETA[a.pago.formaCobro] : "—"}</span>;
    case "registro":
      return (
        <span className={a.registradoPor.origen === "oficina" ? "text-dsm-violeta" : undefined}>
          {a.registradoPor.nombre}
        </span>
      );
    case "llegada":
      return <span className="font-mono text-[12px]">{a.recibidoAt ? cuando(a.recibidoAt) : "—"}</span>;
    case "fotos":
      if (renderFotos) return <>{renderFotos(a)}</>;
      return a.numFotos > 0 ? (
        <span className="inline-flex items-center gap-1"><Camera aria-hidden className="h-3.5 w-3.5" />{a.numFotos}</span>
      ) : (
        <span className="text-muted-foreground">—</span>
      );
    case "gps":
      return a.conGps ? (
        <MapPin aria-label="Con GPS" className="h-3.5 w-3.5 text-muted-foreground" />
      ) : (
        <span title="Sin GPS" className="text-dsm-ambar"><MapPinOff aria-hidden className="h-3.5 w-3.5" /></span>
      );
  }
}

export function TablaActividad({
  items,
  columnas,
  filtros,
  agrupacion,
  onOrdenar,
  onAbrir,
  renderFotos,
}: {
  items: readonly Actividad[];
  columnas: readonly ClaveColumna[];
  filtros: FiltrosActividad;
  agrupacion: Agrupacion;
  onOrdenar: (orden: OrdenActividad) => void;
  onAbrir: (a: Actividad) => void;
  renderFotos?: (a: Actividad) => ReactNode;
}) {
  const visibles = COLUMNAS.filter((c) => columnas.includes(c.clave));
  const renglon = (a: Actividad) => (
    <tr key={a.id} onClick={() => onAbrir(a)} className="cursor-pointer border-b border-border hover:bg-muted/50">
      {visibles.map((c) => (
        <td key={c.clave} className={cn("px-3 py-2.5 text-[13px]", c.derecha && "text-right")} onClick={c.clave === "fotos" ? (e) => e.stopPropagation() : undefined}>
          <Celda clave={c.clave} a={a} renderFotos={renderFotos} />
        </td>
      ))}
    </tr>
  );
  return (
    <div className="overflow-x-auto rounded-[14px] bg-dsm-tarjeta ring-1 ring-border">
      <table className="w-full">
        <thead>
          <tr className="border-b border-border">
            {visibles.map((c) => {
              const activa = c.orden !== null && filtros.orden === c.orden;
              const Flecha = filtros.dir === "asc" ? ArrowUp : ArrowDown;
              return (
                <th key={c.clave} className={cn("px-3 py-2 text-left font-normal", c.derecha && "text-right")}>
                  {c.orden ? (
                    <button
                      type="button"
                      onClick={() => onOrdenar(c.orden as OrdenActividad)}
                      className="inline-flex items-center gap-1 font-mono text-[10.5px] uppercase tracking-[0.08em] text-muted-foreground hover:text-foreground"
                    >
                      {c.titulo}
                      {activa && <Flecha aria-hidden className="h-3 w-3" />}
                    </button>
                  ) : (
                    <span className="font-mono text-[10.5px] uppercase tracking-[0.08em] text-muted-foreground">{c.titulo}</span>
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {agrupacion === ""
            ? items.map(renglon)
            : agruparConsecutivos(items, agrupacion).map((g) => [
                <tr key={`g-${g.clave}-${g.items[0].id}`} data-testid="grupo" className="bg-muted/40">
                  <td colSpan={visibles.length} className="px-3 py-1.5 text-[12.5px] font-medium">{g.clave}</td>
                </tr>,
                ...g.items.map(renglon),
              ])}
        </tbody>
      </table>
    </div>
  );
}
```

`src/modules/desempeno/components/actividad/ActividadTab.tsx`:

```tsx
import { useCallback, useMemo, useState, type ReactNode } from "react";
import {
  FILTROS_ACTIVIDAD_INICIALES,
  type Actividad,
  type FiltrosActividad,
  type OrdenActividad,
  type UsuarioTablero,
} from "../../domain/entities";
import { useActividad } from "../../presentation/hooks/useActividad";
import { useColumnasActividad } from "../../presentation/hooks/useColumnasActividad";
import { useVistasGuardadas } from "../../presentation/hooks/useVistasGuardadas";
import { dinero, entero, plural } from "../lib/format";
import { ErrorConReintento, EstadoVacio } from "../comunes/Estados";
import { FiltrosActividadBar } from "./FiltrosActividadBar";
import { VistasActividad } from "./VistasActividad";
import { MenuColumnas } from "./MenuColumnas";
import { TablaActividad } from "./TablaActividad";
import { AGRUPACIONES, ORDEN_PARA_AGRUPAR, type Agrupacion } from "./agrupar";
import { VISTAS_PREDEFINIDAS } from "./vistasPredefinidas";

// Actividad de TODA la zona del usuario, cualquier registrador (contrato §4).
export function ActividadTab({
  usuario,
  opcionesRegistro,
  onAbrirActividad,
  renderFotos,
}: {
  usuario: UsuarioTablero;
  opcionesRegistro: ReadonlyArray<{ valor: string; etiqueta: string }>;
  onAbrirActividad: (a: Actividad) => void;
  renderFotos?: (a: Actividad) => ReactNode;
}) {
  const [filtros, setFiltros] = useState<FiltrosActividad>(FILTROS_ACTIVIDAD_INICIALES);
  const [vistaActiva, setVistaActiva] = useState<string | null>("todo");
  const [agrupacion, setAgrupacion] = useState<Agrupacion>("");
  const { columnas, alternar, fijar } = useColumnasActividad();
  const { vistas, guardar, eliminar } = useVistasGuardadas();
  const actividad = useActividad(usuario.uid, filtros);

  const cambiar = useCallback((parcial: Partial<FiltrosActividad>) => {
    setFiltros((f) => ({ ...f, ...parcial }));
    setVistaActiva(null);
  }, []);

  const predefinidas = useMemo(
    () => VISTAS_PREDEFINIDAS.map((v) => ({ id: v.id, nombre: v.nombre(usuario.nombre) })),
    [usuario.nombre],
  );

  const aplicarVista = (id: string) => {
    const predefinida = VISTAS_PREDEFINIDAS.find((v) => v.id === id);
    if (predefinida) {
      setFiltros((f) => predefinida.filtros(usuario.uid, f));
    } else {
      const guardada = vistas.find((v) => v.id === id);
      if (!guardada) return;
      setFiltros({ ...FILTROS_ACTIVIDAD_INICIALES, ...guardada.filtros });
      fijar(guardada.columnas);
    }
    setVistaActiva(id);
  };

  const ordenar = (orden: OrdenActividad) =>
    setFiltros((f) =>
      f.orden === orden
        ? { ...f, dir: f.dir === "asc" ? "desc" : "asc" }
        : { ...f, orden, dir: orden === "hora" || orden === "monto" ? "desc" : "asc" },
    );

  const agrupar = (g: Agrupacion) => {
    setAgrupacion(g);
    if (g !== "") setFiltros((f) => ({ ...f, orden: ORDEN_PARA_AGRUPAR[g], dir: g === "dia" ? "desc" : "asc" }));
  };

  const t = actividad.totales;
  return (
    <div className="flex flex-col gap-3">
      <VistasActividad
        predefinidas={predefinidas}
        guardadas={vistas}
        activa={vistaActiva}
        onAplicar={aplicarVista}
        onEliminar={eliminar}
        onGuardar={(nombre) => setVistaActiva(guardar(nombre, filtros, columnas).id)}
      />
      <FiltrosActividadBar
        filtros={filtros}
        opcionesRegistro={opcionesRegistro}
        onCambiar={cambiar}
        onLimpiar={() => aplicarVista("todo")}
      />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap gap-4 text-[13px] text-muted-foreground">
          {t && (
            <>
              <span>{`${entero(t.n)} movimientos`}</span>
              <span>{plural(t.porTipo.pago, "pago", "pagos")}</span>
              <span>{plural(t.porTipo.visita, "visita", "visitas")}</span>
              <span>{plural(t.porTipo.condonacion, "condonación", "condonaciones")}</span>
              <span>{`importe ${dinero(t.importe)}`}</span>
            </>
          )}
        </div>
        <div className="flex items-center gap-2">
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            Agrupar
            <select
              className="h-9 rounded-full bg-dsm-tarjeta pl-3 pr-7 text-[13px] ring-1 ring-border"
              value={agrupacion}
              onChange={(e) => agrupar(e.target.value as Agrupacion)}
            >
              {AGRUPACIONES.map(([v, texto]) => (
                <option key={v} value={v}>{texto}</option>
              ))}
            </select>
          </label>
          <MenuColumnas columnas={columnas} onAlternar={alternar} />
        </div>
      </div>

      {actividad.error && actividad.items.length === 0 ? (
        <ErrorConReintento titulo="No se pudo cargar la actividad" mensaje={actividad.error.message} onReintentar={actividad.refresh} />
      ) : actividad.isLoading ? (
        <div className="h-48 animate-pulse rounded-[14px] bg-dsm-tarjeta" />
      ) : actividad.items.length === 0 ? (
        <div className="rounded-[14px] bg-dsm-tarjeta ring-1 ring-border">
          <EstadoVacio titulo="Ningún movimiento con estos filtros" detalle="Quita algún filtro o usa “Limpiar”." />
        </div>
      ) : (
        <TablaActividad
          items={actividad.items}
          columnas={columnas}
          filtros={filtros}
          agrupacion={agrupacion}
          onOrdenar={ordenar}
          onAbrir={onAbrirActividad}
          renderFotos={renderFotos}
        />
      )}
      {actividad.hayMas && (
        <div className="flex justify-center">
          <button
            type="button"
            disabled={actividad.isLoadingMas}
            onClick={actividad.cargarMas}
            className="h-9 rounded-full bg-dsm-tarjeta px-4 text-[13px] ring-1 ring-border disabled:opacity-50"
          >
            Mostrar 100 más
          </button>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 8: Correr las pruebas de la tarea**

Run: `npx vitest run src/modules/desempeno/presentation/hooks src/modules/desempeno/components/actividad`
Expected: PASS (useActividad 4, preferenciasActividad 3, agrupar 2, ActividadTab 12 = 21 pruebas nuevas, más las de hooks de tareas anteriores).

- [ ] **Step 9: Tipos y lint**

Run: `npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: sin salida.

- [ ] **Step 10: Commit**

```bash
git add src/modules/desempeno/presentation/actividadPreferencias.ts src/modules/desempeno/presentation/hooks/useActividad.ts src/modules/desempeno/presentation/hooks/useVistasGuardadas.ts src/modules/desempeno/presentation/hooks/useColumnasActividad.ts src/modules/desempeno/presentation/hooks/__tests__/useActividad.test.tsx src/modules/desempeno/presentation/hooks/__tests__/preferenciasActividad.test.tsx src/modules/desempeno/components/actividad
git commit -m "feat(desempeno): pestaña Actividad de la zona con filtros, vistas guardadas y paginación por cursor"
```

---

### Tarea 2.3 (Carril 2): pestaña Cuentas de la semana (segmentos + Sin nada)

**Carril 2, rama `feat/desempeno-ficha`, después de la Tarea 2.2.**

**Files:**
- Create: `src/modules/desempeno/presentation/hooks/useCuentas.ts`
- Create: `src/modules/desempeno/components/cuentas/TarjetaCuenta.tsx`
- Create: `src/modules/desempeno/components/cuentas/TablaCuentas.tsx`
- Create: `src/modules/desempeno/components/cuentas/CuentasTab.tsx`
- Test: `src/modules/desempeno/components/cuentas/__tests__/CuentasTab.test.tsx`

**Interfaces:**
- Consumes: Tarea 0 (`Cuenta`, `CuentasSemana`, `SegmentoFiltro`, `OrdenCuentas`, `ConsultaCuentas`, `obtenerCuentas`, `useCargaRemota`, `useDesempenoPort`, `renderConDesempeno`, `ChipTrato`, `InfoRapida`, tarjetas, formato, `etiquetaDeIdActividad`, `Estados`).
- Produces:
  - `useCuentas(uid, consulta): { cuentas: CuentasSemana | null; isLoading; error; refresh }`.
  - `CuentasTab(props: { usuario: UsuarioTablero; onAbrirCuenta: (c: Cuenta) => void })`.

- [ ] **Step 1: Prueba (falla)**

`src/modules/desempeno/components/cuentas/__tests__/CuentasTab.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CuentasTab } from "../CuentasTab";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import {
  FakeDesempenoPort,
  makeCuenta,
  makeCuentasSemana,
  makeUsuarioTablero,
} from "../../../application/__tests__/fakeDesempenoPort";
import { DomainError } from "../../../domain/errors";

function montar(port = new FakeDesempenoPort(), onAbrirCuenta = vi.fn()) {
  renderConDesempeno(<CuentasTab usuario={makeUsuarioTablero()} onAbrirCuenta={onAbrirCuenta} />, { port });
  return { port, onAbrirCuenta };
}
const ultima = (port: FakeDesempenoPort) => port.cuentasCalls.at(-1)!.consulta;

describe("CuentasTab", () => {
  it("abre en Sin visitar, ordenado por más atrasos", async () => {
    const { port } = montar();
    await screen.findByText("GERARDO XICALE ANALCO");
    expect(ultima(port)).toEqual({ segmento: "sin_visitar", q: "", orden: "atrasos", dir: "desc" });
    expect(port.cuentasCalls[0].uid).toBe("uid-juan");
  });

  it("pastillas con los conteos del API y 'Sin nada' aparte", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("GERARDO XICALE ANALCO");
    expect(within(screen.getByRole("button", { name: /Pagados/ })).getByText("140")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Pagados/ }));
    await waitFor(() => expect(ultima(port).segmento).toBe("pagados"));
    await user.click(screen.getByRole("button", { name: /Sin nada/ }));
    await waitFor(() => expect(ultima(port).segmento).toBe("sin_nada"));
  });

  it("orden y búsqueda viajan al API", async () => {
    const user = userEvent.setup();
    const { port } = montar();
    await screen.findByText("GERARDO XICALE ANALCO");
    await user.selectOptions(screen.getByLabelText("Ordenar"), "saldo");
    await waitFor(() => expect(ultima(port)).toMatchObject({ orden: "saldo", dir: "desc" }));
    await user.type(screen.getByPlaceholderText("Cliente o folio"), "xicale");
    await waitFor(() => expect(ultima(port).q).toBe("xicale"));
  });

  it("chip del estado como en el teléfono, con popover del servidor", async () => {
    const user = userEvent.setup();
    montar();
    const chip = await screen.findByText("Pagó esta semana");
    expect(chip.closest("[data-trato]")).toHaveAttribute("data-trato", "pagado");
    await user.hover(chip);
    expect(await screen.findByText("No hace falta volver")).toBeInTheDocument();
    expect(screen.getByText("Pago · mié 14 oct 08:20")).toBeInTheDocument();
    expect(screen.getByText("$150 de $150")).toBeInTheDocument();
  });

  it("atrasos y clic en la cuenta", async () => {
    const user = userEvent.setup();
    const { onAbrirCuenta } = montar();
    expect(await screen.findByText("Al corriente")).toBeInTheDocument();
    expect(screen.getByText("6 atrasos")).toBeInTheDocument();
    await user.click(screen.getByText("GERARDO XICALE ANALCO"));
    expect(onAbrirCuenta).toHaveBeenCalledWith(expect.objectContaining({ ventaId: 13791100 }));
  });

  it("una cuenta sin docto_pv_id no se puede abrir", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    port.cuentasResponse = makeCuentasSemana({ cuentas: [makeCuenta({ doctoPvId: null })] });
    const { onAbrirCuenta } = montar(port);
    const fila = (await screen.findByText("JUANA ANDRADE LECHUGA")).closest("tr")!;
    expect(fila).toHaveAttribute("aria-disabled", "true");
    await user.click(fila);
    expect(onAbrirCuenta).not.toHaveBeenCalled();
  });

  it("grupo vacío y error", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    port.cuentasResponse = makeCuentasSemana({ cuentas: [] });
    montar(port);
    expect(await screen.findByText("No hay cuentas en este grupo")).toBeInTheDocument();

    port.throwOnNext.obtenerCuentas = new DomainError("desempeno_usuario_sin_inicio_semana", "el usuario no tiene inicio de semana");
    await user.click(screen.getByRole("button", { name: /Volver/ }));
    expect(await screen.findByText("el usuario no tiene inicio de semana")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Correrla y verla fallar**

Run: `npx vitest run src/modules/desempeno/components/cuentas`
Expected: FAIL con `Failed to resolve import "../CuentasTab"`.

- [ ] **Step 3: Implementar**

`src/modules/desempeno/presentation/hooks/useCuentas.ts`:

```ts
import { useCallback } from "react";
import type { CuentasSemana } from "../../domain/entities";
import type { DomainError } from "../../domain/errors";
import type { ConsultaCuentas } from "../../application/ports/DesempenoPort";
import { obtenerCuentas } from "../../application/usecases/consultas";
import { useDesempenoPort } from "../context/useDesempenoPort";
import { useCargaRemota } from "./lib/useCargaRemota";

// El llamador memoiza `consulta`.
export function useCuentas(
  uid: string,
  consulta: ConsultaCuentas,
): { cuentas: CuentasSemana | null; isLoading: boolean; error: DomainError | null; refresh: () => void } {
  const port = useDesempenoPort();
  const cargar = useCallback(
    (signal: AbortSignal) => obtenerCuentas(port, uid, consulta, signal),
    [port, uid, consulta],
  );
  const { datos, isLoading, error, refresh } = useCargaRemota(cargar);
  return { cuentas: datos, isLoading, error, refresh };
}
```

`src/modules/desempeno/components/cuentas/TarjetaCuenta.tsx`:

```tsx
import type { Cuenta } from "../../domain/entities";
import { diaDeNegocioDe, dinero, fechaCorta, horaCorta } from "../lib/format";
import { etiquetaDeIdActividad } from "../lib/tipoActividadMeta";
import { TarjetaFilas, TarjetaNota, TarjetaTitulo } from "../comunes/InfoRapida";

function cuando(iso: string): string {
  return `${fechaCorta(diaDeNegocioDe(iso))} ${horaCorta(iso)}`;
}

// Popover del chip: etiqueta, detalle y qué movimiento lo determinó, todo del
// servidor (contrato §5).
export function TarjetaCuenta({ cuenta: c }: { cuenta: Cuenta }) {
  const determino = c.determinadoPor
    ? c.ultimaActividad && c.ultimaActividad.id === c.determinadoPor
      ? `${etiquetaDeIdActividad(c.determinadoPor)} · ${cuando(c.ultimaActividad.hora)}`
      : etiquetaDeIdActividad(c.determinadoPor)
    : "—";
  return (
    <>
      <TarjetaTitulo>{c.etiqueta}</TarjetaTitulo>
      <TarjetaNota>{c.detalle}</TarjetaNota>
      <TarjetaFilas
        filas={[
          ["Lo determinó", determino],
          ["Abonado en la semana", `${dinero(c.abonoSemana)} de ${dinero(c.parcialidad)}`],
          ["Última actividad", c.ultimaActividad ? cuando(c.ultimaActividad.hora) : "—"],
        ]}
      />
    </>
  );
}
```

`src/modules/desempeno/components/cuentas/TablaCuentas.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { Cuenta } from "../../domain/entities";
import { diaDeNegocioDe, dinero, fechaCorta, horaCorta } from "../lib/format";
import { TIPO_ACTIVIDAD_META } from "../lib/tipoActividadMeta";
import { ChipTrato } from "../comunes/ChipTrato";
import { InfoRapida } from "../comunes/InfoRapida";
import { TarjetaCuenta } from "./TarjetaCuenta";

const ATRASOS_ALTOS = 5;
const ENCABEZADOS = ["Estado", "Cliente", "Venta", "Saldo", "Semanal", "Atrasos", "Última actividad"];

function Atrasos({ n }: { n: number }) {
  if (n === 0) return <span className="text-[12px] text-dsm-ok">Al corriente</span>;
  return (
    <span className={cn("text-[12px]", n >= ATRASOS_ALTOS ? "text-dsm-rojo" : "text-dsm-ambar")}>
      {`${n} ${n === 1 ? "atraso" : "atrasos"}`}
    </span>
  );
}

export function TablaCuentas({ cuentas, onAbrir }: { cuentas: readonly Cuenta[]; onAbrir: (c: Cuenta) => void }) {
  return (
    <div className="overflow-x-auto rounded-[14px] bg-dsm-tarjeta ring-1 ring-border">
      <table className="w-full">
        <thead>
          <tr className="border-b border-border">
            {ENCABEZADOS.map((t, i) => (
              <th
                key={t}
                className={cn(
                  "px-3 py-2 text-left font-mono text-[10.5px] font-normal uppercase tracking-[0.08em] text-muted-foreground",
                  (i === 3 || i === 4) && "text-right",
                )}
              >
                {t}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {cuentas.map((c) => {
            const abrible = c.doctoPvId !== null;
            return (
              <tr
                key={c.ventaId}
                aria-disabled={abrible ? undefined : "true"}
                title={abrible ? undefined : "Sin detalle de venta"}
                onClick={() => abrible && onAbrir(c)}
                className={cn("border-b border-border", abrible ? "cursor-pointer hover:bg-muted/50" : "opacity-80")}
              >
                <td className="px-3 py-2.5">
                  <InfoRapida contenido={<TarjetaCuenta cuenta={c} />}>
                    <span tabIndex={0} className="inline-block cursor-help">
                      <ChipTrato trato={c.trato} etiqueta={c.etiqueta} />
                    </span>
                  </InfoRapida>
                </td>
                <td className="px-3 py-2.5 text-[13px]">
                  {c.clienteNombre}
                  {c.nueva && <span className="ml-2 rounded-full bg-dsm-marca-fondo px-1.5 text-[10px] text-dsm-marca">Nueva</span>}
                </td>
                <td className="px-3 py-2.5">
                  <div className="text-[13px]">{c.producto ?? "—"}</div>
                  <div className="font-mono text-[11px] text-muted-foreground">{c.folio ?? "—"}</div>
                </td>
                <td className="px-3 py-2.5 text-right tabular">{dinero(c.saldo)}</td>
                <td className="px-3 py-2.5 text-right tabular">{dinero(c.parcialidad)}</td>
                <td className="px-3 py-2.5"><Atrasos n={c.numAtrasos} /></td>
                <td className="px-3 py-2.5 text-[12px]">
                  {c.ultimaActividad ? (
                    <>
                      <span className="font-mono">{`${fechaCorta(diaDeNegocioDe(c.ultimaActividad.hora))} ${horaCorta(c.ultimaActividad.hora)}`}</span>
                      <span className="text-muted-foreground">{` · ${TIPO_ACTIVIDAD_META[c.ultimaActividad.tipo].etiqueta.toLowerCase()}`}</span>
                    </>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
```

`src/modules/desempeno/components/cuentas/CuentasTab.tsx`:

```tsx
import { useEffect, useMemo, useState } from "react";
import { CircleOff, Search } from "lucide-react";
import { cn } from "@/lib/utils";
import type {
  ConteosCuentas,
  Cuenta,
  Direccion,
  OrdenCuentas,
  SegmentoFiltro,
  UsuarioTablero,
} from "../../domain/entities";
import type { ConsultaCuentas } from "../../application/ports/DesempenoPort";
import { useCuentas } from "../../presentation/hooks/useCuentas";
import { ErrorConReintento, EstadoVacio } from "../comunes/Estados";
import { InfoRapida, TarjetaNota, TarjetaTitulo } from "../comunes/InfoRapida";
import { TablaCuentas } from "./TablaCuentas";

const PILDORAS: ReadonlyArray<readonly [SegmentoFiltro, string, keyof ConteosCuentas]> = [
  ["sin_visitar", "Sin visitar", "sinVisitar"],
  ["volver", "Volver", "volver"],
  ["despues", "Después", "despues"],
  ["pagados", "Pagados", "pagados"],
];

const ORDENES: Record<string, { texto: string; orden: OrdenCuentas; dir: Direccion }> = {
  atrasos: { texto: "Más atrasos", orden: "atrasos", dir: "desc" },
  saldo: { texto: "Mayor saldo", orden: "saldo", dir: "desc" },
  ultima_actividad: { texto: "Actividad reciente", orden: "ultima_actividad", dir: "desc" },
  cliente: { texto: "Cliente A–Z", orden: "cliente", dir: "asc" },
};
const ESPERA_BUSQUEDA_MS = 300;
const PILDORA = "inline-flex h-8 items-center gap-2 rounded-full px-3.5 text-[12.5px] ring-1 ring-border";

// Cuentas de la semana del usuario (contrato §5). Segmentos y estados los
// calcula el servidor igual que el teléfono (D5, D6).
export function CuentasTab({ usuario, onAbrirCuenta }: { usuario: UsuarioTablero; onAbrirCuenta: (c: Cuenta) => void }) {
  const [segmento, setSegmento] = useState<SegmentoFiltro>("sin_visitar");
  const [clave, setClave] = useState("atrasos");
  const [escrito, setEscrito] = useState("");
  const [q, setQ] = useState("");

  useEffect(() => {
    const t = window.setTimeout(() => setQ(escrito.trim()), ESPERA_BUSQUEDA_MS);
    return () => window.clearTimeout(t);
  }, [escrito]);

  const consulta = useMemo<ConsultaCuentas>(
    () => ({ segmento, q, orden: ORDENES[clave].orden, dir: ORDENES[clave].dir }),
    [segmento, q, clave],
  );
  const { cuentas, isLoading, error, refresh } = useCuentas(usuario.uid, consulta);
  const conteos = cuentas?.conteos;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-1.5">
          {PILDORAS.map(([valor, texto, campo]) => (
            <button
              key={valor}
              type="button"
              aria-pressed={segmento === valor}
              onClick={() => setSegmento(valor)}
              className={cn(PILDORA, segmento === valor ? "bg-foreground text-background" : "bg-dsm-tarjeta")}
            >
              {texto}
              <b className="font-medium">{conteos ? conteos[campo] : "·"}</b>
            </button>
          ))}
          <span aria-hidden className="mx-1 h-5 w-px bg-border" />
          <InfoRapida
            contenido={
              <>
                <TarjetaTitulo>Sin nada</TarjetaTitulo>
                <TarjetaNota>Ventas del universo sin pago ni visita esta semana.</TarjetaNota>
              </>
            }
          >
            <button
              type="button"
              aria-pressed={segmento === "sin_nada"}
              onClick={() => setSegmento("sin_nada")}
              className={cn(PILDORA, segmento === "sin_nada" ? "bg-foreground text-background" : "bg-dsm-tarjeta")}
            >
              <CircleOff aria-hidden className="h-3.5 w-3.5" />
              Sin nada
              <b className="font-medium">{conteos ? conteos.sinNada : "·"}</b>
            </button>
          </InfoRapida>
        </div>
        <div className="flex items-center gap-2">
          <label className="relative">
            <Search aria-hidden className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              value={escrito}
              onChange={(e) => setEscrito(e.target.value)}
              placeholder="Cliente o folio"
              className="h-9 w-[200px] rounded-full bg-dsm-tarjeta pl-8 pr-3 text-[13px] ring-1 ring-border outline-none"
            />
          </label>
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            Ordenar
            <select
              className="h-9 rounded-full bg-dsm-tarjeta pl-3 pr-7 text-[13px] ring-1 ring-border"
              value={clave}
              onChange={(e) => setClave(e.target.value)}
            >
              {Object.entries(ORDENES).map(([k, o]) => (
                <option key={k} value={k}>{o.texto}</option>
              ))}
            </select>
          </label>
        </div>
      </div>

      {error && !cuentas ? (
        <ErrorConReintento titulo="No se pudieron cargar las cuentas" mensaje={error.message} onReintentar={refresh} />
      ) : isLoading && !cuentas ? (
        <div className="h-48 animate-pulse rounded-[14px] bg-dsm-tarjeta" />
      ) : cuentas && cuentas.cuentas.length === 0 ? (
        <div className="rounded-[14px] bg-dsm-tarjeta ring-1 ring-border">
          <EstadoVacio titulo="No hay cuentas en este grupo" />
        </div>
      ) : cuentas ? (
        <TablaCuentas cuentas={cuentas.cuentas} onAbrir={onAbrirCuenta} />
      ) : null}
    </div>
  );
}
```

- [ ] **Step 4: Correr, tipos y lint**

Run: `npx vitest run src/modules/desempeno/components/cuentas && npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: PASS (7 pruebas), sin salida de tsc/eslint.

- [ ] **Step 5: Commit**

```bash
git add src/modules/desempeno/presentation/hooks/useCuentas.ts src/modules/desempeno/components/cuentas
git commit -m "feat(desempeno): pestaña Cuentas de la semana con segmentos del teléfono y Sin nada"
```

---

### Tarea 2.4 (Carril 2): pantalla de la ficha (encabezado, números y pestañas)

**Carril 2, rama `feat/desempeno-ficha`, después de la Tarea 2.3.** La ficha recibe el usuario por props: la integración lo saca de `useTablero` (carril 1) y le conecta modales (carril 3) y línea (carril 1) por los huecos.

**Files:**
- Create: `src/modules/desempeno/components/ficha/pestanas.ts`
- Create: `src/modules/desempeno/components/ficha/FichaEncabezado.tsx`
- Create: `src/modules/desempeno/components/ficha/FichaScreen.tsx`
- Test: `src/modules/desempeno/components/ficha/__tests__/FichaScreen.test.tsx`

**Interfaces:**
- Consumes: Tarea 0 (`UsuarioTablero`, `Periodo`, `Actividad`, `Jornada`, `Cuenta`, formato, `PuntoEstado`, `SegmentoPeriodo`, `AvisoPantallaAngosta`), Tareas 2.1–2.3 (`JornadaTab`, `ActividadTab`, `CuentasTab`).
- Produces:
  - `PESTANAS = ["jornada", "actividad", "cuentas"] as const`, `type PestanaFicha`, `esPestana(v)`.
  - `FichaScreen(props: FichaScreenProps)` con `FichaScreenProps = { usuario: UsuarioTablero; periodo: Periodo; generadoAt: string; opcionesRegistro: ReadonlyArray<{ valor: string; etiqueta: string }>; pestana: PestanaFicha; dia: string; hoy: string; onCambiarPestana: (p: PestanaFicha) => void; onCambiarDia: (dia: string) => void; onVolver: () => void; onAbrirActividad: (a: Actividad) => void; onAbrirCuenta: (c: Cuenta) => void; renderFotos?: (a: Actividad) => ReactNode; renderLinea?: (j: Jornada, ahora: string | null) => ReactNode; controlesLinea?: ReactNode }`.

- [ ] **Step 1: Prueba (falla)**

`src/modules/desempeno/components/ficha/__tests__/FichaScreen.test.tsx`:

```tsx
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FichaScreen } from "../FichaScreen";
import type { PestanaFicha } from "../pestanas";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import { makeUsuarioTablero } from "../../../application/__tests__/fakeDesempenoPort";

function montar(pestana: PestanaFicha = "jornada") {
  const props = {
    onCambiarPestana: vi.fn(),
    onCambiarDia: vi.fn(),
    onVolver: vi.fn(),
    onAbrirActividad: vi.fn(),
    onAbrirCuenta: vi.fn(),
  };
  renderConDesempeno(
    <FichaScreen
      usuario={makeUsuarioTablero()}
      periodo="hoy"
      generadoAt="2026-10-14T22:40:00Z"
      opcionesRegistro={[{ valor: "uid-juan", etiqueta: "Juan Carlos Méndez" }]}
      pestana={pestana}
      dia="2026-10-14"
      hoy="2026-10-14"
      {...props}
    />,
  );
  return props;
}

describe("FichaScreen", () => {
  beforeEach(() => localStorage.clear());

  it("encabezado con zona, inicio de semana, estado y cifras del usuario (sin totales del equipo)", () => {
    montar();
    expect(screen.getByRole("heading", { name: "Juan Carlos Méndez" })).toBeInTheDocument();
    expect(screen.getByText("RUTA 21")).toBeInTheDocument();
    expect(screen.getByText("Semana desde vie 9 oct · hace 5 d")).toBeInTheDocument();
    expect(screen.getByText("Activo")).toBeInTheDocument();
    expect(screen.getByText("Cobrado hoy")).toBeInTheDocument();
    expect(screen.getByText("$8,600")).toBeInTheDocument();
    expect(screen.getByText("46 de 232")).toBeInTheDocument();
    expect(screen.getByText("65.9%")).toBeInTheDocument();
    expect(screen.queryByText(/total/i)).not.toBeInTheDocument();
  });

  it("pestañas: marca la actual y avisa al cambiar", async () => {
    const user = userEvent.setup();
    const props = montar("jornada");
    expect(screen.getByRole("tab", { name: "Jornada" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /Cuentas de la semana/ })).toHaveTextContent("232");
    await user.click(screen.getByRole("tab", { name: /Actividad de la zona/ }));
    expect(props.onCambiarPestana).toHaveBeenCalledWith("actividad");
  });

  it("muestra el contenido de la pestaña pedida", async () => {
    montar("cuentas");
    expect(await screen.findByRole("button", { name: /Sin nada/ })).toBeInTheDocument();
  });

  it("Equipo regresa al tablero", async () => {
    const user = userEvent.setup();
    const props = montar();
    await user.click(screen.getByRole("button", { name: /Equipo/ }));
    expect(props.onVolver).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Correrla y verla fallar**

Run: `npx vitest run src/modules/desempeno/components/ficha`
Expected: FAIL con `Failed to resolve import "../FichaScreen"`.

- [ ] **Step 3: Implementar**

`src/modules/desempeno/components/ficha/pestanas.ts`:

```ts
export const PESTANAS = ["jornada", "actividad", "cuentas"] as const;
export type PestanaFicha = (typeof PESTANAS)[number];

export function esPestana(v: unknown): v is PestanaFicha {
  return typeof v === "string" && (PESTANAS as readonly string[]).includes(v);
}
```

`src/modules/desempeno/components/ficha/FichaEncabezado.tsx`:

```tsx
import type { Periodo, UsuarioTablero } from "../../domain/entities";
import { diaDeNegocioDe, diasEntre, dinero, duracionCorta, fechaCorta, horaCorta, porcentaje } from "../lib/format";
import { PuntoEstado } from "../comunes/PuntoEstado";
import { SegmentoPeriodo } from "../comunes/SegmentoPeriodo";

const ESTADO = {
  activo: "Activo",
  sin_actividad: "Sin actividad",
  no_ha_empezado: "No ha empezado",
} as const;

function Cifra({ titulo, valor }: { titulo: string; valor: string }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-[14px] bg-dsm-tarjeta px-4 py-3.5 shadow-sm ring-1 ring-border">
      <span className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">{titulo}</span>
      <span className="font-serif text-2xl tracking-[-0.01em]">{valor}</span>
    </div>
  );
}

export function FichaEncabezado({ usuario: u, periodo, hoy }: { usuario: UsuarioTablero; periodo: Periodo; hoy: string }) {
  const inicioDia = u.inicioSemana ? diaDeNegocioDe(u.inicioSemana) : null;
  const haceDias = inicioDia ? diasEntre(inicioDia, hoy).length - 1 : null;
  return (
    <>
      <header className="flex flex-wrap items-end justify-between gap-6">
        <div>
          <div className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">{u.zonaNombre}</div>
          <h1 className="font-serif text-[52px] font-light leading-none tracking-[-0.03em]">{u.nombre}</h1>
          <div className="mt-3 flex flex-wrap items-center gap-2 text-[13px] text-muted-foreground">
            {inicioDia && <span>{`Semana desde ${fechaCorta(inicioDia)} · hace ${haceDias} d`}</span>}
            <span className="inline-flex items-center gap-1.5 rounded-full bg-dsm-tarjeta px-2.5 py-0.5 text-foreground ring-1 ring-border">
              <PuntoEstado estado={u.estadoAhora} />
              {ESTADO[u.estadoAhora]}
            </span>
            {u.estadoAhora === "sin_actividad" && u.estadoDesde && (
              <span className="font-mono text-[11px]">{`desde ${horaCorta(u.estadoDesde)}`}</span>
            )}
          </div>
        </div>
        <div className="flex flex-col items-end gap-2">
          <SegmentoPeriodo />
          <span className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
            {periodo === "hoy" ? "Cobrado hoy" : "Cobrado en la semana"}
          </span>
          <span className="font-serif text-[72px] leading-none tracking-[-0.03em]">{dinero(u.pagos.importe)}</span>
        </div>
      </header>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(130px,1fr))] gap-3">
        <Cifra titulo="Horas activas" valor={u.minutosActivos > 0 ? duracionCorta(u.minutosActivos) : "—"} />
        <Cifra titulo="Pausas" valor={`${u.pausas.n} · ${duracionCorta(u.pausas.minutos)}`} />
        <Cifra titulo="Pagos" valor={String(u.pagos.n)} />
        <Cifra titulo="Visitas" valor={String(u.visitas)} />
        <Cifra titulo="Condonaciones" valor={`${u.condonaciones.n} · ${dinero(u.condonaciones.importe)}`} />
        <Cifra titulo="Faltan" valor={`${u.coberturaPorSegmento.sinNada} de ${u.cobertura.total}`} />
        <Cifra titulo="% cobro semana" valor={porcentaje(u.cobro.pct)} />
      </div>
    </>
  );
}
```

(`hace N d` es la distancia en días de calendario entre el inicio de semana y hoy: rotulación, no una cifra del negocio.)

`src/modules/desempeno/components/ficha/FichaScreen.tsx`:

```tsx
import type { ReactNode } from "react";
import { ChevronLeft } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Actividad, Cuenta, Jornada, Periodo, UsuarioTablero } from "../../domain/entities";
import { AvisoPantallaAngosta } from "../comunes/Estados";
import { JornadaTab } from "../jornada/JornadaTab";
import { ActividadTab } from "../actividad/ActividadTab";
import { CuentasTab } from "../cuentas/CuentasTab";
import { FichaEncabezado } from "./FichaEncabezado";
import type { PestanaFicha } from "./pestanas";

export type FichaScreenProps = {
  usuario: UsuarioTablero;
  periodo: Periodo;
  generadoAt: string;
  opcionesRegistro: ReadonlyArray<{ valor: string; etiqueta: string }>;
  pestana: PestanaFicha;
  dia: string;
  hoy: string;
  onCambiarPestana: (p: PestanaFicha) => void;
  onCambiarDia: (dia: string) => void;
  onVolver: () => void;
  onAbrirActividad: (a: Actividad) => void;
  onAbrirCuenta: (c: Cuenta) => void;
  renderFotos?: (a: Actividad) => ReactNode;
  renderLinea?: (j: Jornada, ahora: string | null) => ReactNode;
  controlesLinea?: ReactNode;
};

export function FichaScreen(p: FichaScreenProps) {
  const pestanas: ReadonlyArray<readonly [PestanaFicha, string, string | null]> = [
    ["jornada", "Jornada", null],
    ["actividad", "Actividad de la zona", null],
    ["cuentas", "Cuentas de la semana", String(p.usuario.ventasUniverso)],
  ];
  return (
    <section className="flex min-h-full flex-col gap-6 bg-dsm-canvas px-10 pb-24 pt-9">
      <nav className="flex items-center gap-2 text-[13px]">
        <button type="button" onClick={p.onVolver} className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground">
          <ChevronLeft aria-hidden className="h-3.5 w-3.5" />
          Equipo
        </button>
        <span className="text-muted-foreground/60">/</span>
        <span>{p.usuario.nombre}</span>
      </nav>
      <AvisoPantallaAngosta />
      <FichaEncabezado usuario={p.usuario} periodo={p.periodo} hoy={p.hoy} />
      <div role="tablist" className="flex w-max max-w-full gap-1 overflow-x-auto rounded-full bg-dsm-tarjeta p-1 shadow-sm ring-1 ring-border">
        {pestanas.map(([valor, texto, n]) => (
          <button
            key={valor}
            type="button"
            role="tab"
            aria-selected={p.pestana === valor}
            onClick={() => p.onCambiarPestana(valor)}
            className={cn(
              "inline-flex h-[34px] items-center gap-2 rounded-full px-[18px] text-[13px]",
              p.pestana === valor ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {texto}
            {n && <span className="opacity-60">{n}</span>}
          </button>
        ))}
      </div>
      {p.pestana === "jornada" && (
        <JornadaTab
          usuario={p.usuario}
          dia={p.dia}
          hoy={p.hoy}
          ahora={p.generadoAt}
          onCambiarDia={p.onCambiarDia}
          onAbrirActividad={p.onAbrirActividad}
          renderFotos={p.renderFotos}
          renderLinea={p.renderLinea}
          controlesLinea={p.controlesLinea}
        />
      )}
      {p.pestana === "actividad" && (
        <ActividadTab
          usuario={p.usuario}
          opcionesRegistro={p.opcionesRegistro}
          onAbrirActividad={p.onAbrirActividad}
          renderFotos={p.renderFotos}
        />
      )}
      {p.pestana === "cuentas" && <CuentasTab usuario={p.usuario} onAbrirCuenta={p.onAbrirCuenta} />}
    </section>
  );
}
```

- [ ] **Step 4: Correr todo el carril, tipos y lint**

Run: `npx vitest run src/modules/desempeno && npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: PASS (todas las del módulo; la ficha suma 4), sin salida de tsc/eslint.

- [ ] **Step 5: Commit**

```bash
git add src/modules/desempeno/components/ficha
git commit -m "feat(desempeno): ficha del usuario con encabezado, cifras de su semana y pestañas"
```

---

## CARRIL 3 — modales, fotos y HTTP (rama `feat/desempeno-modales`, worktree `../sistema-cobro-web-modales`)

### Tarea 3.1 (Carril 3): `AuthenticatedImage` y visor generalizados con un descargador (regresión de Ventas Locales)

**Carril 3, rama `feat/desempeno-modales`, worktree propio.** Depende solo de que exista la rama base (no usa nada de Tarea 0).

**Files:**
- Create: `src/modules/ventasLocales/components/detalle/fuenteImagen.ts`
- Modify: `src/modules/ventasLocales/components/detalle/AuthenticatedImage.tsx` (props `ventaId/imagenId` → `fuente`)
- Modify: `src/modules/ventasLocales/components/detalle/VentaImagenLightbox.tsx` (prop `ventaId` → `fuentePara`; tipo `ImagenDeVisor`)
- Modify: `src/modules/ventasLocales/components/detalle/VentaImagenesGrid.tsx`
- Modify: `src/modules/ventasLocales/components/EditarVentaModal/piezas/ImagenTileEditable.tsx:43-45`
- Modify: `src/modules/ventasLocales/components/EditarVentaModal/tabs/ImagenesTab.tsx:118-120`
- Modify: `src/modules/ventasLocales/components/detalle/index.ts` (exportar `fuenteImagenVenta` y tipos)
- Modify: `src/modules/ventasLocales/components/detalle/VentaImagenLightbox.test.tsx`
- Test: `src/modules/ventasLocales/components/detalle/AuthenticatedImage.test.tsx`
- Test: `src/modules/ventasLocales/components/detalle/fuenteImagen.test.ts`

**Interfaces:**
- Consumes: `ventaV2Http` (`@/services/api/ventaV2Types`).
- Produces (los usan las Tareas 3.2 y 3.3):
  - `type FuenteImagen = { readonly cacheKey: string; readonly descargar: () => Promise<Blob> }`, `fuenteImagenVenta(ventaId, imagenId): FuenteImagen` (en `fuenteImagen.ts`).
  - `AuthenticatedImage({ fuente: FuenteImagen; alt?; className?; onLoad? })`; error con `role="img"` y `aria-label="No se pudo cargar la imagen"`.
  - `type ImagenDeVisor = { id: string; descripcion: string | null; created_at: string }`; `VentaImagenLightbox({ imagenes: readonly ImagenDeVisor[]; fuentePara: (imagenId: string) => FuenteImagen; initialIndex: number; onClose: () => void })`.
  - `VentaImagenesGrid({ ventaId, imagenes })` **sin cambio de firma**.

- [ ] **Step 1: Medir la línea base de lint de los archivos que se tocan**

Run: `npx eslint src/modules/ventasLocales/components/detalle/AuthenticatedImage.tsx src/modules/ventasLocales/components/detalle/VentaImagenLightbox.tsx src/modules/ventasLocales/components/detalle/VentaImagenesGrid.tsx src/modules/ventasLocales/components/EditarVentaModal/piezas/ImagenTileEditable.tsx src/modules/ventasLocales/components/EditarVentaModal/tabs/ImagenesTab.tsx 2>&1 | tail -3`
Expected: anota el total de problemas ("✖ N problems" o nada). Al final de la tarea no debe haber más.

- [ ] **Step 2: Escribir las pruebas nuevas (fallan)**

`src/modules/ventasLocales/components/detalle/AuthenticatedImage.test.tsx`:

```tsx
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import AuthenticatedImage from "./AuthenticatedImage";

// jsdom no trae URL.createObjectURL/revokeObjectURL.
beforeEach(() => {
  URL.createObjectURL = vi.fn(() => "blob:foto");
  URL.revokeObjectURL = vi.fn();
});
afterEach(() => vi.restoreAllMocks());

describe("AuthenticatedImage con descargador", () => {
  it("dos consumidores de la misma clave comparten UNA descarga", async () => {
    const descargar = vi.fn(async () => new Blob(["x"], { type: "image/jpeg" }));
    const fuente = { cacheKey: "prueba/compartida", descargar };
    render(
      <>
        <AuthenticatedImage fuente={fuente} alt="miniatura" />
        <AuthenticatedImage fuente={fuente} alt="visor" />
      </>,
    );
    expect(await screen.findByAltText("miniatura")).toHaveAttribute("src", "blob:foto");
    expect(await screen.findByAltText("visor")).toBeInTheDocument();
    expect(descargar).toHaveBeenCalledTimes(1);
  });

  it("al desmontar al último consumidor libera el blob", async () => {
    const fuente = { cacheKey: "prueba/liberar", descargar: async () => new Blob(["x"]) };
    const { unmount } = render(<AuthenticatedImage fuente={fuente} alt="foto" />);
    await screen.findByAltText("foto");
    unmount();
    await waitFor(() => expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:foto"));
  });

  it("si la descarga falla lo dice", async () => {
    const fuente = { cacheKey: "prueba/falla", descargar: async () => Promise.reject(new Error("404")) };
    render(<AuthenticatedImage fuente={fuente} alt="foto" />);
    expect(await screen.findByRole("img", { name: "No se pudo cargar la imagen" })).toBeInTheDocument();
  });
});
```

`src/modules/ventasLocales/components/detalle/fuenteImagen.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { ventaV2Http } from "@/services/api/ventaV2Types";
import { fuenteImagenVenta } from "./fuenteImagen";

describe("fuenteImagenVenta (Ventas Locales sigue igual)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("pide /ventas/{venta}/imagenes/{imagen} como blob, con la misma clave de caché por venta", async () => {
    const blob = new Blob(["jpg"]);
    const get = vi.spyOn(ventaV2Http, "get").mockResolvedValue({ data: blob });
    const fuente = fuenteImagenVenta("v-1", "img-9");
    expect(fuente.cacheKey).toBe("venta/v-1/img-9");
    await expect(fuente.descargar()).resolves.toBe(blob);
    expect(get).toHaveBeenCalledWith("/ventas/v-1/imagenes/img-9", { responseType: "blob" });
  });
});
```

En `src/modules/ventasLocales/components/detalle/VentaImagenLightbox.test.tsx`, reemplazar el bloque `renderLightbox` para usar la firma nueva:

```tsx
const renderLightbox = (onClose = vi.fn()) => {
  const utils = render(
    <VentaImagenLightbox
      imagenes={imagenes}
      fuentePara={(id) => ({ cacheKey: `venta/v1/${id}`, descargar: async () => new Blob() })}
      initialIndex={0}
      onClose={onClose}
    />,
  );
  const root = utils.container.querySelector("[data-lightbox]") as HTMLElement;
  return { ...utils, root, onClose };
};
```

- [ ] **Step 3: Correr y ver fallar**

Run: `npx vitest run src/modules/ventasLocales/components/detalle`
Expected: FAIL (`Failed to resolve import "./fuenteImagen"` y errores de props en el lightbox).

- [ ] **Step 4: Implementar**

`src/modules/ventasLocales/components/detalle/fuenteImagen.ts`:

```ts
import { ventaV2Http } from "@/services/api/ventaV2Types";

// FuenteImagen: de dónde se descarga una imagen protegida y con qué clave se
// comparte en caché. Ventas Locales, Clientes (fotos de pago) y Desempeño
// (fotos de pago y de visita) pasan su propio descargador.
export type FuenteImagen = {
  readonly cacheKey: string;
  readonly descargar: () => Promise<Blob>;
};

export function fuenteImagenVenta(ventaId: string, imagenId: string): FuenteImagen {
  return {
    cacheKey: `venta/${ventaId}/${imagenId}`,
    descargar: async () => {
      const res = await ventaV2Http.get<Blob>(`/ventas/${ventaId}/imagenes/${imagenId}`, {
        responseType: "blob",
      });
      return res.data;
    },
  };
}
```

`src/modules/ventasLocales/components/detalle/AuthenticatedImage.tsx` (archivo completo):

```tsx
import { useEffect, useRef, useState } from "react";
import { ImageOff, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { FuenteImagen } from "./fuenteImagen";

interface Props {
  fuente: FuenteImagen;
  alt?: string;
  className?: string;
  onLoad?: (el: HTMLImageElement) => void;
}

// Ref-counted blob cache. Multiple consumers (thumbnail + lightbox) of the
// same image share one fetch and one object URL. When the last consumer
// unmounts, the URL is revoked so we don't leak memory across long sessions.
// The key now comes from the caller's FuenteImagen (venta/…, pago/…, visita/…).
interface Entry {
  promise: Promise<string>;
  refs: number;
}
const cache = new Map<string, Entry>();

// Concurrency throttle so a 20-image grid doesn't open 20 parallel XHRs.
const MAX_INFLIGHT = 4;
let inFlight = 0;
const waiting: Array<() => void> = [];

const acquireSlot = (): Promise<void> =>
  new Promise((resolve) => {
    if (inFlight < MAX_INFLIGHT) {
      inFlight++;
      resolve();
    } else {
      waiting.push(() => {
        inFlight++;
        resolve();
      });
    }
  });

const releaseSlot = () => {
  inFlight--;
  const next = waiting.shift();
  if (next) next();
};

const fetchImage = async (descargar: () => Promise<Blob>): Promise<string> => {
  await acquireSlot();
  try {
    return URL.createObjectURL(await descargar());
  } finally {
    releaseSlot();
  }
};

const acquire = (key: string, descargar: () => Promise<Blob>): Promise<string> => {
  let entry = cache.get(key);
  if (!entry) {
    entry = { promise: fetchImage(descargar), refs: 0 };
    cache.set(key, entry);
  }
  entry.refs++;
  return entry.promise;
};

const release = (key: string) => {
  const entry = cache.get(key);
  if (!entry) return;
  entry.refs--;
  if (entry.refs <= 0) {
    cache.delete(key);
    entry.promise
      .then((url) => URL.revokeObjectURL(url))
      .catch(() => {});
  }
};

export const AuthenticatedImage = ({ fuente, alt, className, onLoad }: Props) => {
  const key = fuente.cacheKey;
  // El descargador puede ser una función nueva en cada render; la clave manda.
  const fuenteRef = useRef(fuente);
  fuenteRef.current = fuente;
  const [url, setUrl] = useState<string | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setUrl(null);
    setError(false);
    acquire(key, fuenteRef.current.descargar)
      .then((resolved) => {
        if (!cancelled) setUrl(resolved);
      })
      .catch(() => {
        if (!cancelled) setError(true);
      });
    return () => {
      cancelled = true;
      release(key);
    };
  }, [key]);

  if (error) {
    return (
      <div
        role="img"
        aria-label="No se pudo cargar la imagen"
        className={cn("flex items-center justify-center bg-muted text-muted-foreground", className)}
      >
        <ImageOff className="h-5 w-5" />
      </div>
    );
  }

  if (!url) {
    return (
      <div aria-label="Cargando imagen" className={cn("flex items-center justify-center bg-muted", className)}>
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <img
      src={url}
      alt={alt ?? "imagen"}
      className={className}
      onLoad={(e) => onLoad?.(e.currentTarget)}
      draggable={false}
    />
  );
};

export default AuthenticatedImage;
```

En `src/modules/ventasLocales/components/detalle/VentaImagenLightbox.tsx`:

1. Reemplazar `import { ImagenV2 } from "@/services/api/ventaV2Types";` por:

```tsx
import type { FuenteImagen } from "./fuenteImagen";

// Lo mínimo que el visor necesita de una imagen. ImagenV2 (ventas) lo cumple
// tal cual; Clientes y Desempeño mapean sus imágenes a esta forma.
export type ImagenDeVisor = { id: string; descripcion: string | null; created_at: string };
```

2. Reemplazar la interfaz `Props` y la firma:

```tsx
interface Props {
  imagenes: readonly ImagenDeVisor[];
  fuentePara: (imagenId: string) => FuenteImagen;
  initialIndex: number;
  onClose: () => void;
}
```

```tsx
export const VentaImagenLightbox = ({ imagenes, fuentePara, initialIndex, onClose }: Props) => {
```

3. En el `<AuthenticatedImage ...>` del final, reemplazar `ventaId={ventaId}` + `imagenId={current.id}` por `fuente={fuentePara(current.id)}`.

En `src/modules/ventasLocales/components/detalle/VentaImagenesGrid.tsx`:
- Agregar `import { fuenteImagenVenta } from "./fuenteImagen";`
- En la miniatura, reemplazar `ventaId={ventaId}` + `imagenId={img.id}` por `fuente={fuenteImagenVenta(ventaId, img.id)}`.
- En `<VentaImagenLightbox ...>`, reemplazar `ventaId={ventaId}` por `fuentePara={(id) => fuenteImagenVenta(ventaId, id)}`.

En `src/modules/ventasLocales/components/EditarVentaModal/piezas/ImagenTileEditable.tsx` (líneas 43–45): agregar `import { fuenteImagenVenta } from "../../detalle/fuenteImagen";` y reemplazar `ventaId={ventaId}` + `imagenId={imagen.id}` por `fuente={fuenteImagenVenta(ventaId, imagen.id)}`.

En `src/modules/ventasLocales/components/EditarVentaModal/tabs/ImagenesTab.tsx` (líneas 118–120): agregar `import { fuenteImagenVenta } from "../../detalle/fuenteImagen";` y en `<VentaImagenLightbox ...>` reemplazar `ventaId={ventaId}` por `fuentePara={(id) => fuenteImagenVenta(ventaId, id)}`.

En `src/modules/ventasLocales/components/detalle/index.ts` agregar al final:

```ts
export { fuenteImagenVenta } from "./fuenteImagen";
export type { FuenteImagen } from "./fuenteImagen";
export type { ImagenDeVisor } from "./VentaImagenLightbox";
```

- [ ] **Step 5: Correr la regresión completa de Ventas Locales**

Run: `npx vitest run src/modules/ventasLocales`
Expected: PASS — todas las pruebas que pasaban antes (incluidas `VentaImagenesGrid.test.tsx`, `VentaImagenLightbox.test.tsx`, `VentaDetalleModal.test.tsx` y las de `EditarVentaModal`) más las 4 nuevas.

- [ ] **Step 6: Tipos y lint sin problemas nuevos**

Run: `npx tsc --noEmit && npx eslint src/modules/ventasLocales/components/detalle/fuenteImagen.ts src/modules/ventasLocales/components/detalle/AuthenticatedImage.tsx src/modules/ventasLocales/components/detalle/VentaImagenLightbox.tsx src/modules/ventasLocales/components/detalle/VentaImagenesGrid.tsx src/modules/ventasLocales/components/EditarVentaModal/piezas/ImagenTileEditable.tsx src/modules/ventasLocales/components/EditarVentaModal/tabs/ImagenesTab.tsx 2>&1 | tail -3`
Expected: `tsc` sin salida; eslint con igual o menos problemas que en el Step 1.

- [ ] **Step 7: Commit**

```bash
git add src/modules/ventasLocales/components/detalle/fuenteImagen.ts src/modules/ventasLocales/components/detalle/fuenteImagen.test.ts src/modules/ventasLocales/components/detalle/AuthenticatedImage.tsx src/modules/ventasLocales/components/detalle/AuthenticatedImage.test.tsx src/modules/ventasLocales/components/detalle/VentaImagenLightbox.tsx src/modules/ventasLocales/components/detalle/VentaImagenLightbox.test.tsx src/modules/ventasLocales/components/detalle/VentaImagenesGrid.tsx src/modules/ventasLocales/components/detalle/index.ts src/modules/ventasLocales/components/EditarVentaModal/piezas/ImagenTileEditable.tsx src/modules/ventasLocales/components/EditarVentaModal/tabs/ImagenesTab.tsx
git commit -m "refactor(ventas-locales): la imagen protegida y el visor reciben un descargador para reutilizarse"
```

---

### Tarea 3.2 (Carril 3): Clientes — fotos en `PagoModal` e índice público del módulo

**Carril 3, rama `feat/desempeno-modales`, después de la Tarea 3.1.**

**Files:**
- Modify: `src/modules/clientes/infrastructure/http/dtos.ts` (`PagoDetalleDTO` + `ImagenPagoDTO`)
- Modify: `src/modules/clientes/domain/entities/PagoDetalle.ts`
- Create: `src/modules/clientes/domain/entities/ImagenPago.ts`
- Modify: `src/modules/clientes/domain/entities/index.ts`
- Modify: `src/modules/clientes/infrastructure/mappers/dtoToPagoDetalle.ts`
- Create: `src/modules/clientes/infrastructure/mappers/dtoToImagenPago.ts`
- Modify: `src/modules/clientes/application/ports/ClientesPort.ts`
- Modify: `src/modules/clientes/infrastructure/http/HttpClientesAdapter.ts`
- Modify: `src/modules/clientes/application/__tests__/fakeClientesPort.ts`
- Create: `src/modules/clientes/presentation/hooks/useImagenesPago.ts`
- Create: `src/modules/clientes/components/detalle/PagoFotos.tsx`
- Modify: `src/modules/clientes/components/detalle/PagoModal.tsx` (sección "Comprobante" → `PagoFotos`; botones opcionales de pie)
- Create: `src/modules/clientes/index.ts`
- Test: `src/modules/clientes/infrastructure/mappers/__tests__/dtoToPagoDetalle.fotos.test.ts`
- Test: `src/modules/clientes/infrastructure/http/HttpClientesAdapter.imagenes.test.ts`
- Test: `src/modules/clientes/components/detalle/PagoModal.fotos.test.tsx`
- Test: `src/modules/clientes/index.test.ts`

**Interfaces:**
- Consumes: Tarea 3.1 (`AuthenticatedImage`, `VentaImagenLightbox`, `FuenteImagen`, `ImagenDeVisor`).
- Produces:
  - `PagoDetalle` gana `pagoRecibidoId: string | null` y `numFotos: number`.
  - `ImagenPago = { id; mime; sizeBytes; descripcion: string | null; createdAt; updatedAt }`.
  - `ClientesPort.listarImagenesPago(pagoRecibidoId, signal?)`, `ClientesPort.descargarImagenPago(pagoRecibidoId, imagenId, signal?)`.
  - `PagoModal({ clienteId, doctoCcId, onClose, onAbrirVenta?: (doctoPvId: number) => void, onAbrirCliente?: () => void })`.
  - `src/modules/clientes/index.ts`: `ClienteFicha`, `VentaModal`, `PagoModal`, `ClientesContainer`, `ClientesProvider`.

- [ ] **Step 1: Línea base de lint de archivos existentes**

Run: `npx eslint src/modules/clientes/components/detalle/PagoModal.tsx src/modules/clientes/infrastructure/http/HttpClientesAdapter.ts src/modules/clientes/infrastructure/mappers/dtoToPagoDetalle.ts src/modules/clientes/application/__tests__/fakeClientesPort.ts 2>&1 | tail -3`
Expected: anota el total.

- [ ] **Step 2: Pruebas (fallan)**

`src/modules/clientes/infrastructure/mappers/__tests__/dtoToPagoDetalle.fotos.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { dtoToPagoDetalle } from "../dtoToPagoDetalle";
import type { PagoDetalleDTO } from "../../http/dtos";

const base: PagoDetalleDTO = {
  importe: "150.00",
  iva: "0.00",
  fecha: "2026-10-14T14:20:00Z",
  forma_cobro_id: 157,
  forma_cobro: "EFECTIVO",
  referencia: "",
  cobrador_id: 11502,
  cobrador: "JUAN CARLOS MENDEZ",
  concepto_cc_id: 87327,
  concepto: "ABONO",
  categoria: "pago",
  es_ingreso: true,
  folio: "AB-15082369",
  aplica_a_cargo_id: 13785217,
  docto_pv_id: 9921,
  cancelado: false,
  aplicado: true,
  origen: "app",
};

describe("dtoToPagoDetalle — fotos del pago (contrato §6)", () => {
  it("mapea pago_recibido_id y num_fotos", () => {
    const d = dtoToPagoDetalle({ ...base, pago_recibido_id: "7a1f39e4-2b6c-4d1e-9f0a-3c5b8e2d1a47", num_fotos: 2 });
    expect(d.pagoRecibidoId).toBe("7a1f39e4-2b6c-4d1e-9f0a-3c5b8e2d1a47");
    expect(d.numFotos).toBe(2);
  });

  it("API anterior sin los campos, o pago de Microsip: sin comprobante, no truena", () => {
    const d = dtoToPagoDetalle(base);
    expect(d.pagoRecibidoId).toBeNull();
    expect(d.numFotos).toBe(0);
    expect(dtoToPagoDetalle({ ...base, pago_recibido_id: null, num_fotos: 0 }).pagoRecibidoId).toBeNull();
  });
});
```

`src/modules/clientes/infrastructure/http/HttpClientesAdapter.imagenes.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import type { AxiosInstance } from "axios";
import { HttpClientesAdapter } from "./HttpClientesAdapter";

describe("HttpClientesAdapter — fotos de pago", () => {
  it("lista /cobranza/pagos/{id}/imagenes y mapea", async () => {
    const get = vi.fn().mockResolvedValue({
      data: [{ id: "img-1", mime: "image/jpeg", size_bytes: 1024, created_at: "2026-10-14T14:20:10Z", updated_at: "2026-10-14T14:20:10Z" }],
    });
    const adapter = new HttpClientesAdapter({ get } as unknown as AxiosInstance);
    const imgs = await adapter.listarImagenesPago("p-1");
    expect(get).toHaveBeenCalledWith("/cobranza/pagos/p-1/imagenes", { signal: undefined });
    expect(imgs).toEqual([
      { id: "img-1", mime: "image/jpeg", sizeBytes: 1024, descripcion: null, createdAt: "2026-10-14T14:20:10Z", updatedAt: "2026-10-14T14:20:10Z" },
    ]);
  });

  it("descarga una foto como blob", async () => {
    const blob = new Blob(["jpg"]);
    const get = vi.fn().mockResolvedValue({ data: blob });
    const adapter = new HttpClientesAdapter({ get } as unknown as AxiosInstance);
    await expect(adapter.descargarImagenPago("p-1", "img-1")).resolves.toBe(blob);
    expect(get).toHaveBeenCalledWith("/cobranza/pagos/p-1/imagenes/img-1", { responseType: "blob", signal: undefined });
  });
});
```

`src/modules/clientes/components/detalle/PagoModal.fotos.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ClientesProvider } from "../../presentation/context/ClientesContext";
import { FakeClientesPort, makeFakeImagenPago, makeFakePagoDetalle } from "../../application/__tests__/fakeClientesPort";
import { PagoModal } from "./PagoModal";

vi.mock("../ficha/FichaMapEmbed", () => ({ FichaMapEmbed: () => <div data-testid="mock-map" /> }));
vi.mock("@/modules/ventasLocales/components/detalle/AuthenticatedImage", () => ({
  default: ({ alt }: { alt?: string }) => <img alt={alt ?? "imagen"} />,
}));

function montar(port: FakeClientesPort, extra: { onAbrirVenta?: (id: number) => void; onAbrirCliente?: () => void } = {}) {
  return render(
    <ClientesProvider port={port}>
      <PagoModal clienteId={1544855} doctoCcId={15082369} onClose={vi.fn()} {...extra} />
    </ClientesProvider>,
  );
}

describe("PagoModal — fotos", () => {
  it("sin pago_recibido_id: 'Sin comprobante' y no pide fotos", async () => {
    const port = new FakeClientesPort();
    port.obtenerPagoDetalleResponse = makeFakePagoDetalle({ pagoRecibidoId: null, numFotos: 0 });
    montar(port);
    expect(await screen.findByText("Sin comprobante")).toBeInTheDocument();
    expect(port.listarImagenesPagoCalls).toHaveLength(0);
  });

  it("con pago_recibido_id: pide la lista y muestra las miniaturas", async () => {
    const port = new FakeClientesPort();
    port.obtenerPagoDetalleResponse = makeFakePagoDetalle({ pagoRecibidoId: "p-1", numFotos: 2 });
    port.imagenesPagoResponse = [makeFakeImagenPago({ id: "a", descripcion: "Recibo firmado" }), makeFakeImagenPago({ id: "b", descripcion: null })];
    montar(port);
    expect(await screen.findByText("Fotos · 2")).toBeInTheDocument();
    expect(screen.getByAltText("Recibo firmado")).toBeInTheDocument();
    expect(port.listarImagenesPagoCalls[0].pagoRecibidoId).toBe("p-1");
  });

  it("los botones de pie solo aparecen si quien abre el modal los pide", async () => {
    const user = userEvent.setup();
    const onAbrirVenta = vi.fn();
    const port = new FakeClientesPort();
    port.obtenerPagoDetalleResponse = makeFakePagoDetalle({ doctoPvId: 30015 });
    montar(port, { onAbrirVenta, onAbrirCliente: vi.fn() });
    await user.click(await screen.findByRole("button", { name: "Detalle de venta" }));
    expect(onAbrirVenta).toHaveBeenCalledWith(30015);
    expect(screen.getByRole("button", { name: "Ficha del cliente" })).toBeInTheDocument();
  });
});
```

`src/modules/clientes/index.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import * as clientes from "./index";

describe("índice público de Clientes", () => {
  it("expone lo que reutiliza Desempeño", () => {
    expect(typeof clientes.ClienteFicha).toBe("function");
    expect(typeof clientes.VentaModal).toBe("function");
    expect(typeof clientes.PagoModal).toBe("function");
    expect(typeof clientes.ClientesContainer).toBe("function");
    expect(typeof clientes.ClientesProvider).toBe("function");
  });
});
```

- [ ] **Step 3: Correr y ver fallar**

Run: `npx vitest run src/modules/clientes/infrastructure/mappers/__tests__/dtoToPagoDetalle.fotos.test.ts src/modules/clientes/infrastructure/http/HttpClientesAdapter.imagenes.test.ts src/modules/clientes/components/detalle/PagoModal.fotos.test.tsx src/modules/clientes/index.test.ts`
Expected: FAIL (`pagoRecibidoId` undefined, `listarImagenesPago is not a function`, no existe `./index`).

- [ ] **Step 4: Implementar**

En `src/modules/clientes/infrastructure/http/dtos.ts`, dentro de `PagoDetalleDTO` después de `origen: string; // "app" | "microsip"`, agregar:

```ts
  // Contrato desempeño §6. Opcionales: un API anterior no los manda.
  pago_recibido_id?: string | null;
  num_fotos?: number;
```

y al final del archivo:

```ts
// ImagenPagoDTO matches GET /v2/cobranza/pagos/{id}/imagenes (array).
export type ImagenPagoDTO = {
  id: string;
  mime: string;
  size_bytes: number;
  descripcion?: string | null;
  created_at: string;
  updated_at: string;
};
```

En `src/modules/clientes/domain/entities/PagoDetalle.ts`, después de `readonly origen: "app" | "microsip";`:

```ts
  // UUID de MSP_PAGOS_RECIBIDOS: null en pagos de Microsip o anteriores a 2.18.0.
  readonly pagoRecibidoId: string | null;
  readonly numFotos: number;
```

`src/modules/clientes/domain/entities/ImagenPago.ts`:

```ts
// ImagenPago: foto o comprobante adjunto a un pago de la app.
export type ImagenPago = {
  readonly id: string;
  readonly mime: string;
  readonly sizeBytes: number;
  readonly descripcion: string | null;
  readonly createdAt: string;
  readonly updatedAt: string;
};
```

En `src/modules/clientes/domain/entities/index.ts` agregar: `export type { ImagenPago } from "./ImagenPago";`

En `src/modules/clientes/infrastructure/mappers/dtoToPagoDetalle.ts`, en el objeto que regresa, después de `origen,`:

```ts
    pagoRecibidoId: dto.pago_recibido_id ?? null,
    numFotos: typeof dto.num_fotos === "number" ? dto.num_fotos : 0,
```

`src/modules/clientes/infrastructure/mappers/dtoToImagenPago.ts`:

```ts
import type { ImagenPago } from "../../domain/entities";
import type { ImagenPagoDTO } from "../http/dtos";

export function dtoToImagenPago(dto: ImagenPagoDTO): ImagenPago {
  return {
    id: dto.id,
    mime: dto.mime,
    sizeBytes: dto.size_bytes,
    descripcion: dto.descripcion ?? null,
    createdAt: dto.created_at,
    updatedAt: dto.updated_at,
  };
}
```

En `src/modules/clientes/application/ports/ClientesPort.ts`: agregar `ImagenPago` al import de entidades y, al final de la interfaz:

```ts
  // Fotos de un pago de la app (MSP_PAGOS_RECIBIDOS), por su UUID.
  listarImagenesPago(pagoRecibidoId: string, signal?: AbortSignal): Promise<ImagenPago[]>;
  descargarImagenPago(pagoRecibidoId: string, imagenId: string, signal?: AbortSignal): Promise<Blob>;
```

En `src/modules/clientes/infrastructure/http/HttpClientesAdapter.ts`: agregar `ImagenPago` al import de entidades, `ImagenPagoDTO` al import de `./dtos`, `import { dtoToImagenPago } from "../mappers/dtoToImagenPago";`, y estos métodos dentro de la clase:

```ts
  async listarImagenesPago(pagoRecibidoId: string, signal?: AbortSignal): Promise<ImagenPago[]> {
    try {
      const { data } = await this.client.get<ImagenPagoDTO[]>(
        `/cobranza/pagos/${encodeURIComponent(pagoRecibidoId)}/imagenes`,
        { signal },
      );
      return data.map(dtoToImagenPago);
    } catch (e) {
      throw apperrorToDomainError(e);
    }
  }

  async descargarImagenPago(pagoRecibidoId: string, imagenId: string, signal?: AbortSignal): Promise<Blob> {
    try {
      const { data } = await this.client.get<Blob>(
        `/cobranza/pagos/${encodeURIComponent(pagoRecibidoId)}/imagenes/${encodeURIComponent(imagenId)}`,
        { responseType: "blob", signal },
      );
      return data;
    } catch (e) {
      throw apperrorToDomainError(e);
    }
  }
```

En `src/modules/clientes/application/__tests__/fakeClientesPort.ts`:
- agregar `ImagenPago` al import de entidades;
- en `makeFakePagoDetalle`, dentro de `base` después de `origen: "app",`: `pagoRecibidoId: null,` y `numFotos: 0,`;
- en la clase, junto a los demás registros y respuestas:

```ts
  listarImagenesPagoCalls: Array<{ pagoRecibidoId: string; signal?: AbortSignal }> = [];
  imagenesPagoResponse: ImagenPago[] | (() => ImagenPago[]) = [];

  async listarImagenesPago(pagoRecibidoId: string, signal?: AbortSignal): Promise<ImagenPago[]> {
    this.listarImagenesPagoCalls.push({ pagoRecibidoId, signal });
    const e = this.takeThrow("listarImagenesPago");
    if (e) throw e;
    return resolve(this.imagenesPagoResponse);
  }

  async descargarImagenPago(): Promise<Blob> {
    const e = this.takeThrow("descargarImagenPago");
    if (e) throw e;
    return new Blob(["jpg"], { type: "image/jpeg" });
  }
```

- y al final del archivo:

```ts
export function makeFakeImagenPago(overrides: Partial<ImagenPago> = {}): ImagenPago {
  return {
    id: "0b6f1c2e-5a7d-4e3b-8c9f-1d2e3f4a5b6c",
    mime: "image/jpeg",
    sizeBytes: 184320,
    descripcion: "Recibo firmado",
    createdAt: "2026-10-14T14:20:10Z",
    updatedAt: "2026-10-14T14:20:10Z",
    ...overrides,
  };
}
```

`src/modules/clientes/presentation/hooks/useImagenesPago.ts`:

```ts
import { useEffect, useState } from "react";
import type { ImagenPago } from "../../domain/entities";
import { DomainError } from "../../domain/errors";
import { useClientesPort } from "../context/ClientesContext";
import { toDomainError } from "./lib/toDomainError";

// Fotos de un pago; con pagoRecibidoId null no hace nada ("Sin comprobante").
export function useImagenesPago(pagoRecibidoId: string | null) {
  const port = useClientesPort();
  const [imagenes, setImagenes] = useState<ImagenPago[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<DomainError | null>(null);

  useEffect(() => {
    if (pagoRecibidoId === null) {
      setImagenes([]);
      setIsLoading(false);
      setError(null);
      return undefined;
    }
    const ctrl = new AbortController();
    setIsLoading(true);
    setError(null);
    port
      .listarImagenesPago(pagoRecibidoId, ctrl.signal)
      .then((r) => {
        if (!ctrl.signal.aborted) setImagenes(r);
      })
      .catch((e: unknown) => {
        if (!ctrl.signal.aborted) setError(toDomainError(e));
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setIsLoading(false);
      });
    return () => ctrl.abort();
  }, [port, pagoRecibidoId]);

  return { imagenes, isLoading, error };
}
```

`src/modules/clientes/components/detalle/PagoFotos.tsx`:

```tsx
import { useState } from "react";
import { createPortal } from "react-dom";
import { Skeleton } from "@/components/ui/skeleton";
import AuthenticatedImage from "@/modules/ventasLocales/components/detalle/AuthenticatedImage";
import VentaImagenLightbox from "@/modules/ventasLocales/components/detalle/VentaImagenLightbox";
import type { FuenteImagen } from "@/modules/ventasLocales/components/detalle/fuenteImagen";
import { useClientesPort } from "../../presentation/context/ClientesContext";
import { useImagenesPago } from "../../presentation/hooks/useImagenesPago";

const TITULO = "mb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground";

// Sección de fotos del pago (spec §3.3: reemplaza "Sin comprobante" cuando el
// pago tiene fila en MSP_PAGOS_RECIBIDOS).
export function PagoFotos({ pagoRecibidoId }: { pagoRecibidoId: string | null }) {
  const port = useClientesPort();
  const { imagenes, isLoading, error } = useImagenesPago(pagoRecibidoId);
  const [abierta, setAbierta] = useState<number | null>(null);

  if (pagoRecibidoId === null) {
    return (
      <section>
        <h4 className={TITULO}>Comprobante</h4>
        <p className="font-mono text-[11px] italic text-muted-foreground">Sin comprobante</p>
      </section>
    );
  }

  const fuentePara = (imagenId: string): FuenteImagen => ({
    cacheKey: `pago/${pagoRecibidoId}/${imagenId}`,
    descargar: () => port.descargarImagenPago(pagoRecibidoId, imagenId),
  });

  return (
    <section>
      <h4 className={TITULO}>{imagenes.length > 0 ? `Fotos · ${imagenes.length}` : "Fotos"}</h4>
      {isLoading && <Skeleton className="h-24 w-full" />}
      {error && <p className="text-[12px] text-muted-foreground">No se pudieron cargar las fotos</p>}
      {!isLoading && !error && imagenes.length === 0 && (
        <p className="font-mono text-[11px] italic text-muted-foreground">No se tomaron fotos</p>
      )}
      <div className="grid grid-cols-3 gap-2">
        {imagenes.map((img, i) => (
          <button
            key={img.id}
            type="button"
            onClick={() => setAbierta(i)}
            className="aspect-square overflow-hidden rounded-md border border-border/60 bg-muted"
          >
            <AuthenticatedImage fuente={fuentePara(img.id)} alt={img.descripcion ?? `Foto ${i + 1}`} className="h-full w-full object-cover" />
          </button>
        ))}
      </div>
      {abierta !== null &&
        createPortal(
          <VentaImagenLightbox
            imagenes={imagenes.map((img) => ({ id: img.id, descripcion: img.descripcion, created_at: img.createdAt }))}
            fuentePara={fuentePara}
            initialIndex={abierta}
            onClose={() => setAbierta(null)}
          />,
          document.body,
        )}
    </section>
  );
}
```

En `src/modules/clientes/components/detalle/PagoModal.tsx`:

1. Agregar `import { PagoFotos } from "./PagoFotos";`.
2. Cambiar `Props` y la firma:

```tsx
interface Props {
  clienteId: number;
  doctoCcId: number | null;
  onClose: () => void;
  // Desempeño los pasa para encadenar modales; Clientes no.
  onAbrirVenta?: (doctoPvId: number) => void;
  onAbrirCliente?: () => void;
}

export function PagoModal({ clienteId, doctoCcId, onClose, onAbrirVenta, onAbrirCliente }: Props) {
```

3. Reemplazar toda la sección `{/* ── Comprobante ── */}` (el `<section>` con "Sin comprobante") por:

```tsx
              {/* ── Fotos / comprobante ── */}
              <PagoFotos pagoRecibidoId={detalle.pagoRecibidoId} />

              {(onAbrirVenta || onAbrirCliente) && (
                <footer className="flex justify-end gap-2 border-t border-border/60 pt-4">
                  {onAbrirCliente && (
                    <Button variant="outline" size="sm" onClick={onAbrirCliente}>
                      Ficha del cliente
                    </Button>
                  )}
                  {onAbrirVenta && (
                    <Button size="sm" onClick={() => onAbrirVenta(detalle.doctoPvId)}>
                      Detalle de venta
                    </Button>
                  )}
                </footer>
              )}
```

`src/modules/clientes/index.ts`:

```ts
// Índice público de Clientes: lo que otros módulos (Desempeño) pueden usar sin
// entrar a sus capas internas. Los componentes necesitan el contexto del
// módulo: envolver con ClientesContainer (producción) o ClientesProvider (pruebas).
export { ClienteFicha } from "./components/ficha/ClienteFicha";
export { VentaModal } from "./components/detalle/VentaModal";
export { PagoModal } from "./components/detalle/PagoModal";
export { ClientesContainer } from "./presentation/composition/ClientesContainer";
export { ClientesProvider } from "./presentation/context/ClientesContext";
```

- [ ] **Step 5: Regresión completa de Clientes**

Run: `npx vitest run src/modules/clientes`
Expected: PASS — las pruebas que ya pasaban (incluida `PagoModal.test.tsx`, que sigue viendo "Sin comprobante" porque `makeFakePagoDetalle` trae `pagoRecibidoId: null`) más las 8 nuevas.

- [ ] **Step 6: Tipos y lint**

Run: `npx tsc --noEmit && npx eslint src/modules/clientes/index.ts src/modules/clientes/components/detalle/PagoFotos.tsx src/modules/clientes/presentation/hooks/useImagenesPago.ts src/modules/clientes/infrastructure/mappers/dtoToImagenPago.ts src/modules/clientes/domain/entities/ImagenPago.ts --max-warnings 0 && npx eslint src/modules/clientes/components/detalle/PagoModal.tsx src/modules/clientes/infrastructure/http/HttpClientesAdapter.ts src/modules/clientes/infrastructure/mappers/dtoToPagoDetalle.ts src/modules/clientes/application/__tests__/fakeClientesPort.ts 2>&1 | tail -3`
Expected: `tsc` sin salida (si marca otra implementación de `ClientesPort`, agrégale los dos métodos); archivos nuevos sin problemas; existentes con igual o menos problemas que en el Step 1.

- [ ] **Step 7: Commit**

```bash
git add src/modules/clientes
git commit -m "feat(clientes): fotos del pago en el detalle e índice público para reutilizar la ficha y los modales"
```

---

### Tarea 3.3 (Carril 3): fotos de actividades, modal de visita nuevo y pila de modales de la ficha

**Carril 3, rama `feat/desempeno-modales`, después de la Tarea 3.2.**

**Files:**
- Create: `src/modules/desempeno/presentation/hooks/useImagenes.ts`
- Create: `src/modules/desempeno/components/modales/GaleriaFotos.tsx`
- Create: `src/modules/desempeno/components/modales/CeldaFotos.tsx`
- Create: `src/modules/desempeno/components/modales/ModalActividadDetalle.tsx`
- Create: `src/modules/desempeno/components/modales/ModalesFicha.tsx`
- Test: `src/modules/desempeno/components/modales/__tests__/fotos.test.tsx`
- Test: `src/modules/desempeno/components/modales/__tests__/ModalActividadDetalle.test.tsx`
- Test: `src/modules/desempeno/components/modales/__tests__/ModalesFicha.test.tsx`

**Interfaces:**
- Consumes: Tarea 0 (`Actividad`, `RefImagenes`, `refImagenesDeActividad`, `listarImagenes`, `descargarImagen`, `useCargaRemota`, `useDesempenoPort`, `renderConDesempeno`, `InfoRapida`, formato, metas), Tarea 3.1 (`AuthenticatedImage`, `VentaImagenLightbox`, `FuenteImagen`, importados por archivo desde `@/modules/ventasLocales/components/detalle/`), Tarea 3.2 (`PagoModal`, `VentaModal`, `ClienteFicha` desde `@/modules/clientes`).
- Produces:
  - `useImagenes(ref: RefImagenes | null)`, `claveDeRef(ref)`.
  - `GaleriaFotos({ refImagenes, mensajeSinFotos })`, `CeldaFotos({ actividad })` (la integración la pasa como `renderFotos`).
  - `ModalActividadDetalle({ actividad, onClose, onAbrirCliente })` — visita, o pago aún sin `docto_cc_id`.
  - `type ModalFicha = { tipo: "actividad"; actividad: Actividad } | { tipo: "pago"; clienteId: number; doctoCcId: number } | { tipo: "venta"; clienteId: number; doctoPvId: number } | { tipo: "cliente"; clienteId: number }`; `ModalesFicha({ pila, onCerrar, onAbrir })`.

- [ ] **Step 1: Pruebas (fallan)**

`src/modules/desempeno/components/modales/__tests__/fotos.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { GaleriaFotos } from "../GaleriaFotos";
import { CeldaFotos } from "../CeldaFotos";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import {
  FakeDesempenoPort,
  makeActividadPago,
  makeActividadVisita,
  makeImagen,
} from "../../../application/__tests__/fakeDesempenoPort";

vi.mock("@/modules/ventasLocales/components/detalle/AuthenticatedImage", () => ({
  default: ({ alt }: { alt?: string }) => <img alt={alt ?? "imagen"} />,
}));

describe("GaleriaFotos", () => {
  it("sin referencia dice por qué no hay fotos", () => {
    renderConDesempeno(<GaleriaFotos refImagenes={null} mensajeSinFotos="Sin comprobante" />);
    expect(screen.getByText("Sin comprobante")).toBeInTheDocument();
  });

  it("pide las fotos de la visita y abre el visor", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    port.imagenesResponse = [makeImagen({ id: "a", descripcion: "Fachada" }), makeImagen({ id: "b", descripcion: "Puerta" })];
    renderConDesempeno(
      <GaleriaFotos refImagenes={{ tipo: "visita", visitaId: "v-1" }} mensajeSinFotos="No se tomaron fotos" />,
      { port },
    );
    await user.click(await screen.findByRole("button", { name: "Ver Fachada" }));
    expect(screen.getByText("1 / 2")).toBeInTheDocument();
    expect(port.imagenesCalls[0].ref).toEqual({ tipo: "visita", visitaId: "v-1" });
  });
});

describe("CeldaFotos", () => {
  it("sin fotos, guion", () => {
    renderConDesempeno(<CeldaFotos actividad={makeActividadPago({ numFotos: 0 })} />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("popover con miniaturas que solo se piden al abrirlo", async () => {
    const user = userEvent.setup();
    const port = new FakeDesempenoPort();
    renderConDesempeno(<CeldaFotos actividad={makeActividadVisita()} />, { port });
    expect(port.imagenesCalls).toHaveLength(0);
    await user.hover(screen.getByText("2"));
    expect(await screen.findByText("2 fotos")).toBeInTheDocument();
    expect(await screen.findByAltText("Recibo firmado")).toBeInTheDocument();
    expect(port.imagenesCalls).toHaveLength(1);
  });
});
```

`src/modules/desempeno/components/modales/__tests__/ModalActividadDetalle.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ModalActividadDetalle } from "../ModalActividadDetalle";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import { makeActividadPago, makeActividadVisita } from "../../../application/__tests__/fakeDesempenoPort";

vi.mock("@/modules/ventasLocales/components/detalle/AuthenticatedImage", () => ({
  default: ({ alt }: { alt?: string }) => <img alt={alt ?? "imagen"} />,
}));

describe("ModalActividadDetalle", () => {
  it("visita: resultado, nota, sin ubicación, horas de captura y llegada en la oficina, fotos", async () => {
    const user = userEvent.setup();
    const onAbrirCliente = vi.fn();
    renderConDesempeno(
      <ModalActividadDetalle actividad={makeActividadVisita()} onClose={vi.fn()} onAbrirCliente={onAbrirCliente} />,
    );
    expect(screen.getByText("Visitas")).toBeInTheDocument();
    expect(screen.getAllByText("No se encontraba").length).toBeGreaterThan(0);
    expect(screen.getByText("“Vecina dice que regresa el lunes”")).toBeInTheDocument();
    expect(screen.getByText("El teléfono no mandó ubicación.")).toBeInTheDocument();
    expect(screen.getByText("08:45")).toBeInTheDocument();
    expect(screen.getByText("10:58")).toBeInTheDocument();
    expect(screen.getByText("Fotos · 2")).toBeInTheDocument();
    expect(await screen.findByAltText("Recibo firmado")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Ficha del cliente" }));
    expect(onAbrirCliente).toHaveBeenCalledWith(1602231);
  });

  it("pago que aún no llega a Microsip: importe, forma, estado y dónde se registró", () => {
    const base = makeActividadPago();
    renderConDesempeno(
      <ModalActividadDetalle
        actividad={{ ...base, pago: { ...base.pago!, doctoCcId: null, aplicado: false } }}
        onClose={vi.fn()}
        onAbrirCliente={vi.fn()}
      />,
    );
    expect(screen.getByText("Pagos")).toBeInTheDocument();
    expect(screen.getByText("$150.00")).toBeInTheDocument();
    expect(screen.getByText("Sin aplicar")).toBeInTheDocument();
    expect(screen.getByText("Efectivo")).toBeInTheDocument();
    expect(screen.getByText("18.4617, -97.3928")).toBeInTheDocument();
    expect(screen.queryByText(/Depósito|Casa/)).not.toBeInTheDocument();
  });
});
```

`src/modules/desempeno/components/modales/__tests__/ModalesFicha.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ModalesFicha } from "../ModalesFicha";
import { renderConDesempeno } from "../../../test/renderConDesempeno";
import { makeActividadPago, makeActividadVisita } from "../../../application/__tests__/fakeDesempenoPort";

vi.mock("@/modules/clientes", () => ({
  PagoModal: (p: { clienteId: number; doctoCcId: number; onAbrirVenta?: (id: number) => void }) => (
    <div data-testid="pago-modal">
      {`${p.clienteId}-${p.doctoCcId}`}
      <button type="button" onClick={() => p.onAbrirVenta?.(9921)}>abrir venta</button>
    </div>
  ),
  VentaModal: (p: { clienteId: number; doctoPvId: number }) => <div data-testid="venta-modal">{`${p.clienteId}-${p.doctoPvId}`}</div>,
  ClienteFicha: (p: { clienteId: number }) => <div data-testid="cliente-ficha">{p.clienteId}</div>,
}));
vi.mock("@/modules/ventasLocales/components/detalle/AuthenticatedImage", () => ({
  default: ({ alt }: { alt?: string }) => <img alt={alt ?? "imagen"} />,
}));

describe("ModalesFicha", () => {
  it("un pago con docto_cc_id abre el PagoModal de Clientes y encadena la venta", async () => {
    const user = userEvent.setup();
    const onAbrir = vi.fn();
    renderConDesempeno(
      <ModalesFicha pila={[{ tipo: "actividad", actividad: makeActividadPago() }]} onCerrar={vi.fn()} onAbrir={onAbrir} />,
    );
    expect(screen.getByTestId("pago-modal")).toHaveTextContent("1544855-15082369");
    await user.click(screen.getByRole("button", { name: "abrir venta" }));
    expect(onAbrir).toHaveBeenCalledWith({ tipo: "venta", clienteId: 1544855, doctoPvId: 9921 });
  });

  it("un pago que aún no llega a Microsip (docto_cc_id nulo) abre el modal propio, no el de Clientes", () => {
    const base = makeActividadPago();
    renderConDesempeno(
      <ModalesFicha
        pila={[{ tipo: "actividad", actividad: { ...base, pago: { ...base.pago!, doctoCcId: null } } }]}
        onCerrar={vi.fn()}
        onAbrir={vi.fn()}
      />,
    );
    expect(screen.queryByTestId("pago-modal")).not.toBeInTheDocument();
    expect(screen.getByText("Pagos")).toBeInTheDocument();
  });

  it("una visita abre el modal de visita nuevo", () => {
    renderConDesempeno(
      <ModalesFicha pila={[{ tipo: "actividad", actividad: makeActividadVisita() }]} onCerrar={vi.fn()} onAbrir={vi.fn()} />,
    );
    expect(screen.getByText("Visitas")).toBeInTheDocument();
  });

  it("venta y cliente usan los componentes de Clientes; se apilan", () => {
    renderConDesempeno(
      <ModalesFicha
        pila={[
          { tipo: "venta", clienteId: 1544855, doctoPvId: 9921 },
          { tipo: "cliente", clienteId: 1544855 },
        ]}
        onCerrar={vi.fn()}
        onAbrir={vi.fn()}
      />,
    );
    expect(screen.getByTestId("venta-modal")).toHaveTextContent("1544855-9921");
    expect(screen.getByTestId("cliente-ficha")).toHaveTextContent("1544855");
  });
});
```

- [ ] **Step 2: Correr y ver fallar**

Run: `npx vitest run src/modules/desempeno/components/modales`
Expected: FAIL con `Failed to resolve import "../GaleriaFotos"`.

- [ ] **Step 3: Implementar**

`src/modules/desempeno/presentation/hooks/useImagenes.ts`:

```ts
import { useCallback } from "react";
import type { ImagenAdjunta, RefImagenes } from "../../domain/entities";
import type { DomainError } from "../../domain/errors";
import { listarImagenes } from "../../application/usecases/consultas";
import { useDesempenoPort } from "../context/useDesempenoPort";
import { useCargaRemota } from "./lib/useCargaRemota";

export function claveDeRef(ref: RefImagenes): string {
  return ref.tipo === "pago" ? `pago/${ref.pagoRecibidoId}` : `visita/${ref.visitaId}`;
}

// Fotos de un pago o de una visita (contrato §6). Con ref null no pide nada.
export function useImagenes(ref: RefImagenes | null): {
  imagenes: ImagenAdjunta[] | null;
  isLoading: boolean;
  error: DomainError | null;
} {
  const port = useDesempenoPort();
  const tipo = ref?.tipo ?? null;
  const id = ref ? (ref.tipo === "pago" ? ref.pagoRecibidoId : ref.visitaId) : null;
  const cargar = useCallback(
    (signal: AbortSignal) =>
      listarImagenes(
        port,
        tipo === "pago" ? { tipo: "pago", pagoRecibidoId: id ?? "" } : { tipo: "visita", visitaId: id ?? "" },
        signal,
      ),
    [port, tipo, id],
  );
  const { datos, isLoading, error } = useCargaRemota(tipo === null ? null : cargar);
  return { imagenes: datos, isLoading, error };
}
```

`src/modules/desempeno/components/modales/GaleriaFotos.tsx`:

```tsx
import { useState } from "react";
import { createPortal } from "react-dom";
import { Skeleton } from "@/components/ui/skeleton";
// Imports directos (no el index de detalle) para no arrastrar todo el modal de ventas.
import AuthenticatedImage from "@/modules/ventasLocales/components/detalle/AuthenticatedImage";
import VentaImagenLightbox from "@/modules/ventasLocales/components/detalle/VentaImagenLightbox";
import type { FuenteImagen } from "@/modules/ventasLocales/components/detalle/fuenteImagen";
import type { RefImagenes } from "../../domain/entities";
import { descargarImagen } from "../../application/usecases/consultas";
import { useDesempenoPort } from "../../presentation/context/useDesempenoPort";
import { claveDeRef, useImagenes } from "../../presentation/hooks/useImagenes";

// Galería + visor de Ventas Locales (spec §3.3) con el descargador de Desempeño.
export function GaleriaFotos({
  refImagenes,
  mensajeSinFotos,
  limite,
}: {
  refImagenes: RefImagenes | null;
  mensajeSinFotos: string;
  limite?: number;
}) {
  const port = useDesempenoPort();
  const { imagenes, isLoading, error } = useImagenes(refImagenes);
  const [abierta, setAbierta] = useState<number | null>(null);

  if (refImagenes === null) {
    return <p className="font-mono text-[11px] italic text-muted-foreground">{mensajeSinFotos}</p>;
  }
  if (isLoading) return <Skeleton className="h-20 w-full" />;
  if (error) return <p className="text-[12px] text-muted-foreground">No se pudieron cargar las fotos</p>;
  if (!imagenes || imagenes.length === 0) {
    return <p className="font-mono text-[11px] italic text-muted-foreground">{mensajeSinFotos}</p>;
  }

  const ref = refImagenes;
  const fuentePara = (imagenId: string): FuenteImagen => ({
    cacheKey: `${claveDeRef(ref)}/${imagenId}`,
    descargar: () => descargarImagen(port, ref, imagenId),
  });
  const visibles = limite ? imagenes.slice(0, limite) : imagenes;

  return (
    <>
      <div className="grid grid-cols-3 gap-2">
        {visibles.map((img, i) => (
          <button
            key={img.id}
            type="button"
            aria-label={`Ver ${img.descripcion ?? `foto ${i + 1}`}`}
            onClick={() => setAbierta(i)}
            className="aspect-[4/3] overflow-hidden rounded-md bg-muted ring-1 ring-border"
          >
            <AuthenticatedImage fuente={fuentePara(img.id)} alt={img.descripcion ?? `Foto ${i + 1}`} className="h-full w-full object-cover" />
          </button>
        ))}
      </div>
      {abierta !== null &&
        createPortal(
          <VentaImagenLightbox
            imagenes={imagenes.map((img) => ({ id: img.id, descripcion: img.descripcion, created_at: img.createdAt }))}
            fuentePara={fuentePara}
            initialIndex={abierta}
            onClose={() => setAbierta(null)}
          />,
          document.body,
        )}
    </>
  );
}
```

`src/modules/desempeno/components/modales/CeldaFotos.tsx`:

```tsx
import { Camera } from "lucide-react";
import { refImagenesDeActividad, type Actividad } from "../../domain/entities";
import { plural } from "../lib/format";
import { InfoRapida, TarjetaSub, TarjetaTitulo } from "../comunes/InfoRapida";
import { GaleriaFotos } from "./GaleriaFotos";

const MINIATURAS_EN_POPOVER = 3;

function TarjetaFotos({ actividad }: { actividad: Actividad }) {
  return (
    <div className="w-[260px]">
      <TarjetaTitulo>{plural(actividad.numFotos, "foto", "fotos")}</TarjetaTitulo>
      <TarjetaSub>clic en el renglón para verlas</TarjetaSub>
      <div className="mt-2">
        <GaleriaFotos
          refImagenes={refImagenesDeActividad(actividad)}
          mensajeSinFotos="Sin comprobante"
          limite={MINIATURAS_EN_POPOVER}
        />
      </div>
    </div>
  );
}

// Celda "Fotos" de la lista y la tabla (la integración la pasa como renderFotos).
// Las miniaturas se piden solo al abrir el popover (el contenido se monta al abrir).
export function CeldaFotos({ actividad }: { actividad: Actividad }) {
  if (actividad.numFotos === 0) return <span className="text-muted-foreground">—</span>;
  return (
    <InfoRapida contenido={<TarjetaFotos actividad={actividad} />}>
      <span tabIndex={0} className="inline-flex cursor-help items-center gap-1 text-muted-foreground">
        <Camera aria-hidden className="h-3.5 w-3.5" />
        {actividad.numFotos}
      </span>
    </InfoRapida>
  );
}
```

`src/modules/desempeno/components/modales/ModalActividadDetalle.tsx`:

```tsx
import type { ReactNode } from "react";
import { MapPinOff, User, X } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { refImagenesDeActividad, type Actividad } from "../../domain/entities";
import { diaDeNegocioDe, dinero2, fechaLarga, horaCorta } from "../lib/format";
import { FORMA_COBRO_ETIQUETA } from "../lib/tipoActividadMeta";
import { GaleriaFotos } from "./GaleriaFotos";

function Seccion({ titulo, children, ancha }: { titulo: string; children: ReactNode; ancha?: boolean }) {
  return (
    <section className={cn(ancha && "col-span-2")}>
      <h4 className="mb-2 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">{titulo}</h4>
      {children}
    </section>
  );
}

function Fila({ k, v }: { k: string; v: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-0.5">
      <dt className="font-mono text-[11px] text-muted-foreground">{k}</dt>
      <dd className="text-right text-sm">{v}</dd>
    </div>
  );
}

function estadoPago(a: Actividad): string | null {
  if (!a.pago) return null;
  if (a.pago.rechazado) return "Rechazado";
  return a.pago.aplicado ? "Aplicado" : "Sin aplicar";
}

// Modal de visita (nuevo, mismo estilo que PagoModal de Clientes) y de pago que
// aún no tiene docto_cc_id (no se puede abrir el detalle de Clientes). Todo sale
// del objeto Actividad del contrato; las fotos, de GET .../imagenes.
export function ModalActividadDetalle({
  actividad: a,
  onClose,
  onAbrirCliente,
}: {
  actividad: Actividad;
  onClose: () => void;
  onAbrirCliente: (clienteId: number) => void;
}) {
  const esVisita = a.visita !== null;
  const estado = estadoPago(a);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="block max-h-[90vh] w-[94vw] max-w-[640px] gap-0 overflow-hidden p-0 [&_button.absolute.right-4.top-4]:hidden">
        <DialogTitle className="sr-only">{esVisita ? "Detalle de visita" : "Detalle de pago"}</DialogTitle>
        <DialogDescription className="sr-only">detalle del movimiento</DialogDescription>
        <header className="flex h-[60px] items-center justify-between border-b border-border/60 px-6 text-xs">
          <div className="flex items-center gap-3">
            <span className="text-muted-foreground">{esVisita ? "Visitas" : "Pagos"}</span>
            <span className="text-muted-foreground/40">/</span>
            <span className="font-mono tracking-wider">{a.id}</span>
            {estado && (
              <span className="rounded-full bg-dsm-ambar-fondo px-2 py-0.5 font-mono text-[10px] uppercase text-dsm-ambar">{estado}</span>
            )}
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Cerrar" className="h-7 w-7">
            <X className="h-3.5 w-3.5" />
          </Button>
        </header>
        <div className="overflow-y-auto px-8 pb-10 pt-6" style={{ maxHeight: "calc(90vh - 60px - 64px)" }}>
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="font-mono text-[11px] text-muted-foreground first-letter:uppercase">
                {`${fechaLarga(diaDeNegocioDe(a.hora))} · ${horaCorta(a.hora)}`}
              </p>
              <h2 className="font-serif text-2xl">{a.clienteNombre}</h2>
              <p className="font-mono text-[11px] text-muted-foreground">{a.folio ?? "Sin venta"}</p>
            </div>
            <div className="text-right">
              <p className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">{esVisita ? "Resultado" : "Importe"}</p>
              <p className="font-serif text-[28px] leading-tight">{a.visita ? a.visita.tipoVisita : a.pago ? dinero2(a.pago.importe) : "—"}</p>
            </div>
          </div>
          <div className="mt-6 grid grid-cols-2 gap-6">
            {a.visita?.nota && (
              <Seccion titulo="Nota del cobrador" ancha>
                <blockquote className="border-l-2 border-border pl-3 text-sm italic">{`“${a.visita.nota}”`}</blockquote>
              </Seccion>
            )}
            <Seccion titulo={esVisita ? "Visita" : "Cómo se pagó"}>
              <dl>
                {a.visita && <Fila k="Tipo" v={a.visita.tipoVisita} />}
                {a.pago && <Fila k="Forma" v={FORMA_COBRO_ETIQUETA[a.pago.formaCobro]} />}
                <Fila k="Venta" v={a.folio ?? "—"} />
              </dl>
            </Seccion>
            <Seccion titulo="Quién lo registró">
              <dl>
                <Fila k="Registró" v={a.registradoPor.nombre} />
                <Fila k="Origen" v={a.registradoPor.origen === "oficina" ? "Oficina" : "App de cobranza"} />
              </dl>
            </Seccion>
            <Seccion titulo="Dónde se registró">
              {a.conGps && a.lat !== null && a.lng !== null ? (
                <div className="flex items-center justify-between">
                  <span className="font-mono text-[12px]">{`${a.lat}, ${a.lng}`}</span>
                  <a
                    href={`https://www.google.com/maps?q=${a.lat},${a.lng}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-mono text-[10px] text-muted-foreground underline-offset-2 hover:underline"
                  >
                    Ver en Maps ↗
                  </a>
                </div>
              ) : (
                <p className="flex items-center gap-2 text-[13px] text-dsm-ambar">
                  <MapPinOff aria-hidden className="h-3.5 w-3.5" />
                  {a.registradoPor.origen === "oficina" ? "Capturado en oficina, sin ubicación." : "El teléfono no mandó ubicación."}
                </p>
              )}
            </Seccion>
            <Seccion titulo="Captura y llegada">
              <dl>
                <Fila k="Capturado en el teléfono" v={horaCorta(a.hora)} />
                <Fila k="Llegó al servidor" v={a.recibidoAt ? horaCorta(a.recibidoAt) : "—"} />
              </dl>
            </Seccion>
            <Seccion titulo={a.numFotos > 0 ? `Fotos · ${a.numFotos}` : "Fotos"} ancha>
              <GaleriaFotos
                refImagenes={refImagenesDeActividad(a)}
                mensajeSinFotos={a.pago && !a.pago.pagoRecibidoId ? "Sin comprobante" : "No se tomaron fotos"}
              />
            </Seccion>
          </div>
        </div>
        <footer className="flex h-16 items-center justify-end gap-2 border-t border-border/60 px-6">
          <Button variant="outline" size="sm" onClick={() => onAbrirCliente(a.clienteId)}>
            <User aria-hidden className="mr-1.5 h-3.5 w-3.5" />
            Ficha del cliente
          </Button>
        </footer>
      </DialogContent>
    </Dialog>
  );
}
```

`src/modules/desempeno/components/modales/ModalesFicha.tsx`:

```tsx
import { X } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ClienteFicha, PagoModal, VentaModal } from "@/modules/clientes";
import type { Actividad } from "../../domain/entities";
import { ModalActividadDetalle } from "./ModalActividadDetalle";

export type ModalFicha =
  | { tipo: "actividad"; actividad: Actividad }
  | { tipo: "pago"; clienteId: number; doctoCcId: number }
  | { tipo: "venta"; clienteId: number; doctoPvId: number }
  | { tipo: "cliente"; clienteId: number };

// Pila de modales de la ficha (D11: se reutilizan los de Clientes). Un pago con
// docto_cc_id abre el PagoModal de Clientes; una visita o un pago que aún no
// llega a Microsip abre el modal propio. Se pueden encadenar (pago → venta →
// cliente) como en el mock.
export function ModalesFicha({
  pila,
  onCerrar,
  onAbrir,
}: {
  pila: readonly ModalFicha[];
  onCerrar: () => void;
  onAbrir: (m: ModalFicha) => void;
}) {
  return (
    <>
      {pila.map((m, i) => {
        const clave = `${m.tipo}-${i}`;
        if (m.tipo === "actividad") {
          const a = m.actividad;
          if (a.pago && a.pago.doctoCcId !== null) {
            return (
              <PagoModal
                key={clave}
                clienteId={a.clienteId}
                doctoCcId={a.pago.doctoCcId}
                onClose={onCerrar}
                onAbrirVenta={(doctoPvId) => onAbrir({ tipo: "venta", clienteId: a.clienteId, doctoPvId })}
                onAbrirCliente={() => onAbrir({ tipo: "cliente", clienteId: a.clienteId })}
              />
            );
          }
          return (
            <ModalActividadDetalle
              key={clave}
              actividad={a}
              onClose={onCerrar}
              onAbrirCliente={(clienteId) => onAbrir({ tipo: "cliente", clienteId })}
            />
          );
        }
        if (m.tipo === "pago") {
          return (
            <PagoModal
              key={clave}
              clienteId={m.clienteId}
              doctoCcId={m.doctoCcId}
              onClose={onCerrar}
              onAbrirVenta={(doctoPvId) => onAbrir({ tipo: "venta", clienteId: m.clienteId, doctoPvId })}
              onAbrirCliente={() => onAbrir({ tipo: "cliente", clienteId: m.clienteId })}
            />
          );
        }
        if (m.tipo === "venta") {
          return (
            <VentaModal
              key={clave}
              clienteId={m.clienteId}
              doctoPvId={m.doctoPvId}
              open
              onClose={onCerrar}
              onPagoClick={(doctoCcId) => onAbrir({ tipo: "pago", clienteId: m.clienteId, doctoCcId })}
            />
          );
        }
        return (
          <Dialog key={clave} open onOpenChange={(o) => !o && onCerrar()}>
            <DialogContent className="block h-[92vh] w-[96vw] max-w-[1180px] overflow-y-auto p-0 [&_button.absolute.right-4.top-4]:hidden">
              <DialogTitle className="sr-only">Ficha del cliente</DialogTitle>
              <DialogDescription className="sr-only">ficha del cliente</DialogDescription>
              <div className="sticky top-0 z-10 flex justify-end bg-background/95 px-4 py-2">
                <Button variant="ghost" size="icon" aria-label="Cerrar" onClick={onCerrar} className="h-7 w-7">
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>
              <ClienteFicha clienteId={m.clienteId} />
            </DialogContent>
          </Dialog>
        );
      })}
    </>
  );
}
```

(`ClienteFicha` escribe `?tab=` en la URL con `useSearchParams`; la pantalla de Desempeño usa `usuario`/`pestana`/`dia`, así que no chocan.)

- [ ] **Step 4: Correr, tipos y lint**

Run: `npx vitest run src/modules/desempeno/components/modales && npx tsc --noEmit && npx eslint src/modules/desempeno --max-warnings 0`
Expected: PASS (4 + 2 + 4 = 10 pruebas), sin salida de tsc/eslint.

- [ ] **Step 5: Commit**

```bash
git add src/modules/desempeno/presentation/hooks/useImagenes.ts src/modules/desempeno/components/modales
git commit -m "feat(desempeno): fotos de pagos y visitas, modal de visita y pila de modales que reutiliza Clientes"
```

---

### Tarea 3.4 (Carril 3): adapter HTTP real, DTOs del contrato, mappers y MSW (D2)

**Carril 3, rama `feat/desempeno-modales`, después de la Tarea 3.3** (no depende de ella; puede ir antes si el agente quiere).

**Files:**
- Create: `src/modules/desempeno/infrastructure/http/apiClient.ts`
- Create: `src/modules/desempeno/infrastructure/http/dtos.ts`
- Create: `src/modules/desempeno/infrastructure/http/params.ts`
- Create: `src/modules/desempeno/infrastructure/http/HttpDesempenoAdapter.ts`
- Create: `src/modules/desempeno/infrastructure/mappers/dtoADominio.ts`
- Create: `src/modules/desempeno/infrastructure/mappers/errorMapper.ts`
- Create: `src/test/msw/fixtures/desempeno.ts`
- Create: `src/test/msw/handlers/desempeno.ts`
- Test: `src/modules/desempeno/infrastructure/mappers/__tests__/dtoADominio.test.ts`
- Test: `src/modules/desempeno/infrastructure/mappers/__tests__/errorMapper.test.ts`
- Test: `src/modules/desempeno/infrastructure/http/HttpDesempenoAdapter.test.ts`

**Interfaces:**
- Consumes: Tarea 0 (entidades, `DesempenoPort`, guardas `esEstadoCuenta`, `esTrato`, `esSegmento`, `esTipoSenal`, `esPeriodo`, `esEstadoAhora`, `esMotivoNoConfigurado`, `esUno`, `TIPOS_ACTIVIDAD`, `FORMAS_COBRO`, `DomainError`).
- Produces: `HttpDesempenoAdapter(client: AxiosInstance) implements DesempenoPort`, `apiClient`, `apperrorToDomainError(err)`, mappers `dtoToTablero`, `dtoToJornada`, `dtoToPaginaActividad`, `dtoToCuentas`, `dtoToImagen`, `dtoToActividad`; fixtures `tableroDTO`, `jornadaDTO`, `paginaActividadDTO`, `cuentasDTO`, `imagenesDTO`; handlers `desempenoHandlers(opciones)` con base `*/v2`.

- [ ] **Step 1: Fixtures del contrato (byte a byte) y handlers MSW**

`src/test/msw/fixtures/desempeno.ts`:

```ts
import type {
  ActividadDTO,
  CuentasDTO,
  ImagenDTO,
  JornadaDTO,
  LineaDelDiaDTO,
  PaginaActividadDTO,
  TableroDTO,
} from "../../../modules/desempeno/infrastructure/http/dtos";

// Copia de los ejemplos de 2026-10-14-desempeno-contratos.md con los "..."
// completados. Los NOMBRES de campo son ley: si el contrato cambia, esto cambia.

export const actividadPagoDTO: ActividadDTO = {
  id: "pago:15082369",
  tipo: "pago",
  hora: "2026-10-14T16:31:00Z",
  recibido_at: "2026-10-14T16:31:05Z",
  cliente_id: 1544855,
  cliente_nombre: "JUANA ANDRADE LECHUGA",
  venta_id: 13785217,
  folio: "Z1677035",
  registrado_por: { usuario_uid: "abc123", cobrador_id: 11502, nombre: "JUAN CARLOS MENDEZ", origen: "app" },
  lat: 18.4617,
  lng: -97.3928,
  con_gps: true,
  num_fotos: 2,
  pago: {
    importe: "150.00",
    forma_cobro: "efectivo",
    forma_cobro_id: 157,
    aplicado: true,
    rechazado: false,
    docto_cc_id: 15082369,
    pago_recibido_id: "7a1f39e4-2b6c-4d1e-9f0a-3c5b8e2d1a47",
  },
  visita: null,
};

export const actividadVisitaDTO: ActividadDTO = {
  id: "visita:412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e",
  tipo: "visita",
  hora: "2026-10-14T17:05:00Z",
  recibido_at: null,
  cliente_id: 1602231,
  cliente_nombre: "MARÍA ELENA CUAUTLE TLAPA",
  venta_id: null,
  folio: null,
  registrado_por: { usuario_uid: null, cobrador_id: null, nombre: "Oficina", origen: "oficina" },
  lat: null,
  lng: null,
  con_gps: false,
  num_fotos: 0,
  pago: null,
  visita: {
    visita_id: "412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e",
    tipo_visita: "No se encontraba",
    nota: "Vecina dice que regresa el lunes",
    estado: "no_estaba",
  },
};

export const lineaDTO: LineaDelDiaDTO = {
  dia: "2026-10-14",
  puntos: [
    { id: "pago:15082369", tipo: "pago", hora: "2026-10-14T16:31:00Z" },
    { id: "visita:412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e", tipo: "visita", hora: "2026-10-14T17:05:00Z" },
  ],
  tramos: [
    {
      desde: "2026-10-14T16:31:00Z",
      hasta: "2026-10-14T17:05:00Z",
      pagos: { n: 12, importe: "3150.00" },
      visitas: 5,
      condonaciones: 0,
      clientes: 14,
    },
  ],
  por_hora: Array.from({ length: 13 }, (_, i) => ({
    hora: 7 + i,
    pagos: i === 3 ? 3 : 0,
    visitas: i === 3 ? 1 : 0,
    condonaciones: 0,
    importe: i === 3 ? "450.00" : "0.00",
  })),
  pausas: [
    {
      desde: "2026-10-14T17:05:00Z",
      hasta: "2026-10-14T18:17:00Z",
      minutos: 72,
      anterior_id: "visita:412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e",
      siguiente_id: null,
      en_curso: false,
    },
  ],
};

export const tableroDTO: TableroDTO = {
  generado_at: "2026-10-14T22:40:00Z",
  periodo: "hoy",
  pausa_min: 45,
  usuarios: [
    {
      uid: "abc123",
      nombre: "Juan Carlos Méndez",
      email: "juan.mendez@muebleriamsp.mx",
      zona_id: 21563,
      zona_nombre: "RUTA 21",
      configurado: true,
      motivo_no_configurado: null,
      inicio_semana: "2026-10-09T13:46:00Z",
      ventas_universo: 232,
      estado_ahora: "activo",
      estado_desde: "2026-10-14T22:10:00Z",
      ultima: { id: "pago:15082369", tipo: "pago", hora: "2026-10-14T22:10:00Z", cliente_nombre: "JUANA ANDRADE LECHUGA" },
      primera_actividad: "2026-10-14T14:20:00Z",
      ultima_actividad: "2026-10-14T22:10:00Z",
      minutos_activos: 346,
      pausas: { n: 1, minutos: 72, en_curso: false },
      pagos: { n: 31, importe: "8600.00" },
      pagos_por_forma: {
        efectivo: { n: 28, importe: "7800.00" },
        transferencia: { n: 3, importe: "800.00" },
        cheque: { n: 0, importe: "0.00" },
      },
      condonaciones: { n: 0, importe: "0.00" },
      no_aplicados: { n: 0, importe: "0.00" },
      visitas: 13,
      cobertura: { con_algo: 186, total: 232 },
      cobertura_por_segmento: { sin_visitar: 46, volver: 20, despues: 3, pagados: 140, sin_nada: 46 },
      cobro: { cobrado: "8600.00", esperado: "13050.00", pct: "65.90" },
      lineas: [lineaDTO],
    },
    {
      uid: "def456",
      nombre: "Norma Velázquez Rojas",
      email: "norma.velazquez@muebleriamsp.mx",
      zona_id: null,
      zona_nombre: null,
      configurado: false,
      motivo_no_configurado: "sin_zona",
      inicio_semana: null,
      ventas_universo: 0,
      estado_ahora: "no_ha_empezado",
      estado_desde: null,
      ultima: null,
      primera_actividad: null,
      ultima_actividad: null,
      minutos_activos: 0,
      pausas: { n: 0, minutos: 0, en_curso: false },
      pagos: { n: 0, importe: "0.00" },
      pagos_por_forma: {
        efectivo: { n: 0, importe: "0.00" },
        transferencia: { n: 0, importe: "0.00" },
        cheque: { n: 0, importe: "0.00" },
      },
      condonaciones: { n: 0, importe: "0.00" },
      no_aplicados: { n: 0, importe: "0.00" },
      visitas: 0,
      cobertura: { con_algo: 0, total: 0 },
      cobertura_por_segmento: { sin_visitar: 0, volver: 0, despues: 0, pagados: 0, sin_nada: 0 },
      cobro: { cobrado: "0.00", esperado: "0.00", pct: null },
      lineas: [],
    },
  ],
};

export const jornadaDTO: JornadaDTO = {
  uid: "abc123",
  fecha: "2026-10-14",
  pausa_min: 45,
  actividades: [actividadPagoDTO, actividadVisitaDTO],
  linea: lineaDTO,
  senales: [
    {
      tipo: "mismo_punto",
      actividad_ids: ["pago:15082369"],
      desde: "2026-10-14T16:31:00Z",
      hasta: "2026-10-14T16:37:00Z",
      detalle: "4 cuentas desde el mismo punto en 6 min",
      regla: "≥ 4 clientes distintos en 25 m dentro de 10 min",
    },
  ],
  resumen: {
    primera_actividad: "2026-10-14T16:31:00Z",
    ultima_actividad: "2026-10-14T17:05:00Z",
    minutos_activos: 346,
    pausas: { n: 1, minutos: 72, en_curso: false },
    pagos: { n: 31, importe: "8600.00" },
    visitas: 13,
    condonaciones: 0,
    clientes: 41,
  },
};

export const paginaActividadDTO: PaginaActividadDTO = {
  items: [actividadPagoDTO, actividadVisitaDTO],
  siguiente_cursor: "opaco-2",
  totales: { n: 1188, importe: "289450.00", por_tipo: { pago: 980, visita: 200, condonacion: 8 } },
};

export const cuentasDTO: CuentasDTO = {
  inicio_semana: "2026-10-09T13:46:00Z",
  conteos: { sin_visitar: 46, volver: 20, despues: 3, pagados: 140, sin_nada: 46, total: 232 },
  cuentas: [
    {
      venta_id: 13785217,
      docto_pv_id: 9921,
      folio: "Z1677035",
      cliente_id: 1544855,
      cliente_nombre: "JUANA ANDRADE LECHUGA",
      producto: "ROPERO LABRADO CALIFORNIA VINO",
      estado: "pago",
      trato: "pagado",
      etiqueta: "Pagó esta semana",
      detalle: "No hace falta volver",
      segmento: "pagados",
      con_algo: true,
      determinado_por: "pago:15082369",
      ultima_actividad: { id: "pago:15082369", tipo: "pago", hora: "2026-10-14T16:31:00Z" },
      saldo: "16650.00",
      parcialidad: "150.00",
      abono_semana: "150.00",
      num_atrasos: 0,
      nueva: false,
    },
  ],
};

export const imagenesDTO: ImagenDTO[] = [
  {
    id: "0b6f1c2e-5a7d-4e3b-8c9f-1d2e3f4a5b6c",
    mime: "image/jpeg",
    size_bytes: 184320,
    descripcion: null,
    created_at: "2026-10-14T16:31:10Z",
    updated_at: "2026-10-14T16:31:10Z",
  },
];
```

`src/test/msw/handlers/desempeno.ts`:

```ts
import { http, HttpResponse } from "msw";
import { cuentasDTO, imagenesDTO, jornadaDTO, paginaActividadDTO, tableroDTO } from "../fixtures/desempeno";

export const DESEMPENO_BASE = "*/v2";

type Opciones = {
  vistoPor?: (url: URL) => void;
  error?: { status: number; body: Record<string, unknown> };
};

function responder<T>(cuerpo: T, op: Opciones, request: Request) {
  op.vistoPor?.(new URL(request.url));
  if (op.error) return HttpResponse.json(op.error.body, { status: op.error.status });
  return HttpResponse.json(cuerpo as Record<string, unknown>);
}

// Handlers de /v2/desempeno/* y de las fotos (contrato §2–§6). Las pruebas los
// registran con server.use(...desempenoHandlers({...})).
export function desempenoHandlers(op: Opciones = {}) {
  return [
    http.get(`${DESEMPENO_BASE}/desempeno/usuarios`, ({ request }) => responder(tableroDTO, op, request)),
    http.get(`${DESEMPENO_BASE}/desempeno/usuarios/:uid/jornada`, ({ request }) => responder(jornadaDTO, op, request)),
    http.get(`${DESEMPENO_BASE}/desempeno/usuarios/:uid/actividad`, ({ request }) => responder(paginaActividadDTO, op, request)),
    http.get(`${DESEMPENO_BASE}/desempeno/usuarios/:uid/cuentas`, ({ request }) => responder(cuentasDTO, op, request)),
    http.get(`${DESEMPENO_BASE}/cobranza/pagos/:id/imagenes`, ({ request }) => responder(imagenesDTO, op, request)),
    http.get(`${DESEMPENO_BASE}/visitas/:id/imagenes`, ({ request }) => responder(imagenesDTO, op, request)),
  ];
}
```

- [ ] **Step 2: Pruebas del adapter, mappers y errores (fallan)**

`src/modules/desempeno/infrastructure/mappers/__tests__/dtoADominio.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { dtoToCuentas, dtoToJornada, dtoToPaginaActividad, dtoToTablero } from "../dtoADominio";
import { cuentasDTO, jornadaDTO, paginaActividadDTO, tableroDTO } from "../../../../../test/msw/fixtures/desempeno";
import { DomainError } from "../../../domain/errors";

describe("DTO del contrato → dominio", () => {
  it("tablero: usuario configurado y no configurado, sin totales del equipo", () => {
    const t = dtoToTablero(tableroDTO);
    expect(t.generadoAt).toBe("2026-10-14T22:40:00Z");
    expect(t.usuarios[0]).toMatchObject({
      uid: "abc123",
      zonaNombre: "RUTA 21",
      pagos: { n: 31, importe: "8600.00" },
      coberturaPorSegmento: { sinVisitar: 46, sinNada: 46 },
      cobro: { pct: "65.90" },
    });
    expect(t.usuarios[0].lineas[0].porHora).toHaveLength(13);
    expect(t.usuarios[0].lineas[0].pausas[0]).toMatchObject({ anteriorId: "visita:412cc2fc-7d0e-4b8a-9c1f-5e6a7b8c9d0e", enCurso: false });
    expect(t.usuarios[1]).toMatchObject({ configurado: false, motivoNoConfigurado: "sin_zona", zonaId: null, lineas: [] });
    expect(Object.keys(t)).toEqual(["generadoAt", "periodo", "pausaMin", "usuarios"]);
  });

  it("jornada: actividades con pago y visita, señales y resumen", () => {
    const j = dtoToJornada(jornadaDTO);
    expect(j.actividades[0].pago).toMatchObject({ formaCobro: "efectivo", pagoRecibidoId: "7a1f39e4-2b6c-4d1e-9f0a-3c5b8e2d1a47" });
    expect(j.actividades[1].visita).toMatchObject({ tipoVisita: "No se encontraba", estado: "no_estaba" });
    expect(j.actividades[1].registradoPor).toEqual({ usuarioUid: null, cobradorId: null, nombre: "Oficina", origen: "oficina" });
    expect(j.senales[0]).toMatchObject({ tipo: "mismo_punto", actividadIds: ["pago:15082369"] });
    expect(j.resumen.clientes).toBe(41);
  });

  it("actividad y cuentas", () => {
    const p = dtoToPaginaActividad(paginaActividadDTO);
    expect(p.siguienteCursor).toBe("opaco-2");
    expect(p.totales).toEqual({ n: 1188, importe: "289450.00", porTipo: { pago: 980, visita: 200, condonacion: 8 } });
    const c = dtoToCuentas(cuentasDTO);
    expect(c.conteos.sinNada).toBe(46);
    expect(c.cuentas[0]).toMatchObject({ trato: "pagado", determinadoPor: "pago:15082369", abonoSemana: "150.00" });
  });

  it("un código que el contrato no conoce es un error, no un dato", () => {
    const malo = { ...cuentasDTO, cuentas: [{ ...cuentasDTO.cuentas[0], trato: "deposito" }] };
    expect(() => dtoToCuentas(malo)).toThrow(DomainError);
    expect(() => dtoToCuentas(malo)).toThrow(/trato/);
  });
});
```

`src/modules/desempeno/infrastructure/mappers/__tests__/errorMapper.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { AxiosError, type AxiosResponse } from "axios";
import { apperrorToDomainError, normalizarErrorBlob } from "../errorMapper";

function errorHttp(status: number, data: unknown): AxiosError {
  return new AxiosError("falla", "ERR_BAD_REQUEST", undefined, undefined, {
    status,
    data,
    statusText: "",
    headers: {},
    config: {} as AxiosResponse["config"],
  } as AxiosResponse);
}

describe("apperrorToDomainError", () => {
  it("lee el código del formato Huma de msp-api (errors[].message = 'code=...')", () => {
    const e = apperrorToDomainError(
      errorHttp(409, {
        title: "Conflict",
        status: 409,
        detail: "el usuario no tiene zona asignada",
        errors: [{ message: "code=desempeno_usuario_sin_zona" }],
      }),
    );
    expect(e.code).toBe("desempeno_usuario_sin_zona");
    expect(e.message).toBe("el usuario no tiene zona asignada");
  });

  it("también acepta `code` plano (Problem Details)", () => {
    expect(apperrorToDomainError(errorHttp(422, { code: "desempeno_parametro_invalido", detail: "pausa_min fuera de rango" })).code).toBe(
      "desempeno_parametro_invalido",
    );
  });

  it("sin respuesta = error de red", () => {
    const e = apperrorToDomainError(new AxiosError("Network Error", "ERR_NETWORK"));
    expect(e.code).toBe("network_error");
  });
});

describe("normalizarErrorBlob", () => {
  it("convierte un cuerpo Blob con JSON en objeto para poder leer el código", async () => {
    const blob = new Blob([JSON.stringify({ detail: "no existe la foto", errors: [{ message: "code=not_found" }] })], {
      type: "application/json",
    });
    const e = apperrorToDomainError(await normalizarErrorBlob(errorHttp(404, blob)));
    expect(e.code).toBe("not_found");
    expect(e.message).toBe("no existe la foto");
  });
});
```

`src/modules/desempeno/infrastructure/http/HttpDesempenoAdapter.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import axios, { type AxiosInstance } from "axios";
import { server } from "../../../../test/msw/server";
import { desempenoHandlers } from "../../../../test/msw/handlers/desempeno";
import { HttpDesempenoAdapter } from "./HttpDesempenoAdapter";
import { FILTROS_ACTIVIDAD_INICIALES } from "../../domain/entities";
import { DomainError } from "../../domain/errors";

const adapter = () => new HttpDesempenoAdapter(axios.create({ baseURL: "http://api.test/v2" }));

describe("HttpDesempenoAdapter", () => {
  it("tablero: periodo, pausa_min y horario como parámetros", async () => {
    let url: URL | undefined;
    server.use(...desempenoHandlers({ vistoPor: (u) => (url = u) }));
    const t = await adapter().obtenerTablero({ periodo: "semana", pausaMin: 30, horario: { desde: "08:00", hasta: "18:00" } });
    expect(url?.pathname).toBe("/v2/desempeno/usuarios");
    expect(url?.searchParams.get("periodo")).toBe("semana");
    expect(url?.searchParams.get("pausa_min")).toBe("30");
    expect(url?.searchParams.get("desde_hora")).toBe("08:00");
    expect(url?.searchParams.get("hasta_hora")).toBe("18:00");
    expect(t.usuarios).toHaveLength(2);
  });

  it("jornada: fecha y sin horario no manda desde/hasta_hora", async () => {
    let url: URL | undefined;
    server.use(...desempenoHandlers({ vistoPor: (u) => (url = u) }));
    await adapter().obtenerJornada("abc 123", "2026-10-13", { pausaMin: 45, horario: null });
    expect(url?.pathname).toBe("/v2/desempeno/usuarios/abc%20123/jornada");
    expect(url?.searchParams.get("fecha")).toBe("2026-10-13");
    expect(url?.searchParams.has("desde_hora")).toBe(false);
  });

  it("actividad: arreglos repetidos sin corchetes, booleanos y paginación", async () => {
    let url: URL | undefined;
    server.use(...desempenoHandlers({ vistoPor: (u) => (url = u) }));
    const p = await adapter().listarActividad(
      "abc123",
      {
        ...FILTROS_ACTIVIDAD_INICIALES,
        tipos: ["pago", "condonacion"],
        registradoPor: ["abc123", "oficina"],
        conGps: false,
        q: "  andrade ",
        montoMin: "400",
      },
      { cursor: "opaco-1", limit: 100 },
    );
    expect(url?.searchParams.getAll("tipo")).toEqual(["pago", "condonacion"]);
    expect(url?.searchParams.getAll("registrado_por")).toEqual(["abc123", "oficina"]);
    expect(url?.searchParams.get("con_gps")).toBe("false");
    expect(url?.searchParams.has("con_fotos")).toBe(false);
    expect(url?.searchParams.get("q")).toBe("andrade");
    expect(url?.searchParams.get("monto_min")).toBe("400");
    expect(url?.searchParams.get("orden")).toBe("hora");
    expect(url?.searchParams.get("dir")).toBe("desc");
    expect(url?.searchParams.get("cursor")).toBe("opaco-1");
    expect(url?.searchParams.get("limit")).toBe("100");
    expect(p.totales.n).toBe(1188);
  });

  it("cuentas: segmento sin_nada", async () => {
    let url: URL | undefined;
    server.use(...desempenoHandlers({ vistoPor: (u) => (url = u) }));
    const c = await adapter().obtenerCuentas("abc123", { segmento: "sin_nada", q: "", orden: "atrasos", dir: "desc" });
    expect(url?.searchParams.get("segmento")).toBe("sin_nada");
    expect(url?.searchParams.has("q")).toBe(false);
    expect(c.conteos.total).toBe(232);
  });

  it("fotos: pago por pago_recibido_id y visita por visita_id", async () => {
    const vistas: string[] = [];
    server.use(...desempenoHandlers({ vistoPor: (u) => vistas.push(u.pathname) }));
    await adapter().listarImagenes({ tipo: "pago", pagoRecibidoId: "p-1" });
    const imgs = await adapter().listarImagenes({ tipo: "visita", visitaId: "v-1" });
    expect(vistas).toEqual(["/v2/cobranza/pagos/p-1/imagenes", "/v2/visitas/v-1/imagenes"]);
    expect(imgs[0].descripcion).toBeNull();
  });

  it("descarga de foto como blob", async () => {
    const blob = new Blob(["jpg"]);
    const get = vi.fn().mockResolvedValue({ data: blob });
    const a = new HttpDesempenoAdapter({ get } as unknown as AxiosInstance);
    await expect(a.descargarImagen({ tipo: "visita", visitaId: "v-1" }, "img-1")).resolves.toBe(blob);
    expect(get).toHaveBeenCalledWith("/visitas/v-1/imagenes/img-1", { responseType: "blob", signal: undefined });
  });

  it("errores del API llegan como DomainError con el código del contrato", async () => {
    server.use(
      ...desempenoHandlers({
        error: {
          status: 503,
          body: { title: "Service Unavailable", status: 503, detail: "no se pudo consultar a los usuarios", errors: [{ message: "code=desempeno_firestore_unavailable" }] },
        },
      }),
    );
    const e = await adapter().obtenerTablero({ periodo: "hoy", pausaMin: 45, horario: null }).catch((x: unknown) => x);
    expect(e).toBeInstanceOf(DomainError);
    expect((e as DomainError).code).toBe("desempeno_firestore_unavailable");
  });
});
```

- [ ] **Step 3: Correr y ver fallar**

Run: `npx vitest run src/modules/desempeno/infrastructure`
Expected: FAIL con `Failed to resolve import "../dtoADominio"` (y `dtos` en los fixtures).

- [ ] **Step 4: Implementar DTOs, parámetros, mappers, errores, cliente y adapter**

`src/modules/desempeno/infrastructure/http/dtos.ts`:

```ts
// DTOs de /v2/desempeno/* — nombres JSON EXACTOS del contrato
// (docs/superpowers/specs/2026-10-14-desempeno-contratos.md en msp-api).
// Los enums llegan como string y el mapper los valida.

export type ConteoDTO = { n: number; importe: string };

export type RegistradoPorDTO = {
  usuario_uid: string | null;
  cobrador_id: number | null;
  nombre: string;
  origen: string;
};

export type PagoActividadDTO = {
  importe: string;
  forma_cobro: string;
  forma_cobro_id: number;
  aplicado: boolean;
  rechazado: boolean;
  docto_cc_id: number | null;
  pago_recibido_id: string | null;
};

export type VisitaActividadDTO = {
  visita_id: string;
  tipo_visita: string;
  nota: string | null;
  estado: string;
};

export type ActividadDTO = {
  id: string;
  tipo: string;
  hora: string;
  recibido_at: string | null;
  cliente_id: number;
  cliente_nombre: string;
  venta_id: number | null;
  folio: string | null;
  registrado_por: RegistradoPorDTO;
  lat: number | null;
  lng: number | null;
  con_gps: boolean;
  num_fotos: number;
  pago: PagoActividadDTO | null;
  visita: VisitaActividadDTO | null;
};

export type PausaDTO = {
  desde: string;
  hasta: string;
  minutos: number;
  anterior_id: string | null;
  siguiente_id: string | null;
  en_curso: boolean;
};

export type TramoDTO = {
  desde: string;
  hasta: string;
  pagos: ConteoDTO;
  visitas: number;
  condonaciones: number;
  clientes: number;
};

export type HoraDTO = { hora: number; pagos: number; visitas: number; condonaciones: number; importe: string };

export type PuntoDTO = { id: string; tipo: string; hora: string };

export type LineaDelDiaDTO = {
  dia: string;
  puntos: PuntoDTO[];
  tramos: TramoDTO[];
  por_hora: HoraDTO[];
  pausas: PausaDTO[];
};

export type SenalDTO = {
  tipo: string;
  actividad_ids: string[];
  desde: string | null;
  hasta: string | null;
  detalle: string;
  regla: string;
};

export type ResumenPausasDTO = { n: number; minutos: number; en_curso: boolean };

export type UsuarioTableroDTO = {
  uid: string;
  nombre: string;
  email: string;
  zona_id: number | null;
  zona_nombre: string | null;
  configurado: boolean;
  motivo_no_configurado: string | null;
  inicio_semana: string | null;
  ventas_universo: number;
  estado_ahora: string;
  estado_desde: string | null;
  ultima: { id: string; tipo: string; hora: string; cliente_nombre: string } | null;
  primera_actividad: string | null;
  ultima_actividad: string | null;
  minutos_activos: number;
  pausas: ResumenPausasDTO;
  pagos: ConteoDTO;
  pagos_por_forma: { efectivo: ConteoDTO; transferencia: ConteoDTO; cheque: ConteoDTO };
  condonaciones: ConteoDTO;
  no_aplicados: ConteoDTO;
  visitas: number;
  cobertura: { con_algo: number; total: number };
  cobertura_por_segmento: { sin_visitar: number; volver: number; despues: number; pagados: number; sin_nada: number };
  cobro: { cobrado: string; esperado: string; pct: string | null };
  lineas: LineaDelDiaDTO[];
};

export type TableroDTO = {
  generado_at: string;
  periodo: string;
  pausa_min: number;
  usuarios: UsuarioTableroDTO[];
};

export type JornadaDTO = {
  uid: string;
  fecha: string;
  pausa_min: number;
  actividades: ActividadDTO[];
  linea: LineaDelDiaDTO;
  senales: SenalDTO[];
  resumen: {
    primera_actividad: string | null;
    ultima_actividad: string | null;
    minutos_activos: number;
    pausas: ResumenPausasDTO;
    pagos: ConteoDTO;
    visitas: number;
    condonaciones: number;
    clientes: number;
  };
};

export type PaginaActividadDTO = {
  items: ActividadDTO[];
  siguiente_cursor: string | null;
  totales: { n: number; importe: string; por_tipo: { pago: number; visita: number; condonacion: number } };
};

export type CuentaDTO = {
  venta_id: number;
  docto_pv_id: number | null;
  folio: string | null;
  cliente_id: number;
  cliente_nombre: string;
  producto: string | null;
  estado: string;
  trato: string;
  etiqueta: string;
  detalle: string;
  segmento: string | null;
  con_algo: boolean;
  determinado_por: string | null;
  ultima_actividad: { id: string; tipo: string; hora: string } | null;
  saldo: string;
  parcialidad: string;
  abono_semana: string;
  num_atrasos: number;
  nueva: boolean;
};

export type CuentasDTO = {
  inicio_semana: string;
  conteos: { sin_visitar: number; volver: number; despues: number; pagados: number; sin_nada: number; total: number };
  cuentas: CuentaDTO[];
};

// Misma forma que ImagenPagoDTO del API; `descripcion` puede omitirse.
export type ImagenDTO = {
  id: string;
  mime: string;
  size_bytes: number;
  descripcion?: string | null;
  created_at: string;
  updated_at: string;
};
```

`src/modules/desempeno/infrastructure/http/params.ts`:

```ts
import type { FiltrosActividad } from "../../domain/entities";
import type { ConsultaCuentas, ConsultaJornada } from "../../application/ports/DesempenoPort";

type Params = Record<string, string | number | string[]>;

export function paramsDeJornada(c: ConsultaJornada): Params {
  const p: Params = { pausa_min: c.pausaMin };
  if (c.horario) {
    p.desde_hora = c.horario.desde;
    p.hasta_hora = c.horario.hasta;
  }
  return p;
}

// Contrato §4. Arreglos vacíos y nulos no se mandan; los arreglos se repiten
// sin corchetes (tipo=pago&tipo=visita) con paramsSerializer { indexes: null }.
export function paramsDeActividad(f: FiltrosActividad): Params {
  const p: Params = { orden: f.orden, dir: f.dir };
  if (f.desde) p.desde = f.desde;
  if (f.hasta) p.hasta = f.hasta;
  if (f.tipos.length) p.tipo = [...f.tipos];
  if (f.formasCobro.length) p.forma_cobro = [...f.formasCobro];
  if (f.tiposVisita.length) p.tipo_visita = [...f.tiposVisita];
  if (f.registradoPor.length) p.registrado_por = [...f.registradoPor];
  if (f.montoMin !== null) p.monto_min = f.montoMin;
  if (f.montoMax !== null) p.monto_max = f.montoMax;
  if (f.conFotos !== null) p.con_fotos = String(f.conFotos);
  if (f.conGps !== null) p.con_gps = String(f.conGps);
  if (f.q.trim() !== "") p.q = f.q.trim();
  return p;
}

export function paramsDeCuentas(c: ConsultaCuentas): Params {
  const p: Params = { orden: c.orden, dir: c.dir };
  if (c.segmento) p.segmento = c.segmento;
  if (c.q.trim() !== "") p.q = c.q.trim();
  return p;
}
```

`src/modules/desempeno/infrastructure/mappers/dtoADominio.ts`:

```ts
import {
  FORMAS_COBRO,
  TIPOS_ACTIVIDAD,
  esEstadoAhora,
  esMotivoNoConfigurado,
  esPeriodo,
  esTipoSenal,
  type Actividad,
  type CuentasSemana,
  type ImagenAdjunta,
  type Jornada,
  type LineaDelDia,
  type PaginaActividad,
  type Senal,
  type Tablero,
  type UsuarioTablero,
} from "../../domain/entities";
import { esEstadoCuenta, esSegmento, esTrato } from "../../domain/values/estadoCuenta";
import { esUno } from "../../domain/values/esUno";
import { DomainError } from "../../domain/errors";
import type {
  ActividadDTO,
  CuentasDTO,
  ImagenDTO,
  JornadaDTO,
  LineaDelDiaDTO,
  PaginaActividadDTO,
  SenalDTO,
  TableroDTO,
  UsuarioTableroDTO,
} from "../http/dtos";

// Un código fuera del contrato es un error del servidor, no un dato: se falla
// fuerte en vez de pintar algo que nadie definió.
function exigir<T>(valor: unknown, guarda: (v: unknown) => v is T, campo: string): T {
  if (!guarda(valor)) {
    throw new DomainError("desempeno_respuesta_invalida", `respuesta inválida del servidor: ${campo}`);
  }
  return valor;
}

const esTipoActividad = (v: unknown) => esUno(TIPOS_ACTIVIDAD, v);
const esFormaCobro = (v: unknown) => esUno(FORMAS_COBRO, v);
const esOrigen = (v: unknown): v is "app" | "oficina" => v === "app" || v === "oficina";

export function dtoToActividad(d: ActividadDTO): Actividad {
  return {
    id: d.id,
    tipo: exigir(d.tipo, esTipoActividad, "actividad.tipo"),
    hora: d.hora,
    recibidoAt: d.recibido_at,
    clienteId: d.cliente_id,
    clienteNombre: d.cliente_nombre,
    ventaId: d.venta_id,
    folio: d.folio,
    registradoPor: {
      usuarioUid: d.registrado_por.usuario_uid,
      cobradorId: d.registrado_por.cobrador_id,
      nombre: d.registrado_por.nombre,
      origen: exigir(d.registrado_por.origen, esOrigen, "registrado_por.origen"),
    },
    lat: d.lat,
    lng: d.lng,
    conGps: d.con_gps,
    numFotos: d.num_fotos,
    pago: d.pago && {
      importe: d.pago.importe,
      formaCobro: exigir(d.pago.forma_cobro, esFormaCobro, "pago.forma_cobro"),
      formaCobroId: d.pago.forma_cobro_id,
      aplicado: d.pago.aplicado,
      rechazado: d.pago.rechazado,
      doctoCcId: d.pago.docto_cc_id,
      pagoRecibidoId: d.pago.pago_recibido_id,
    },
    visita: d.visita && {
      visitaId: d.visita.visita_id,
      tipoVisita: d.visita.tipo_visita,
      nota: d.visita.nota,
      estado: exigir(d.visita.estado, esEstadoCuenta, "visita.estado"),
    },
  };
}

export function dtoToLinea(d: LineaDelDiaDTO): LineaDelDia {
  return {
    dia: d.dia,
    puntos: d.puntos.map((p) => ({ id: p.id, tipo: exigir(p.tipo, esTipoActividad, "punto.tipo"), hora: p.hora })),
    tramos: d.tramos.map((t) => ({ ...t, pagos: { n: t.pagos.n, importe: t.pagos.importe } })),
    porHora: d.por_hora.map((h) => ({ ...h })),
    pausas: d.pausas.map((p) => ({
      desde: p.desde,
      hasta: p.hasta,
      minutos: p.minutos,
      anteriorId: p.anterior_id,
      siguienteId: p.siguiente_id,
      enCurso: p.en_curso,
    })),
  };
}

function dtoToSenal(d: SenalDTO): Senal {
  return {
    tipo: exigir(d.tipo, esTipoSenal, "senal.tipo"),
    actividadIds: [...d.actividad_ids],
    desde: d.desde,
    hasta: d.hasta,
    detalle: d.detalle,
    regla: d.regla,
  };
}

function dtoToUsuario(d: UsuarioTableroDTO): UsuarioTablero {
  return {
    uid: d.uid,
    nombre: d.nombre,
    email: d.email,
    zonaId: d.zona_id,
    zonaNombre: d.zona_nombre,
    configurado: d.configurado,
    motivoNoConfigurado:
      d.motivo_no_configurado === null ? null : exigir(d.motivo_no_configurado, esMotivoNoConfigurado, "motivo_no_configurado"),
    inicioSemana: d.inicio_semana,
    ventasUniverso: d.ventas_universo,
    estadoAhora: exigir(d.estado_ahora, esEstadoAhora, "estado_ahora"),
    estadoDesde: d.estado_desde,
    ultima: d.ultima && {
      id: d.ultima.id,
      tipo: exigir(d.ultima.tipo, esTipoActividad, "ultima.tipo"),
      hora: d.ultima.hora,
      clienteNombre: d.ultima.cliente_nombre,
    },
    primeraActividad: d.primera_actividad,
    ultimaActividad: d.ultima_actividad,
    minutosActivos: d.minutos_activos,
    pausas: { n: d.pausas.n, minutos: d.pausas.minutos, enCurso: d.pausas.en_curso },
    pagos: { ...d.pagos },
    pagosPorForma: {
      efectivo: { ...d.pagos_por_forma.efectivo },
      transferencia: { ...d.pagos_por_forma.transferencia },
      cheque: { ...d.pagos_por_forma.cheque },
    },
    condonaciones: { ...d.condonaciones },
    noAplicados: { ...d.no_aplicados },
    visitas: d.visitas,
    cobertura: { conAlgo: d.cobertura.con_algo, total: d.cobertura.total },
    coberturaPorSegmento: {
      sinVisitar: d.cobertura_por_segmento.sin_visitar,
      volver: d.cobertura_por_segmento.volver,
      despues: d.cobertura_por_segmento.despues,
      pagados: d.cobertura_por_segmento.pagados,
      sinNada: d.cobertura_por_segmento.sin_nada,
    },
    cobro: { cobrado: d.cobro.cobrado, esperado: d.cobro.esperado, pct: d.cobro.pct },
    lineas: d.lineas.map(dtoToLinea),
  };
}

export function dtoToTablero(d: TableroDTO): Tablero {
  return {
    generadoAt: d.generado_at,
    periodo: exigir(d.periodo, esPeriodo, "periodo"),
    pausaMin: d.pausa_min,
    usuarios: d.usuarios.map(dtoToUsuario),
  };
}

export function dtoToJornada(d: JornadaDTO): Jornada {
  return {
    uid: d.uid,
    fecha: d.fecha,
    pausaMin: d.pausa_min,
    actividades: d.actividades.map(dtoToActividad),
    linea: dtoToLinea(d.linea),
    senales: d.senales.map(dtoToSenal),
    resumen: {
      primeraActividad: d.resumen.primera_actividad,
      ultimaActividad: d.resumen.ultima_actividad,
      minutosActivos: d.resumen.minutos_activos,
      pausas: { n: d.resumen.pausas.n, minutos: d.resumen.pausas.minutos, enCurso: d.resumen.pausas.en_curso },
      pagos: { ...d.resumen.pagos },
      visitas: d.resumen.visitas,
      condonaciones: d.resumen.condonaciones,
      clientes: d.resumen.clientes,
    },
  };
}

export function dtoToPaginaActividad(d: PaginaActividadDTO): PaginaActividad {
  return {
    items: d.items.map(dtoToActividad),
    siguienteCursor: d.siguiente_cursor,
    totales: { n: d.totales.n, importe: d.totales.importe, porTipo: { ...d.totales.por_tipo } },
  };
}

export function dtoToCuentas(d: CuentasDTO): CuentasSemana {
  return {
    inicioSemana: d.inicio_semana,
    conteos: {
      sinVisitar: d.conteos.sin_visitar,
      volver: d.conteos.volver,
      despues: d.conteos.despues,
      pagados: d.conteos.pagados,
      sinNada: d.conteos.sin_nada,
      total: d.conteos.total,
    },
    cuentas: d.cuentas.map((c) => ({
      ventaId: c.venta_id,
      doctoPvId: c.docto_pv_id,
      folio: c.folio,
      clienteId: c.cliente_id,
      clienteNombre: c.cliente_nombre,
      producto: c.producto,
      estado: exigir(c.estado, esEstadoCuenta, "cuenta.estado"),
      trato: exigir(c.trato, esTrato, "cuenta.trato"),
      etiqueta: c.etiqueta,
      detalle: c.detalle,
      segmento: c.segmento === null ? null : exigir(c.segmento, esSegmento, "cuenta.segmento"),
      conAlgo: c.con_algo,
      determinadoPor: c.determinado_por,
      ultimaActividad: c.ultima_actividad && {
        id: c.ultima_actividad.id,
        tipo: exigir(c.ultima_actividad.tipo, esTipoActividad, "cuenta.ultima_actividad.tipo"),
        hora: c.ultima_actividad.hora,
      },
      saldo: c.saldo,
      parcialidad: c.parcialidad,
      abonoSemana: c.abono_semana,
      numAtrasos: c.num_atrasos,
      nueva: c.nueva,
    })),
  };
}

export function dtoToImagen(d: ImagenDTO): ImagenAdjunta {
  return {
    id: d.id,
    mime: d.mime,
    sizeBytes: d.size_bytes,
    descripcion: d.descripcion ?? null,
    createdAt: d.created_at,
    updatedAt: d.updated_at,
  };
}
```

`src/modules/desempeno/infrastructure/mappers/errorMapper.ts`:

```ts
import axios from "axios";
import { DomainError } from "../../domain/errors";

// Mensajes propios solo para lo que el servidor no explica bien. Lo demás usa
// el `detail` en español que manda msp-api.
const MENSAJES: Record<string, string> = {
  network_error: "no hay conexión con el servidor",
  timeout: "el servidor tardó demasiado en responder",
};

// msp-api manda los errores de Huma como { detail, errors: [{ message: "code=..." }] }
// (internal/rutas/infra/rutashttp/auth.go:mapAppError). También se acepta
// `code` plano (Problem Details) y "code: mensaje".
function codigoHuma(data: Record<string, unknown>): string | null {
  if (!Array.isArray(data.errors)) return null;
  for (const e of data.errors) {
    const m = e && typeof e === "object" ? (e as { message?: unknown }).message : null;
    if (typeof m !== "string") continue;
    const igual = /^code=([a-z0-9_]+)$/.exec(m);
    if (igual) return igual[1];
    const dosPuntos = /^([a-z0-9_]+):/.exec(m);
    if (dosPuntos) return dosPuntos[1];
  }
  return null;
}

export function apperrorToDomainError(err: unknown): DomainError {
  if (err instanceof DomainError) return err;
  if (axios.isAxiosError(err)) {
    if (!err.response) {
      const code = err.code === "ECONNABORTED" ? "timeout" : "network_error";
      return new DomainError(code, MENSAJES[code]);
    }
    const data = (err.response.data ?? {}) as Record<string, unknown>;
    const code =
      (typeof data.code === "string" && data.code) || codigoHuma(data) || `http_${err.response.status}`;
    const mensaje =
      (typeof data.detail === "string" && data.detail) ||
      (typeof data.message === "string" && data.message) ||
      (typeof data.title === "string" && data.title) ||
      MENSAJES[code] ||
      "error del servidor";
    return new DomainError(code, mensaje);
  }
  if (err instanceof Error) return new DomainError("error_inesperado", err.message);
  return new DomainError("error_inesperado", String(err));
}

// Con responseType "blob" el cuerpo de error también llega como Blob; se lee
// (FileReader: jsdom no trae Blob.text) y se convierte a JSON para poder leer
// el código. Si no es JSON, se deja como está.
export async function normalizarErrorBlob(err: unknown): Promise<unknown> {
  if (!axios.isAxiosError(err) || !err.response || !(err.response.data instanceof Blob)) return err;
  try {
    const texto = await new Promise<string>((resolve, reject) => {
      const lector = new FileReader();
      lector.onload = () => resolve(String(lector.result ?? ""));
      lector.onerror = () => reject(lector.error);
      lector.readAsText(err.response!.data as Blob);
    });
    err.response.data = JSON.parse(texto);
  } catch {
    // Cuerpo no JSON: se conserva el Blob y el código cae a http_<status>.
  }
  return err;
}
```

`src/modules/desempeno/infrastructure/http/apiClient.ts`:

```ts
import axios, { type AxiosInstance } from "axios";
import { URL_API_V2 } from "../../../../constants/api";
import { auth } from "../../../../../firebase";

export const apiClient: AxiosInstance = axios.create({ baseURL: `${URL_API_V2}/v2` });

apiClient.interceptors.request.use(async (config) => {
  // Igual que rutas: esperar a que Firebase restaure la sesión antes de leer el
  // token; sin esto, el primer fetch tras recargar sale sin token (401).
  await auth.authStateReady();
  const token = await auth.currentUser?.getIdToken();
  if (token) {
    config.headers = config.headers ?? {};
    (config.headers as Record<string, string>).Authorization = `Bearer ${token}`;
  }
  return config;
});
```

`src/modules/desempeno/infrastructure/http/HttpDesempenoAdapter.ts`:

```ts
import type { AxiosInstance } from "axios";
import type {
  ConsultaCuentas,
  ConsultaJornada,
  ConsultaTablero,
  DesempenoPort,
  PaginaSolicitada,
} from "../../application/ports/DesempenoPort";
import type {
  CuentasSemana,
  FiltrosActividad,
  ImagenAdjunta,
  Jornada,
  PaginaActividad,
  RefImagenes,
  Tablero,
} from "../../domain/entities";
import type { CuentasDTO, ImagenDTO, JornadaDTO, PaginaActividadDTO, TableroDTO } from "./dtos";
import { paramsDeActividad, paramsDeCuentas, paramsDeJornada } from "./params";
import { dtoToCuentas, dtoToImagen, dtoToJornada, dtoToPaginaActividad, dtoToTablero } from "../mappers/dtoADominio";
import { apperrorToDomainError, normalizarErrorBlob } from "../mappers/errorMapper";

const USUARIOS = "/desempeno/usuarios";
const SERIALIZAR = { indexes: null } as const;

function rutaImagenes(ref: RefImagenes): string {
  return ref.tipo === "pago"
    ? `/cobranza/pagos/${encodeURIComponent(ref.pagoRecibidoId)}/imagenes`
    : `/visitas/${encodeURIComponent(ref.visitaId)}/imagenes`;
}

// HttpDesempenoAdapter: implementación de producción de DesempenoPort contra
// /v2/desempeno/* y las fotos de cobranza/visitas. Todo error sale como
// DomainError con el código del contrato.
export class HttpDesempenoAdapter implements DesempenoPort {
  constructor(private readonly client: AxiosInstance) {}

  async obtenerTablero(c: ConsultaTablero, signal?: AbortSignal): Promise<Tablero> {
    try {
      const { data } = await this.client.get<TableroDTO>(USUARIOS, {
        params: { periodo: c.periodo, ...paramsDeJornada(c) },
        paramsSerializer: SERIALIZAR,
        signal,
      });
      return dtoToTablero(data);
    } catch (e) {
      throw apperrorToDomainError(e);
    }
  }

  async obtenerJornada(uid: string, fecha: string, c: ConsultaJornada, signal?: AbortSignal): Promise<Jornada> {
    try {
      const { data } = await this.client.get<JornadaDTO>(`${USUARIOS}/${encodeURIComponent(uid)}/jornada`, {
        params: { fecha, ...paramsDeJornada(c) },
        paramsSerializer: SERIALIZAR,
        signal,
      });
      return dtoToJornada(data);
    } catch (e) {
      throw apperrorToDomainError(e);
    }
  }

  async listarActividad(
    uid: string,
    filtros: FiltrosActividad,
    pagina: PaginaSolicitada,
    signal?: AbortSignal,
  ): Promise<PaginaActividad> {
    try {
      const params: Record<string, string | number | string[]> = { ...paramsDeActividad(filtros), limit: pagina.limit };
      if (pagina.cursor) params.cursor = pagina.cursor;
      const { data } = await this.client.get<PaginaActividadDTO>(`${USUARIOS}/${encodeURIComponent(uid)}/actividad`, {
        params,
        paramsSerializer: SERIALIZAR,
        signal,
      });
      return dtoToPaginaActividad(data);
    } catch (e) {
      throw apperrorToDomainError(e);
    }
  }

  async obtenerCuentas(uid: string, c: ConsultaCuentas, signal?: AbortSignal): Promise<CuentasSemana> {
    try {
      const { data } = await this.client.get<CuentasDTO>(`${USUARIOS}/${encodeURIComponent(uid)}/cuentas`, {
        params: paramsDeCuentas(c),
        paramsSerializer: SERIALIZAR,
        signal,
      });
      return dtoToCuentas(data);
    } catch (e) {
      throw apperrorToDomainError(e);
    }
  }

  async listarImagenes(ref: RefImagenes, signal?: AbortSignal): Promise<ImagenAdjunta[]> {
    try {
      const { data } = await this.client.get<ImagenDTO[]>(rutaImagenes(ref), { signal });
      return data.map(dtoToImagen);
    } catch (e) {
      throw apperrorToDomainError(e);
    }
  }

  async descargarImagen(ref: RefImagenes, imagenId: string, signal?: AbortSignal): Promise<Blob> {
    try {
      const { data } = await this.client.get<Blob>(`${rutaImagenes(ref)}/${encodeURIComponent(imagenId)}`, {
        responseType: "blob",
        signal,
      });
      return data;
    } catch (e) {
      throw apperrorToDomainError(await normalizarErrorBlob(e));
    }
  }
}
```

- [ ] **Step 5: Correr, tipos y lint**

Run: `npx vitest run src/modules/desempeno/infrastructure && npx tsc --noEmit && npx eslint src/modules/desempeno src/test/msw/handlers/desempeno.ts src/test/msw/fixtures/desempeno.ts --max-warnings 0`
Expected: PASS (4 + 4 + 7 = 15 pruebas), sin salida de tsc/eslint.

- [ ] **Step 6: Commit**

```bash
git add src/modules/desempeno/infrastructure src/test/msw/handlers/desempeno.ts src/test/msw/fixtures/desempeno.ts
git commit -m "feat(desempeno): adapter HTTP contra /v2/desempeno con DTOs del contrato, errores Huma y MSW"
```

---

## INTEGRACIÓN (rama `feat/desempeno`, cuando terminan los 3 carriles)

### Tarea 4 (integración): unir carriles, Inicio detrás de `DESEMPENO_COBRADORES`, retirar Home viejo (D3)

**Rama `feat/desempeno` en el repo principal, cuando los 3 carriles terminaron.** Es la única tarea que toca `App.tsx`, `constants/modules.ts`, `utils/permissions.ts` y `AppSidebar.tsx`.

**Files:**
- Create: `src/modules/desempeno/presentation/composition/DesempenoContainer.tsx`
- Create: `src/modules/desempeno/components/DesempenoScreen.tsx`
- Create: `src/modules/desempeno/Desempeno.tsx`
- Create: `src/modules/desempeno/index.ts`
- Create: `src/modules/inicio/Inicio.tsx`
- Create: `src/modules/inicio/InicioSencillo.tsx`
- Modify: `src/constants/modules.ts` (módulo `DESEMPENO_COBRADORES`)
- Modify: `src/utils/permissions.ts` (`modulosParaMenu`)
- Modify: `src/utils/permissions.test.ts` (13 → 14 interruptores; pruebas de `modulosParaMenu`)
- Modify: `src/components/AppSidebar.tsx:96-98`
- Modify: `src/App.tsx:9` y `src/App.tsx:68`
- Delete: `src/modules/home/Home.tsx`
- Delete: `src/services/api/getPagosAndVisitas.ts`
- Test: `src/modules/desempeno/components/__tests__/DesempenoScreen.test.tsx`
- Test: `src/modules/inicio/__tests__/Inicio.test.tsx`

**Interfaces:**
- Consumes: Carril 1 (`TableroScreen`, `useTablero`, `LineaActividad`, `SelectorVista`, `Leyenda`), Carril 2 (`FichaScreen`, `esPestana`, `PestanaFicha`, `describirActividades`), Carril 3 (`HttpDesempenoAdapter`, `apiClient`, `ModalesFicha`, `ModalFicha`, `CeldaFotos`, `ClientesContainer` de `@/modules/clientes`), Tarea 0 (`DesempenoProvider`, `usePreferencias`, formato, `EstadoVacio`, `ErrorConReintento`).
- Produces: `Desempeno({ puedeConfigurar })` (exportado por `src/modules/desempeno/index.ts`), `Inicio` (default), `InicioSencillo`, `modulosParaMenu(modulos)`.

- [ ] **Step 1: Unir los carriles a `feat/desempeno`**

```bash
cd /Volumes/M2-1TB/Developer/sistema-cobro-web
git checkout feat/desempeno
git merge --no-ff feat/desempeno-modales -m "merge: carril 3 de desempeño (modales, fotos, adapter HTTP)"
git merge --no-ff feat/desempeno-tablero -m "merge: carril 1 de desempeño (tablero y línea de actividad)"
git merge --no-ff feat/desempeno-ficha -m "merge: carril 2 de desempeño (ficha: jornada, actividad, cuentas)"
npm install
npx vitest run src/modules/desempeno src/modules/clientes src/modules/ventasLocales && npx tsc --noEmit
```

Expected: tres merges sin conflictos (los carriles no comparten archivos) y la suite de los tres módulos en verde. Si `npm install` cambia `package-lock.json`, es porque un worktree instaló algo distinto: revertir con `git checkout package-lock.json` (ningún carril agrega dependencias; la única nueva es la de Tarea 0).

- [ ] **Step 2: Pruebas de la pantalla integrada y del Inicio (fallan)**

`src/modules/desempeno/components/__tests__/DesempenoScreen.test.tsx`:

```tsx
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { DesempenoScreen } from "../DesempenoScreen";
import { DesempenoProvider } from "../../presentation/context/DesempenoProvider";
import { FakeDesempenoPort } from "../../application/__tests__/fakeDesempenoPort";

vi.mock("@/modules/clientes", () => ({
  PagoModal: (p: { clienteId: number; doctoCcId: number }) => <div data-testid="pago-modal">{`${p.clienteId}-${p.doctoCcId}`}</div>,
  VentaModal: (p: { clienteId: number; doctoPvId: number }) => <div data-testid="venta-modal">{`${p.clienteId}-${p.doctoPvId}`}</div>,
  ClienteFicha: (p: { clienteId: number }) => <div data-testid="cliente-ficha">{p.clienteId}</div>,
}));
vi.mock("@/modules/ventasLocales/components/detalle/AuthenticatedImage", () => ({
  default: ({ alt }: { alt?: string }) => <img alt={alt ?? "imagen"} />,
}));

function Ubicacion() {
  const l = useLocation();
  return <div data-testid="ubicacion">{`${l.pathname}${l.search}`}</div>;
}

function montar(ruta = "/", port = new FakeDesempenoPort()) {
  render(
    <MemoryRouter initialEntries={[ruta]}>
      <DesempenoProvider port={port}>
        <DesempenoScreen puedeConfigurar />
      </DesempenoProvider>
      <Ubicacion />
    </MemoryRouter>,
  );
  return port;
}

describe("DesempenoScreen (carriles conectados)", () => {
  beforeEach(() => localStorage.clear());

  it("tablero → ficha por la URL y de regreso con Equipo", async () => {
    const user = userEvent.setup();
    montar();
    await user.click(await screen.findByText("$8,600"));
    expect(screen.getByTestId("ubicacion")).toHaveTextContent("/?usuario=uid-juan");
    expect(await screen.findByRole("heading", { name: "Juan Carlos Méndez" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Equipo/ }));
    expect(screen.getByTestId("ubicacion")).toHaveTextContent(/^\/$/);
  });

  it("la jornada recibe la línea del carril 1, la vista y las fotos del carril 3", async () => {
    const user = userEvent.setup();
    montar("/?usuario=uid-juan&dia=2026-10-14");
    expect(await screen.findByTestId("linea-actividad")).toHaveAttribute("data-vista", "bloques");
    expect(screen.getByRole("button", { name: "Bloques de trabajo" })).toBeInTheDocument();
    // La celda de fotos conectada es CeldaFotos (carril 3): su popover dice "1 foto".
    const renglon = (await screen.findByText("PEDRO TEHUITZIL ZEPEDA")).closest("li")!;
    await user.hover(within(renglon).getByText("1"));
    expect(await screen.findByText("1 foto")).toBeInTheDocument();
  });

  it("un pago abre el PagoModal de Clientes y una visita el modal de visita", async () => {
    const user = userEvent.setup();
    montar("/?usuario=uid-juan&dia=2026-10-14");
    await user.click(await screen.findByText("JUANA ANDRADE LECHUGA"));
    expect(screen.getByTestId("pago-modal")).toHaveTextContent("1544855-15082369");
    await user.click(screen.getByText("MARÍA ELENA CUAUTLE TLAPA"));
    expect(await screen.findByText("Visitas")).toBeInTheDocument();
  });

  it("Cuentas: la pestaña va en la URL y la cuenta abre el VentaModal", async () => {
    const user = userEvent.setup();
    montar("/?usuario=uid-juan&dia=2026-10-14");
    await user.click(await screen.findByRole("tab", { name: /Cuentas de la semana/ }));
    expect(screen.getByTestId("ubicacion")).toHaveTextContent("pestana=cuentas");
    await user.click(await screen.findByText("GERARDO XICALE ANALCO"));
    expect(screen.getByTestId("venta-modal")).toHaveTextContent("1610045-9950");
  });

  it("Actividad: 'Registró' ofrece solo usuarios de la misma zona y Oficina", async () => {
    montar("/?usuario=uid-juan&pestana=actividad");
    const select = await screen.findByLabelText("Registró");
    expect(within(select).getByRole("option", { name: "Juan Carlos Méndez" })).toBeInTheDocument();
    expect(within(select).getByRole("option", { name: "Oficina" })).toBeInTheDocument();
    expect(within(select).queryByRole("option", { name: "Rosa Isela Pacheco" })).not.toBeInTheDocument();
  });

  it("usuario inexistente o sin configurar", async () => {
    montar("/?usuario=uid-nadie");
    expect(await screen.findByText("No encontramos a este usuario")).toBeInTheDocument();
  });

  it("usuario sin inicio de semana: explica por qué no hay ficha", async () => {
    montar("/?usuario=uid-norma");
    expect(await screen.findByText(/No tiene inicio de semana/)).toBeInTheDocument();
  });
});
```

`src/modules/inicio/__tests__/Inicio.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { Contact, Route } from "lucide-react";
import Inicio from "../Inicio";

const permisos = { modulos: [] as string[] };

vi.mock("@/hooks/usePermissions", () => ({
  usePermissions: () => ({
    canAccessModule: (k: string) => permisos.modulos.includes(k),
    getAvailableModulesWithConfig: () => [
      { key: "HOME", label: "Inicio", path: "/" },
      { key: "CLIENTES", label: "Clientes", path: "/clientes", icon: Contact },
      { key: "RUTAS", label: "Rutas", path: "/rutas", icon: Route },
    ],
  }),
}));
vi.mock("@/hooks/useAuth", () => ({ useAuth: () => ({ userData: { NOMBRE: "Martha Ríos Coyotl" } }) }));
vi.mock("@/modules/desempeno", () => ({
  Desempeno: ({ puedeConfigurar }: { puedeConfigurar: boolean }) => (
    <div data-testid="desempeno">{`configurar:${puedeConfigurar}`}</div>
  ),
}));

describe("Inicio (D9)", () => {
  it("con el permiso, Inicio es Desempeño de cobradores", () => {
    permisos.modulos = ["DESEMPENO_COBRADORES", "USUARIOS"];
    render(<MemoryRouter><Inicio /></MemoryRouter>);
    expect(screen.getByTestId("desempeno")).toHaveTextContent("configurar:true");
  });

  it("sin el permiso, un inicio sencillo con sus pantallas", () => {
    permisos.modulos = [];
    render(<MemoryRouter><Inicio /></MemoryRouter>);
    expect(screen.queryByTestId("desempeno")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Hola, Martha" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Clientes" })).toHaveAttribute("href", "/clientes");
    expect(screen.queryByRole("link", { name: "Inicio" })).not.toBeInTheDocument();
  });
});
```

En `src/utils/permissions.test.ts`:
- agregar `modulosParaMenu` al import de `./permissions`;
- reemplazar el caso `"cubre todas las pantallas salvo Inicio — hoy son 13"` por:

```ts
  it("cubre todas las pantallas salvo Inicio — hoy son 14 (con Desempeño de cobradores)", () => {
    const esperados = DESKTOP_MODULES.filter((m) => m.key !== "HOME").map((m) => m.key);

    expect(desktopModules.map((m) => m.key)).toEqual(esperados);
    expect(desktopModules).toHaveLength(14);
    expect(desktopModules.map((m) => m.key)).toContain("DESEMPENO_COBRADORES");
  });
```

- y agregar al final del archivo:

```ts
describe("modulosParaMenu", () => {
  const home = { key: "HOME", label: "Inicio", path: "/" };
  const desempeno = { key: "DESEMPENO_COBRADORES", label: "Desempeño de cobradores", path: "/" };
  const clientes = { key: "CLIENTES", label: "Clientes", path: "/clientes" };

  it("con Desempeño, Inicio no se repite en el menú (las dos apuntan a /)", () => {
    expect(modulosParaMenu([home, desempeno, clientes]).map((m) => m.key)).toEqual(["DESEMPENO_COBRADORES", "CLIENTES"]);
  });

  it("sin Desempeño, Inicio se queda", () => {
    expect(modulosParaMenu([home, clientes]).map((m) => m.key)).toEqual(["HOME", "CLIENTES"]);
  });

  it("Desempeño exige su interruptor (no es público)", () => {
    expect(canUserAccessModule(usuario(ROLES.ADMIN), "DESEMPENO_COBRADORES")).toBe(false);
    expect(canUserAccessModule(usuario(ROLES.ADMIN, ["DESEMPENO_COBRADORES"]), "DESEMPENO_COBRADORES")).toBe(true);
  });
});
```

(`usuario(...)` y `ROLES` ya existen en `permissions.test.ts`; si `canUserAccessModule` no está en el import, agrégalo.)

- [ ] **Step 3: Correr y ver fallar**

Run: `npx vitest run src/modules/desempeno/components/__tests__/DesempenoScreen.test.tsx src/modules/inicio src/utils/permissions.test.ts`
Expected: FAIL (`../DesempenoScreen`, `../Inicio` no existen; `modulosParaMenu` no exportado; 13 ≠ 14).

- [ ] **Step 4: Implementar la integración del módulo**

`src/modules/desempeno/presentation/composition/DesempenoContainer.tsx`:

```tsx
import { useMemo, type ReactNode } from "react";
import { apiClient } from "../../infrastructure/http/apiClient";
import { HttpDesempenoAdapter } from "../../infrastructure/http/HttpDesempenoAdapter";
import { DesempenoProvider } from "../context/DesempenoProvider";

// Raíz de composición: el adapter HTTP real una sola vez para todo el subárbol.
export function DesempenoContainer({ children }: { children: ReactNode }) {
  const port = useMemo(() => new HttpDesempenoAdapter(apiClient), []);
  return <DesempenoProvider port={port}>{children}</DesempenoProvider>;
}
```

`src/modules/desempeno/components/DesempenoScreen.tsx`:

```tsx
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router-dom";
import type { Actividad, Cuenta, Jornada } from "../domain/entities";
import { useTablero } from "../presentation/hooks/useTablero";
import { usePreferencias } from "../presentation/context/usePreferencias";
import { hoyDeNegocio } from "./lib/format";
import { ErrorConReintento, EstadoVacio } from "./comunes/Estados";
import { TableroScreen } from "./tablero/TableroScreen";
import { LineaActividad } from "./linea/LineaActividad";
import { SelectorVista } from "./linea/SelectorVista";
import { Leyenda } from "./linea/Leyenda";
import { FichaScreen } from "./ficha/FichaScreen";
import { esPestana, type PestanaFicha } from "./ficha/pestanas";
import { describirActividades } from "./jornada/describirActividades";
import { CeldaFotos } from "./modales/CeldaFotos";
import { ModalesFicha, type ModalFicha } from "./modales/ModalesFicha";

const FORMATO_DIA = /^\d{4}-\d{2}-\d{2}$/;
const MOTIVO = {
  sin_zona: "No tiene zona asignada. Sin zona no se puede medir su jornada ni sus cuentas.",
  sin_inicio_semana: "No tiene inicio de semana. Sin él no se puede medir su jornada ni sus cuentas.",
} as const;

// Pantalla del módulo: tablero o ficha según `?usuario=`. La pestaña y el día
// también van en la URL (`pestana`, `dia`), así el botón "atrás" funciona.
export function DesempenoScreen({ puedeConfigurar }: { puedeConfigurar: boolean }) {
  const [params, setParams] = useSearchParams();
  const uid = params.get("usuario");
  if (!uid) {
    return <TableroScreen onAbrirUsuario={(u) => setParams({ usuario: u })} puedeConfigurar={puedeConfigurar} />;
  }
  return <FichaConectada uid={uid} />;
}

function FichaConectada({ uid }: { uid: string }) {
  const [params, setParams] = useSearchParams();
  const { tablero, isLoading, error, refresh } = useTablero();
  const { vista, horario } = usePreferencias();
  const [pila, setPila] = useState<ModalFicha[]>([]);
  const hoy = hoyDeNegocio();
  const crudaPestana = params.get("pestana");
  const pestana: PestanaFicha = esPestana(crudaPestana) ? crudaPestana : "jornada";
  const crudoDia = params.get("dia") ?? "";
  const dia = FORMATO_DIA.test(crudoDia) && crudoDia <= hoy ? crudoDia : hoy;

  const cambiarParam = useCallback(
    (clave: string, valor: string) =>
      setParams((previos) => {
        const siguientes = new URLSearchParams(previos);
        siguientes.set(clave, valor);
        return siguientes;
      }),
    [setParams],
  );
  const abrir = useCallback((m: ModalFicha) => setPila((p) => [...p, m]), []);
  const volver = () => setParams({});

  if (!tablero) {
    if (error) return <ErrorConReintento titulo="No se pudo cargar la ficha" mensaje={error.message} onReintentar={refresh} />;
    return <div className="m-10 h-48 animate-pulse rounded-[14px] bg-dsm-tarjeta" />;
  }
  const usuario = tablero.usuarios.find((u) => u.uid === uid);
  if (!usuario || !usuario.configurado) {
    return (
      <div className="flex flex-col items-center gap-3 p-10">
        <EstadoVacio
          titulo={usuario ? usuario.nombre : "No encontramos a este usuario"}
          detalle={usuario ? MOTIVO[usuario.motivoNoConfigurado ?? "sin_zona"] : "Puede que ya no tenga acceso o cambió de zona."}
        />
        <button type="button" onClick={volver} className="h-9 rounded-full px-4 text-[13px] ring-1 ring-border">
          Volver al equipo
        </button>
      </div>
    );
  }

  const opcionesRegistro = tablero.usuarios
    .filter((u) => u.configurado && u.zonaId === usuario.zonaId)
    .map((u) => ({ valor: u.uid, etiqueta: u.nombre }));

  return (
    <>
      <FichaScreen
        usuario={usuario}
        periodo={tablero.periodo}
        generadoAt={tablero.generadoAt}
        opcionesRegistro={opcionesRegistro}
        pestana={pestana}
        dia={dia}
        hoy={hoy}
        onCambiarPestana={(p) => cambiarParam("pestana", p)}
        onCambiarDia={(d) => cambiarParam("dia", d)}
        onVolver={volver}
        onAbrirActividad={(a: Actividad) => abrir({ tipo: "actividad", actividad: a })}
        onAbrirCuenta={(c: Cuenta) => {
          if (c.doctoPvId !== null) abrir({ tipo: "venta", clienteId: c.clienteId, doctoPvId: c.doctoPvId });
        }}
        renderFotos={(a) => <CeldaFotos actividad={a} />}
        renderLinea={(j: Jornada, ahora) => (
          <LineaActividad
            linea={j.linea}
            vista={vista}
            pausaMin={j.pausaMin}
            horario={horario}
            ahora={ahora}
            tamano="grande"
            describir={describirActividades(j.actividades)}
          />
        )}
        controlesLinea={
          <>
            <SelectorVista />
            <Leyenda vista={vista} />
          </>
        }
      />
      <ModalesFicha pila={pila} onCerrar={() => setPila((p) => p.slice(0, -1))} onAbrir={abrir} />
    </>
  );
}
```

`src/modules/desempeno/Desempeno.tsx`:

```tsx
import { ClientesContainer } from "@/modules/clientes";
import { DesempenoContainer } from "./presentation/composition/DesempenoContainer";
import { DesempenoScreen } from "./components/DesempenoScreen";

// Entrada del módulo. ClientesContainer da contexto a PagoModal, VentaModal y
// ClienteFicha (D11).
export function Desempeno({ puedeConfigurar }: { puedeConfigurar: boolean }) {
  return (
    <DesempenoContainer>
      <ClientesContainer>
        <DesempenoScreen puedeConfigurar={puedeConfigurar} />
      </ClientesContainer>
    </DesempenoContainer>
  );
}
```

`src/modules/desempeno/index.ts`:

```ts
export { Desempeno } from "./Desempeno";
```

`src/modules/inicio/InicioSencillo.tsx`:

```tsx
import { Link } from "react-router-dom";
import { useAuth } from "@/hooks/useAuth";
import { usePermissions } from "@/hooks/usePermissions";

// Inicio para quien no tiene "Desempeño de cobradores" (D9).
export function InicioSencillo() {
  const { userData } = useAuth();
  const { getAvailableModulesWithConfig } = usePermissions();
  const modulos = getAvailableModulesWithConfig().filter((m) => m.path !== "/");
  const nombre = userData?.NOMBRE?.split(" ")[0];
  return (
    <section className="flex flex-col gap-6 px-10 pt-9">
      <div>
        <h1 className="font-serif text-4xl">{nombre ? `Hola, ${nombre}` : "Inicio"}</h1>
        <p className="mt-1 text-sm text-muted-foreground">Elige a dónde ir</p>
      </div>
      {modulos.length === 0 ? (
        <p className="text-sm text-muted-foreground">No tienes pantallas asignadas. Pide acceso a un administrador.</p>
      ) : (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-3">
          {modulos.map((m) => (
            <li key={m.key}>
              <Link to={m.path} className="flex h-14 items-center gap-3 rounded-xl border border-border px-4 text-sm hover:bg-muted">
                {m.icon && <m.icon aria-hidden className="h-4 w-4 text-muted-foreground" />}
                {m.label}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
```

`src/modules/inicio/Inicio.tsx`:

```tsx
import { usePermissions } from "@/hooks/usePermissions";
import { Desempeno } from "@/modules/desempeno";
import { InicioSencillo } from "./InicioSencillo";

// "/" (D9): con el interruptor DESEMPENO_COBRADORES, la pantalla de desempeño;
// sin él, un inicio sencillo. "Configurar" del tablero lleva a Usuarios
// (/settings), por eso pide el módulo USUARIOS.
export default function Inicio() {
  const { canAccessModule } = usePermissions();
  if (canAccessModule("DESEMPENO_COBRADORES")) {
    return <Desempeno puedeConfigurar={canAccessModule("USUARIOS")} />;
  }
  return <InicioSencillo />;
}
```

- [ ] **Step 5: Permisos, menú y rutas de la app**

En `src/constants/modules.ts`:
- agregar `Activity` al import de `lucide-react`;
- insertar, inmediatamente después del objeto `HOME` dentro de `DESKTOP_MODULES`:

```ts
  {
    // D9: con este interruptor, "/" es Desempeño de cobradores. Comparte la
    // ruta con Inicio; el menú muestra solo uno (modulosParaMenu).
    key: 'DESEMPENO_COBRADORES',
    label: 'Desempeño de cobradores',
    path: '/',
    icon: Activity,
    color: 'blue'
  },
```

(`ROUTE_TO_MODULE['/']` se queda en `'HOME'`: la ruta es pública y `Inicio` decide qué pintar.)

En `src/utils/permissions.ts`:
- agregar `import type { ModuleConfig } from '../types/auth';` (junto al import existente de `UserData, RoleType`, o en esa misma línea);
- al final del archivo:

```ts
/**
 * Módulos que pinta el menú lateral. Con "Desempeño de cobradores", Inicio ES
 * esa pantalla (las dos apuntan a "/"): no se pintan dos entradas iguales.
 */
export const modulosParaMenu = (modulos: ModuleConfig[]): ModuleConfig[] => {
  const conDesempeno = modulos.some((m) => m.key === 'DESEMPENO_COBRADORES');
  return conDesempeno ? modulos.filter((m) => m.key !== 'HOME') : modulos;
};
```

En `src/components/AppSidebar.tsx`:
- agregar `import { modulosParaMenu } from "@/utils/permissions"`;
- cambiar `const modules = getAvailableModulesWithConfig()` por `const modules = modulosParaMenu(getAvailableModulesWithConfig())`.

En `src/App.tsx`:
- reemplazar `import Home from "./modules/home/Home";` por `import Inicio from "./modules/inicio/Inicio";`;
- reemplazar `<Route path="/" element={<Home />} />` por `<Route path="/" element={<Inicio />} />`.

Retirar el Inicio viejo y el servicio Node sin token:

```bash
git rm src/modules/home/Home.tsx src/services/api/getPagosAndVisitas.ts
```

- [ ] **Step 6: Comprobar que no queda ningún consumidor del Home viejo**

Run: `grep -rn "modules/home\|getPagosAndVisitas\|pagos-visitas" src || echo "sin referencias"`
Expected: `sin referencias`. (Control positivo: el mismo `grep` antes del Step 5 encuentra `src/App.tsx:9` y `src/modules/home/Home.tsx:27`.)

- [ ] **Step 7: Correr las pruebas de la tarea**

Run: `npx vitest run src/modules/desempeno/components/__tests__/DesempenoScreen.test.tsx src/modules/inicio src/utils/permissions.test.ts`
Expected: PASS (7 + 2 + las de permisos, con 3 nuevas).

- [ ] **Step 8: Verificación final del plan completo**

Run: `npm test && npx tsc --noEmit && npx eslint src/modules/desempeno src/modules/inicio src/test/msw/handlers/desempeno.ts src/test/msw/fixtures/desempeno.ts --max-warnings 0 && npm run build`
Expected: toda la suite en verde (incluida la regresión de `src/modules/ventasLocales`, `src/modules/clientes`, `src/modules/rutas` y `src/utils`), `tsc` y eslint sin salida, `npm run build` termina en `✓ built in`. Para los archivos existentes tocados (`App.tsx`, `AppSidebar.tsx`, `constants/modules.ts`, `utils/permissions.ts`) correr `npx eslint <archivo>` antes y después: igual o menos problemas.

- [ ] **Step 9: Commit**

```bash
git add src/modules/desempeno/presentation/composition src/modules/desempeno/components/DesempenoScreen.tsx src/modules/desempeno/components/__tests__ src/modules/desempeno/Desempeno.tsx src/modules/desempeno/index.ts src/modules/inicio src/constants/modules.ts src/utils/permissions.ts src/utils/permissions.test.ts src/components/AppSidebar.tsx src/App.tsx
git commit -m "feat(desempeno): Inicio pasa a ser Desempeño de cobradores detrás de su permiso y se retira Pagos y visitas"
```

- [ ] **Step 10: Prueba manual (solo cuando C3 y B del API estén en desarrollo)**

Run: `npm run tauri:dev` con un usuario que tenga el interruptor `DESEMPENO_COBRADORES` **y** el permiso de API `desempeno:ver`.
Expected: el tablero carga, las 4 vistas cambian la línea, cada popover abre, la ficha muestra Jornada/Actividad/Cuentas, un pago abre el detalle de Clientes con sus fotos y una visita abre su modal con fotos. Sin el permiso del API se ve el error 403 con su mensaje, no una pantalla en blanco. **No** se despliega a la flota hasta C4 (spec §8: medición de cifras en producción).

---

## Apéndice A — Posterior (fuera de este plan por decisión del dueño, 2026-10-14)

**Exportación a Excel de la pestaña Actividad** (`GET /v2/desempeno/usuarios/{uid}/actividad.xlsx`, contrato §4). No hay botón ni método en el puerto en esta fase; una prueba de la Tarea 2.2 fija que el botón **no** aparece. Cuando se retome, en una tarea propia:

1. Puerto: `exportarActividad(uid, filtros, signal?): Promise<Blob>`; fake con `exportarCalls`/`exportarResponse`; caso de uso pass-through.
2. Adapter: `GET .../actividad.xlsx` con `paramsDeActividad(filtros)` (sin `cursor`/`limit`), `responseType: "blob"`, y en el `catch` `apperrorToDomainError(await normalizarErrorBlob(e))` — así el 422 `desempeno_export_demasiadas_filas` muestra el mensaje del servidor (el cuerpo de error llega como Blob). `normalizarErrorBlob` ya existe (Tarea 3.4) y tiene prueba.
3. Presentación: guardar con `save` de `@tauri-apps/plugin-dialog` + `writeFile` de `@tauri-apps/plugin-fs` (mismo camino que `src/modules/clientes/presentation/hooks/reporteActions.ts:guardarReporte`); en pruebas, un objeto con `arrayBuffer()` porque jsdom no trae `Blob.arrayBuffer`.
4. UI: botón "Excel" junto a "Columnas", toast de éxito o con el mensaje del servidor.

## Apéndice B — Contradicciones y huecos encontrados (reportados, no inventados)

Nivel de evidencia: **LEÍDO** salvo donde se indica. Ninguno se resolvió inventando un campo o un cálculo; donde el contrato no alcanza, la pantalla **omite** el dato.

**Mock vs contrato / D10**
1. El estilo B pinta en Jornada un área de "Cobrado hasta ahora" acumulada **calculada en JS** (`desempeno-cobradores.html`, `jornadaTab`). El contrato no trae serie acumulada y D10 prohíbe sumarla en el escritorio → **omitida**. Si se quiere, agregar `acumulado [{hora, importe}]` a `jornada` en el contrato.
2. Popovers de la línea en el **tablero**: `LineaDelDia.puntos` solo trae `id, tipo, hora` → el popover de punto y el "Antes/Después" de una pausa dicen solo hora y tipo (el mock muestra cliente y monto). En la Jornada sí se muestran completos (vienen las actividades).
3. `Tramo` no trae duración → el popover de tramo no dice "Tramo · 2 h 10" (sería restar horas).
4. Popover de % cobro del mock: "Falta por cobrar" = esperado − cobrado → **omitido** (resta de dinero).
5. "Faltan N de M": el plan muestra `cobertura_por_segmento.sin_nada` de `cobertura.total`. Que `sin_nada = total − con_algo` es **INFERIDO** del ejemplo del contrato (232 − 186 = 46); confirmarlo en el contrato. La barra usa `con_algo/total` solo como ancho.
6. "Sin nada" del mock = sin pago ni visita **esta semana ni la anterior** y "ya están dentro de Sin visitar"; el spec §2 define cobertura solo con la semana. El plan sigue el contrato (pastilla `sin_nada` con el conteo del servidor) y el texto del popover dice "esta semana".
7. `Actividad.visita` trae `estado` pero **no `trato` ni `etiqueta`**; el teléfono colorea por trato (`EstadoCuentaUi.kt:tratoDe`: una promesa sin fecha se pinta como "regresas"). Replicar `tratoDe` violaría D10 → las visitas en lista/tabla muestran el literal `tipo_visita` en gris. Propuesta: agregar `trato` y `etiqueta` a `visita` en el contrato.
8. No hay marca de "pago parcial" por actividad (el mock pinta en teal los abonos parciales); el popover de pagos del mock trae "Abonos parciales" y "Llegaron tarde", que el contrato no tiene → omitidos.
9. Actividad del mock: filtro por rango de **hora del día**, vista "Llegaron tarde" y columnas **Ruta** y **Colonia** → el contrato §4 no tiene esos parámetros/campos → omitidos. Solo se puede ordenar por `hora|cliente|monto|tipo|registrado_por`; "Agrupar por venta" no tiene orden propio → se pide `orden=cliente` y se agrupan renglones consecutivos (los grupos de venta pueden partirse si el servidor no ordena por venta dentro del cliente).
10. Los valores de `forma_cobro` del filtro no están definidos en el contrato (¿códigos `efectivo|…` o ids `157|…`?). El plan manda los códigos de `Actividad.pago.forma_cobro`. Confirmar.
11. `tipo_visita` es "literal exacto" pero no hay endpoint de catálogo → el plan copia los 14 literales de `msp-api internal/visitas/domain/tipo_visita.go:76-114`. Duplicación a quitar cuando exista el catálogo.
12. `totales.importe` de Actividad: no dice si incluye condonaciones → se rotula "importe", no "pagos".
13. Nulabilidad no declarada: `zona_id`/`zona_nombre` de un usuario `sin_zona` y `cobro.pct` con esperado 0 aparecen como número/string en el ejemplo; el DTO del plan los acepta `null`. Declararlo en el contrato.
14. La mock fecha "Martes 14 de octubre" está mal: el 14-oct-2026 es **miércoles** (las pruebas del plan lo fijan).
15. El mock incluye "Depósito" y el modelo del teléfono: excluidos (spec §10); hay pruebas que lo verifican.

**Spec vs código real**
16. Spec §5: "sin configurar … enlace a Configuración". La zona y `FECHA_CARGA_INICIAL` del usuario se editan en **Usuarios** (`/settings`, `src/components/settings/UserCard.tsx`), no en `/configuracion` → el enlace va a `/settings` y aparece solo si el usuario tiene el módulo `USUARIOS`. Decidir.
17. Spec §4.5: detalle de visita por `GET /v2/visitas/{id}`. Ese DTO existente (`msp-api internal/visitas/infra/visitashttp/dto.go:16-30`) no trae cliente, folio ni número de fotos, y `nota` es `omitempty` (choca con la convención `null` explícito) → el modal de visita se arma con la `Actividad` del contrato + `GET /v2/visitas/{id}/imagenes`; no se llama a `GET /v2/visitas/{id}`.
18. `Actividad` trae `venta_id` (DOCTO_CC_ID) pero no `docto_pv_id`; `VentaModal` de Clientes pide `doctoPvId` → desde una visita no se puede abrir "Detalle de venta" (el mock lo tiene). Desde un pago sí (el detalle de pago trae `doctoPvId`). Propuesta: agregar `docto_pv_id` a `Actividad`.
19. Un pago con `docto_cc_id = null` no puede abrir `PagoModal` (Clientes busca por `doctoCcId`) → modal propio (Tarea 3.3).
20. `jornada` no trae `generado_at` → la marca de "ahora" de la Jornada usa el `generado_at` del tablero.
21. "Para revisar" del mock lista también señales de **otros días de la semana**; `jornada` solo trae las del día → se muestran solo las del día.
22. Tres sistemas de permisos: el interruptor de escritorio `DESEMPENO_COBRADORES` (Firestore `MODULOS_DESKTOP`) es independiente del permiso de API `desempeno:ver`; hacen falta los dos. Además `SUPER_ADMIN` ve todos los módulos (`src/utils/permissions.ts:36`): al desplegar, **todo** `SUPER_ADMIN` verá la pantalla nueva en `/` sin encender nada — "primero solo para el dueño" (spec §8) se cumple solo si él es el único `SUPER_ADMIN`.

**Hallazgos del código existente (no se arreglan en este plan)**
23. `ROUTE_TO_MODULE` se consulta por coincidencia exacta (`src/hooks/usePermissions.ts:18`): rutas con parámetro como `/clientes/:id` nunca coinciden con `/clientes/123`, así que `ProtectedRoute` no las protege por módulo. El plan no agrega rutas nuevas (la ficha va por `?usuario=`) para no heredar el hueco.
24. `rutas/infrastructure/mappers/errorMapper.ts:extractCodeFromHuma` busca `"code: mensaje"`, pero msp-api manda `errors[].message = "code=…"` (`internal/rutas/infra/rutashttp/auth.go:59`) → en Rutas nunca se extrae el código. El mapper de Desempeño acepta ambos formatos (prueba en Tarea 3.4).
25. `npm run lint` ya falla en `main` (88 problemas: 57 errores, 31 advertencias, medido 2026-10-14) → la compuerta del plan es eslint por archivo.
26. El árbol de trabajo tiene cambios ajenos sin commitear en `src/modules/failedIntents/**` (`git status` del 2026-10-14) → nunca `git add -A`.
27. `PagoModal` y `VentaImagenLightbox` formatean fechas con `dayjs` en la zona del navegador, no en la del negocio. Preexistente; no se toca.
28. El mock usa Fraunces peso 340; `index.html` carga 400–700 → los títulos caen a 400. Menor.

## Self-review

**1. Cobertura del spec (§3.3, §5, §6, §7, §8-D) y del encargo:**
- Módulo hexagonal con dominio de los contratos, puerto, casos de uso, fake → Tarea 0 A. Contexto, preferencias, carga remota, tokens, hover card → Tarea 0 B. Formato en zona de negocio, escala, popover, chips, estados, selector de periodo → Tarea 0 C.
- Tablero (Hoy/Semana, umbral 15–90, horario, búsqueda, hora de actualización, fila por usuario, 4 vistas, popovers en nombre/estado/pausas/pagos/cuentas/% y en cada punto/tramo/barra/pausa, ordenable, cada minuto, sin configurar, sin totales) → Tareas 1.1–1.2.
- Ficha: encabezado y números → 2.4; Jornada (día con flechas, eje, lista con pausas intercaladas, mapa con puntos numerados, Para revisar) → 2.1; Actividad (filtros, orden, agrupar, columnas, vistas guardadas, paginación por cursor) → 2.2; Cuentas (segmentos + Sin nada, chip ícono+texto+color) → 2.3.
- Modales: visita nuevo → 3.3; `PagoModal` con fotos (`pago_recibido_id`) → 3.2; venta y cliente de Clientes por índice público → 3.2/3.3; lightbox de Ventas Locales → 3.1/3.3; `AuthenticatedImage` con descargador y regresión → 3.1.
- D2 adapter + DTOs + mappers + MSW + pruebas contra fixtures → 3.4.
- D3: módulo y permiso `DESEMPENO_COBRADORES`, inicio sencillo, retiro de Home y `getPagosAndVisitas.ts`, conexión de carriles → Tarea 4.
- Estados de carga/error/vacío/sin configurar/pantalla angosta → `Estados` (0 C) usados en 1.2, 2.1–2.4, Tarea 4. Vista recordada por computadora → 0 B + 1.1.
- Excel: fuera por decisión del dueño → Apéndice A.

**2. Placeholders:** sin "TBD/TODO/similar a"; cada paso de código trae el código. Las ediciones a archivos existentes indican la línea o el bloque exacto a reemplazar.

**3. Consistencia de tipos y nombres** (revisada entre tareas): `DesempenoPort` (6 métodos) ↔ fake ↔ adapter; `useTablero` (1.2) ↔ `DesempenoScreen` (4); `JornadaTab.renderLinea(jornada, ahora)` (2.1) ↔ `FichaScreen.renderLinea` (2.4) ↔ `DesempenoScreen` (4); `describirActividades` en `components/jornada/describirActividades.ts`; `CeldaFotos`, `ModalesFicha`, `ModalFicha` en `components/modales/`; `PagoModal.onAbrirVenta/onAbrirCliente` (3.2) ↔ `ModalesFicha` (3.3); `FuenteImagen`/`ImagenDeVisor` (3.1) ↔ `PagoFotos` (3.2) ↔ `GaleriaFotos` (3.3); `SegmentoPeriodo` vive en `components/comunes/` (0 C) y lo usan 1.2 y 2.4.

**4. Review Focus:** las cinco entradas tienen su prueba en la tarea dueña (0 B + 1.2; 0 C; 3.3; 3.2; 2.2).
