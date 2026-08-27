package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tombstone builds the email a soft-deleted usuario carries in the listing:
// Service.Desactivar rewrites EMAIL to "deleted-<uuid>-<email>".
func tombstone(email string) string {
	return "deleted-6f1d0f6e-6c9a-4a5e-9a3f-0d1b2c3d4e5f-" + email
}

// activo is the shorthand for a live row in the API listing.
func activo(id, uid, email, nombre string) apiUser {
	return apiUser{ID: id, FirebaseUID: uid, Email: email, Nombre: nombre, Activo: true}
}

// only builds a catalog from an API listing alone, for the cases where the
// Firestore side has no duplicates to declare.
func only(users ...apiUser) catalog {
	return newCatalog(users, nil)
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"  Humberto.Quintana@MSP.COM ": "humberto.quintana@msp.com",
		"humberto.quintana@msp.com":    "humberto.quintana@msp.com",
		"\tANA@MSP.COM\n":              "ana@msp.com",
		"   ":                          "",
	}
	for in, want := range cases {
		assert.Equal(t, want, normalizeEmail(in), "entrada %q", in)
	}
}

func TestRealEmail(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		// The soft-delete prefix is stripped so the row still joins on the
		// address the person actually uses.
		tombstone("ana.lopez@msp.com"): "ana.lopez@msp.com",
		// Anything that is not "deleted-<uuid>-" is left alone: a person may
		// legitimately have "deleted" or a dash in their address.
		"ana.lopez@msp.com":     "ana.lopez@msp.com",
		"deleted-ana@msp.com":   "deleted-ana@msp.com",
		"deleted-no-uuid-a@b.c": "deleted-no-uuid-a@b.c",
		"":                      "",
	}
	for in, want := range cases {
		assert.Equal(t, want, realEmail(in), "entrada %q", in)
	}
}

func TestIndexByEmail_MismoCorreoDistintaCapitalizacion(t *testing.T) {
	t.Parallel()

	index := indexByEmail([]apiUser{
		activo("u-1", "uid-1", "  Ana.Lopez@MSP.COM ", "Ana López"),
		activo("u-2", "uid-2", "", "sin correo"),
	})

	require.Len(t, index, 1, "el correo vacío no debe indexarse")
	found, ok := index["ana.lopez@msp.com"]
	require.True(t, ok, "el correo debe encontrarse normalizado")
	assert.Equal(t, "u-1", found.ID)
}

// TestIndexByEmail_LaFilaActivaGanaALaLapida: a person who was deactivated
// and later given a fresh row appears twice in the listing, tombstone first
// (it is older, and the listing is ordered by CREATED_AT). Keeping the
// tombstone would report a live employee as "dado de baja".
func TestIndexByEmail_LaFilaActivaGanaALaLapida(t *testing.T) {
	t.Parallel()

	for _, orden := range []string{"lápida primero", "activa primero"} {
		lapida := apiUser{ID: "u-vieja", Email: tombstone("ana@msp.com"), Nombre: "Ana López"}
		viva := activo("u-nueva", "uid-ana", "ana@msp.com", "Ana López")
		listado := []apiUser{lapida, viva}
		if orden == "activa primero" {
			listado = []apiUser{viva, lapida}
		}

		index := indexByEmail(listado)

		require.Len(t, index, 1, "%s: ambas filas son la misma persona", orden)
		assert.Equal(t, "u-nueva", index["ana@msp.com"].ID, "%s", orden)
	}
}

