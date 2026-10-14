//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// IndiceUsuarios resuelve quién registró cada actividad.
type IndiceUsuarios struct {
	porUID      map[string]UsuarioCobranza
	porCobrador map[int][]UsuarioCobranza
}

// NuevoIndiceUsuarios indexa por uid y por COBRADOR_ID.
func NuevoIndiceUsuarios(us []UsuarioCobranza) IndiceUsuarios {
	ix := IndiceUsuarios{porUID: map[string]UsuarioCobranza{}, porCobrador: map[int][]UsuarioCobranza{}}
	for _, u := range us {
		ix.porUID[u.UID] = u
		if u.CobradorID > 0 {
			ix.porCobrador[u.CobradorID] = append(ix.porCobrador[u.CobradorID], u)
		}
	}
	return ix
}

// Completar llena RegistradoPor:
//   - oficina: nombre "Oficina", sin uid;
//   - app con uid (MSP_PAGOS_RECIBIDOS.CREATED_BY → MSP_USUARIOS.FIREBASE_UID):
//     nombre de Firestore si el uid está en el índice;
//   - app sin uid: si UN SOLO usuario tiene ese COBRADOR_ID, ése; si varios lo
//     comparten, queda sin uid (no se adivina) y conserva el nombre crudo.
func (ix IndiceUsuarios) Completar(a Actividad) Actividad {
	rp := a.RegistradoPor
	if rp.Origen == OrigenOficina {
		rp.UsuarioUID = ""
		rp.Nombre = NombreOficina
		a.RegistradoPor = rp
		return a
	}
	if rp.UsuarioUID != "" {
		if u, ok := ix.porUID[rp.UsuarioUID]; ok {
			rp.Nombre = u.Nombre
			if rp.CobradorID == 0 {
				rp.CobradorID = u.CobradorID
			}
		}
		a.RegistradoPor = rp
		return a
	}
	if cands := ix.porCobrador[rp.CobradorID]; rp.CobradorID > 0 && len(cands) == 1 {
		rp.UsuarioUID = cands[0].UID
		rp.Nombre = cands[0].Nombre
	}
	a.RegistradoPor = rp
	return a
}

// RegistradaPor dice si la actividad (ya completada) la registró u: por uid
// cuando se resolvió; si no, por COBRADOR_ID (dos usuarios que comparten
// COBRADOR_ID ven ambos esas actividades — decisión D-API-4).
func RegistradaPor(a Actividad, u UsuarioCobranza) bool {
	rp := a.RegistradoPor
	if rp.Origen != OrigenApp {
		return false
	}
	if rp.UsuarioUID != "" {
		return rp.UsuarioUID == u.UID
	}
	return u.CobradorID > 0 && rp.CobradorID == u.CobradorID
}
