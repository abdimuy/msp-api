/* ---------------------------------------------------------------------------
 * db-test-anonymize.sql — sustituye por datos sintéticos los datos personales
 * que SOBREVIVEN al `-skip_data` de scripts/db-test-skip-tables.txt.
 *
 * El padrón de clientes (CLIENTES, DIRS_CLIENTES, LIBRES_CLIENTES y su cierre
 * transitivo) NO se toca aquí: sale del artefacto por `-skip_data`, sin datos
 * que sustituir.
 *
 * Lo que queda son catálogos de PERSONAS que los tests SÍ necesitan poblados
 * —cobradores, vendedores y cajeros los referencia MSP_CFG_ZONA_CAJA, y
 * LISTAS_ATRIBUTOS lo lee el módulo config— más unos catálogos chicos que
 * nadie lee pero que traen nombres, teléfonos, RFC y CURP reales. Sustituir
 * esas ~950 filas es más barato que arrastrar el cierre transitivo de
 * EMPLEADOS (29 tablas) a la lista de omisión.
 *
 * Los identificadores NO se tocan: las llaves foráneas y las referencias desde
 * MSP_CFG_ZONA_CAJA siguen resolviendo. Sólo cambian nombre, contacto,
 * teléfono, correo, RFC/CURP y domicilio.
 *
 * Se ejecuta contra la base RECORTADA, nunca contra MUEBLERA.FDB.
 *
 *   docker exec -i mueblera-firebird /usr/local/firebird/bin/isql \
 *     -user sysdba -password masterkey \
 *     -i /tmp/db-test-anonymize.sql /firebird/data/CAT.FDB
 *
 * Los nombres sintéticos se derivan del identificador de cada fila con MOD(),
 * así que la corrida es determinista: la misma base de entrada produce la
 * misma salida.
 * ------------------------------------------------------------------------- */

/* --- COBRADORES (52) — el nombre real venía como "RUTA 01 - <persona>" ----- */
UPDATE COBRADORES SET NOMBRE =
  'RUTA ' || COBRADOR_ID || ' - ' ||
  CASE MOD(COBRADOR_ID, 12)
    WHEN 0 THEN 'ALEJANDRO' WHEN 1 THEN 'MARIA GUADALUPE' WHEN 2 THEN 'JOSE ANTONIO'
    WHEN 3 THEN 'LUCIA'     WHEN 4 THEN 'RICARDO'         WHEN 5 THEN 'ANA SOFIA'
    WHEN 6 THEN 'MIGUEL'    WHEN 7 THEN 'ELENA'           WHEN 8 THEN 'FERNANDO'
    WHEN 9 THEN 'PATRICIA'  WHEN 10 THEN 'HECTOR'         ELSE 'ROSA MARIA' END
  || ' ' ||
  CASE MOD(COBRADOR_ID, 11)
    WHEN 0 THEN 'ZUBIETA'    WHEN 1 THEN 'ARRIETA'   WHEN 2 THEN 'GOROSTIZA'
    WHEN 3 THEN 'ELIZONDO'   WHEN 4 THEN 'MONTEMAYOR' WHEN 5 THEN 'ITURBE'
    WHEN 6 THEN 'OYARZUN'    WHEN 7 THEN 'ARCEO'     WHEN 8 THEN 'AMEZQUITA'
    WHEN 9 THEN 'BERRUECOS'  ELSE 'DOSAMANTES' END;

/* --- VENDEDORES (46) — hoy son claves de ruta, pero puede entrar un nombre - */
UPDATE VENDEDORES SET NOMBRE =
  'VENDEDOR ' || VENDEDOR_ID || ' ' ||
  CASE MOD(VENDEDOR_ID, 10)
    WHEN 0 THEN 'ECHEGARAY' WHEN 1 THEN 'FIGUEROLA' WHEN 2 THEN 'IRIARTE'
    WHEN 3 THEN 'LANDEROS'  WHEN 4 THEN 'MERCADANTE' WHEN 5 THEN 'OSORNIO'
    WHEN 6 THEN 'RUELAS'    WHEN 7 THEN 'TAMEZ'     WHEN 8 THEN 'URIBARRI'
    ELSE 'VILLALPANDO' END;

