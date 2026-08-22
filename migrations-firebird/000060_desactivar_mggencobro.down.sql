-- Down para 000060_desactivar_mggencobro.
--
-- Vuelve a encender el encolado de MGGENCOBRO. Reversión limpia: el cuerpo del
-- trigger nunca se borró, sólo se marcó inactivo.
--
-- OJO con lo que significa revertir. Encender el trigger devuelve el conflicto
-- que la 000060 vino a quitar: nuestras ventas vuelven a entrar en la cola de
-- un programa que emite el MISMO enganche (concepto 24533) y cancela el
-- nuestro. Sólo tiene sentido correr este down si se decidió que MGGENCOBRO
-- vuelva a ser el que genera el enganche — y en ese caso hay que quitar antes
-- el enganche de venta_writer.go, o los pares regresan.
--
-- Las filas que se hayan dejado de encolar mientras estuvo apagado NO se
-- recuperan: el trigger sólo actúa sobre el INSERT. Si alguna vez hiciera
-- falta, se reconstruyen desde DOCTOS_PV con el mismo predicado
-- (TIPO_DOCTO='V' AND ESTATUS='N') sobre el rango de fechas afectado.

ALTER TRIGGER MGGENCOBRO_DOCTOS_PV_AI0 ACTIVE;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 60;
COMMIT;
