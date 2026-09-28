package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTPMConfig_CreateUpdateReadRoundTrip(t *testing.T) {
	admin := &stubAdminService{apiKeys: []service.APIKey{{ID: 7, TPMLimit: 12}}}
	handler := NewAdminAPIKeyHandler(admin)
	for _, value := range []string{"31", "0"} {
		response := httptest.NewRecorder()
		request, _ := gin.CreateTestContext(response)
		request.Params = gin.Params{{Key: "id", Value: "7"}}
		request.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/7/tpm", strings.NewReader(`{"tpm_limit":`+value+`}`))
		request.Request.Header.Set("Content-Type", "application/json")
		handler.UpdateTPMLimit(request)
		require.Equal(t, 200, response.Code)
		var result struct {
			Data struct {
				TPMLimit int64 `json:"tpm_limit"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, admin.apiKeys[0].TPMLimit, result.Data.TPMLimit)
	}
}

func TestTPMConfig_RejectsNullNegativeFractionAndOverflow(t *testing.T) {
	for _, value := range []string{`null`, `-1`, `1.5`, `"2"`, `9223372036854775808`} {
		payload := []byte(`{"tpm_limit":` + value + `}`)
		for _, request := range []any{&CreateAccountRequest{}, &UpdateAccountRequest{}, &BulkUpdateAccountsRequest{}, &CreateGroupRequest{}, &UpdateGroupRequest{}, &DataAccount{}} {
			require.Error(t, json.Unmarshal(payload, request), "%T: %s", request, value)
		}
	}
}

func TestTPMConfig_OmittedAndZeroAreDifferent(t *testing.T) {
	var update UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &update))
	require.Nil(t, update.TPMLimit.Pointer())
	require.NoError(t, json.Unmarshal([]byte(`{"tpm_limit":0}`), &update))
	require.NotNil(t, update.TPMLimit.Pointer())
	require.Zero(t, *update.TPMLimit.Pointer())
	var bulk BulkUpdateAccountsRequest
	require.NoError(t, json.Unmarshal([]byte(`{"priority":3}`), &bulk))
	require.Nil(t, bulk.TPMLimit.Pointer())
}

func TestTPMConfig_BulkOnlyTPMLimitIsAnUpdate(t *testing.T) {
	admin:=&stubAdminService{}
	handler:=&AccountHandler{adminService:admin}
	response:=httptest.NewRecorder();request,_:=gin.CreateTestContext(response)
	request.Request=httptest.NewRequest(http.MethodPost,"/api/v1/admin/accounts/bulk-update",strings.NewReader(`{"account_ids":[1,2],"tpm_limit":0}`))
	request.Request.Header.Set("Content-Type","application/json")
	handler.BulkUpdate(request)
	require.Equal(t,200,response.Code,response.Body.String())
	require.NotNil(t,admin.lastBulkUpdateAccountInput)
	require.NotNil(t,admin.lastBulkUpdateAccountInput.TPMLimit)
	require.Zero(t,*admin.lastBulkUpdateAccountInput.TPMLimit)
}
