-- 000053_grant_msp_procedures_to_microsip_users
--
-- HOTFIX (2026-07-31). Third and final layer of the same root cause behind the
-- Cxc.exe crash on "Aplicar cobro" (see 000051 for tables, 000052 for
-- generators).
--
-- Our AFTER triggers on Microsip's native tables (DOCTOS_CC, IMPORTES_DOCTOS_CC,
-- FORMAS_COBRO_DOCTOS/CLIENTES) run with the INVOKER's privileges (the Microsip
-- desktop user, e.g. LISDED) and their body does EXECUTE PROCEDURE on our
-- recompute procedures. In Firebird, executing a stored procedure requires a
-- separate GRANT EXECUTE — neither the table grants (000051) nor the generator
-- grants (000052) cover it. So the desktop user still failed with:
--   "no permission for EXECUTE access to PROCEDURE MSP_RECOMPUTE_SALDO_VENTA"
-- and Cxc.exe again turned the Firebird error into an access violation.
--
-- These two procedures are exactly the ones the invoker-context triggers call
-- (confirmed via RDB$DEPENDENCIES, RDB$DEPENDED_ON_TYPE = 5). Neither procedure
-- calls another procedure, and neither depends on any UDF/function (their only
-- other dependencies are the MSP_* tables and generators already granted in
-- 000051/000052, plus the UTF8 character set which needs no grant). This
-- completes the invoker privilege chain: EXECUTE (procs) + table access +
-- generator USAGE.
--
-- Long-term hardening (deferred, same as 000051/000052): recreate these
-- procedures and the triggers with SQL SECURITY DEFINER so they run as the
-- definer regardless of invoker, and drop all these PUBLIC grants.

GRANT EXECUTE ON PROCEDURE MSP_RECOMPUTE_PAGO TO PUBLIC;
GRANT EXECUTE ON PROCEDURE MSP_RECOMPUTE_SALDO_VENTA TO PUBLIC;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (53, '000053_grant_msp_procedures_to_microsip_users', CURRENT_TIMESTAMP);
COMMIT;
