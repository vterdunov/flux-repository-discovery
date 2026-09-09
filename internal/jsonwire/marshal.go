// Package jsonwire defines the stable JSON representation shared by HTTP,
// CLI reports, configuration revisions and response-size checks.
package jsonwire

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Marshal uses v2 with explicit output options that preserve the v1 wire
// contract. Decoders deliberately use v2's strict defaults instead.
// In particular, nil diagnostic collections remain null; a successful Flux
// input collection is constructed as a non-nil slice and stays [] when empty.
func Marshal(value any) ([]byte, error) {
	return json.Marshal(value,
		json.Deterministic(true),
		json.FormatNilSliceAsNull(true),
		json.FormatNilMapAsNull(true),
		jsontext.EscapeForHTML(true),
		jsontext.EscapeForJS(true),
		jsontext.PreserveRawStrings(true),
	)
}
