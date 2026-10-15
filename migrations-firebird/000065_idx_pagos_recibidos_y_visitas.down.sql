-- Down for 000065_idx_pagos_recibidos_y_visitas
-- Drops the three indexes added in the up migration. The SET STATISTICS of
-- the pre-existing indexes is not reverted: it is a recalculated estimate,
-- not structure, and the old values were stale.

DROP INDEX IDX_MSP_VISITAS_ZONA_FECHA;
DROP INDEX IDX_MSP_PAGOS_REC_DOCTO_IMPTE_ID;
DROP INDEX IDX_MSP_PAGOS_REC_IMPTE_ID;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 65;
COMMIT;