/* --- CAJEROS (66) ---------------------------------------------------------- */
UPDATE CAJEROS SET NOMBRE =
  'CAJERO ' || CAJERO_ID || ' ' ||
  CASE MOD(CAJERO_ID, 9)
    WHEN 0 THEN 'YBARRA'    WHEN 1 THEN 'ZUBIETA'  WHEN 2 THEN 'ARRIETA'
    WHEN 3 THEN 'GOROSTIZA' WHEN 4 THEN 'ELIZONDO' WHEN 5 THEN 'MONTEMAYOR'
    WHEN 6 THEN 'ITURBE'    WHEN 7 THEN 'OYARZUN'  ELSE 'ARCEO' END;

/* --- AGENTES (41) — nombre y celular reales -------------------------------- */
UPDATE AGENTES SET
  NOMBRE  = 'AGENTE ' || AGENTE_ID || ' ' ||
    CASE MOD(AGENTE_ID, 8)
      WHEN 0 THEN 'AMEZQUITA' WHEN 1 THEN 'BERRUECOS' WHEN 2 THEN 'DOSAMANTES'
      WHEN 3 THEN 'ECHEGARAY' WHEN 4 THEN 'FIGUEROLA' WHEN 5 THEN 'IRIARTE'
      WHEN 6 THEN 'LANDEROS'  ELSE 'MERCADANTE' END,
  CELULAR = '(238)100-' || LPAD(CAST(MOD(AGENTE_ID, 10000) AS VARCHAR(4)), 4, '0');

/* --- BENEFICIARIOS (26) — nombre + RFC (el RFC codifica fecha de nacimiento) */
UPDATE BENEFICIARIOS SET
  NOMBRE = 'BENEFICIARIO ' || BENEFICIARIO_ID,
  RFC    = CASE WHEN RFC IS NULL THEN NULL ELSE 'XAXX010101000' END,
  EMAIL  = CASE WHEN EMAIL IS NULL THEN NULL ELSE 'beneficiario' || BENEFICIARIO_ID || '@example.invalid' END;

/* --- EMPLEADOS (1) — CURP, RFC, domicilio, nombres del padre y de la madre - */
UPDATE EMPLEADOS SET
  NOMBRES          = 'EMPLEADO',
  APELLIDO_PATERNO = 'SINTETICO',
  APELLIDO_MATERNO = 'DE PRUEBA',
  NOMBRE_COMPLETO  = 'EMPLEADO SINTETICO DE PRUEBA',
  NOMBRE_PADRE     = CASE WHEN NOMBRE_PADRE IS NULL THEN NULL ELSE 'PADRE SINTETICO' END,
  NOMBRE_MADRE     = CASE WHEN NOMBRE_MADRE IS NULL THEN NULL ELSE 'MADRE SINTETICA' END,
  CURP             = CASE WHEN CURP IS NULL THEN NULL ELSE 'XAXX010101HDFXXX00' END,
  RFC              = CASE WHEN RFC IS NULL THEN NULL ELSE 'XAXX010101000' END,
  EMAIL            = CASE WHEN EMAIL IS NULL THEN NULL ELSE 'empleado@example.invalid' END,
  TELEFONO1        = CASE WHEN TELEFONO1 IS NULL THEN NULL ELSE '5550000000' END,
  TELEFONO2        = CASE WHEN TELEFONO2 IS NULL THEN NULL ELSE '5550000000' END,
  CALLE            = CASE WHEN CALLE IS NULL THEN NULL ELSE 'CALLE SINTETICA' END,
  NOMBRE_CALLE     = CASE WHEN NOMBRE_CALLE IS NULL THEN NULL ELSE 'CALLE SINTETICA' END,
  NUM_EXTERIOR     = CASE WHEN NUM_EXTERIOR IS NULL THEN NULL ELSE '0' END,
  NUM_INTERIOR     = CASE WHEN NUM_INTERIOR IS NULL THEN NULL ELSE '0' END,
  COLONIA          = CASE WHEN COLONIA IS NULL THEN NULL ELSE 'COLONIA SINTETICA' END,
  CODIGO_POSTAL    = CASE WHEN CODIGO_POSTAL IS NULL THEN NULL ELSE '00000' END;

