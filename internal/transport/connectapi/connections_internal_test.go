package connectapi

import (
	"testing"

	"connectrpc.com/connect"
)

// parseConnectionID must return the CANONICAL (lowercase, hyphenated) form,
// not the caller's spelling: google/uuid's Parse accepts non-standard
// encodings (uppercase, no hyphens, urn:uuid:, braces — its docs say not to
// use it for validation), and downstream the id becomes the AEAD AAD. A
// non-canonical id would update the same database row (PostgreSQL compares
// uuid values) while binding the ciphertext to a string no later canonical
// request can present — permanently undecryptable (external review).
func TestParseConnectionIDCanonicalizes(t *testing.T) {
	t.Parallel()
	const canonical = "0f60e9a7-c908-4103-9b91-c2e6f9375464"
	for _, raw := range []string{
		canonical,
		"0F60E9A7-C908-4103-9B91-C2E6F9375464",
		"0f60e9a7c90841039b91c2e6f9375464",
		"urn:uuid:0f60e9a7-c908-4103-9b91-c2e6f9375464",
		"{0f60e9a7-c908-4103-9b91-c2e6f9375464}",
	} {
		id, err := parseConnectionID(raw)
		if err != nil {
			t.Errorf("parseConnectionID(%q) error: %v", raw, err)
			continue
		}
		if string(id) != canonical {
			t.Errorf("parseConnectionID(%q) = %q, want canonical %q", raw, id, canonical)
		}
	}
}

func TestParseConnectionIDRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "not-a-uuid", "0f60e9a7-c908-4103-9b91"} {
		if _, err := parseConnectionID(raw); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("parseConnectionID(%q) code = %v, want InvalidArgument", raw, connect.CodeOf(err))
		}
	}
}
