package outbound

import (
	"testing"
	"time"
)

// TestProductionClock_Now cubre la única sentencia de producción que vive en el
// paquete de puertos.
//
// Existe por lo que destapó el Arreglo B: la compuerta de cobertura pasó a
// exigir un piso por paquete DESCUBIERTO, y `ports/outbound` —que el reporte de
// la Task 25 daba por "solo interfaces, sin sentencias"— resultó tener una:
// `ProductionClock.Now`, al 0.0%. No era un paquete vacío, era un paquete que
// nadie miraba.
//
// La aserción que importa es la zona. `ProductionClock` promete UTC en su
// comentario ("timestamps used to build a Visita are normalized at the source"),
// y un `time.Now()` sin `.UTC()` compila igual y devuelve la hora local del
// servidor: la visita quedaría sellada con un instante correcto pero una zona
// que no es la del contrato. Quitar el `.UTC()` pone este test en rojo.
func TestProductionClock_Now(t *testing.T) {
	t.Parallel()

	antes := time.Now().UTC()
	ahora := ProductionClock{}.Now()
	despues := time.Now().UTC()

	if ahora.Location() != time.UTC {
		t.Fatalf("ProductionClock.Now debe devolver UTC, devolvió %s", ahora.Location())
	}
	if ahora.Before(antes) || ahora.After(despues) {
		t.Fatalf("ProductionClock.Now devolvió %s, fuera de la ventana [%s, %s]", ahora, antes, despues)
	}
}

// TestProductionClock_ImplementaClock fija que la implementación real siga
// satisfaciendo el puerto: si alguien le cambia la firma a `Now`, esto no
// compila, que es el momento correcto para enterarse.
func TestProductionClock_ImplementaClock(t *testing.T) {
	t.Parallel()

	var reloj Clock = ProductionClock{}
	if reloj.Now().IsZero() {
		t.Fatal("el reloj de producción devolvió el instante cero")
	}
}
