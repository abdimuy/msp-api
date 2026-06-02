//nolint:misspell // Microsip catálogo names are Spanish.
package outbound

// Default catálogo IDs for the cliente auto-create flow. These are catalogue
// values the MicrosipClienteWriter expects; they live in the outbound port
// package so the app layer can build MicrosipClienteInput without importing
// infra. Verified against DESARROLLO.FDB (Mueblería Tehuacán, single-sucursal).
// When a second sucursal or empresa is added, lift these into a
// MSP_CFG_CLIENTE_DEFAULTS row and resolve at AplicarConfig-time. YAGNI for v1.
const (
	DefaultCondPagoID               = 21497 // CONDS_PAGO: contado
	DefaultTipoClienteID            = 21499 // TIPOS_CLIENTES: particular
	DefaultMonedaID                 = 1     // MONEDAS: MXN
	DefaultCiudadID                 = 338   // CIUDADES: Tehuacán
	DefaultEstadoID                 = 337   // ESTADOS: Puebla
	DefaultPaisID                   = 336   // PAISES: México
	DefaultViaEmbarqueID            = 87621 // VIAS_EMBARQUE
	DefaultComprobanteDomicilioID   = 2992  // catálogo Mueblera
	DefaultIdentificacionOficialID  = 6597  // catálogo Mueblera (INE)
	DefaultRolClaveClientePrincipal = 2     // ROLES_CLAVES_CLIENTES: principal
	DefaultLocalidad                = -1    // sin localidad
)