func TestValidateDoc(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		doc     firestoreDoc
		wantErr error
	}{
		{"completo", firestoreDoc{UID: "uid-1", Nombre: "Ana López", Email: "ana@msp.com"}, nil},
		{"sin uid", firestoreDoc{Nombre: "Ana López", Email: "ana@msp.com"}, errDocNoUID},
		{"sin correo", firestoreDoc{UID: "uid-1", Nombre: "Ana López"}, errDocNoEmail},
		{"correo vacío", firestoreDoc{UID: "uid-1", Nombre: "Ana López", Email: "   "}, errDocNoEmail},
		{"sin nombre", firestoreDoc{UID: "uid-1", Email: "ana@msp.com"}, errDocNoNombre},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateDoc(tc.doc)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestDecide(t *testing.T) {
	t.Parallel()

	cat := only(
		activo("u-humberto", "uid-3", "humberto.quintana@msp.com", "humberto.quintana"),
		activo("u-ana", "uid-2", "ana.lopez@msp.com", "Ana López"),
		activo("u-vend", "", "vendedor@msp.com", "vendedor"),
		apiUser{
			ID: "u-baja", FirebaseUID: "deleted-6f1d0f6e-6c9a-4a5e-9a3f-0d1b2c3d4e5f",
			Email: tombstone("beatriz@msp.com"), Nombre: "Beatriz Salgado",
		},
	)

	cases := []struct {
		name       string
		doc        firestoreDoc
		wantAction actionKind
		wantID     string
		wantReason error
	}{
		{
			name:       "falta en el api: se crea",
			doc:        firestoreDoc{UID: "uid-1", Nombre: "Beatriz Salgado", Email: "beatriz.salgado@msp.com"},
			wantAction: actionCreate,
		},
		{
			name:       "mismo correo con otra capitalización y espacios: se omite",
			doc:        firestoreDoc{UID: "uid-2", Nombre: "  ana   lópez ", Email: "  ANA.LOPEZ@MSP.COM  "},
			wantAction: actionSkip,
			wantID:     "u-ana",
		},
		{
			name:       "nombre distinto: sólo aviso",
			doc:        firestoreDoc{UID: "uid-3", Nombre: "Humberto Quintana", Email: "humberto.quintana@msp.com"},
			wantAction: actionWarn,
			wantID:     "u-humberto",
		},
		{
			// The row exists with the right address but bound to a different
			// Firebase account: the person cannot log in, and creating a
			// second row is impossible (UNIQUE on EMAIL). Only a human fixes
			// this, so it must never read as "ya existe".
			name:       "otro firebase_uid: avisa, no omite",
			doc:        firestoreDoc{UID: "uid-recreado", Nombre: "Ana López", Email: "ana.lopez@msp.com"},
			wantAction: actionWarn,
			wantID:     "u-ana",
		},
		{
			// A VENDEDOR_ONLY row carries no firebase_uid; the login path
			// promotes it and binds the uid on its own.
			name:       "fila sin firebase_uid: se omite, el login la promueve",
			doc:        firestoreDoc{UID: "uid-v", Nombre: "vendedor", Email: "vendedor@msp.com"},
			wantAction: actionSkip,
			wantID:     "u-vend",
		},
		{
			// GET /v2/usuarios has no ACTIVO filter, so the tombstone comes
			// back in the listing. Treating it as "no existe" would recreate
			// a person somebody deliberately deactivated.
			name:       "fila dada de baja: ni se crea ni se omite",
			doc:        firestoreDoc{UID: "uid-b", Nombre: "Beatriz Salgado", Email: "beatriz@msp.com"},
			wantAction: actionDeactivated,
			wantID:     "u-baja",
		},
		{
			name:       "documento sin correo: se salta",
			doc:        firestoreDoc{UID: "uid-4", Nombre: "Sin Correo"},
			wantAction: actionInvalid,
			wantReason: errDocNoEmail,
		},
		{
			name:       "documento sin nombre: se salta",
			doc:        firestoreDoc{UID: "uid-5", Email: "nuevo@msp.com"},
			wantAction: actionInvalid,
			wantReason: errDocNoNombre,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := decide(tc.doc, cat)
			assert.Equal(t, tc.wantAction, got.action)
			assert.Equal(t, tc.wantID, got.existing.ID)
			if tc.wantReason != nil {
				require.ErrorIs(t, got.reason, tc.wantReason)
			}
		})
	}
}

// TestDecide_CorreoRepetidoEnFirestore: two Firebase accounts claiming one
// address. Whichever is created first takes the address (UNIQUE on EMAIL) and
// the other person can never log in — so neither is created.
func TestDecide_CorreoRepetidoEnFirestore(t *testing.T) {
	t.Parallel()

	docs := []firestoreDoc{
		{UID: "uid-a", Nombre: "Ana López", Email: "ana@msp.com"},
		{UID: "uid-b", Nombre: "Ana Lopez Ruiz", Email: "  ANA@MSP.COM "},
		{UID: "uid-c", Nombre: "Beatriz Salgado", Email: "beatriz@msp.com"},
	}
	cat := newCatalog(nil, docs)

	for _, doc := range docs[:2] {
		got := decide(doc, cat)
		assert.Equal(t, actionInvalid, got.action, "uid=%s", doc.UID)
		require.ErrorIs(t, got.reason, errDocDuplicated, "uid=%s", doc.UID)
	}
	assert.Equal(t, actionCreate, decide(docs[2], cat).action, "el correo único no se ve afectado")
}

