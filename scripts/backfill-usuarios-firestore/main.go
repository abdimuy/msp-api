// Command backfill-usuarios-firestore creates the API usuarios that already
// exist in Firebase but never reached MSP_USUARIOS.
//
// SINGLE-USE, THROWAWAY TOOL — SAFE TO DELETE ONCE RUN.
//
// Why it exists: the desktop app used to provision people in Firebase without
// ever telling the API, so a handful of employees (five, as of 2026-08-27)
// had a Firebase account and no row in MSP_USUARIOS. The desktop now calls
// POST /v2/usuarios on alta, which closes the hole going forward; this program
// only fills the backlog that predates that fix. When the backlog is gone,
// this directory has no reason to exist — delete it.
//
// The identity is read from Firestore rather than typed by the operator on
// purpose: a mistyped email creates a duplicate usuario the day the person
// logs in, which is exactly the failure this job exists to close.
//
// What it deliberately does NOT do, because each needs a human decision:
//   - It never revives a usuario that was deactivated in the API. A soft
//     delete renames the row to deleted-<uuid>-<email> and GET /v2/usuarios
//     still returns it; the backfill recognizes that shape and reports it.
//   - It never repairs a row whose firebase_uid differs from the Firestore
//     document id. PATCH cannot change firebase_uid, so the fix is manual.
//   - It never picks a winner when two Firestore documents share one email.
//
// It runs against PRODUCTION, so it simulates by default: nothing is written
// without an explicit -apply. Pass the token through the environment, not the
// command line — an argument is visible to anyone who can list processes.
//
//	export MSP_API_TOKEN=...
//	go run ./scripts/backfill-usuarios-firestore -cred key.json -base https://...
//	go run ./scripts/backfill-usuarios-firestore -cred key.json -base https://... -apply
//
// Exit codes: 0 nothing pending, 1 something needs a human (failed alta,
// unusable document, or a 409 the listing should have predicted), 2 the run
// could not start or could not read one of the two sides.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const (
	// httpTimeout bounds every call to the API. The default client has no
	// timeout at all, which is how a backfill hangs forever on a bad tunnel.
	httpTimeout = 30 * time.Second
	// firestoreTimeout bounds the read of the users collection only. The API
	// phase gets its own budget: a slow Firestore must not abort the run
	// half-way through writing identities.
	firestoreTimeout = 2 * time.Minute
	// apiTimeout bounds the whole API phase — the listing walk plus every
	// alta. Generous because each alta is a separate round trip.
	apiTimeout = 10 * time.Minute
	// listPageSize is the page size asked of GET /v2/usuarios. The HTTP layer
	// accepts up to pagination.MaxLimit (500) but the auth repository clamps
	// every page to maxPageSize=100 (internal/auth/infra/firebird/pagination.go),
	// so asking for more only makes the request lie about what it gets back.
	listPageSize = 100
	// maxListPages guards against a server that keeps handing out cursors.
	// Reaching it is an error, never a silent truncation: a quiet cut here
	// would look like "four people missing" when forty are.
	maxListPages = 100
	// maxErrBody caps how much of an error response is echoed to the operator.
	maxErrBody = 512

	usersPath   = "/v2/usuarios"
	authEnvName = "MSP_API_TOKEN"

	// softDeletePrefix is what Service.Desactivar prepends to the email and
	// the firebase_uid of a deactivated usuario: "deleted-" + the row's uuid
	// + "-". See internal/auth/app/usuarios.go.
	softDeletePrefix = "deleted-"
	// uuidLen is the length of the canonical uuid text that follows
	// softDeletePrefix inside a soft-deleted email.
	uuidLen = 36
)

