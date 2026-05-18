# Firebird Snapshots — Workflow

Backup/restore del dev DB de Firebird (`mueblera-firebird`) usando `gbak`, la herramienta nativa de Firebird. Útil cuando quieres experimentar (correr migraciones nuevas, smoke tests con datos reales, lo que sea) y poder volver al estado anterior sin colaterales.

> **Solo dev.** Producción tiene su propio proceso de backup gestionado por DBA / runbook de operaciones.

## Targets disponibles

| Comando | Qué hace |
|---|---|
| `make fb-snapshot` | Backup hot (sin downtime) → `data/firebird-snapshots/snapshot_YYYYMMDD_HHMMSS.fbk` |
| `make fb-snapshot NAME=foo` | Backup con nombre custom → `data/firebird-snapshots/foo.fbk` |
| `make fb-snapshot-list` | Lista los snapshots disponibles, ordenados por fecha desc |
| `make fb-restore` | **DESTRUCTIVO.** Restaura desde el snapshot más reciente. Pide confirmar tecleando `restore` |
| `make fb-restore NAME=foo` | Restaura desde `foo.fbk` específicamente |
| `make fb-snapshot-delete NAME=foo` | Borra `foo.fbk` |

## Cómo es un snapshot

- **Formato:** `.fbk` (logical backup de gbak) — portable entre cualquier instancia de Firebird de la misma versión major.
- **Tamaño:** ~60% del tamaño de la DB (3.6 GB → 2.2 GB típico). gbak comprime al guardar y rehidrata al restaurar.
- **Tiempo:** ~30s para snapshot, ~1-2 min para restore.
- **Online:** gbak hace consistent backup sin tomar locks. Tu API y tus tests pueden seguir corriendo durante el snapshot.
- **Local-only:** la carpeta `data/firebird-snapshots/` está en `.gitignore`. No se commitea (son 2GB cada uno).

## Workflow típico

### "Voy a hacer cosas raras, quiero poder regresar"

```bash
# 1. Snapshot del estado actual
make fb-snapshot NAME=before_experiment

# 2. Hacer las cosas raras:
make fb-migrate-up                    # ej. aplicar una migración nueva
isql ... -i some_destructive.sql      # ej. tests manuales
./bin/api serve                       # ej. crear ventas reales

# 3a. Si todo salió bien: solo borra el snapshot
make fb-snapshot-delete NAME=before_experiment

# 3b. Si algo se rompió: restaurar
make fb-restore NAME=before_experiment
# (te pedirá confirmar tecleando "restore")
```

### "Probar la migración 000003 contra datos reales sin miedo"

```bash
make fb-snapshot NAME=pre_000003
make fb-migrate-up                  # ahora corre la 000003
# ... validación manual ...
# Si rompe → make fb-restore NAME=pre_000003
# Si funciona → make fb-snapshot-delete NAME=pre_000003
```

### "Quiero un estado base congelado para volver a él cuando quiera"

```bash
# Al final de un día de trabajo limpio:
make fb-snapshot NAME=clean_baseline

# Cuando quieras resetear a ese estado:
make fb-restore NAME=clean_baseline
```

### Baseline canónico: `clean-with-admin`

El repo asume que existe un snapshot llamado `clean-with-admin` con la DB
en estado "schema migrado + admin funcional + cero data de ventas". Es el
estado al que conviene volver entre experimentos manuales o demos:

```bash
echo "restore" | make fb-restore NAME=clean-with-admin
```

Cuando algo lo invalida (nueva migration, nuevo permiso en
`domain.AllPermissions()`, drift de Microsip que quieras congelar),
regenéralo así:

```bash
echo "restore" | make fb-restore NAME=clean-with-admin   # punto de partida
make fb-migrate-up                                        # nueva migration si aplica
go run ./cmd/api auth-bootstrap \
    --email admin@muebleriamsp.mx \
    --nombre "Administrador MSP" \
    --create-in-firebase --reset                          # refresca admin + permisos
make fb-snapshot-delete NAME=clean-with-admin             # tira el viejo
make fb-snapshot NAME=clean-with-admin                    # toma nuevo
```

Ver [`docs/firebase-setup.md`](firebase-setup.md) §3 para los modos del
auth-bootstrap.

## Lo que NO hace

- **No restaura cambios al schema migrado posteriormente.** Si tomas snapshot, aplicas 000003, y restauras: vuelves al schema previo a 000003. La tabla `MSP_MIGRATIONS` también se restaura, así que `make fb-migrate-up` vuelve a verlo como "no aplicada".
- **No es para CI.** Los tests usan `WithTestTransaction` (rollback-only). El snapshot/restore es para experimentación manual.
- **No es backup de prod.** El DBA de producción tiene su propio proceso.
- **No protege contra el dev que borre `data/firebird-snapshots/` por accidente.** Si te importa un snapshot, cópialo a otro lado.

## Detrás de cuadros (cómo funciona)

```
make fb-snapshot:
  docker exec mueblera-firebird gbak -b -v -t /firebird/data/MUEBLERA.FDB /tmp/foo.fbk
  docker cp mueblera-firebird:/tmp/foo.fbk ./data/firebird-snapshots/foo.fbk
  docker exec mueblera-firebird rm /tmp/foo.fbk

make fb-restore:
  docker cp ./data/firebird-snapshots/foo.fbk mueblera-firebird:/tmp/restore.fbk
  docker exec mueblera-firebird gfix -shut full -force 30 /firebird/data/MUEBLERA.FDB
  docker exec mueblera-firebird gbak -c -replace_database /tmp/restore.fbk /firebird/data/MUEBLERA.FDB
  docker exec mueblera-firebird gfix -online normal /firebird/data/MUEBLERA.FDB
  docker exec mueblera-firebird rm /tmp/restore.fbk
```

- `gbak -b` = backup (extract). `-t` = transportable (cross-version safe). `-v` = verbose.
- `gbak -c` = create (restore). `-replace_database` = blow away the existing target.
- `gfix -shut full -force 30` = drop all connections in 30s before restore.
- `gfix -online normal` = bring the DB back to read-write after restore.

## Compatibilidad con bonanza-api

`bonanza-api` ya tiene un snapshot histórico montado en el contenedor (`/tmp/backup.fbk` ← `bonanza-api/firebird/MUEBLERA_SNP_20260219_2230.fbk`). Si quieres bootstraping desde ese estado prístino de febrero 2026, puedes:

```bash
docker cp mueblera-firebird:/tmp/backup.fbk data/firebird-snapshots/feb_2026_baseline.fbk
make fb-restore NAME=feb_2026_baseline
```

Esto te devuelve a antes de cualquier MSP_* migración aplicada — útil si quieres reproducir un bug que requiere DB "vacío".

## Troubleshooting

**`gfix shutdown failed`** durante restore — significa que hay conexiones abiertas al DB que no se pudieron dropear. El restore intentará proceder de todos modos. Si falla, cierra todas las conexiones manualmente (parar API, cerrar isql sessions) y reintenta.

**`docker cp` lento** — los snapshots son 2GB. En una M2 SSD toma ~5s. Si tu Docker está en disco lento, espera más.

**Restore en estado inconsistente** (gbak falla a mitad) — `make fb-restore` con el mismo NAME otra vez normalmente lo arregla. Si no, el último recurso es `docker compose down -v` y rebuild del contenedor (perderás toda la data; restaurar después).
