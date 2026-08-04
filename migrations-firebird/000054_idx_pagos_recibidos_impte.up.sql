-- 000054_idx_pagos_recibidos_impte
--
-- Supports the correlated scalar subquery added to selectPagoColsP in the
-- /sync/pagos and /pagos/by-ids projections (see
-- internal/cobranza/infra/ventfb/pagos_repo.go):
--
--   (SELECT MIN(pr.ID) FROM MSP_PAGOS_RECIBIDOS pr
--     WHERE pr.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID)
--
-- which exposes MSP_PAGOS_RECIBIDOS.ID (the client-generated pago UUID) as
-- `pago_recibido_id` so the Android app can exact-match a numeric synced pago
-- to its local UUID row.
--
-- Deliberately NOT a JOIN: a JOIN on a non-unique predicate can fan out (one
-- MSP_PAGOS_VENTAS row multiplying into duplicate sync rows). The scalar
-- subquery with MIN() can only ever return zero or one value per outer row,
-- making duplication structurally impossible rather than merely unlikely.
-- This index is what turns that subquery into an indexed lookup per row
-- instead of a full scan of MSP_PAGOS_RECIBIDOS for every page of
-- MSP_PAGOS_VENTAS.

CREATE INDEX IDX_MSP_PAGOS_RECIB_IMPTE ON MSP_PAGOS_RECIBIDOS (IMPTE_DOCTO_CC_ID);
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (54, '000054_idx_pagos_recibidos_impte', CURRENT_TIMESTAMP);
COMMIT;