var (
	errMissingBase  = errors.New("falta -base: la url del api")
	errMissingToken = errors.New("falta -token (o la variable de entorno " + authEnvName + ")")
	errMissingCred  = errors.New("falta -cred: la ruta al json de la credencial de servicio")

	errDocNoEmail    = errors.New("el documento no trae EMAIL")
	errDocNoNombre   = errors.New("el documento no trae NOMBRE")
	errDocNoUID      = errors.New("el documento no trae id (firebase uid)")
	errDocDuplicated = errors.New("el correo se repite en firestore: resuélvalo allá antes de dar de alta")

	errTooManyPages   = errors.New("el listado no terminó: demasiadas páginas")
	errCursorRepeated = errors.New("el listado devolvió el mismo cursor dos veces")
	errUnexpectedCode = errors.New("respuesta inesperada del api")
	errConflict       = errors.New("ya existía en el api")
	errDecode         = errors.New("no se pudo leer la respuesta del api")
)

func main() {
	// main only wires flags and reports: the work lives in run so every
	// deferred Close actually runs (log.Fatal would skip them).
	code, err := run()
	if err != nil {
		sayf("error: %v", err)
	}
	os.Exit(code)
}

// options is the parsed command line.
type options struct {
	cred    string
	project string
	base    string
	token   string
	apply   bool
}

func parseOptions() (options, error) {
	var o options
	flag.StringVar(&o.cred, "cred", "serviceAccountKeyProduction.json", "service account json")
	flag.StringVar(&o.project, "project", "msp-db-1c2ce", "firebase project id")
	flag.StringVar(&o.base, "base", "", "base url of the API, e.g. https://apidev.loclx.io")
	flag.StringVar(&o.token, "token", "", "bearer token; prefer the env var $"+authEnvName)
	flag.BoolVar(&o.apply, "apply", false, "write for real; without it the run only simulates")
	flag.Parse()

	if o.token == "" {
		o.token = strings.TrimSpace(os.Getenv(authEnvName))
	}
	switch {
	case strings.TrimSpace(o.cred) == "":
		return options{}, errMissingCred
	case strings.TrimSpace(o.base) == "":
		return options{}, errMissingBase
	case o.token == "":
		return options{}, errMissingToken
	}
	return o, nil
}

// run returns the process exit code alongside the error, so a failed alta
// still gets a readable summary before the non-zero exit.
func run() (int, error) {
	opts, err := parseOptions()
	if err != nil {
		return 2, err
	}

	if opts.apply {
		sayf("modo escritura: se darán de alta los faltantes")
	} else {
		sayf("modo simulación: no se escribe nada (use -apply para dar de alta)")
	}

	docs, err := readFirestore(opts)
	if err != nil {
		return 2, err
	}
	sayf("firestore: %d documentos en users", len(docs))

	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	client := newAPIClient(opts.base, opts.token)
	existing, err := client.listUsuarios(ctx)
	if err != nil {
		return 2, err
	}
	sayf("api: %d usuarios en el listado (%d dados de baja)", len(existing), countInactive(existing))
	sayf("")

	cat := newCatalog(existing, docs)
	var t tally
	for _, doc := range docs {
		apply(ctx, client, doc, cat, opts.apply, &t)
	}

	sayf("")
	t.report(opts.apply)
	return exitCode(t), nil
}

// readFirestore reads the users collection under its own deadline.
func readFirestore(opts options) ([]firestoreDoc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), firestoreTimeout)
	defer cancel()
	return loadFirestoreUsers(ctx, opts.project, opts.cred)
}

// ─────────────────────────── decision (pure logic) ────────────────────────

// actionKind is what the backfill decided to do with one Firestore document.
type actionKind int

const (
	// actionInvalid means the document cannot be used at all.
	actionInvalid actionKind = iota
	// actionCreate means there is no usuario with that email yet.
	actionCreate
	// actionSkip means the usuario already exists and matches.
	actionSkip
	// actionWarn means the usuario exists and is active but something about
	// it disagrees with Firestore. The backfill reports it and writes
	// nothing: the repair is a PATCH or a manual decision, done apart.
	actionWarn
	// actionDeactivated means the email belongs to a usuario that was
	// deliberately deactivated in the API. Creating a fresh row would undo
	// that decision behind the operator's back, so it never happens here.
	actionDeactivated
)