// TestDecisionLine_LlevaElIDParaQueElOperadorPuedaActuar covers the whole
// point of the [aviso] and [baja] lines: without the API id the operator
// cannot issue the PATCH nor find the row.
func TestDecisionLine_LlevaElIDParaQueElOperadorPuedaActuar(t *testing.T) {
	t.Parallel()

	t.Run("nombre distinto", func(t *testing.T) {
		t.Parallel()

		cat := only(activo("u-humberto", "uid-h", "humberto.quintana@msp.com", "humberto.quintana"))
		doc := firestoreDoc{UID: "uid-h", Nombre: "Humberto Quintana", Email: "Humberto.Quintana@msp.com"}

		line := decide(doc, cat).line(doc)

		assert.Contains(t, line, "[aviso]")
		assert.Contains(t, line, "id=u-humberto")
		assert.Contains(t, line, "PATCH /v2/usuarios/u-humberto")
		assert.Contains(t, line, `api="humberto.quintana"`)
		assert.Contains(t, line, `firestore="Humberto Quintana"`)
	})

	t.Run("dado de baja", func(t *testing.T) {
		t.Parallel()

		cat := only(apiUser{ID: "u-baja", Email: tombstone("ana@msp.com"), Nombre: "Ana López"})
		doc := firestoreDoc{UID: "uid-a", Nombre: "Ana López", Email: "ana@msp.com"}

		line := decide(doc, cat).line(doc)

		assert.Contains(t, line, "[baja]")
		assert.Contains(t, line, "id=u-baja")
	})

	t.Run("otro firebase_uid", func(t *testing.T) {
		t.Parallel()

		cat := only(activo("u-ana", "uid-viejo", "ana@msp.com", "Ana López"))
		doc := firestoreDoc{UID: "uid-nuevo", Nombre: "Ana López", Email: "ana@msp.com"}

		line := decide(doc, cat).line(doc)

		assert.Contains(t, line, "[aviso]")
		assert.Contains(t, line, "id=u-ana")
		assert.Contains(t, line, "uid-viejo")
		assert.Contains(t, line, "uid-nuevo")
	})
}

func TestNewCrearUsuarioRequest_NormalizaYOmiteTelefonoVacio(t *testing.T) {
	t.Parallel()

	sinTel := newCrearUsuarioRequest(firestoreDoc{UID: " uid-1 ", Nombre: " Ana López ", Email: " ANA@MSP.COM ", Telefono: "  "})
	assert.Equal(t, "uid-1", sinTel.FirebaseUID)
	assert.Equal(t, "ana@msp.com", sinTel.Email)
	assert.Equal(t, "Ana López", sinTel.Nombre)
	assert.Nil(t, sinTel.Telefono)

	conTel := newCrearUsuarioRequest(firestoreDoc{UID: "uid-2", Nombre: "Beatriz", Email: "b@msp.com", Telefono: " 3121234567 "})
	require.NotNil(t, conTel.Telefono)
	assert.Equal(t, "3121234567", *conTel.Telefono)

	// The body must carry no telefono key at all when there is none: the API
	// rejects an empty string (validate:"omitempty,max=30") but accepts an
	// absent field.
	raw, err := json.Marshal(sinTel)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "telefono")
}

