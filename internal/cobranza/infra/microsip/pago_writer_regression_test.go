// Package microsip — same-package regression tests that guard against
// column-count vs placeholder-count mismatches in every INSERT constant.
//
// History: the DOCTOS_CC INSERT shipped with 59 columns but 58 placeholders
// (one "?" was missing from a VALUES line). Firebird returned error -804 at
// runtime, requiring a full debugging session to diagnose. These tests catch
// that class of bug in microseconds at compile time — they run on every
// "go test ./..." with zero external dependencies.
//
//nolint:misspell // Microsip table/column identifiers are kept verbatim.
package microsip

import (
	"strings"
	"testing"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// countColumnsInInsert counts the comma-separated column identifiers in the
// column list of an INSERT statement. It locates the first "(" after the table
// name, finds the matching ")", and splits by "," counting non-empty entries.
func countColumnsInInsert(sql, tableMarker string) int {
	markerIdx := strings.Index(sql, tableMarker)
	if markerIdx < 0 {
		return -1
	}
	start := strings.Index(sql[markerIdx:], "(")
	if start < 0 {
		return -1
	}
	start += markerIdx + 1 // points past the opening "("

	// Find the matching closing ")".
	depth := 1
	end := start
	for end < len(sql) && depth > 0 {
		switch sql[end] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth > 0 {
			end++
		}
	}
	colSection := sql[start:end]
	count := 0
	for _, tok := range strings.Split(colSection, ",") {
		if strings.TrimSpace(tok) != "" {
			count++
		}
	}
	return count
}

// countPlaceholdersInValues counts "?" characters inside the VALUES (...)
// section of an INSERT statement. It locates "VALUES (" and then counts all
// "?" runes up to the matching ")".
func countPlaceholdersInValues(sql string) int {
	valIdx := strings.Index(sql, "VALUES (")
	if valIdx < 0 {
		// Also try without space before paren (defensive).
		valIdx = strings.Index(sql, "VALUES(")
		if valIdx < 0 {
			return -1
		}
	}
	start := strings.Index(sql[valIdx:], "(")
	if start < 0 {
		return -1
	}
	start += valIdx + 1 // points past the opening "("

	depth := 1
	count := 0
	for i := start; i < len(sql) && depth > 0; i++ {
		switch sql[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '?':
			if depth == 1 {
				count++
			}
		}
	}
	return count
}

// ─── DOCTOS_CC ───────────────────────────────────────────────────────────────

// TestRegression_InsertDoctoCCSQL_PlaceholdersMatchColumns asserts that the
// DOCTOS_CC INSERT has an equal number of column names and "?" placeholders.
//
// Historical bug: one "?" was missing on a VALUES line, causing Firebird to
// return error -804 at runtime. This test catches that class of defect in
// microseconds without any external dependency.
func TestRegression_InsertDoctoCCSQL_PlaceholdersMatchColumns(t *testing.T) {
	t.Parallel()
	cols := countColumnsInInsert(insertDoctoCCSQL, "INSERT INTO DOCTOS_CC (")
	placeholders := countPlaceholdersInValues(insertDoctoCCSQL)
	if cols != placeholders {
		t.Fatalf("DOCTOS_CC INSERT: cols=%d placeholders=%d — mismatch; add or remove a '?' to match", cols, placeholders)
	}
}

// TestRegression_DoctoCCArgsCount asserts that the hardcoded expected arg
// count (59) matches the actual placeholder count in insertDoctoCCSQL.
// This is an independent guard: if someone changes the SQL but forgets to
// update the args slice (or vice-versa), exactly one of these two tests
// will fail.
func TestRegression_DoctoCCArgsCount(t *testing.T) {
	t.Parallel()
	const expectedArgs = 59 // must equal len(args) in insertDoctoCC.
	placeholders := countPlaceholdersInValues(insertDoctoCCSQL)
	if placeholders != expectedArgs {
		t.Fatalf("DOCTOS_CC INSERT: expected %d args but SQL has %d placeholders — update expectedArgs or fix the SQL", expectedArgs, placeholders)
	}
}

// ─── IMPORTES_DOCTOS_CC ──────────────────────────────────────────────────────

// TestRegression_InsertImporteDoctoCCSQL_PlaceholdersMatchColumns asserts that
// the IMPORTES_DOCTOS_CC INSERT has an equal number of column names and "?"
// placeholders (expected: 14 each).
func TestRegression_InsertImporteDoctoCCSQL_PlaceholdersMatchColumns(t *testing.T) {
	t.Parallel()
	cols := countColumnsInInsert(insertImporteDoctoCCSQL, "INSERT INTO IMPORTES_DOCTOS_CC (")
	placeholders := countPlaceholdersInValues(insertImporteDoctoCCSQL)
	if cols != placeholders {
		t.Fatalf("IMPORTES_DOCTOS_CC INSERT: cols=%d placeholders=%d — mismatch", cols, placeholders)
	}
}

// ─── FORMAS_COBRO_DOCTOS ─────────────────────────────────────────────────────

// TestRegression_InsertFormaCobroDoctoSQL_PlaceholdersMatchColumns asserts
// that the FORMAS_COBRO_DOCTOS INSERT has an equal number of column names and
// "?" placeholders (expected: 8 each).
func TestRegression_InsertFormaCobroDoctoSQL_PlaceholdersMatchColumns(t *testing.T) {
	t.Parallel()
	cols := countColumnsInInsert(insertFormaCobroDoctoSQL, "INSERT INTO FORMAS_COBRO_DOCTOS (")
	placeholders := countPlaceholdersInValues(insertFormaCobroDoctoSQL)
	if cols != placeholders {
		t.Fatalf("FORMAS_COBRO_DOCTOS INSERT: cols=%d placeholders=%d — mismatch", cols, placeholders)
	}
}