// decision is the outcome of decide for a single document.
type decision struct {
	action   actionKind
	reason   error   // only for actionInvalid
	existing apiUser // only for actionSkip / actionWarn / actionDeactivated
	note     string  // operator-facing detail for actionWarn / actionDeactivated
}

// firestoreDoc is one document of the users collection. The document id is
// the Firebase UID; the fields are upper-case by legacy convention.
type firestoreDoc struct {
	UID      string
	Nombre   string
	Email    string
	Telefono string
}

// label identifies a document in the operator-facing output.
func (d firestoreDoc) label() string {
	nombre := strings.TrimSpace(d.Nombre)
	if nombre == "" {
		nombre = "(sin nombre)"
	}
	email := strings.TrimSpace(d.Email)
	if email == "" {
		email = "(sin correo)"
	}
	return fmt.Sprintf("%s <%s> uid=%s", nombre, email, d.UID)
}

// apiUser is the subset of the API's usuario projection this tool reads.
// Activo and FirebaseUID are not decoration: without them a deactivated row
// reads as a missing person and a rebound Firebase account reads as a match.
type apiUser struct {
	ID          string `json:"id"`
	FirebaseUID string `json:"firebase_uid"`
	Email       string `json:"email"`
	Nombre      string `json:"nombre"`
	Activo      bool   `json:"activo"`
}

// realEmail returns the address a usuario had before it was soft-deleted.
// Service.Desactivar rewrites EMAIL to "deleted-<uuid>-<email>" so the UNIQUE
// index frees the address, and GET /v2/usuarios still returns the row (the
// listing query has no ACTIVO filter), so the join key has to be the address
// underneath. A normal email is returned unchanged.
func realEmail(stored string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(stored), softDeletePrefix)
	if !ok {
		return stored
	}
	// The uuid itself contains '-', so the separator is at a fixed offset
	// rather than at the first dash.
	if len(rest) <= uuidLen || rest[uuidLen] != '-' {
		return stored
	}
	if _, err := uuid.Parse(rest[:uuidLen]); err != nil {
		return stored
	}
	return rest[uuidLen+1:]
}