// pagedServer serves the listing in pages of pageSize, so the walk is
// exercised end to end: a first-page-only reader would report the tail as
// missing and create duplicates for people that already exist.
func pagedServer(t *testing.T, users []apiUser, pageSize int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// assert, not require: a require inside a handler aborts the server
		// goroutine instead of the test.
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer tok-123", r.Header.Get("Authorization"))

		start := 0
		if after := r.URL.Query().Get("after"); after != "" {
			_, err := fmt.Sscanf(after, "cur-%d", &start)
			assert.NoError(t, err)
		}
		end := min(start+pageSize, len(users))

		body := listResponse{Items: users[start:end]}
		if end < len(users) {
			body.NextCursor = fmt.Sprintf("cur-%d", end)
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestListUsuarios_RecorreTodasLasPaginas(t *testing.T) {
	t.Parallel()

	const total = 69
	users := make([]apiUser, 0, total)
	for i := range total {
		users = append(users, apiUser{
			ID:    fmt.Sprintf("u-%d", i),
			Email: fmt.Sprintf("persona%d@msp.com", i),
		})
	}
	srv := pagedServer(t, users, 10)

	got, err := newAPIClient(srv.URL+"/", "tok-123").listUsuarios(t.Context())

	require.NoError(t, err)
	require.Len(t, got, total, "quedarse en la primera página oculta el resto del padrón")
	assert.Equal(t, "u-0", got[0].ID)
	assert.Equal(t, "u-68", got[total-1].ID)
}

// TestListUsuarios_PideUnLimiteQueElRepoNoRecorta guards the page size the
// request asks for. internal/auth/infra/firebird/pagination.go clamps every
// page to maxPageSize=100; asking for more is not an error but makes the walk
// claim a page size it never gets, and asking for nothing drops to the
// server's default of 50 without anybody noticing.
func TestListUsuarios_PideUnLimiteQueElRepoNoRecorta(t *testing.T) {
	t.Parallel()

	const repoMaxPageSize = 100

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("limit")
		assert.NoError(t, json.NewEncoder(w).Encode(listResponse{}))
	}))
	t.Cleanup(srv.Close)

	_, err := newAPIClient(srv.URL, "tok-123").listUsuarios(t.Context())

	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(repoMaxPageSize), got)
}

func TestListUsuarios_CursorRepetidoNoSeCicla(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		assert.NoError(t, json.NewEncoder(w).Encode(listResponse{
			Items:      []apiUser{{ID: "u-1", Email: "a@msp.com"}},
			NextCursor: "siempre-el-mismo",
		}))
	}))
	t.Cleanup(srv.Close)

	_, err := newAPIClient(srv.URL, "tok-123").listUsuarios(t.Context())

	require.ErrorIs(t, err, errCursorRepeated)
}

// TestListUsuarios_TopeDePaginasNoCortaEnSilencio is the control that keeps
// maxListPages honest. A cap that returned the rows gathered so far would
// look like a healthy short listing, and every person past the cut would be
// created a second time. It has to be an error.
func TestListUsuarios_TopeDePaginasNoCortaEnSilencio(t *testing.T) {
	t.Parallel()

	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		assert.NoError(t, json.NewEncoder(w).Encode(listResponse{
			Items:      []apiUser{{ID: fmt.Sprintf("u-%d", n), Email: fmt.Sprintf("p%d@msp.com", n)}},
			NextCursor: fmt.Sprintf("cur-%d", n), // siempre uno nuevo: nunca termina
		}))
	}))
	t.Cleanup(srv.Close)

	got, err := newAPIClient(srv.URL, "tok-123").listUsuarios(t.Context())

	require.ErrorIs(t, err, errTooManyPages)
	assert.Nil(t, got, "un listado incompleto no debe devolverse como si fuera completo")
	assert.Equal(t, maxListPages, n, "debe agotar el tope antes de rendirse")
}

// TestListUsuarios_CursorVacioAMediasTermina documents the server contract:
// next_cursor is omitempty, so an absent or empty cursor is the only end of
// the walk. There is nothing else to distinguish "no more pages" from a
// server bug, and the caller must not invent a retry.
func TestListUsuarios_CursorVacioAMediasTermina(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":"u-1","email":"a@msp.com","activo":true}]}`))
	}))
	t.Cleanup(srv.Close)

	got, err := newAPIClient(srv.URL, "tok-123").listUsuarios(t.Context())

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Activo, "el campo activo debe decodificarse: de él depende no resucitar bajas")
}

func TestListUsuarios_ErrorHTTP(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token inválido"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := newAPIClient(srv.URL, "tok-123").listUsuarios(t.Context())

	require.ErrorIs(t, err, errUnexpectedCode)
	assert.Contains(t, err.Error(), "401")
	assert.NotContains(t, err.Error(), "tok-123", "el token no puede acabar en un mensaje de error")
}

