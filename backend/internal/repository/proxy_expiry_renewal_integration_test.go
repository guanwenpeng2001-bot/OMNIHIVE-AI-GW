//go:build integration

package repository

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"time"
)

// Reproduce the ordering deterministically without sleeps: scan snapshot,
// administrator edit, then the per-proxy transaction consuming that snapshot.
func (suite *ProxyExpirySuite) TestSweep_SkipsChangedSnapshot() {
	for _, edit := range []string{"renew", "clear expiry", "disable", "change mode", "change backup"} {
		suite.Run(edit, func() {
			now := time.Now()
			past := now.Add(-time.Hour)
			future := now.Add(24 * time.Hour)
			backup := suite.mkProxy("snapshot-backup", service.FallbackModeNone, &future, nil)
			source := suite.mkProxy("snapshot-source", service.FallbackModeDirect, &past, nil)
			if edit == "change backup" {
				_, err := suite.tx.ExecContext(suite.ctx, `UPDATE proxies SET fallback_mode='proxy', backup_proxy_id=$1 WHERE id=$2`, backup, source)
				suite.Require().NoError(err)
			}
			account := suite.mkAccountWithProxy(source)
			snapshot, err := suite.repo.GetByID(suite.ctx, source)
			suite.Require().NoError(err)
			target, change := service.ResolveProxyFallbackTarget(*snapshot, map[int64]service.Proxy{
				backup: {ID: backup, Status: service.StatusActive, ExpiresAt: &future},
			}, now)
			suite.Require().True(change)
			updated := *snapshot
			switch edit {
			case "renew":
				updated.ExpiresAt = &future
			case "clear expiry":
				updated.ExpiresAt = nil
			case "disable":
				updated.Status = "inactive"
			case "change mode":
				updated.FallbackMode = service.FallbackModeNone
			case "change backup":
				otherBackup := suite.mkProxy("replacement-backup", service.FallbackModeNone, &future, nil)
				updated.BackupProxyID = &otherBackup
			}
			suite.Require().NoError(suite.repo.Update(suite.ctx, &updated))
			var eventsBefore int64
			suite.Require().NoError(scanSingleRow(suite.ctx, suite.tx, `SELECT COUNT(*) FROM scheduler_outbox`, nil, &eventsBefore))
			changed, err := suite.repo.sweepOneExpiredProxy(suite.ctx, *snapshot, now, target, change)
			suite.Require().NoError(err)
			suite.Empty(changed)
			got, err := suite.repo.GetByID(suite.ctx, source)
			suite.Require().NoError(err)
			suite.Equal(updated.Status, got.Status)
			suite.Equal(&source, suite.accountProxyID(account))
			var origin *int64
			suite.Require().NoError(scanSingleRow(suite.ctx, suite.tx, `SELECT proxy_fallback_origin_id FROM accounts WHERE id=$1`, []any{account}, &origin))
			suite.Nil(origin)
			var eventsAfter int64
			suite.Require().NoError(scanSingleRow(suite.ctx, suite.tx, `SELECT COUNT(*) FROM scheduler_outbox`, nil, &eventsAfter))
			suite.Equal(eventsBefore, eventsAfter, "stale sweep must not publish account changes")
		})
	}
}

func (suite *ProxyExpirySuite) TestSweep_ChangedModeIsUsedOnNextScan() {
	now := time.Now()
	past := now.Add(-time.Hour)
	source := suite.mkProxy("mode-source", service.FallbackModeDirect, &past, nil)
	account := suite.mkAccountWithProxy(source)
	snapshot, err := suite.repo.GetByID(suite.ctx, source)
	suite.Require().NoError(err)
	updated := *snapshot
	updated.FallbackMode = service.FallbackModeNone
	suite.Require().NoError(suite.repo.Update(suite.ctx, &updated))
	changed, err := suite.repo.sweepOneExpiredProxy(suite.ctx, *snapshot, now, nil, true)
	suite.Require().NoError(err)
	suite.Empty(changed)
	_, err = suite.repo.SweepExpiredProxies(suite.ctx, now)
	suite.Require().NoError(err)
	got, err := suite.repo.GetByID(suite.ctx, source)
	suite.Require().NoError(err)
	suite.Equal(service.StatusExpired, got.Status)
	suite.Equal(&source, suite.accountProxyID(account), "new none policy must preserve account binding")
}
