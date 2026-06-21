# Task 15 — Report: define servicio ollama en compose + variables LLM

## Status

DONE. Compose service defined, volume declared, LLM vars added to tracked env file. Nothing started. `LLM_ENABLED=false`.

## Changes

### `compose.yml`

Added `ollama` service after `meilisearch`, following the same style (no healthcheck — peers have none):

```yaml
ollama:
  image: ollama/ollama
  container_name: msp-ollama
  restart: unless-stopped
  ports:
    - "11434:11434"
  volumes:
    - msp-ollama-data:/root/.ollama
  networks:
    - default
```

Added `msp-ollama-data` to the top-level `volumes:` block with explicit `name: msp-ollama-data`.

No `depends_on` from the API — the API starts fine with `LLM_ENABLED=false`.

### Env files

Only `.env.example` is git-tracked. `.env` is gitignored but was also updated for local dev consistency.

LLM block appended to both files:

```
# ── LLM local (Ollama dev / llama-server prod) — apagado por defecto ────────
# URL cuando la API corre en el host: http://localhost:11434/v1
# URL cuando la API corre dentro de compose: http://ollama:11434/v1
LLM_ENABLED=false
LLM_BASE_URL=http://localhost:11434/v1
LLM_MODEL=qwen3:4b
LLM_TIMEOUT=30s
```

## Verification

### `docker compose config`

```
$ docker compose config --quiet && echo "compose config: OK"
compose config: OK
```

Ollama service visible in parsed output:

```
  ollama:
    container_name: msp-ollama
    image: ollama/ollama
    networks:
      default: null
    ports:
      ...
```

Volume `msp-ollama-data` visible:

```
  msp-ollama-data:
    name: msp-ollama-data
```

### `go build ./...`

```
go build: OK
```

No compilation impact (LLM wiring is a future task).

### `LLM_ENABLED`

```
.env.example:LLM_ENABLED=false
.env:LLM_ENABLED=false
```

## Concerns

None. No services started, no model pulled, no Go code touched. The `ollama` service is defined but not referenced by any other service (`depends_on` intentionally absent).

## Files modified

- `/Volumes/M2-1TB/Developer/msp-api/compose.yml` — added `ollama` service + `msp-ollama-data` volume
- `/Volumes/M2-1TB/Developer/msp-api/.env.example` — added LLM vars block (tracked)
- `/Volumes/M2-1TB/Developer/msp-api/.env` — added LLM vars block (gitignored, local dev only)