/* --- MSP_USUARIOS (9) — correos personales del equipo ---------------------- */
/* El correo es la llave con la que el auto-aprovisionador reconoce a un
 * usuario; se sustituye por uno sintético del dominio de la empresa para que
 * siga viéndose como un correo válido. Los tests siembran los suyos. */
UPDATE MSP_USUARIOS SET
  NOMBRE   = 'USUARIO DE PRUEBA ' || SUBSTRING(ID FROM 1 FOR 8),
  EMAIL    = 'usuario.' || LOWER(SUBSTRING(ID FROM 1 FOR 8)) || '@muebleriamsp.mx',
  TELEFONO = NULL;

/* --- CTI_* del sistema legado (1 fila cada uno) ---------------------------- */
UPDATE CTI_USUARIOS_SIVER SET NOMBRE = 'USUARIO SIVER';
UPDATE CTI_CONFIGURA_SIVER SET
  NOMBRE        = 'EMPRESA DE PRUEBA',
  DIRECCION     = CASE WHEN DIRECCION IS NULL THEN NULL ELSE 'CALLE SINTETICA 0' END,
  RFC           = CASE WHEN RFC IS NULL THEN NULL ELSE 'XAXX010101000' END,
  CTACORREO     = CASE WHEN CTACORREO IS NULL THEN NULL ELSE 'correo@example.invalid' END,
  CONTRACORREO  = CASE WHEN CONTRACORREO IS NULL THEN NULL ELSE '' END,
  COPIACORREO   = CASE WHEN COPIACORREO IS NULL THEN NULL ELSE 'correo@example.invalid' END,
  SMTPCORREO    = CASE WHEN SMTPCORREO IS NULL THEN NULL ELSE 'smtp.example.invalid' END;

/* --- LISTAS_ATRIBUTOS — el padrón de vendedores del "campo libre" ----------
 * Microsip guarda en LIBRES_CARGOS_CC.VENDEDOR_1/2/3 el ID de una lista, y el
 * nombre humano vive aquí. Los atributos 11350/11351/11702 y 19985/19986/19987
 * son esas listas: ~620 filas con NOMBRES DE PERSONAS reales, algunos de los
 * cuales también son clientes.
 *
 * Sólo se sustituyen esos seis atributos. Los demás son enumeraciones que el
 * código compara por valor —`UPPER(VALOR_DESPLEGADO) = 'CONTADO'` en
 * internal/clientes/infra/clientesfb/queries.go— y el atributo 787502 es el
 * catálogo público de colonias.
 *
 * El nombre sintético se deriva de HASH() del valor original, no del ID de la
 * fila: la MISMA persona aparece en los tres atributos y debe seguir
 * apareciendo con el MISMO nombre, porque ListarIdentidadesMicrosip agrupa por
 * VALOR_DESPLEGADO. Un reemplazo por fila rompería esa agrupación.          */
UPDATE LISTAS_ATRIBUTOS SET VALOR_DESPLEGADO =
  CASE MOD(ABS(HASH(TRIM(VALOR_DESPLEGADO))), 12)
    WHEN 0 THEN 'ALEJANDRO' WHEN 1 THEN 'MARIA GUADALUPE' WHEN 2 THEN 'JOSE ANTONIO'
    WHEN 3 THEN 'LUCIA'     WHEN 4 THEN 'RICARDO'         WHEN 5 THEN 'ANA SOFIA'
    WHEN 6 THEN 'MIGUEL'    WHEN 7 THEN 'ELENA'           WHEN 8 THEN 'FERNANDO'
    WHEN 9 THEN 'PATRICIA'  WHEN 10 THEN 'HECTOR'         ELSE 'ROSA MARIA' END
  || ' ' ||
  CASE MOD(ABS(HASH(TRIM(VALOR_DESPLEGADO))), 11)
    WHEN 0 THEN 'ZUBIETA'    WHEN 1 THEN 'ARRIETA'   WHEN 2 THEN 'GOROSTIZA'
    WHEN 3 THEN 'ELIZONDO'   WHEN 4 THEN 'MONTEMAYOR' WHEN 5 THEN 'ITURBE'
    WHEN 6 THEN 'OYARZUN'    WHEN 7 THEN 'ARCEO'     WHEN 8 THEN 'AMEZQUITA'
    WHEN 9 THEN 'BERRUECOS'  ELSE 'DOSAMANTES' END
  || ' ' || MOD(ABS(HASH(TRIM(VALOR_DESPLEGADO))), 1000000)
