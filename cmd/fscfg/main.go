// Command fscfg inspects and updates config/api_settings in Firestore.
//
// TEMPORARY OPERATIONAL TOOL — not part of the API. It exists to set the
// version-gate fields without a Firebase CLI. It NEVER overwrites the
// document: reads are plain gets and writes use firestore.Update, which
// touches only the named paths. config/api_settings also holds the baseURL
// kill-switch, so a clobbering write would take the whole fleet down.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
)

// maxValueWidth truncates long values (APK URLs) so the dump stays readable.
const maxValueWidth = 70

// fields are the version-gate keys this tool owns. Everything else in the
// document — baseURL above all — is never written and never deleted.
type fields struct {
	code     int
	name     string
	url      string
	sha      string
	size     int64
	deadline string
}

func main() {
	// main only wires flags and reports: the work lives in run so that every
	// deferred Close actually runs (log.Fatal would skip them).
	if err := run(); err != nil {
		log.Printf("fscfg: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cred := flag.String("cred", "serviceAccountKeyProduction.json", "service account json")
	project := flag.String("project", "msp-db-1c2ce", "firebase project id")
	var f fields
	flag.IntVar(&f.code, "set-code", -1, "MIN_VERSION_CODE to write (-1 = read only)")
	flag.StringVar(&f.name, "set-name", "", "MIN_VERSION_NAME to write")
	flag.StringVar(&f.url, "set-url", "", "MIN_VERSION_APK_URL to write")
	flag.StringVar(&f.sha, "set-sha", "", "MIN_VERSION_APK_SHA256 to write")
	flag.Int64Var(&f.size, "set-size", 0, "MIN_VERSION_APK_SIZE to write (bytes)")
	flag.StringVar(&f.deadline, "set-deadline", "", `MIN_VERSION_DEADLINE to write (label, e.g. "vie 22")`)
	clearAll := flag.Bool("clear", false, "delete every MIN_VERSION_* field")
	flag.Parse()

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, *project, option.WithCredentialsFile(*cred))
	if err != nil {
		return fmt.Errorf("firestore: %w", err)
	}
	defer func() { _ = client.Close() }()

	ref := client.Collection("config").Doc("api_settings")

	switch {
	case *clearAll:
		if err := apply(ctx, ref, deletions()); err != nil {
			return fmt.Errorf("clear: %w", err)
		}
		say("MIN_VERSION_* eliminados")
	case f.code >= 0:
		if err := apply(ctx, ref, f.updates()); err != nil {
			return fmt.Errorf("update: %w", err)
		}
		say(fmt.Sprintf("MIN_VERSION_CODE=%d MIN_VERSION_NAME=%q escritos", f.code, f.name))
	}

	return dump(ctx, ref)
}

// apply writes only the named paths. Update (never Set) is what keeps baseURL
// and the rest of the document untouched.
func apply(ctx context.Context, ref *firestore.DocumentRef, ups []firestore.Update) error {
	_, err := ref.Update(ctx, ups)
	return err
}

func deletions() []firestore.Update {
	paths := []string{
		"MIN_VERSION_CODE", "MIN_VERSION_NAME", "MIN_VERSION_APK_URL",
		"MIN_VERSION_APK_SHA256", "MIN_VERSION_APK_SIZE", "MIN_VERSION_DEADLINE",
	}
	ups := make([]firestore.Update, 0, len(paths))
	for _, p := range paths {
		ups = append(ups, firestore.Update{Path: p, Value: firestore.Delete})
	}
	return ups
}

// updates builds the write set. Empty/zero values are omitted rather than
// written as blanks, so a partial invocation never clears a field it did not
// mean to touch.
func (f fields) updates() []firestore.Update {
	ups := []firestore.Update{{Path: "MIN_VERSION_CODE", Value: f.code}}
	optional := []struct {
		path string
		set  bool
		val  any
	}{
		{"MIN_VERSION_NAME", f.name != "", f.name},
		{"MIN_VERSION_APK_URL", f.url != "", f.url},
		{"MIN_VERSION_APK_SHA256", f.sha != "", f.sha},
		{"MIN_VERSION_APK_SIZE", f.size > 0, f.size},
		{"MIN_VERSION_DEADLINE", f.deadline != "", f.deadline},
	}
	for _, o := range optional {
		if o.set {
			ups = append(ups, firestore.Update{Path: o.path, Value: o.val})
		}
	}
	return ups
}

func dump(ctx context.Context, ref *firestore.DocumentRef) error {
	snap, err := ref.Get(ctx)
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	data := snap.Data()
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	say("── config/api_settings ──")
	for _, k := range keys {
		v := fmt.Sprint(data[k])
		if len(v) > maxValueWidth {
			v = v[:maxValueWidth] + "…"
		}
		say(fmt.Sprintf("  %-22s = %s", k, v))
	}
	return nil
}

// say prints a line to stdout. Wrapped so the (never actionable) write error
// is discarded in one place instead of at every call site.
func say(line string) {
	_, _ = fmt.Fprintln(os.Stdout, line)
}