// normalizeEmail is the join key between Firestore and the API. Case and
// surrounding whitespace must never split one person into two rows. It
// mirrors domain.NewEmail (TrimSpace + ToLower) exactly, deliberately: any
// extra folding here would claim a match the API's UNIQUE index would not
// honour, and the alta would then fail or, worse, land on someone else.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// normalizeNombre collapses runs of whitespace and folds case, so that only a
// real difference in the name raises a warning.
func normalizeNombre(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// catalog is the two sides of the join, prepared once for the whole run.
type catalog struct {
	// byEmail is the API listing keyed by the real (un-tombstoned) email.
	byEmail map[string]apiUser
	// dupes counts how many Firestore documents claim each email. Anything
	// above one is unresolvable without a human.
	dupes map[string]int
}

func newCatalog(users []apiUser, docs []firestoreDoc) catalog {
	return catalog{byEmail: indexByEmail(users), dupes: countEmails(docs)}
}

// indexByEmail keys the API listing by the normalized real email. An active
// row always wins over a deactivated one: a person may have been given a
// fresh row after a soft delete, and the tombstone sorts first by CREATED_AT.
func indexByEmail(users []apiUser) map[string]apiUser {
	index := make(map[string]apiUser, len(users))
	for _, u := range users {
		key := normalizeEmail(realEmail(u.Email))
		if key == "" {
			continue
		}
		if prev, dup := index[key]; dup && (prev.Activo || !u.Activo) {
			continue
		}
		index[key] = u
	}
	return index
}

// countEmails counts the Firestore documents per normalized email.
func countEmails(docs []firestoreDoc) map[string]int {
	seen := make(map[string]int, len(docs))
	for _, d := range docs {
		if key := normalizeEmail(d.Email); key != "" {
			seen[key]++
		}
	}
	return seen
}

// validateDoc rejects documents that cannot become a usuario.
func validateDoc(d firestoreDoc) error {
	if strings.TrimSpace(d.UID) == "" {
		return errDocNoUID
	}
	if normalizeEmail(d.Email) == "" {
		return errDocNoEmail
	}
	if strings.TrimSpace(d.Nombre) == "" {
		return errDocNoNombre
	}
	return nil
}

// decide is the whole rule set, with no I/O in it: what happens to one
// Firestore document given the current API listing.
func decide(doc firestoreDoc, cat catalog) decision {
	if err := validateDoc(doc); err != nil {
		return decision{action: actionInvalid, reason: err}
	}
	key := normalizeEmail(doc.Email)
	if cat.dupes[key] > 1 {
		// Two Firebase accounts, one address: whichever is created first
		// takes the address and the other person can never log in. Which one
		// is the real employee is not a question this program may answer.
		return decision{action: actionInvalid, reason: errDocDuplicated}
	}

	found, ok := cat.byEmail[key]
	if !ok {
		return decision{action: actionCreate}
	}
	if !found.Activo {
		return decision{
			action:   actionDeactivated,
			existing: found,
			note:     "existe pero está dado de baja en el api; no se recrea — reactívelo a mano si hace falta",
		}
	}
	// An empty firebase_uid on the API side is a VENDEDOR_ONLY row; the login
	// path promotes it and binds the uid by itself, so it needs no action.
	if uid := strings.TrimSpace(found.FirebaseUID); uid != "" && uid != strings.TrimSpace(doc.UID) {
		return decision{
			action:   actionWarn,
			existing: found,
			note: fmt.Sprintf("existe con otro firebase_uid: api=%q firestore=%q — no podrá entrar y PATCH no cambia el uid",
				uid, strings.TrimSpace(doc.UID)),
		}
	}
	if normalizeNombre(found.Nombre) != normalizeNombre(doc.Nombre) {
		return decision{
			action:   actionWarn,
			existing: found,
			note: fmt.Sprintf("existe con otro nombre: api=%q firestore=%q — corrija con PATCH %s/%s",
				found.Nombre, strings.TrimSpace(doc.Nombre), usersPath, found.ID),
		}
	}
	return decision{action: actionSkip, existing: found}
}

// line renders the operator-facing report for a decision that needs no I/O.
// Every line about an existing row carries that row's id, because the id is
// what the operator needs in order to act on it.
func (d decision) line(doc firestoreDoc) string {
	switch d.action {
	case actionInvalid:
		return fmt.Sprintf("[saltado]  %s: %v", doc.label(), d.reason)
	case actionSkip:
		return fmt.Sprintf("[omitido]  %s ya existe (id=%s)", doc.label(), d.existing.ID)
	case actionWarn:
		return fmt.Sprintf("[aviso]    %s (id=%s): %s", doc.label(), d.existing.ID, d.note)
	case actionDeactivated:
		return fmt.Sprintf("[baja]     %s (id=%s): %s", doc.label(), d.existing.ID, d.note)
	case actionCreate:
		return "[por crear] " + doc.label()
	}
	return ""
}

// ─────────────────────────── execution ────────────────────────────────────

// tally counts the run for the closing summary.
type tally struct {
	created     int
	skipped     int
	warned      int
	deactivated int
	invalid     int
	conflicted  int
	failed      int
	pending     int
}

// exitCode maps the tally to the process status. A failed alta, an unusable
// document and a 409 all mean the same thing to whoever runs this: somebody
// in Firebase still has no usable row, or the listing did not show a row that
// does exist. None of those may exit zero — the operator reads the tail of a
// console over SSH, and a zero there reads as "done".
func exitCode(t tally) int {
	if t.failed+t.invalid+t.conflicted > 0 {
		return 1
	}
	return 0
}

func (t *tally) report(applied bool) {
	if applied {
		sayf("resumen: %d creados, %d omitidos, %d avisos, %d dados de baja, %d saltados, %d ya existían (409), %d fallidos",
			t.created, t.skipped, t.warned, t.deactivated, t.invalid, t.conflicted, t.failed)
	} else {
		sayf("resumen (simulación): %d por crear, %d omitidos, %d avisos, %d dados de baja, %d saltados",
			t.pending, t.skipped, t.warned, t.deactivated, t.invalid)
		sayf("nada se escribió: vuelva a correr con -apply para dar de alta")
	}
	if n := t.warned + t.deactivated; n > 0 {
		sayf("%d requieren decisión manual: revise las líneas [aviso] y [baja]", n)
	}
	if t.conflicted > 0 {
		sayf("%d dieron 409: el listado no mostró filas que sí existen — revise antes de volver a correr", t.conflicted)
	}
	if t.invalid > 0 {
		sayf("%d documentos no se pudieron usar: esas personas siguen sin fila en el api", t.invalid)
	}
}

// apply carries out one decision, printing exactly one line per document.
func apply(ctx context.Context, c *apiClient, doc firestoreDoc, cat catalog, write bool, t *tally) {
	d := decide(doc, cat)
	switch d.action {
	case actionInvalid:
		t.invalid++
		sayf("%s", d.line(doc))
	case actionSkip:
		t.skipped++
		sayf("%s", d.line(doc))
	case actionWarn:
		t.warned++
		sayf("%s", d.line(doc))
	case actionDeactivated:
		t.deactivated++
		sayf("%s", d.line(doc))
	case actionCreate:
		create(ctx, c, doc, d, write, t)
	}
}

// create posts the alta, or announces it when simulating. A failure here is
// counted and the run continues: one bad alta must not hide the rest.
func create(ctx context.Context, c *apiClient, doc firestoreDoc, d decision, write bool, t *tally) {
	if !write {
		t.pending++
		sayf("%s", d.line(doc))
		return
	}
	created, err := c.crearUsuario(ctx, newCrearUsuarioRequest(doc))
	switch {
	case err == nil:
		t.created++
		sayf("[creado]   %s id=%s", doc.label(), created.ID)
	case errors.Is(err, errConflict):
		t.conflicted++
		sayf("[409]      %s: el api dice que ya existe pero el listado no lo trajo — %v", doc.label(), err)
	default:
		t.failed++
		sayf("[fallido]  %s: %v", doc.label(), err)
	}
}

// countInactive reports how many listed rows are tombstones, so the operator
// can tell the size of the listing from the actual headcount.
func countInactive(users []apiUser) int {
	n := 0
	for _, u := range users {
		if !u.Activo {
			n++
		}
	}
	return n
}

// ─────────────────────────── firestore ────────────────────────────────────

func loadFirestoreUsers(ctx context.Context, project, cred string) ([]firestoreDoc, error) {
	client, err := firestore.NewClient(ctx, project, option.WithCredentialsFile(cred))
	if err != nil {
		return nil, fmt.Errorf("firestore: %w", err)
	}
	defer func() { _ = client.Close() }()

	it := client.Collection("users").Documents(ctx)
	defer it.Stop()

	docs := make([]firestoreDoc, 0, 128)
	for {
		snap, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: %w", err)
		}
		data := snap.Data()
		docs = append(docs, firestoreDoc{
			UID:      snap.Ref.ID,
			Nombre:   stringField(data, "NOMBRE"),
			Email:    stringField(data, "EMAIL"),
			Telefono: stringField(data, "TELEFONO"),
		})
	}
}

