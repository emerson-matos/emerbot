// Package envelope reads only the routing header of an import envelope. It
// deliberately does not parse the body — that is the provider parser's job, so
// the object is interpreted in exactly one place. The same peek works whether
// the envelope arrives from S3, HTTP, EventBridge or SQS.
package envelope

import (
	"encoding/json"
	"fmt"

	"github.com/emerson/emerbot/packages/payments"
)

// Metadata is the header the importer needs to choose a parser.
type Metadata struct {
	Provider payments.Provider `json:"provider"`
}

// ReadMetadata extracts the provider from raw envelope bytes without decoding
// the rest of the payload. An unknown or missing provider is an error.
func ReadMetadata(raw []byte) (Metadata, error) {
	var m Metadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return Metadata{}, fmt.Errorf("read envelope metadata: %w", err)
	}
	if !m.Provider.Valid() {
		return Metadata{}, fmt.Errorf("envelope: unknown or missing provider %q", m.Provider)
	}
	return m, nil
}
