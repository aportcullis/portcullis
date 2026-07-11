package crypto

// AAD builds the canonical associated data for an envelope (ADR-0003):
//
//	portcullis/aad/v1|<record_type>|<organization_id>|<record_id>
//
// UTF-8, `|`-separated — safe without escaping because every field is a fixed
// token, UUID, or integer. Both envelope layers authenticate it, so a sealed
// value can't be swapped across records, record types, or organizations.
// (The chunked variant with a trailing chunk index is added with its first
// consumer, the result store.)
func AAD(recordType, organizationID, recordID string) []byte {
	return []byte(aadPrefix + "|" + recordType + "|" + organizationID + "|" + recordID)
}
