# Plan — Área de "Configuración" (admin de tablas MSP_CFG_*): Vendedores Microsip + Zonas/Cajas

## Context

Hoy las tablas de configuración `MSP_CFG_*` que alimentan el flujo de **aplicar venta a Microsip** se
editan a mano por SQL y **no tienen UI**. Dos duelen:

- **`MSP_CFG_VENDEDOR_MICROSIP` está vacía (0 filas)** → al aplicar una venta a **crédito**, la capa
  `app` resuelve `usuario → LISTA_ATRIB_ID` contra esa tabla, no encuentra nada, y escribe el
  centinela **`-1`** en `LIBRES_CARGOS_CC.VENDEDOR_1/2/3`. Microsip lo interpreta como *"sin
  vendedor"*. **Este es el bug reportado**. El writer y la lógica de resolución YA funcionan
  (`internal/ventas/app/aplicar_venta.go` `resolveVendedorListaIDs`); solo falta poblar la tabla.
- **`MSP_CFG_ZONA_CAJA` (44 filas)** mapea `zona → caja·cajero·vendedor·cobrador`, crítica para
  crear-venta, solo editable por SQL.

Decisión: construir **un área de "Configuración"** en la app de oficina (`sistema-cobro-web`) que
administre ambas tablas, con backend nuevo en `msp-api`. Alcance de resolución de vendedor: **solo
crédito**. UX de vendedores: "pick por nombre + auto-resuelve los 3 IDs" (124/140 en las 3 listas,
16 con hueco → override manual por slot).

Datos Microsip (test DB, solo lectura):
- Campos particulares de crédito = atributos **19985 (VENDEDOR_1), 19986 (VENDEDOR_2), 19987
  (VENDEDOR_3)** sobre `CLAVE_OBJETO='CARGOS_CC'`; roster en `LISTAS_ATRIBUTOS`
  (`LISTA_ATRIB_ID, ATRIBUTO_ID, VALOR_DESPLEGADO, POSICION`). Uso en `LIBRES_CARGOS_CC`: V1 99.9%,
  V2 85%, V3 3.4% → soportar 3 slots.
- `MSP_CFG_ZONA_CAJA`: `ZONA_CLIENTE_ID`(PK)→`CAJA_ID, CAJERO_ID, VENDEDOR_ID, COBRADOR_ID`; `-1`
  = "sin mapeo". FKs a `ZONAS_CLIENTES`, `CAJAS`, cajeros, `VENDEDORES`, `COBRADORES`.

No se toca el read path de `ventas` ni el flujo de aplicar venta. No hay migración nueva. Fuera de
alcance: contado (`LIBRES_VTA_PV`), trigger de `DOCTOS_PV.VENDEDOR_ID`, las 4 tablas ya sembradas.

## Arquitectura

Módulo nuevo `internal/config/` (msp-api), Huma sobre chi. Referencia de forma: `rutas`; de CRUD:
`ventas`. Superficie: GET/PUT/DELETE sobre `/v2/config/...`. Sin outbox. Estructura estándar:
`domain/`, `ports/outbound/`, `app/`, `infra/{configfb, confighttp}`, `config_contracts.go`.

Permisos (en `auth`): `config:leer` y `config:administrar` en
`internal/auth/domain/permission_codes.go` (const + `AllPermissions()`), re-export en
`auth_contracts.go`, actualizar `permission_codes_test.go`. Boot-sync los UPSERTea + concede a
super_admin al reiniciar.

Listar usuarios: `config` no importa `auth/domain` (depguard). Agregar a `auth_contracts.go` un
contrato `UsuarioResumen{ID, Nombre, Email, Estatus}` + `ListarUsuarios(ctx) ([]UsuarioResumen,
error)` reusando `outbound.UsuarioRepo.List` (paginar internamente) + mapper. `config` define un
puerto outbound `UsuariosReader` satisfecho por un cliente que llama a ese contrato.

Gating: patrón Huma in-handler (como `rutas`): copiar `requirePerm(cu, perms...)` de
`internal/rutas/infra/rutashttp/auth.go` a `confighttp`, llamarlo tras `currentUserOrError(ctx)`.
Lectura → `config:leer`; escritura → `config:administrar`.

Upserts: UPDATE-luego-INSERT, NO MERGE. Sin timestamps. Nombres catálogo = string plano UTF-8.

## Fase A — scaffolding + permisos + contrato usuarios
1. Permisos en auth: `config:leer`, `config:administrar` (const + AllPermissions + re-export + test).
   Categoría "Configuración".
2. Contrato usuarios en auth: `UsuarioResumen` + `ListarUsuarios(ctx)` reusando `UsuarioRepo.List`
   (paginar internamente hasta traer todos) + mapper entidad→contrato.
3. Scaffold `internal/config/`: domain (VOs `VendedorMapping`, `ZonaCajaConfig` + constructores +
   validación de IDs), ports/outbound (`ConfigRepo`, `CatalogoReader`, `UsuariosReader`), app
   (service queries+commands), infra/configfb (repo Firebird), infra/confighttp (handlers Huma +
   router + DTOs + requirePerm).
