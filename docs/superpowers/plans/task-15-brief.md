# Task 15 — Dev infra: Ollama service in compose (DEFINED, not started) + dev env

## Where this fits
Defines the local Ollama service so a developer CAN run the real model at the gated tuning step — but this task does NOT start it or pull any model. The app ships with `LLM_ENABLED=false`, so nothing here affects normal runs. Dev-only (CLAUDE.md §5: no Docker in production — this is the dev compose, same as the existing meilisearch/firebird dev services).

## Read first
- `compose.yml` (repo root) — how existing dev services (`mueblera-firebird`, meilisearch) are declared: image, ports, named volumes, healthcheck style, network. Mirror that style.
- `.env` / `.env.example` (whichever the repo uses for dev) — where FB_/Meili vars live; you'll add the LLM vars.

## Changes

### 1. `compose.yml` — add an `ollama` service
Mirror the existing services' declaration style. Roughly:
```yaml
  ollama:
    image: ollama/ollama
    container_name: msp-ollama
    ports:
      - "11434:11434"
    volumes:
      - msp-ollama-data:/root/.ollama
    restart: unless-stopped
```
And add `msp-ollama-data:` under the top-level `volumes:` block (match how `msp-*-data` volumes are declared). Use the same network as the other services IF the compose file defines one (match exactly; if services use the default network, do likewise). Add a healthcheck only if the other services have one (match their style); otherwise omit. Do NOT add `depends_on` from the API to ollama (the API must start fine with the LLM disabled).

### 2. Dev env — add LLM vars (disabled)
In the dev env file (`.env` and/or `.env.example` — update whichever the repo tracks; if `.env` is gitignored, update `.env.example`):
```
# ── LLM local (Ollama dev / llama-server prod) — apagado por defecto ──
LLM_ENABLED=false
LLM_BASE_URL=http://localhost:11434/v1
LLM_MODEL=qwen3:4b
LLM_TIMEOUT=30s
```
`LLM_ENABLED=false` is the whole point — the model is only turned on at the gated step. (Note: when the API runs on the host and Ollama in compose, the host reaches it at `http://localhost:11434/v1`; if the API itself runs inside compose, the URL would be `http://ollama:11434/v1`. Add a short comment noting both; default the committed value to `http://localhost:11434/v1` since the API currently runs on the host.)

## DO NOT
- Do NOT run `docker compose up`, `docker compose pull`, or `ollama pull` — starting the model is the gated final step, not this task.
- Do NOT enable the LLM anywhere.

## Verification
- `docker compose config` (validates compose.yml syntax WITHOUT starting anything) — run it and confirm it parses and shows the `ollama` service + `msp-ollama-data` volume. If `docker` is unavailable in your environment, instead carefully validate the YAML by inspection against the existing services and say so in the report.
- Confirm the API still builds: `go build ./...` (unaffected, but sanity).
- Grep the env file to confirm `LLM_ENABLED=false`.

## Commit
`chore(dev): define servicio ollama en compose y variables LLM (apagado por defecto)`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-15-report.md` (paste the `docker compose config` confirmation or the inspection note). Reply ≤15 lines: status, commit SHA+subject, one-line verification summary, concerns, report path.
