-- 000051_grant_msp_tables_to_microsip_users
--
-- HOTFIX (2026-07-30). Root cause of the Microsip "Cxc.exe" access violation
-- ("Read of address 00000001") and the "no permission for SELECT access to
-- TABLE MSP_PAGOS_RECIBIDOS" error when a Microsip user applies/updates a cobro:
--
-- Migrations 000010/000013/000014/000021/000023/000024 attach triggers to
-- Microsip's OWN native tables (DOCTOS_CC, IMPORTES_DOCTOS_CC, CLIENTES,
-- FORMAS_COBRO_DOCTOS). Those triggers — and the procedures they call
-- (MSP_RECOMPUTE_PAGO, MSP_RECOMPUTE_SALDO_VENTA) — read/write our MSP_*
-- operational tables. A trigger fired by a Microsip desktop operation runs with
-- the INVOKER's privileges, i.e. the Microsip application user (e.g.
-- YESENIA_CRISTAL). Those users were never granted access to the MSP_* tables,
-- so the trigger's SELECT/INSERT fails and Microsip's Cxc.exe (which mishandles
-- the Firebird error) crashes with an access violation. The legacy Node API did
-- not create these triggers, so it never hit this. Automated tests never hit it
-- because they run as SYSDBA (full access).
--
-- Fix: grant the Microsip users (PUBLIC) exactly the access those triggers need
-- — matching how MSP_CHANGE_LOG is already granted (INSERT to PUBLIC) and how
-- Microsip grants its own tables. MSP_PAGOS_RECIBIDOS stays SELECT-only (it is
-- written only by our API); the caches and append-only logs get read+write so
-- the recompute path can upsert/log from within a Microsip user's transaction.
--
-- Long-term hardening (deferred): recreate the recompute procedures/triggers
-- with SQL SECURITY DEFINER so they run as the definer regardless of invoker,
-- and drop these PUBLIC grants. This migration unblocks production now.

GRANT SELECT ON MSP_PAGOS_RECIBIDOS TO PUBLIC;
GRANT SELECT, INSERT, UPDATE, DELETE ON MSP_PAGOS_VENTAS TO PUBLIC;
GRANT SELECT, INSERT, UPDATE, DELETE ON MSP_SALDOS_VENTAS TO PUBLIC;
GRANT SELECT, INSERT, UPDATE, DELETE ON MSP_PAGOS_CHANGELOG TO PUBLIC;
GRANT SELECT, INSERT, UPDATE, DELETE ON MSP_SALDOS_CHANGELOG TO PUBLIC;
GRANT SELECT, INSERT, UPDATE, DELETE ON MSP_SALDOS_ERRORS TO PUBLIC;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (51, '000051_grant_msp_tables_to_microsip_users', CURRENT_TIMESTAMP);
COMMIT;
