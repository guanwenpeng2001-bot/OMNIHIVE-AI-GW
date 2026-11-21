//go:build unit

package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestLoginAgreementCannotBeEnabledWithTemplateDocuments(t *testing.T) {
	handler, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName: "Example Gateway",
	})
	template := map[string]any{
		"login_agreement_enabled": true,
		"login_agreement_mode":    "checkbox",
		"login_agreement_documents": []map[string]any{{
			"id": "terms", "title": "Terms", "content_md": "[TEMPLATE — replace before enabling registration agreements]\n\n# Terms",
		}},
	}
	recorder := doUpdateSettings(t, handler, template, nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotEqual(t, "true", repo.values[service.SettingKeyLoginAgreementEnabled])

	final := map[string]any{
		"login_agreement_enabled": true,
		"login_agreement_mode":    "checkbox",
		"login_agreement_documents": []map[string]any{{
			"id": "terms", "title": "Terms", "content_md": "# Terms\n\nFinal operator text.",
		}},
	}
	recorder = doUpdateSettings(t, handler, final, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	public, err := handler.settingService.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.True(t, public.LoginAgreementEnabled, "an accepted switch must be visible to registrants")
}
