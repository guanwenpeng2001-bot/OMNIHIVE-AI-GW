//go:build integration

package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"time"
)

func (suite *ProxyExpirySuite) TestSweep_SkipsInactiveBackup() {
	for _, chain := range []bool{false, true} {
		suite.Run(map[bool]string{false: "unresolved", true: "next active backup"}[chain], func() {
			now := time.Now()
			past := now.Add(-time.Hour)
			future := now.Add(time.Hour)
			healthy := suite.mkProxy("active-tail", service.FallbackModeNone, &future, nil)
			backup := suite.mkProxy("disabled-backup", service.FallbackModeNone, &future, nil)
			disabled, err := suite.repo.GetByID(suite.ctx, backup)
			suite.Require().NoError(err)
			disabled.Status = "inactive"
			if chain {
				disabled.FallbackMode = service.FallbackModeProxy
				disabled.BackupProxyID = &healthy
			}
			suite.Require().NoError(suite.repo.Update(suite.ctx, disabled))
			source := suite.mkProxy("expired-source", service.FallbackModeProxy, &past, &backup)
			account := suite.mkAccountWithProxy(source)
			_, err = suite.repo.SweepExpiredProxies(suite.ctx, now)
			suite.Require().NoError(err)
			if chain {
				suite.Equal(&healthy, suite.accountProxyID(account))
			} else {
				suite.Equal(&source, suite.accountProxyID(account))
			}
			got, err := suite.repo.GetByID(suite.ctx, backup)
			suite.Require().NoError(err)
			suite.Equal("inactive", got.Status, "fallback traversal must not reactivate disabled proxies")
		})
	}
}
