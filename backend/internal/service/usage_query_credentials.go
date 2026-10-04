package service

import (
	"net/http"
	"strings"
)

// Only query eligibility and header construction live here. The repository resolves
// the same credential reference as inference and owns secret storage/rotation.
func managementQueryHeaders(account *Account) (http.Header, error) {
	fields := []string{"usage_query_token"}
	if account.Platform == "dmxapi" || account.Platform == "crazyrouter" || account.Platform == "crazyrouter_intl" {
		fields = append(fields, "usage_query_user_id")
	}
	for _, field := range fields {
		value, present := account.Credentials[field]
		if !present || value == nil || value == "" {
			return nil, compatibleQueryError{"missing_query_credentials", "configure management query credentials"}
		}
		text, valid := value.(string)
		if !valid {
			return nil, compatibleQueryError{"invalid_credentials", "invalid management query credentials"}
		}
		if field == "usage_query_user_id" && strings.TrimSpace(text) == "" {
			return nil, compatibleQueryError{"missing_query_credentials", "configure management query credentials"}
		}
		for _, character := range text {
			if character < 33 || character > 126 {
				return nil, compatibleQueryError{"invalid_credentials", "invalid management query credentials"}
			}
		}
	}
	token := account.GetCredential("usage_query_token")
	headers := make(http.Header)
	if account.Platform == "aihubmix" {
		headers.Set("Authorization", token)
	} else {
		headers.Set("Authorization", "Bearer "+token)
	}
	if account.Platform == "dmxapi" {
		headers.Set("Dmx-Api-User", account.GetCredential("usage_query_user_id"))
	}
	if account.Platform == "crazyrouter" || account.Platform == "crazyrouter_intl" {
		headers.Set("New-Api-User", account.GetCredential("usage_query_user_id"))
	}
	return headers, nil
}
