-- Drop the generator if it exists. The down migration is destructive: any
-- traspaso folio numbering already in flight would lose its high-water mark.
-- Only run in test/CI environments where state is reset between runs.

SET TERM ^ ;

EXECUTE BLOCK
AS
BEGIN
  IF (EXISTS (
    SELECT 1 FROM RDB$GENERATORS
    WHERE RDB$GENERATOR_NAME = 'GEN_MST_FOLIO'
  )) THEN
    EXECUTE STATEMENT 'DROP GENERATOR GEN_MST_FOLIO';
END^

SET TERM ; ^

COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 28;
COMMIT;
