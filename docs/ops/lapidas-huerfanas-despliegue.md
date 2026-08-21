# Lápidas huérfanas — medición y despliegue

> Acompaña al arreglo del predicado de ventas (`ventaStatusFilterConVentana`,
> rama `NOT EXISTS`) y a la migración `000058`. Léase junto con
> `docs/COBRANZA-SYNC.md` §8 (orden de despliegue) y §3 (cuándo subir el epoch).

## Qué es una lápida huérfana

Cuando oficina **elimina** una venta en Microsip, el trigger
`MSP_SALDOS_DOCTOS_CC_AD` (migración `000020`) no borra la fila del caché:
deja una lápida.

```
CARGO_CANCELADO = 'S'   SALDO = 0   FECHA_ULT_PAGO = NULL   (y sin fila en DOCTOS_CC)
```

Esa lápida es la **única** señal con la que el teléfono borra la venta. Entre
el 2026-06-02 y el arreglo, no salía por ningún canal: la rama de cancelados
exigía por `EXISTS` que el cargo siguiera en `DOCTOS_CC`, y la propia lápida
había puesto saldo 0 y fecha nula, así que las otras dos ramas del predicado
tampoco la rescataban.

## La medición

**Sólo lectura.** Contra el snapshot de producción, no contra el dev.

```sql
SELECT s.ZONA_CLIENTE_ID, COUNT(*)
FROM MSP_SALDOS_VENTAS s
WHERE s.CARGO_CANCELADO = 'S'
  AND NOT EXISTS (SELECT 1 FROM DOCTOS_CC d WHERE d.DOCTO_CC_ID = s.DOCTO_CC_ID)
GROUP BY 1 ORDER BY 2 DESC;
```

### Dos trampas de esta medición

1. **La base de desarrollo no sirve como fuente.** Medido el 2026-08-20 sobre
   `MUEBLERA.FDB` local: **1,120** lápidas huérfanas, con folios sintéticos
   (`M4P92DD78`, `PRJ99A670`, `SDDE85608`) y `UPDATED_AT` del mismo día. Son
   residuos de la suite de integración —los `t.Cleanup` que descartan el error
   del `DELETE` dejan la fila para siempre, justo lo que advierte `CLAUDE.md`
   §7— no ventas que oficina borró. Un conteo de dev no dice nada de campo.

2. **Las lápidas con `ZONA_CLIENTE_ID` nulo no le llegan a nadie.** El sync es
   por zona (`WHERE s.ZONA_CLIENTE_ID = ?`), así que una lápida sin zona no
   viaja por ningún canal, con o sin este arreglo, y **no se cuenta** para
   decidir el bump del epoch. En dev eran 108 de las 1,120.

**Las zonas del día del análisis no se hardcodean.** El conteo de producción
del 2026-08-20 fueron 29 lápidas en 11 zonas, todas entre el 13 y el 20 de
agosto (22 el día del cutover). Para cuando se aplique la migración ese
conjunto habrá cambiado: **se vuelve a medir en el paso 2** y con esa lista se
regenera `000058`.

## Adopción de `AFTER_ID` — por qué condiciona qué zonas se migran

Subir el epoch mete a cada teléfono de esa zona en un replay completo. Un
teléfono que **no** persiste `AFTER_ID` no puede terminar el replay: se queda
re-descargando por ciclo (`COBRANZA-SYNC.md` §2 y §8, restricción D1).

Cómo verificarlo, en orden de fuerza:

1. **`MIN_APP_VERSION` en producción.** Si la variable está puesta en una
   versión **igual o mayor** a la que introdujo `AFTER_ID` (2.17.x), la
   pregunta de adopción no existe: `middleware.MinAppVersion` responde `409` a
   cualquier build anterior, así que no hay teléfono viejo capaz de
   sincronizar. Se lee del `.env` del servidor de producción. Ojo: el default
   del binario es cadena vacía, que **desactiva la compuerta** — un `.env` sin
   la variable no protege nada.
2. **La compuerta de `:core:appgate`** en Firestore (la versión mínima que la
   flota respeta antes de llegar al API).
3. **Conteo manual de la flota** por zona, como se hizo el 2026-08-17
   (37/57 en la adopción de 2.17.2).

El API **no** registra hoy el `X-App-Version` de cada request: el header sólo
lo consume la compuerta, nadie lo persiste ni lo loguea. No hay, por tanto,
forma de reconstruir la adopción por zona desde el servidor. Si alguna vez
hace falta, esa es la pieza que falta.

**Zona cuyos teléfonos no estén todos por encima de la versión con `AFTER_ID`:
no se migra todavía.** Se deja fuera de `000058` y se bumpea después. No pasa
nada por esperar: desde el paso 1 sus borrados **nuevos** ya se limpian solos.

## Orden de despliegue

| # | Paso | Por qué ahí |
|---|---|---|
| 1 | API con el predicado nuevo, **sin migración** | Los borrados nuevos se limpian solos desde ya. Cero re-descarga para nadie |
| 2 | Repetir la medición + verificar adopción de `AFTER_ID` | Decide qué zonas entran en la migración |
| 3 | `make fb-snapshot NAME=antes-epoch-tombstones` | |
| 4 | Aplicar `000058` con las zonas del paso 2 | Cada teléfono de esas zonas replica **una vez** y termina |

Las migraciones de Firebird no corren al arrancar el API (`make fb-migrate-up`
es manual), que es lo que hace separable el paso 1 del paso 4.

## Verificación posterior (sólo lectura)

- Repetir el conteo por zona: **debe bajar**.
- El caso reportado —ANTONIA JIMENEZ PEREZ, cargo `15881865`, zona `21564`—
  ya no debe estar en el teléfono de su cobrador.
- En dispositivo (`COBRANZA-SYNC.md` §9, con `devlocal` y `adb reverse`,
  **nunca `devserver`**):
  1. Borrar una venta en Microsip → sincronizar → desaparece del teléfono.
  2. Cancelar una venta con más de 7 días → **no** viaja: la ventana sigue
     acotando la rama de cancelaciones, que no se tocó.
  3. Matar la app a media descarga tras el epoch → al reabrir retoma o replica
     una vez, y **termina**.
