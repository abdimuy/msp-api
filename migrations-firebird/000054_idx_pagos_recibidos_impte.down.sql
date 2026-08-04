-- Down for 000054_idx_pagos_recibidos_impte
-- Drops the index added in the up migration.

DROP INDEX IDX_MSP_PAGOS_RECIB_IMPTE;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 54;
COMMIT;