WHERE ATRIBUTO_ID IN (11350, 11351, 11702, 19985, 19986, 19987);

/* --- REGISTRY — la identidad fiscal de la empresa -------------------------- */
/* El RFC que guarda es de 13 caracteres: persona física. Codifica el nombre y
 * la fecha de nacimiento del titular. Ningún paquete de Go lee esta tabla. */
UPDATE REGISTRY SET VALOR = 'XAXX010101000'       WHERE NOMBRE = 'Rfc';
UPDATE REGISTRY SET VALOR = 'EMPRESA DE PRUEBA'   WHERE NOMBRE = 'Nombre';
UPDATE REGISTRY SET VALOR = 'CALLE SINTETICA'     WHERE NOMBRE IN ('Calle', 'NombreCalle');
UPDATE REGISTRY SET VALOR = 'COLONIA SINTETICA'   WHERE NOMBRE IN ('Colonia', 'ColoniaClaveFiscal');
UPDATE REGISTRY SET VALOR = '00000'               WHERE NOMBRE = 'CodigoPostal';
UPDATE REGISTRY SET VALOR = '5550000000'          WHERE NOMBRE IN ('Telefono1', 'Telefono2');

/* --- REG_PATRONALES — el registro patronal del IMSS ------------------------ */
/* Identifica a la empresa, no a una persona, así que estrictamente no es un
 * dato personal. Se sustituye igual: es un identificador real ante una
 * autoridad y no hace falta para probar nada. Ningún paquete de Go la lee. */
UPDATE REG_PATRONALES SET NUM_REG_PATRONAL = 'E0000000000';

/* --- ARTICULOS.NOTAS_VENTAS — nota libre con el nombre de un vendedor ------ */
/* La columna es texto libre del capturista; nadie la lee desde Go. */
UPDATE ARTICULOS SET NOTAS_VENTAS = NULL WHERE NOTAS_VENTAS IS NOT NULL;

/* --- Domicilios y teléfonos de la empresa (no son datos personales, pero
 *     tampoco hacen falta para probar) -------------------------------------- */
/* El NOMBRE de ALMACENES sí se conserva: los tests de ventas resuelven el
 * almacén de origen por nombre (CAMIONETA_ASIGNADA). */
UPDATE ALMACENES SET
  TELEFONO1 = CASE WHEN TELEFONO1 IS NULL THEN NULL ELSE '5550000000' END,
  TELEFONO2 = CASE WHEN TELEFONO2 IS NULL THEN NULL ELSE '5550000000' END;
UPDATE SUCURSALES SET
  TELEFONO1 = CASE WHEN TELEFONO1 IS NULL THEN NULL ELSE '5550000000' END,
  TELEFONO2 = CASE WHEN TELEFONO2 IS NULL THEN NULL ELSE '5550000000' END;

COMMIT;

/* --- Barrido de bitácoras --------------------------------------------------
 * Los UPDATE de arriba disparan los triggers de Microsip. Las bitácoras y las
 * colas de sincronización se vacían DESPUÉS, para que el artefacto no
 * conserve el valor anterior de ningún campo.                                */
DELETE FROM SNUBE_EVENTOS_TEMP;
DELETE FROM SNUBE_SEMAFOROS;
DELETE FROM SNUBE_SEMAFOROS_EXPIRADOS;
DELETE FROM SNUBE_CONCILIACION_INFO;
DELETE FROM MSP_CHANGE_LOG;
COMMIT;
