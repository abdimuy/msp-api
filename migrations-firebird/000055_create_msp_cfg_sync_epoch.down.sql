-- Down para 000055_create_msp_cfg_sync_epoch.
--
-- MSP_CFG_SYNC_EPOCH es greenfield (la creó esta misma migración), así que
-- el down puede borrarla sin más. Consecuencia funcional: sin la tabla, el
-- repositorio Go degrada a epoch efectivo 0 y el sync sigue funcionando —
-- lo único que se pierde es la palanca para forzar resincronizaciones.

DROP TABLE MSP_CFG_SYNC_EPOCH;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 55;
COMMIT;
