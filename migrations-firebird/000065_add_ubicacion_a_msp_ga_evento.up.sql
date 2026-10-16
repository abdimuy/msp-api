-- ============================================================================
-- Migración 000065: MSP_GA_EVENTO.UBICACION_DESDE / UBICACION_HASTA — el rastro
-- ============================================================================
--
-- Por qué:
--   Hasta ahora el evento guardaba la etapa de origen y de destino
--   (ETAPA_DESDE / ETAPA_HASTA) pero no la ubicación física. Y la ubicación es
--   justo el requisito central del módulo (§1): saber dónde está cada cosa.
--   Un artículo podía llegar a `reingresado_inventario` y seguir leyéndose en
--   `domicilio_cliente` — el defecto que la auditoría del dominio reprodujo.
--
--   Con estas dos columnas, todo evento que cambia la ubicación —automático
--   por etapa (R3) o manual (`MoverArticulo`) — deja el par desde/hasta igual
--   que el de etapas, y la línea de tiempo cuenta dónde estuvo el mueble y
--   cuándo se movió, sin deducirlo del estado actual.
--
-- Nullable y sin DEFAULT, a propósito:
--   - **Nullable** porque la mayoría de los eventos NO mueven la ubicación:
--     una nota, una evidencia o un dictamen no cambian dónde está el mueble, y
--     NULL lo dice sin inventar un valor. Sólo los eventos que sí la mueven
--     escriben el par completo.
--   - **Sin DEFAULT** por la regla dura #1 de CLAUDE.md: el valor lo escribe Go
--     (via firebird.ToWallClock el resto; aquí el string del catálogo). No hay
--     ni trigger ni procedimiento que las llenen.
--
-- Ancho y juego de caracteres idénticos a ETAPA_DESDE / ETAPA_HASTA:
--   `VARCHAR(28) CHARACTER SET ASCII`. El catálogo de ubicaciones se serializa
--   con la misma forma ASCII que el de etapas (§3.3 del spec).
--
-- La columna de R6 NO se agrega:
--   "pasó por reparado" se deriva de RUTA + DICTAMEN en el propio agregado
--   (ver el reporte de la tarea 8), así que el artículo no necesita recordarlo
--   en una columna. Se declara en el reporte, como pide el brief.
--
-- Riesgo de aplicación: nulo. MSP_GA_EVENTO está vacía en todos los entornos —
-- el módulo de garantías todavía no tiene escritura desde la API.
-- ============================================================================

ALTER TABLE MSP_GA_EVENTO ADD UBICACION_DESDE VARCHAR(28) CHARACTER SET ASCII;
COMMIT;

ALTER TABLE MSP_GA_EVENTO ADD UBICACION_HASTA VARCHAR(28) CHARACTER SET ASCII;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (65, '000065_add_ubicacion_a_msp_ga_evento', CURRENT_TIMESTAMP);
COMMIT;
