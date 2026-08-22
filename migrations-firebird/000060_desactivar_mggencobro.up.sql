-- ============================================================================
-- Migración 000060: apagar MGGENCOBRO
-- ============================================================================
--
-- Qué es MGGENCOBRO:
--   Un programa de tercero, instalado en esta base desde hace años, que servía
--   para capturar ventas más fácil en la oficina. NO es de Microsip. Cuatro
--   pruebas, medidas el 2026-08-21:
--
--     1. Convención de nombres. Los 118 triggers nativos sobre DOCTOS* van
--        `TABLA_ACCION` (DOCTOS_PV_BEFINS, DOCTOS_CC_AFTUPD_0). Este va
--        `PRODUCTO_TABLA_ACCION` — la misma forma que usan los nuestros
--        (MSP_PAGOS_DOCTOS_CC_AU), y usa `AI0` donde Microsip pondría
--        `AFTINS_0`.
--     2. Ningún objeto nativo depende de él: las 9 filas de RDB$DEPENDENCIES
--        que apuntan a MGGENCOBRO salen de sus propios triggers.
--     3. Sus 5 tablas no tienen NI UNA foreign key hacia el esquema de
--        Microsip — la postura clásica del añadido que no toca al proveedor.
--     4. Se versiona solo, en MGGENCOBRO_VERSION (7 versiones).
--
-- Por qué se apaga:
--   Nuestro API hace hoy lo que hacía él, y los dos emiten el MISMO documento:
--   `MGGENCOBRO_CONFIG` tiene una sola fila, ConceptoCredito=24533, y 24533 se
--   llama "Enganche" — el mismo concepto que escribe venta_writer.go. El
--   resultado en producción son pares diarios: nuestro enganche cancelado y
--   otro idéntico al lado. Medido el 20-ago: 81 enganches nuestros, 20
--   cancelados, 20 creados por usuarios RUTAxx.
--
-- Qué hace esta migración, exactamente:
--   Desactiva el ÚNICO objeto de MGGENCOBRO que toca una tabla de Microsip: un
--   AFTER INSERT sobre DOCTOS_PV que no cobra nada, sólo encola la venta en
--   `MGGENCOBRO_VEMOST_PROCESADAS` con PROCESADA='N' cuando
--   `TIPO_DOCTO='V' AND ESTATUS='N'` — que es literalmente lo que venta_writer
--   inserta. Por eso TODA venta nuestra entraba en su cola de trabajo.
--
--   Se comprobó por búsqueda de texto sobre RDB$TRIGGER_SOURCE,
--   RDB$PROCEDURE_SOURCE y RDB$VIEW_SOURCE que ningún otro objeto de la base
--   menciona la cola ni el producto. Apagado este trigger, la parte de
--   MGGENCOBRO que vive en la base queda muerta.
--
-- INACTIVE y no DROP, a propósito:
--   `ALTER TRIGGER ... INACTIVE` conserva el código fuente en el catálogo. Si
--   hubiera que volver atrás basta un ACTIVE, sin necesitar el instalador del
--   tercero para recuperar un cuerpo que nadie tiene en un repositorio.
--
-- Lo que esta migración NO hace:
--   No toca las 5 tablas de MGGENCOBRO ni borra un solo renglón. Su bitácora
--   —66,441 documentos, 66,212 de ellos enganches— es el registro histórico de
--   años de operación y sigue siendo consultable.
--
-- Cómo se verifica que sirvió:
--   Apagar el trigger detiene el ENCOLADO. Si después de aplicarla siguieran
--   apareciendo pares de enganches, entonces quedaría un ejecutable corriendo
--   en alguna máquina y el trabajo sería localizarlo, no de base de datos.
--   El conteo va sobre DOCTOS_CC con CONCEPTO_CC_ID=24533 y CANCELADO='S'
--   durante 2-3 días.
--
-- Sin ventana: ALTER TRIGGER es un cambio de metadatos. Firebird lo aplica a
--   las transacciones que empiecen después; las en vuelo terminan con la
--   versión que tomaron.
-- ============================================================================

ALTER TRIGGER MGGENCOBRO_DOCTOS_PV_AI0 INACTIVE;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (60, '000060_desactivar_mggencobro', CURRENT_TIMESTAMP);
COMMIT;