func TestCrearUsuario(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		status   int
		body     string
		wantErr  error
		wantID   string
		wantText string
	}{
		{name: "creado", status: http.StatusCreated, body: `{"id":"u-nuevo","email":"b@msp.com"}`, wantID: "u-nuevo"},
		{
			// The body is the only thing that says whether the collision was
			// on EMAIL or on FIREBASE_UID, and they mean opposite things.
			name: "conflicto", status: http.StatusConflict,
			body: `{"error":"firebase_uid_ya_existe"}`, wantErr: errConflict,
			wantText: "firebase_uid_ya_existe",
		},
		{
			// A 422 names the field and the bound: "fallido" alone would send
			// the operator hunting.
			name: "campo fuera de rango", status: http.StatusUnprocessableEntity,
			body: `{"code":"validation_error","fields":{"telefono":"max 30"}}`, wantErr: errUnexpectedCode,
			wantText: "telefono",
		},
		{name: "error del servidor", status: http.StatusInternalServerError, body: `boom`, wantErr: errUnexpectedCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer tok-123", r.Header.Get("Authorization"))

				var got crearUsuarioRequest
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				assert.Equal(t, "uid-9", got.FirebaseUID)

				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			doc := firestoreDoc{UID: "uid-9", Nombre: "Beatriz Salgado", Email: "b@msp.com"}
			created, err := newAPIClient(srv.URL, "tok-123").crearUsuario(t.Context(), newCrearUsuarioRequest(doc))

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				if tc.wantText != "" {
					assert.Contains(t, err.Error(), tc.wantText, "el motivo debe llegar al operador")
				}
				assert.NotContains(t, err.Error(), "tok-123")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, created.ID)
		})
	}
}

// noWriteServer fails the test if anything at all is requested from it.
func noWriteServer(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("el script no debía escribir: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestApply_LosCaminosQueNoEscriben is the guarantee that only a genuine
// "missing person" ever reaches the network. The server fails the test on any
// request, so each case proves the absence of a POST rather than asserting a
// counter that a broken branch could also satisfy.
func TestApply_LosCaminosQueNoEscriben(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		cat   catalog
		doc   firestoreDoc
		write bool
		want  tally // compared whole: the counters that must stay at zero matter
	}{
		{
			name:  "simulación: un alta pendiente no se escribe",
			cat:   only(),
			doc:   firestoreDoc{UID: "uid-b", Nombre: "Beatriz Salgado", Email: "beatriz@msp.com"},
			write: false,
			want:  tally{pending: 1},
		},
		{
			name:  "nombre distinto: avisa y no da de alta",
			cat:   only(activo("u-h", "uid-h", "humberto.quintana@msp.com", "humberto.quintana")),
			doc:   firestoreDoc{UID: "uid-h", Nombre: "Humberto Quintana", Email: "Humberto.Quintana@msp.com"},
			write: true,
			want:  tally{warned: 1},
		},
		{
			name:  "nombre igual: no produce nada",
			cat:   only(activo("u-ana", "uid-a", "ana.lopez@msp.com", "Ana López")),
			doc:   firestoreDoc{UID: "uid-a", Nombre: "  Ana   López  ", Email: "ANA.LOPEZ@MSP.COM"},
			write: true,
			want:  tally{skipped: 1},
		},
		{
			name:  "documento inservible: se salta",
			cat:   only(),
			doc:   firestoreDoc{UID: "uid-c", Nombre: "Sin Correo"},
			write: true,
			want:  tally{invalid: 1},
		},
		{
			name:  "fila dada de baja: no se resucita",
			cat:   only(apiUser{ID: "u-baja", Email: tombstone("beatriz@msp.com"), Nombre: "Beatriz Salgado"}),
			doc:   firestoreDoc{UID: "uid-b", Nombre: "Beatriz Salgado", Email: "beatriz@msp.com"},
			write: true,
			want:  tally{deactivated: 1},
		},
		{
			name:  "otro firebase_uid: avisa y no da de alta",
			cat:   only(activo("u-ana", "uid-viejo", "ana@msp.com", "Ana López")),
			doc:   firestoreDoc{UID: "uid-nuevo", Nombre: "Ana López", Email: "ana@msp.com"},
			write: true,
			want:  tally{warned: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := noWriteServer(t)
			var got tally
			apply(t.Context(), newAPIClient(srv.URL, "tok-123"), tc.doc, tc.cat, tc.write, &got)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestApply_SimulacionNoEscribeNadaEnUnaCorridaCompleta walks a whole mixed
// set in simulation mode against a server that fails on any request.
func TestApply_SimulacionNoEscribeNadaEnUnaCorridaCompleta(t *testing.T) {
	t.Parallel()

	srv := noWriteServer(t)
	docs := []firestoreDoc{
		{UID: "uid-1", Nombre: "Beatriz Salgado", Email: "beatriz@msp.com"},
		{UID: "uid-2", Nombre: "Ana López", Email: "ana@msp.com"},
		{UID: "uid-3", Nombre: "Sin Correo"},
		{UID: "uid-4", Nombre: "Repetida", Email: "rep@msp.com"},
		{UID: "uid-5", Nombre: "Repetida Otra", Email: "rep@msp.com"},
	}
	cat := newCatalog([]apiUser{activo("u-ana", "uid-2", "ana@msp.com", "Ana López")}, docs)

	var got tally
	for _, doc := range docs {
		apply(t.Context(), newAPIClient(srv.URL, "tok-123"), doc, cat, false, &got)
	}

	assert.Equal(t, 1, got.pending)
	assert.Equal(t, 1, got.skipped)
	assert.Equal(t, 3, got.invalid, "el documento sin correo y los dos repetidos")
	assert.Equal(t, 0, got.created)
	assert.Equal(t, 0, got.failed)
}

func TestApply_AltaFallidaCuentaYNoAborta(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"email inválido"}`))
	}))
	t.Cleanup(srv.Close)

	client := newAPIClient(srv.URL, "tok-123")
	var got tally
	apply(t.Context(), client, firestoreDoc{UID: "uid-1", Nombre: "Uno", Email: "uno@msp.com"}, only(), true, &got)
	apply(t.Context(), client, firestoreDoc{UID: "uid-2", Nombre: "Dos", Email: "dos@msp.com"}, only(), true, &got)

	assert.Equal(t, 2, got.failed, "un alta fallida no debe detener las demás")
	assert.Equal(t, 1, exitCode(got))
}

func TestApply_ConflictoSeCuentaAparte(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"firebase_uid_ya_existe"}`))
	}))
	t.Cleanup(srv.Close)

	var got tally
	apply(t.Context(), newAPIClient(srv.URL, "tok-123"), firestoreDoc{UID: "uid-1", Nombre: "Uno", Email: "uno@msp.com"}, only(), true, &got)

	assert.Equal(t, 1, got.conflicted)
	assert.Equal(t, 0, got.failed, "un 409 no es un alta rota")
}

