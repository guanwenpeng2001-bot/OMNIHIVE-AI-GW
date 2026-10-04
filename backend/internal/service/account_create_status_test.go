package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildAccountForCreateStatus(t *testing.T) {
	for _, status := range []string{"", StatusActive, "inactive", StatusError, "disabled"} {
		t.Run("status="+status, func(t *testing.T) {
			input := &CreateAccountInput{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: status}
			account, err := buildAccountForCreate(input, nil)
			if status == "disabled" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if status == "" {
				status = StatusActive
			}
			require.Equal(t, status, account.Status)
			require.True(t, account.Schedulable)
		})
	}
}
