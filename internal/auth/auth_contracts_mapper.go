package auth

import "github.com/abdimuy/msp-api/internal/auth/domain"

// ToContract projects a domain Usuario plus its derived permission codes
// into the cross-module CurrentUser view. The conversion is pure: it
// allocates a fresh slice for Permisos so the caller can hand the result
// off to long-lived context values without aliasing the input slice.
func ToContract(u *domain.Usuario, perms []domain.Permission) CurrentUser {
	codes := make([]string, len(perms))
	for i, p := range perms {
		codes[i] = string(p)
	}
	return CurrentUser{
		ID:          u.ID(),
		FirebaseUID: u.FirebaseUID().Value(),
		Email:       u.Email().Value(),
		Nombre:      u.Nombre().Value(),
		AlmacenID:   u.AlmacenID(),
		Permisos:    codes,
	}
}
