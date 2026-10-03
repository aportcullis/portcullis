package access

import (
	"strings"
	"unicode/utf8"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// The 56 KiB limit includes request narrative, SQL and all parameter names and values, leaving room within the 64 KiB transport limit for the Connect envelope and metadata.
const (
	MaxPayloadBytes      = 56 * 1024
	MaxPayloadParams     = 100
	MaxRequestTitleChars = 200
	MaxRequestBodyChars  = 4000
)

// countPayloadBytes sums SQL and parameter-name/value byte lengths.
func countPayloadBytes(sql string, params []query.Parameter) int {
	total := len(sql)
	for _, p := range params {
		total += len(p.Name) + len(p.Value.Text)
	}
	return total
}

// Payload holds the plaintext narrative, SQL and typed parameters for an approval.
type Payload struct {
	Title  string
	Body   string
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

// NewDescribedPayload validates a request's narrative and SQL payload.
func NewDescribedPayload(title, body, sql string, params []query.Parameter) (Payload, error) {
	if !utf8.ValidString(title) || !utf8.ValidString(body) || strings.ContainsAny(title, "\r\n\x00") || strings.ContainsRune(body, 0) {
		return Payload{}, ErrInvalidPayload
	}
	normalizedTitle := strings.TrimSpace(title)
	if (title != "" && normalizedTitle == "") || utf8.RuneCountInString(normalizedTitle) > MaxRequestTitleChars || utf8.RuneCountInString(body) > MaxRequestBodyChars {
		return Payload{}, ErrInvalidPayload
	}
	payload, err := NewPayload(sql, params)
	if err != nil {
		return Payload{}, err
	}
	if len(normalizedTitle)+len(body)+countPayloadBytes(sql, params) > MaxPayloadBytes {
		return Payload{}, ErrInvalidPayload
	}
	payload.Title, payload.Body = normalizedTitle, body
	return payload, nil
}
