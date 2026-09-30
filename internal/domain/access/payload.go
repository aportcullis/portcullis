package access

import (
	"strings"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// The 56 KiB limit includes SQL and all parameter names and values, leaving room within the 64 KiB transport limit for the Connect envelope and metadata.
const (
	MaxPayloadBytes  = 56 * 1024
	MaxPayloadParams = 100
)

// countPayloadBytes sums SQL and parameter-name/value byte lengths.
func countPayloadBytes(sql string, params []query.Parameter) int {
	total := len(sql)
	for _, p := range params {
		total += len(p.Name) + len(p.Value.Text)
	}
	return total
}

// Payload holds plaintext SQL and typed parameters for an approval.
type Payload struct {
	SQL    string
	Params []query.Parameter
}

// NewPayload validates SQL size, unique parameter names, and typed values.
func NewPayload(sql string, params []query.Parameter) (Payload, error) {
	if strings.TrimSpace(sql) == "" {
		return Payload{}, ErrInvalidPayload
	}
	if len(params) > MaxPayloadParams {
		return Payload{}, ErrInvalidPayload
	}
	if countPayloadBytes(sql, params) > MaxPayloadBytes {
		return Payload{}, ErrInvalidPayload
	}
	seen := make(map[string]struct{}, len(params))
	for _, p := range params {
		if p.Name == "" {
			return Payload{}, ErrInvalidPayload
		}
		if _, dup := seen[p.Name]; dup {
			return Payload{}, ErrInvalidPayload
		}
		seen[p.Name] = struct{}{}
		if err := p.Value.Validate(); err != nil {
			return Payload{}, ErrInvalidPayload
		}
	}
	return Payload{SQL: sql, Params: params}, nil
}

// SealedPayload holds the encrypted approval payload envelope.
type SealedPayload struct {
	KeyVersion uint32
	WrappedDEK []byte
	Nonce      []byte
	Ciphertext []byte
}
