# msp-api

API en Go para el sistema MSP. Esta guía es para arrancar el proyecto en local desde cero.

## Requisitos

Todas las herramientas que usa el proyecto (con sus versiones exactas) están pinneadas en [`.tool-versions`](.tool-versions). Hay dos caminos para instalarlas — usa el que prefieras:

- **Camino A (recomendado): con `mise`.** Una sola línea y todo queda sincronizado con el repo.
- **Camino B: instalación manual.** Si no quieres `mise`, instala cada herramienta a la versión que dice `.tool-versions`.

Independiente del camino que elijas, hay dos cosas que **siempre** se instalan aparte:

- **Docker Desktop** — para Postgres de dev y los containers de tests. [Descarga](https://www.docker.com/products/docker-desktop/) y déjalo corriendo.
- **sqlc** — no está en el registro de `mise` aún. Solo lo necesitas cuando regeneres queries (`make generate`). Si por ahora no vas a tocar SQL generado, puedes saltártelo.

### Camino A — con mise

[Instala `mise`](https://mise.jdx.dev/installing-mise.html) (`brew install mise` en macOS), luego:

```sh
git clone https://github.com/abdimuy/msp-api.git
cd msp-api
mise install        # lee .tool-versions e instala todo a las versiones del repo
```

Con eso ya tienes Go, `golangci-lint`, `lefthook`, `air` y `migrate` en las versiones correctas. Si llegas a tocar sqlc, instálalo aparte (ver abajo).

### Camino B — manual

Lo que necesitas, con la versión exacta:

| Herramienta | Versión | Para qué |
|---|---|---|
| Go | 1.25.0 | Compilar y correr la API |
| `golangci-lint` | 2.11.3 | Linter (lo corre lefthook) |
| `lefthook` | 2.1.3 | Hooks de git (pre-commit y pre-push) |
| `air` | 1.63.7 | Hot-reload en desarrollo (`make dev`) |
| `migrate` (golang-migrate) | 4.19.1 | Aplicar migraciones SQL |

Atajo en macOS con Homebrew:

```sh
brew install go@1.25 golangci-lint lefthook air golang-migrate
```

Para Linux / Windows o si Homebrew no tiene la versión exacta, usa los binarios oficiales o `go install`:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.3
go install github.com/evilmartians/lefthook@v2.1.3
go install github.com/air-verse/air@v1.63.7
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1
```

### sqlc (opcional, solo si vas a regenerar código SQL)

```sh
brew install sqlc
# o:
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
```

## Primera vez

Una vez que tengas las herramientas instaladas (camino A o B):

```sh
# 1. Clonar (si aún no lo hiciste)
git clone https://github.com/abdimuy/msp-api.git
cd msp-api

# 2. Setup inicial — instala los git hooks de lefthook y copia .env.example a .env
make setup

# 3. (Opcional) Edita .env si necesitas cambiar puertos o credenciales.
#    Los defaults funcionan tal cual para desarrollo local.
```

## Levantar la base de datos

Postgres corre en un container vía Docker Compose. La definición está en `compose.yml`.

```sh
make db-up        # arranca Postgres y espera hasta que esté healthy
make migrate-up   # aplica las migraciones
```

Eso deja Postgres escuchando en `localhost:5432` con usuario `msp`, contraseña `msp`, base de datos `msp_dev` (o lo que pongas en `.env`).

Otros comandos útiles:

```sh
make db-down          # apaga Postgres pero conserva los datos
make db-down-volume   # apaga y borra el volumen (datos perdidos)
make db-reset         # equivalente a db-down-volume + db-up + migrate-up
make db-shell         # abre psql dentro del container
make db-logs          # tail de logs del container
```

## Levantar la API

Dos formas, según prefieras:

```sh
make run    # arranca la API una vez (sin hot reload)
make dev    # arranca con air — recompila y reinicia al guardar cambios
```

La API queda en `http://localhost:3001`. Para confirmar que vive:

```sh
curl http://localhost:3001/healthz   # {"status":"ok"}
curl http://localhost:3001/readyz    # incluye chequeo a Postgres
curl http://localhost:3001/version   # versión, build time, started_at
```

## Tests

Los tests corren todos en local — no hay CI remoto. Pre-push de lefthook corre los unit tests; los de integración los corres a mano cuando tocas repo / handlers / outbox / migraciones.

```sh
make test               # solo unit tests
make test-integration   # integration tests (necesita Docker corriendo)
make test-all           # ambos
```

Detalles del setup de tests de integración: [`docs/integration-tests.md`](docs/integration-tests.md).

## Estructura del repo

```
cmd/                 # binarios (api, microsip-sync, migrator)
internal/platform/   # cimientos compartidos (config, postgres, outbox, etc.)
internal/{module}/   # módulos de negocio (vertical slice por módulo)
migrations/          # SQL versionado (golang-migrate)
queries/             # archivos .sql que sqlc compila a Go
docs/                # ADRs, playbooks, estándares
compose.yml          # stack de dev (Postgres)
Makefile             # entrada principal a todos los comandos
```

Las reglas duras del proyecto (qué va y qué no va en migraciones, convenciones de idioma, etc.) están en [`CLAUDE.md`](CLAUDE.md).

## Cuando algo falla

- **`make db-up` falla con "Cannot connect to the Docker daemon"** → Docker Desktop no está corriendo.
- **`make migrate-up` se queja de "no such command: migrate"** → falta instalar `golang-migrate`.
- **La API arranca pero `/readyz` devuelve error de Postgres** → revisa que `make db-up` haya quedado `healthy` (`docker compose ps`).
- **`make dev` no recompila al guardar** → confirma que `air` está instalado y en el PATH.
- **Pre-commit falla en cosas que no tocaste** → corre `make lint-fix && make fmt` y vuelve a intentar.

## Comandos a la mano

```sh
make help          # lista todos los targets disponibles
```
