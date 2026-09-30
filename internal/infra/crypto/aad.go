package crypto

// AAD binds envelope layers to record type, organization, and record ID. Fixed tokens and IDs make the delimiter-separated format unambiguous (ADR-0003).
func AAD(recordType, organizationID, recordID string) []byte {
	return []byte(aadPrefix + "|" + recordType + "|" + organizationID + "|" + recordID)
}
