package authoutbox

import (
	"encoding/json"
	"fmt"
)

// marshalPayload encodes the caller's payload to JSON. A json.RawMessage is
// passed through unchanged so callers that already hold raw bytes do not
// re-encode (which would double-quote string payloads). nil yields an
// explicit JSON null so downstream NOT NULL constraints are honoured.
func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return json.RawMessage("null"), nil
	}
	if raw, ok := payload.(json.RawMessage); ok {
		return raw, nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("authoutbox: marshal payload: %w", err)
	}
	return body, nil
}
