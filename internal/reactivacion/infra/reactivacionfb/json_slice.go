//nolint:misspell // Spanish domain vocabulary by project convention.
package reactivacionfb

import (
	"database/sql"
	"encoding/json"
)

// jsonSliceArg marshals ss to a JSON array string suitable for a BLOB TEXT
// column argument (BANDERAS/SENALES/EVIDENCIA). A nil slice is treated the
// same as an empty one — the column always receives the literal "[]", never
// SQL NULL, because "no items" is a known value, not an unknown one.
func jsonSliceArg(ss []string) (string, error) {
	if ss == nil {
		ss = []string{}
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// scanJSONSlice unmarshals a nullable JSON-array text column into a []string.
// A SQL NULL or an empty string both yield a non-nil empty slice — the
// caller never has to distinguish "column is NULL" from "column holds []".
func scanJSONSlice(raw sql.NullString) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}
