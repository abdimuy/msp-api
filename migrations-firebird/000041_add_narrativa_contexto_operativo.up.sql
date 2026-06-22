-- ============================================================================
-- Migración 000041: agrega CONTEXTO_OPERATIVO a MSP_AN_CLIENTE_NARRATIVA
-- ============================================================================
--
-- Por qué:
--   La "narrativa del analista" ahora también consume la NOTA libre del cobrador
--   (CLIENTES.NOTAS de Microsip: acuerdos de pago, responsables, domicilios
--   compartidos, fechas) como contexto cualitativo que los números no capturan.
--   Además de anclar la narrativa a esa nota, el LLM destila 1-2 señales
--   operativas concretas en una línea aparte "Contexto operativo". Esa línea se
--   materializa junto a la narrativa para no re-llamar al LLM en cada lectura.
--
--   CONTEXTO_OPERATIVO es un blob UTF-8 (texto libre, ya capado/trim en Go).
--   Es nullable: cuando la nota no aporta nada operativo (o el LLM está apagado)
--   la columna queda NULL y la ficha simplemente no muestra la línea. La nota
--   también entra al INPUT_HASH, así que editar la nota en Microsip invalida la
--   fila cacheada y regenera narrativa + contexto en la próxima lectura.
--
-- Restricciones:
--   Sin DEFAULT, triggers ni procedimientos — el valor se pasa desde Go
--   (CLAUDE.md §1). Los invariantes de negocio viven en el domain entity de Go.
-- ============================================================================

ALTER TABLE MSP_AN_CLIENTE_NARRATIVA ADD CONTEXTO_OPERATIVO BLOB SUB_TYPE TEXT CHARACTER SET UTF8;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (41, '000041_add_narrativa_contexto_operativo', CURRENT_TIMESTAMP);
COMMIT;
