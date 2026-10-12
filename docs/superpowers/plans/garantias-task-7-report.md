# Garantías — Tarea 7 — Reporte

## Entrega 1 — Repositorio de lectura

Se completó la parte de lectura de Firebird correspondiente a la bandeja de garantías, repositorio de imágenes y validación de claves duplicadas.

### Implementado

- `BandejaRepo.Listar`
- Filtros por estado, origen, cliente, etapa, ubicación y fechas.
- Paginación por `CREATED_AT DESC, ID DESC`.
- Cursor con desempate por ID.
- `Desde` inclusivo.
- `Hasta` exclusivo.
- Carga de artículos en una segunda consulta.
- Uso de `EXISTS` para filtros de artículos.
- `ImagenRepo.Registrar`.
- `ImagenRepo.ListarPorEvento`.
- Validación de FK en imágenes.
- Validación de transacción requerida para registrar imágenes.
- Helper `esClaveDuplicada`.
- Lecturas protegidas con `RunInReadTx`.

## Consultas por página

La bandeja realiza exactamente 2 consultas por página:

1. Consulta de garantías.
2. Consulta de artículos usando `IN`.

La prueba utilizada fue:

```text
TestBandejaRepo_ListarUsaDosConsultasPorPagina
