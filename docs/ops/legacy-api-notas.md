# API legacy (`sys_msp_backend`) — cosas a considerar

El backend legacy en Node/TypeScript (`C:\projects\sys_msp_backend` en el Windows Server `SERVERM`)
convive con el `msp-api` en Go. Esta nota recoge lo que hay que tener presente para que no se caiga,
sobre todo tras cambios de infraestructura en Firebird.

## Stack del legacy

- **Node + TypeScript**, corre desde `dist/` (compilado). Sirve en `http://localhost:3001`.
- Conecta a **Firebird** vía el driver **`node-firebird` 1.1.9** (fork `hgourvest/node-firebird`, sin mantenimiento).
- También usa **MongoDB** (sincronización automática cada 30s).
- Config de conexión en `src/config/env.ts` + `.env.prod` / `.env.dev`.

## ⚠️ Compatibilidad con Firebird 5.0 (lo que descubrimos — jun 2026)

El server se actualizó de **Firebird 3.0 → 5.0**. El driver `node-firebird` 1.1.9 es viejo y choca con
los defaults endurecidos de FB5. Hubo que relajar **dos** ajustes en
`C:\Program Files\Firebird\Firebird_5_0\firebird.conf`:

### 1. Cifrado de protocolo (WireCrypt)

- **Síntoma:** `Incompatible wire encryption levels requested on client and server` (gdscode **335545064**).
- **Causa:** FB5 trae `WireCrypt = Required` por default. El driver 1.1.9 **no negocia cifrado** (su
  `connection.js` no tiene nada de ChaCha/Arc4).
- **Fix:** en `firebird.conf` →
  ```
  WireCrypt = Enabled
  ```
  `Enabled` = cifra cuando el cliente puede (el Go sigue cifrado), permite plano cuando no (el Node legacy).
  Es el mismo nivel que ya tenía FB 3.0. Tráfico de LAN interno.

### 2. Plugin de autenticación (AuthServer)

- **Síntoma:** `Error occurred during login` (gdscode **335545106**); en `firebird.log`:
  `Authentication error / No matching plugins on server`.
- **Causa:** FB5 solo habilita el plugin **`Srp256`** (SHA-256) por default. El driver 1.1.9 solo sabe
  hablar **`Srp`** (SRP clásico SHA-1) y `Legacy_Auth` — **no implementa `Srp256`** (literal: está comentado
  en `node_modules/node-firebird/lib/wire/const.js`).
- **Fix:** en `firebird.conf` →
  ```
  AuthServer = Srp256, Srp
  ```
  Mantiene `Srp256` (para el `msp-api` en Go) y añade `Srp` (para el Node legacy). No hace falta recrear
  usuarios: el verificador SRP guardado en la BD de seguridad es el mismo para `Srp` y `Srp256` (solo cambia
  el hash del intercambio, no el verificador). SYSDBA/masterkey autentica con ambos.

### Aplicar los cambios

Ambos ajustes requieren **reiniciar el servicio** (corta unos segundos a TODOS los clientes: Microsip POS,
Go api, Android — hacerlo en baja actividad):

```bat
net stop FirebirdServerDefaultInstance && net start FirebirdServerDefaultInstance
```

Backup pristino del conf original guardado en `firebird.conf.bak-claude` (mismo directorio). Para revertir:
copiar el `.bak` encima y reiniciar.

## Notas generales

- El servicio Windows `FirebirdServerDefaultInstance` apunta a `Firebird_5_0\firebird.exe -s DefaultInstance`.
  Sigue existiendo la instalación vieja `Firebird_3_0\` en disco pero **no** es la que corre.
- Si en el futuro se actualiza el driver del legacy a una versión que soporte `Srp256` + WireCrypt, se podrían
  revertir ambos relajamientos y dejar FB5 en sus defaults endurecidos.
- Acceso SSH al server: ver runbook de operaciones / memoria de sesión.
