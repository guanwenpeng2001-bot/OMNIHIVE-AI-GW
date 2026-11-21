//go:build integration

package repository

import (
	"encoding/json"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func (suite *ProxyExpirySuite) TestRevertClearsBillingProbe() {
	original := suite.mkProxy("original", service.FallbackModeDirect, nil, nil)
	backup := suite.mkProxy("backup", service.FallbackModeNone, nil, nil)
	account := suite.mkAccountWithProxy(backup)
	_, err := suite.tx.ExecContext(suite.ctx, `UPDATE accounts SET type='apikey',proxy_fallback_origin_id=$1,extra='{"upstream_billing_probe_enabled":true,"upstream_billing_probe":{"status":"ok","data":{"balance":123}},"keep_me":true}'::jsonb WHERE id=$2`, original, account)
	suite.Require().NoError(err)
	repo := newAccountRepositoryWithSQL(suite.tx.Client(), suite.tx, nil)
	suite.Require().NoError(repo.RevertProxyFallback(suite.ctx, account))
	suite.Require().Equal(&original, suite.accountProxyID(account))
	var raw []byte
	suite.Require().NoError(scanSingleRow(suite.ctx, suite.tx, `SELECT extra FROM accounts WHERE id=$1`, []any{account}, &raw))
	var extra map[string]any
	suite.Require().NoError(json.Unmarshal(raw, &extra))
	suite.NotContains(extra, "upstream_billing_probe", "probe from previous network identity must be invalidated")
	suite.Equal(true, extra["keep_me"])
}

func (suite *ProxyExpirySuite) TestRevertProbeInvalidationScope() {
	for _, tc := range []struct {
		name, kind          string
		same, direct, clear bool
	}{
		{"changed API key", "apikey", false, false, true},
		{"direct API key", "apikey", false, true, true},
		{"same proxy", "apikey", true, false, false},
		{"oauth metadata", "oauth", false, false, false},
	} {
		suite.Run(tc.name, func() {
			original := suite.mkProxy("origin", service.FallbackModeNone, nil, nil)
			current := suite.mkProxy("current", service.FallbackModeNone, nil, nil)
			if tc.same {
				current = original
			}
			account := suite.mkAccountWithProxy(current)
			_, err := suite.tx.ExecContext(suite.ctx, `UPDATE accounts SET type=$1,proxy_fallback_origin_id=$2,extra='{"upstream_billing_probe_enabled":true,"upstream_billing_probe":{"status":"ok"},"keep_me":true}'::jsonb WHERE id=$3`, tc.kind, original, account)
			suite.Require().NoError(err)
			if tc.direct {
				_, err = suite.tx.ExecContext(suite.ctx, `UPDATE accounts SET proxy_id=NULL WHERE id=$1`, account)
				suite.Require().NoError(err)
			}
			repo := newAccountRepositoryWithSQL(suite.tx.Client(), suite.tx, nil)
			suite.Require().NoError(repo.RevertProxyFallback(suite.ctx, account))
			var raw []byte
			suite.Require().NoError(scanSingleRow(suite.ctx, suite.tx, `SELECT extra FROM accounts WHERE id=$1`, []any{account}, &raw))
			var extra map[string]any
			suite.Require().NoError(json.Unmarshal(raw, &extra))
			if tc.clear {
				suite.NotContains(extra, "upstream_billing_probe")
			} else {
				suite.Contains(extra, "upstream_billing_probe")
			}
			suite.Equal(true, extra["upstream_billing_probe_enabled"])
			suite.Equal(true, extra["keep_me"])
			suite.Equal(&original, suite.accountProxyID(account))
		})
	}
}