// stringField reads a string field without trusting its type: a number in
// TELEFONO must not panic the whole run.
func stringField(data map[string]any, key string) string {
	v, ok := data[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// ─────────────────────────── api ──────────────────────────────────────────

// crearUsuarioRequest is the body of POST /v2/usuarios.
type crearUsuarioRequest struct {
	FirebaseUID string  `json:"firebase_uid"`
	Email       string  `json:"email"`
	Nombre      string  `json:"nombre"`
	Telefono    *string `json:"telefono,omitempty"`
}

func newCrearUsuarioRequest(d firestoreDoc) crearUsuarioRequest {
	req := crearUsuarioRequest{
		FirebaseUID: strings.TrimSpace(d.UID),
		Email:       normalizeEmail(d.Email),
		Nombre:      strings.TrimSpace(d.Nombre),
	}
	if tel := strings.TrimSpace(d.Telefono); tel != "" {
		req.Telefono = &tel
	}
	return req
}

// listResponse is the cursor-paginated envelope of GET /v2/usuarios.
type listResponse struct {
	Items      []apiUser `json:"items"`
	NextCursor string    `json:"next_cursor"`
}

// apiClient talks to the API with its own bounded http.Client — sharing
// http.DefaultClient would mean no timeout at all.
type apiClient struct {
	base  string
	token string
	http  *http.Client
}

func newAPIClient(base, token string) *apiClient {
	return &apiClient{
		base:  strings.TrimRight(strings.TrimSpace(base), "/"),
		token: strings.TrimSpace(token),
		http:  &http.Client{Timeout: httpTimeout},
	}
}

// do issues one request. The token travels in the Authorization header and
// nowhere else — never in a URL, a printed line or an error — so no failure
// path can echo it back to the console or to a shoulder.
func (c *apiClient) do(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("petición: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	return resp, nil
}

// listUsuarios walks every page of the listing. Stopping early is the silent
// failure this loop exists to prevent: it would report as missing everybody
// who happened to sit past the page boundary, and -apply would then create a
// duplicate for each of them. Every way this walk can end other than an empty
// next_cursor is an error, never a shortened result.
func (c *apiClient) listUsuarios(ctx context.Context) ([]apiUser, error) {
	all := make([]apiUser, 0, listPageSize)
	seen := make(map[string]struct{})
	cursor := ""
	for range maxListPages {
		items, next, err := c.listPage(ctx, cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		if _, dup := seen[next]; dup {
			return nil, fmt.Errorf("%w: %q", errCursorRepeated, next)
		}
		seen[next] = struct{}{}
		cursor = next
	}
	return nil, fmt.Errorf("%w: %d páginas de %d filas y seguía habiendo cursor",
		errTooManyPages, maxListPages, listPageSize)
}

func (c *apiClient) listPage(ctx context.Context, cursor string) ([]apiUser, string, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(listPageSize))
	if cursor != "" {
		q.Set("after", cursor)
	}
	resp, err := c.do(ctx, http.MethodGet, c.base+usersPath+"?"+q.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", statusError(resp)
	}
	var page listResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, "", fmt.Errorf("%w: listado: %w", errDecode, err)
	}
	return page.Items, page.NextCursor, nil
}

func (c *apiClient) crearUsuario(ctx context.Context, in crearUsuarioRequest) (apiUser, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return apiUser{}, fmt.Errorf("alta: %w", err)
	}
	resp, err := c.do(ctx, http.MethodPost, c.base+usersPath, bytes.NewReader(payload))
	if err != nil {
		return apiUser{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		var created apiUser
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return apiUser{}, fmt.Errorf("%w: alta: %w", errDecode, err)
		}
		return created, nil
	case http.StatusConflict:
		// Carry the body: a 409 on EMAIL and a 409 on FIREBASE_UID mean very
		// different things, and only the body says which one it was.
		return apiUser{}, fmt.Errorf("%w: %s", errConflict, bodySnippet(resp))
	default:
		return apiUser{}, statusError(resp)
	}
}

// statusError turns an unexpected response into an error carrying enough of
// the body for the operator to tell a 401 from a 422 — a 422 names the field
// and the bound it broke (nombre max=200, telefono max=30), which is the only
// way to tell a plain "fallido" from "el teléfono no cabe en la columna".
func statusError(resp *http.Response) error {
	return fmt.Errorf("%w: http %d: %s", errUnexpectedCode, resp.StatusCode, bodySnippet(resp))
}

// bodySnippet reads at most maxErrBody bytes of a response body.
func bodySnippet(resp *http.Response) string {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
	return strings.TrimSpace(string(snippet))
}

// sayf prints one operator-facing line. Wrapped so the (never actionable)
// write error is discarded in one place instead of at every call site.
func sayf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stdout, format+"\n", args...)
}
