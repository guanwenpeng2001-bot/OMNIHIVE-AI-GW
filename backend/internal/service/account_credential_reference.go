package service

import "encoding/json"

// MarshalJSON keeps reference-backed secrets out of scheduler snapshots and
// other serialized Account copies. Runtime consumers reload the authoritative
// account before use; management DTOs continue their existing redaction.
func (a Account) MarshalJSON() ([]byte, error) {
	type accountJSON Account
	copy := accountJSON(a)
	if _, referenced := CredentialReference(a.Credentials); referenced {
		copy.Credentials, _ = SplitCredentialSecrets(a.Credentials)
		copy.Credentials[CredentialSecretVersionKey] = CredentialVersion(a.Credentials)
	}
	return json.Marshal(copy)
}