4. Wiring: `cmd/api/config_wiring.go` (providers fx) + montar router en `provideHTTPServer` en
   `cmd/api/server.go` grupo `/v2` (espejando rutas, ~server.go:249).

## Fase B — slice Vendedores (`MSP_CFG_VENDEDOR_MICROSIP`)
Endpoints `/v2/config/vendedores`:
- GET / — lista TODOS los usuarios app (via UsuariosReader) LEFT JOIN mapeo actual; resuelve nombres
  de cada LISTA_ATRIB_ID contra LISTAS_ATRIBUTOS; devuelve por usuario `{usuarioId, nombre, email,
  mapping:{v1,v2,v3:{listaId,nombre}}, estado:"3/3"|"2/3"|"sin asignar"}`.
- GET /opciones — roster: `SELECT LISTA_ATRIB_ID, ATRIBUTO_ID, VALOR_DESPLEGADO FROM LISTAS_ATRIBUTOS
  WHERE ATRIBUTO_ID IN (19985,19986,19987)`, agrupado por VALOR_DESPLEGADO → identidad `{nombre,
  v1ListaId?, v2ListaId?, v3ListaId?, matchCount}`. IDs de atributo = constantes Go en configfb.
- PUT /{usuarioId} — upsert `{vendedorListaId1?, vendedorListaId2?, vendedorListaId3?}` (nullable;
  UPDATE-luego-INSERT). Valida usuario existe + cada lista_id pertenece al ATRIBUTO_ID esperado del slot.
- DELETE /{usuarioId} — borra fila (vuelve a "sin asignar").

Semántica centinela -1/NULL alineada con `ventfb/aplicar_config_repo.go` `selectVendedorListaIDs`
(no cambiar).

## Fase C — slice Zonas/Cajas (`MSP_CFG_ZONA_CAJA`)
Endpoints `/v2/config/zonas-cajas`:
- GET / — TODAS las zonas de ZONAS_CLIENTES LEFT JOIN MSP_CFG_ZONA_CAJA, nombres resueltos de
  caja/cajero/vendedor/cobrador (zonas sin mapeo también aparecen).
- GET /opciones — catálogos: ZONAS_CLIENTES, CAJAS, cajeros, VENDEDORES, COBRADORES (`{id, nombre}`).
- PUT /{zonaClienteId} — upsert `{cajaId, cajeroId, vendedorId, cobradorId}` (UPDATE-luego-INSERT).
  Validar cada id existe en su catálogo; permitir -1/null explícito para "sin mapeo".

Reusar shape de `aplicar_config_repo.go` (`CajaCajero`) para columnas + sentinel -1 (sin tocar).

## Fase D — FE shell Configuración + pantalla Vendedores
Repo sistema-cobro-web, módulo nuevo `src/modules/configuracion/` (hexagonal). Referencia: rutas
(/rutas read-only) y ventasLocales (adapters+hooks). Nav: sección "Configuración" (gateada por
`config:administrar`), pestañas Vendedores y Zonas y cajas.
Pantalla Vendedores: tabla de usuarios; por fila select buscable de identidad Microsip (GET
/opciones, por nombre); al elegir auto-rellena los 3 lista_id + badge ✓3/3 / ⚠2/3 con override por
slot; guardar por fila (PUT). Estado "sin asignar" visible. Adapter HTTP tipado a Fase B.

## Fase E — FE pantalla Zonas/Cajas
Tabla de 44 zonas (GET /zonas-cajas); por fila selects caja·cajero·vendedor·cobrador (GET /opciones);
guardar por fila con confirmación (config viva). Fila con -1/sin mapeo marcada. Adapter tipado a Fase C.

## Testing
- Backend gates: domain ≥99%, app ≥90%, infra ≥80%.
  - Domain: constructores/validación VendedorMapping + ZonaCajaConfig (ids válidos, sentinel).
  - App: queries (list, opciones, estado 3/3·2/3·sin-asignar) + commands (upsert/delete) con fakes.
  - Infra (configfb): integración Firebird con fbtestutil.WithTestTransaction (rollback) contra dev DB
    — upsert UPDATE-luego-INSERT, resolución de nombres roster (19985/86/87), leer refleja escrito.
  - HTTP: 403 sin permiso; 200 con config:administrar; ids inexistentes → 422/404.
- Frontend (vitest): adapters HTTP (params/paths, auto-resuelve 3 ids, slot sin match), lógica de
  estado de pantallas. Fixtures realistas (nombres MX).

## Secuencia
1. Fase A. 2. Fase B+D (cierra el bug). 3. Fase C+E.
Ramas: feat/config-area en ambos repos. Merge a main local (flujo usual). Delegar mecánico a sonnet.

## A confirmar (túnel expiró) — derivar de código existente
- Catálogo "cajero": CAJEROS vs USUARIOS de Microsip + columna de nombre.
- Columnas de nombre de CAJAS/VENDEDORES/COBRADORES/ZONAS_CLIENTES (asumido NOMBRE).
- Identidades roster 19985/86/87 casan por VALOR_DESPLEGADO idéntico (124/140; 16 hueco → override).
