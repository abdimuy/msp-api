# Evidencia — ventas

Registro de hechos y su prueba para el módulo de ventas (alta, edición, folios,
outbox hacia Microsip y el `VERSION`/CAS de `MSP_VENTAS`).

El diseño y las reglas viven en
[`../registro-de-evidencia.md`](../registro-de-evidencia.md).

## Los cuatro niveles

| Nivel | Qué significa | Qué tiene que enseñar |
|---|---|---|
| **MEDIDO** | Se ejecutó y hay salida | La salida, el comando o la consulta, la fecha, **dónde** (dev / producción / prueba) y un **control positivo** |
| **LEÍDO** | Está escrito en el código | `archivo:línea`. No se ejecutó |
| **INFERIDO** | Se deduce de lo leído | De qué se deduce, y **qué lo volvería MEDIDO** |
| **SUPUESTO** | Nadie lo revisó | Nada. Es una creencia, y se marca como tal |

**Regla dura:** las ventas son dinero. Ningún cambio que las toque puede
apoyarse en una afirmación por debajo de **MEDIDO**. Si está en INFERIDO,
primero se mide; si no se puede medir, no se cambia. Una ausencia no es un
hallazgo hasta enseñar el control positivo que demuestre que la búsqueda habría
encontrado el caso.

## Cómo se cita desde el código

El identificador (`E-VEN-004`) es el puente. Se cita desde el comentario que
explica la trampa, desde el nombre de la prueba que la guarda y desde el mensaje
del commit que la cierra:

```go
// El folio se asigna con GEN_MST_FOLIO dentro de la misma tx. Ver E-VEN-004.
```

```
fix(ventas): ...

Cierra E-VEN-004.
```

Los números son correlativos y **nunca se reutilizan ni se renumeran**. Lo
refutado **no se borra**: cambia de estado a REFUTADA y se queda, con la fecha y
con la condición que lo volvería cierto.

**Siguiente identificador libre: `E-VEN-001`.**

---

*Sin afirmaciones registradas todavía.* El archivo se siembra con la primera
medición de un cambio que toque ventas. Hay material candidato ya escrito —los
incidentes de `docs/ops/` y las notas de ventas bloqueadas por precio tier, del
`VERSION`/CAS y de los combos no editables— pero **nada de eso se ha
reverificado abriendo los archivos**, y sembrarlo sin verificar es exactamente
lo que este registro existe para impedir.
