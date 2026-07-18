package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// SinMapeoZonaCaja is the Microsip sentinel value stored in the NOT NULL
// CAJA_ID/CAJERO_ID/VENDEDOR_ID/COBRADOR_ID columns of MSP_CFG_ZONA_CAJA to
// mean "no mapping assigned" for that slot. Unlike VendedorMapping (whose
// columns are nullable), MSP_CFG_ZONA_CAJA has no NULL option, so -1 is the
// only way to express "sin mapeo" here.
const SinMapeoZonaCaja = -1

// ErrZonaCajaIDInvalido is returned when a ZonaCajaConfig id is invalid: the
// zona id is not strictly positive, or a caja/cajero/vendedor/cobrador slot
// is neither strictly positive nor exactly SinMapeoZonaCaja.
var ErrZonaCajaIDInvalido = apperror.NewValidation(
	"zona_caja_id_invalido",
	"el id de zona/caja es inválido",
)

// ZonaCajaConfig is the value object persisted in MSP_CFG_ZONA_CAJA: the
// caja/cajero/vendedor/cobrador Microsip ids assigned to one client zone.
// Every slot is either a positive Microsip id or SinMapeoZonaCaja.
type ZonaCajaConfig struct {
	ZonaClienteID int
	CajaID        int
	CajeroID      int
	VendedorID    int
	CobradorID    int
}

// NewZonaCajaConfig builds a ZonaCajaConfig, validating that ZonaClienteID is
// strictly positive and that each of caja/cajero/vendedor/cobrador is either
// strictly positive or exactly SinMapeoZonaCaja (-1). Zero and any other
// negative value are rejected.
func NewZonaCajaConfig(zonaClienteID, cajaID, cajeroID, vendedorID, cobradorID int) (ZonaCajaConfig, error) {
	if zonaClienteID <= 0 {
		return ZonaCajaConfig{}, ErrZonaCajaIDInvalido
	}
	for _, id := range []int{cajaID, cajeroID, vendedorID, cobradorID} {
		if id != SinMapeoZonaCaja && id <= 0 {
			return ZonaCajaConfig{}, ErrZonaCajaIDInvalido
		}
	}
	return ZonaCajaConfig{
		ZonaClienteID: zonaClienteID,
		CajaID:        cajaID,
		CajeroID:      cajeroID,
		VendedorID:    vendedorID,
		CobradorID:    cobradorID,
	}, nil
}
