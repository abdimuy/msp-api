-- 000052_grant_msp_generators_to_microsip_users
--
-- HOTFIX (2026-07-31). Follow-up to 000051. Same root cause, one layer deeper.
--
-- 000051 granted the Microsip users (PUBLIC) access to the MSP_* TABLES that our
-- triggers/recompute procedures touch when fired from a Microsip desktop
-- operation (Cxc.exe "Aplicar cobro"). But the recompute path also assigns row
-- IDs by calling gen_id() on three of our GENERATORS, and in Firebird a
-- generator needs a separate GRANT USAGE — a table grant does not cover it.
-- So the desktop user (e.g. AURORA) still failed with:
--   "no permission for USAGE access to GENERATOR GEN_MSP_SALDOS_ERRORS_ID"
-- and Cxc.exe again mishandled the Firebird error into an access violation.
--
-- The three generators below are exactly the ones that MSP_RECOMPUTE_PAGO,
-- MSP_RECOMPUTE_SALDO_VENTA and the triggers on Microsip's native tables
-- (DOCTOS_CC, IMPORTES_DOCTOS_CC, CLIENTES) depend on — confirmed via
-- RDB$DEPENDENCIES (RDB$DEPENDED_ON_TYPE = 14). No other generator is used by
-- any MSP_* trigger/procedure; the *_LISTEN / MSP_CHANGE_LOG path uses none.
--
-- Long-term hardening (deferred, same as 000051): recreate these procedures and
-- triggers with SQL SECURITY DEFINER so they run as the definer regardless of
-- invoker, and drop these PUBLIC grants.

GRANT USAGE ON GENERATOR GEN_MSP_SALDOS_ERRORS_ID TO PUBLIC;
GRANT USAGE ON GENERATOR GEN_MSP_PAGOS_CHANGELOG_SEQ TO PUBLIC;
GRANT USAGE ON GENERATOR GEN_MSP_SALDOS_CHANGELOG_SEQ TO PUBLIC;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (52, '000052_grant_msp_generators_to_microsip_users', CURRENT_TIMESTAMP);
COMMIT;
