# Plan — Ponderado de cobranza por calendario (API + FE)

Estado: en ejecución (subagent-driven-development). BASE msp-api 919e87d, FE e96e08c.

## Modelo (acordado con el usuario, días naturales)
- Mensual: vence el día 1 de cada mes (gracia +2 → a tiempo hasta el 3).
- Quincenal: vence el día 15 y el último día del mes (gracia +2).
- Semanal: ciclo semanal; toda la semana de gracia (el periodo actual no es atraso).
- Primer vencimiento = el primero del calendario estrictamente DESPUÉS de FECHA_CARGO (el enganche cubre el momento de la venta).
- Gracia +2 SOLO en el atraso (vencidas); NUNCA en el denominador (AplicaEnVentana).

## Tareas
- T1 Backend (`internal/rutas/`): `domain/calendario.go` puro + integración (`cobranza.go`, `cobranza_semanal.go`, `listar_rutas.go`, `cobranza_dto.go`) + matriz exhaustiva de tests.
- T2 Frontend (`src/modules/rutas/`): campo `aplicaPonderado` (entidad→DTO→mapper→fake→pantalla columna "Aplica") + tests.

Fuera de alcance: materializar/cachear; validar números reales (paso server/apidev posterior); calendario por-cliente desde historial.