// TestExitCode: the operator reads the tail of a console over SSH. Anything
// that leaves a person in Firebase without a usable row, or that proves the
// listing did not show a row that exists, must not exit zero.
func TestExitCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   tally
		want int
	}{
		{"todo limpio", tally{created: 5, skipped: 60}, 0},
		{"simulación limpia", tally{pending: 5, skipped: 60}, 0},
		{"un alta fallida", tally{created: 4, failed: 1}, 1},
		{"un documento inservible", tally{created: 4, invalid: 1}, 1},
		{"un 409: el listado no lo trajo", tally{created: 4, conflicted: 1}, 1},
		{"avisos y bajas: informativos", tally{skipped: 60, warned: 3, deactivated: 2}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, exitCode(tc.in))
		})
	}
}

func TestCountInactive(t *testing.T) {
	t.Parallel()

	users := []apiUser{
		activo("u-1", "uid-1", "a@msp.com", "A"),
		{ID: "u-2", Email: tombstone("b@msp.com"), Nombre: "B"},
		activo("u-3", "uid-3", "c@msp.com", "C"),
	}
	assert.Equal(t, 1, countInactive(users))
}

func TestStringField(t *testing.T) {
	t.Parallel()

	// Firestore hands back int64 for numbers, not int.
	data := map[string]any{"NOMBRE": "  Ana López  ", "TELEFONO": int64(3121234567), "OTRO": nil}

	assert.Equal(t, "Ana López", stringField(data, "NOMBRE"))
	assert.Empty(t, stringField(data, "TELEFONO"), "un número no debe reventar el recorrido")
	assert.Empty(t, stringField(data, "OTRO"))
	assert.Empty(t, stringField(data, "AUSENTE"))
}
