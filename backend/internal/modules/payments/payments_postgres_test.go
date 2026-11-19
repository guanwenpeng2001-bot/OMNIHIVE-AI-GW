//go:build integration

package payments_test

import (
	"context"
	"database/sql"
	"encoding/json"

	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	ent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	_ "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/runtime"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/payments"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/payments"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment/provider"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

type gate struct {
	actor int64
	sid   string
}

func (g *gate) HasStepUpGrant(_ context.Context, actor int64, sid string) (bool, error) {
	return actor == g.actor && sid == g.sid, nil
}

func TestPaymentWritersPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL job_test")
	}
	ctx := t.Context()
	db, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	id := func(q string, args ...any) int64 {
		t.Helper()
		var result int64
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&result))
		return result
	}
	value := func(q string, args ...any) string {
		t.Helper()
		var result string
		require.NoError(t, db.QueryRowContext(ctx, q, args...).Scan(&result))
		return result
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}
	payer := func(a string) int64 {
		return id("INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',$2) RETURNING id", uuid.NewString()+"@example.test", a)
	}
	balance := func(uid int64) string { return value("SELECT balance FROM users WHERE id=$1", uid) }
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	users := repository.NewUserRepository(client, db)
	groups := repository.NewGroupRepository(client, db)
	subs := service.NewSubscriptionService(groups, repository.NewUserSubscriptionRepository(client), nil, client, nil)
	defer subs.Stop()
	redeem := service.NewRedeemService(repository.NewRedeemCodeRepository(client), users, subs, nil, nil, client, nil, nil)
	svc := service.NewPaymentService(client, nil, nil, redeem, subs, nil, users, groups, nil)
	s := payments.Store{DB: db, Fulfillment: svc}
	w := id("INSERT INTO workspaces(name) VALUES('BE-19') RETURNING id")
	p := id("SELECT id FROM projects WHERE workspace_id=$1 AND is_default", w)
	uid := payer("900000000001.00000001")
	viewer := payer("0")
	require.Equal(t, balance(uid), value("SELECT amount FROM payment_wallet_evidence WHERE payer_id=$1 AND kind='opening'", uid))
	group := id("INSERT INTO groups(name,workspace_id,platform,subscription_type) VALUES($1,$2,'openai','subscription') RETURNING id", uuid.NewString(), w)
	order := func(cur, price, kind string) (int64, payment.PaymentNotification) {
		t.Helper()
		trade := uuid.NewString()
		out := uuid.NewString()
		var groupID any
		var days any
		if kind == "subscription" {
			groupID = group
			days = 10
		}
		oid := id(`INSERT INTO payment_orders(user_id,amount,pay_amount,order_type,payment_type,provider_key,provider_instance_id,provider_snapshot,out_trade_no,
 recharge_code,expires_at,subscription_group_id,subscription_days) VALUES($1,9.01,$2,$3,'stripe','stripe','7',jsonb_build_object('currency',$4::text),$5,$6,now()+interval '1 day',$7,$8) RETURNING id`, uid, price, kind, cur, out, uuid.NewString()[:30], groupID, days)
		minor, err := payment.AmountToMinorUnit(price, cur)
		require.NoError(t, err)
		return oid, payment.PaymentNotification{Status: "success", TradeNo: trade, OrderID: out, EventID: uuid.NewString(), EffectID: trade, InstanceID: "7", Currency: cur, Minor: minor}
	}
	oid, paid := order("KWD", "12.345", "balance")
	// Decode a signed notification through the production adapter before every
	// nominal delivery, including the refund and dispute retries.
	apply := func(want payment.PaymentNotification) error {
		prov, err := provider.NewStripe(want.InstanceID, map[string]string{"secretKey": "fixture", "webhookSecret": "fixture", "currency": want.Currency})
		if err != nil {
			return err
		}
		eventType := "payment_intent.succeeded"
		obj := map[string]any{"id": want.TradeNo, "currency": want.Currency, "amount_received": want.Minor, "amount": want.Minor, "metadata": map[string]string{"orderId": want.OrderID}}
		switch want.Status {
		case "failed":
			eventType = "payment_intent.payment_failed"
		case "refund", "chargeback", "refund_confirmation":
			eventType = "refund.updated"
			status := "succeeded"
			if want.Status == "chargeback" {
				eventType = "charge.dispute.closed"
				status = "lost"
			}
			obj = map[string]any{"id": want.EffectID, "currency": want.Currency, "amount": want.Minor, "status": status, "payment_intent": want.TradeNo}
			if want.Status == "refund_confirmation" {
				obj["metadata"] = map[string]string{"omnihive_refund_order": want.OrderID}
			}
		}
		body, err := json.Marshal(map[string]any{"id": want.EventID, "object": "event", "api_version": stripe.APIVersion, "type": eventType, "data": map[string]any{"object": obj}})
		if err != nil {
			return err
		}
		sig := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: body, Secret: "fixture"})
		decoded, err := prov.VerifyNotification(ctx, string(body), map[string]string{"stripe-signature": sig.Header})
		if err != nil {
			return err
		}
		return s.HandleVerified(ctx, "stripe", decoded)
	}
	reversal := func(status string, minor int64) payment.PaymentNotification {
		n := paid
		n.OrderID = ""
		n.Status = status
		n.Minor = minor
		n.EventID = uuid.NewString()
		n.EffectID = uuid.NewString()
		return n
	}
	refund := reversal("refund", 4115)
	require.ErrorIs(t, apply(refund), payments.ErrPending)
	for _, mut := range []func(*payment.PaymentNotification){func(n *payment.PaymentNotification) { n.Minor-- }, func(n *payment.PaymentNotification) { n.Currency = "USD" }, func(n *payment.PaymentNotification) { n.InstanceID = "8" }} {
		bad := paid
		mut(&bad)
		require.Error(t, apply(bad))
	}
	require.Zero(t, id("SELECT count(*) FROM payment_provider_events WHERE order_id=$1", oid))
	failed := paid
	failed.Status = "failed"
	failed.EventID = uuid.NewString()
	require.NoError(t, apply(failed))
	// Commit the captured evidence, then simulate the application dying before fulfillment.
	unavailable := payments.Store{DB: db}
	racing := make(chan error, 8)
	var admitting sync.WaitGroup
	for range cap(racing) {
		admitting.Go(func() { racing <- unavailable.HandleVerified(ctx, "stripe", &paid) })
	}
	admitting.Wait()
	close(racing)
	for err := range racing {
		require.ErrorIs(t, err, payments.ErrPending)
	}
	require.EqualValues(t, 1, id("SELECT count(*) FROM payment_provider_events WHERE order_id=$1 AND kind='success'", oid))
	require.Equal(t, "PAID", value("SELECT status FROM payment_orders WHERE id=$1", oid))
	require.NoError(t, apply(paid))
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range cap(errs) {
		wg.Go(func() { errs <- apply(paid) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, apply(failed))
	require.Equal(t, "COMPLETED", value("SELECT status FROM payment_orders WHERE id=$1", oid))
	require.Equal(t, "900000000010.01000001", balance(uid))
	require.EqualValues(t, 12345, id("SELECT provider_minor FROM payment_order_terms WHERE order_id=$1", oid))
	require.NoError(t, apply(refund))
	retry := refund
	retry.EventID = uuid.NewString()
	require.NoError(t, apply(retry))
	dis := reversal("chargeback", 8230)
	require.NoError(t, apply(dis))
	require.NoError(t, apply(dis))
	require.NoError(t, apply(paid))
	require.Equal(t, "REFUNDED", value("SELECT status FROM payment_orders WHERE id=$1", oid))
	require.Equal(t, "900000000001.00000001", balance(uid))
	require.EqualValues(t, 2, id("SELECT count(*) FROM payment_reversals WHERE order_id=$1 AND original_wallet_event_id IS NOT NULL AND wallet_event_id IS NOT NULL", oid))
	require.Equal(t, "9.01000000", value("SELECT sum(source_amount) FROM payment_reversals WHERE order_id=$1", oid))
	require.Error(t, apply(reversal("refund", 1)))
	bad := paid
	bad.EffectID = "changed"
	require.ErrorIs(t, s.HandleVerified(ctx, "stripe", &bad), payments.ErrConflict)
	_, err = db.ExecContext(ctx, "DELETE FROM payment_order_history WHERE order_id=$1", oid)
	require.Error(t, err)
	oid2, other := order("JPY", "321", "subscription")
	other.TradeNo = paid.TradeNo
	other.EffectID = paid.TradeNo
	require.Error(t, apply(other))
	other.TradeNo = uuid.NewString()
	other.EffectID = other.TradeNo
	require.NoError(t, apply(other))
	require.NoError(t, apply(other))
	require.EqualValues(t, 321, id("SELECT provider_minor FROM payment_order_terms WHERE order_id=$1", oid2))
	require.EqualValues(t, 1, id("SELECT count(*) FROM payment_entitlement_events WHERE order_id=$1 AND kind='plan_assignment'", oid2))
	other.Status = "chargeback"
	other.OrderID = ""
	other.EffectID = uuid.NewString()
	other.EventID = uuid.NewString()
	require.NoError(t, apply(other))
	require.Equal(t, "expired", value("SELECT status FROM user_subscriptions WHERE user_id=$1 AND group_id=$2", uid, group))
	oid3, two := order("USD", "12.34", "balance")
	// The normal v1 update is also guarded at the PostgreSQL write boundary.
	_, err = db.ExecContext(ctx, "UPDATE payment_orders SET status='PAID',pay_amount=12.33,paid_at=now(),payment_trade_no=$2 WHERE id=$1", oid3, two.TradeNo)
	require.Error(t, err)
	require.EqualValues(t, 1234, id("SELECT provider_minor FROM payment_order_terms WHERE order_id=$1", oid3))

	require.NoError(t, apply(two))
	exec("UPDATE payment_orders SET status='REFUNDED',refund_amount=amount,refund_at=now() WHERE id=$1", oid3)
	confirmed := two
	confirmed.Status = "refund_confirmation"
	confirmed.EventID = uuid.NewString()
	confirmed.EffectID = uuid.NewString()
	beforeConfirm := balance(uid)
	require.NoError(t, apply(confirmed))
	require.NoError(t, apply(confirmed))
	require.Equal(t, beforeConfirm, balance(uid))
	// Restore this independent fixture's opening for the exact-dollar assertions.
	exec("UPDATE users SET balance=balance-9.01 WHERE id=$1", uid)
	code := uuid.NewString()[:30]
	exec("INSERT INTO redeem_codes(code,value) VALUES($1,1.00000001)", code)
	cmd := uuid.New()
	require.NoError(t, s.RedeemBalance(ctx, cmd, uid, code))
	require.NoError(t, s.RedeemBalance(ctx, cmd, uid, code))
	require.Error(t, s.RedeemBalance(ctx, uuid.New(), uid, code))
	require.ErrorIs(t, s.RedeemBalance(ctx, cmd, viewer, code), payments.ErrConflict)
	source := id("SELECT id FROM payment_entitlement_events WHERE command_id=$1", cmd)
	require.NotZero(t, id("SELECT wallet_event_id FROM payment_wallet_links WHERE entitlement_event_id=$1", source))
	promo := uuid.NewString()[:30]
	exec("INSERT INTO promo_codes(code,bonus_amount,max_uses) VALUES($1,0.00000002,1)", promo)
	promoCmd := uuid.New()
	require.NoError(t, s.ApplyPromotion(ctx, promoCmd, uid, promo))
	require.NoError(t, s.ApplyPromotion(ctx, promoCmd, uid, promo))
	require.Error(t, s.ApplyPromotion(ctx, uuid.New(), uid, promo))
	affCmd := uuid.New()
	exec("INSERT INTO user_affiliates(user_id,aff_code,aff_quota,aff_frozen_quota) VALUES($1,$2,0.00000001,0.00000001)", uid, uuid.NewString()[:20])
	exec("INSERT INTO user_affiliate_ledger(user_id,amount,action,frozen_until) VALUES($1,0.00000001,'accrue',now()-interval '1 day')", uid)
	require.NoError(t, s.TransferAffiliate(ctx, affCmd, uid))
	exec("UPDATE user_affiliates SET aff_quota=2 WHERE user_id=$1", uid)
	require.NoError(t, s.TransferAffiliate(ctx, affCmd, uid))
	require.Equal(t, "2.00000000", value("SELECT aff_quota FROM user_affiliates WHERE user_id=$1", uid))
	require.Equal(t, "900000000002.00000006", balance(uid))
	amt, err := money.NewMoney("USD", "-0.00000001")
	require.NoError(t, err)
	require.NoError(t, s.AdjustWallet(ctx, uuid.New(), uid, amt))
	require.Equal(t, balance(uid), value("SELECT sum(amount) FROM payment_wallet_evidence WHERE payer_id=$1", uid))
	require.EqualValues(t, 0, id("SELECT count(*) FROM payment_wallet_evidence WHERE payer_id=$1 AND command_id IS NULL", uid))
	frozen, err := s.FundingResult(ctx, affCmd, uid)
	require.NoError(t, err)
	require.Equal(t, "0.00000002", frozen.Delta)
	require.Equal(t, "900000000002.00000006", frozen.After)
	// A v1 worker scans quota as a float. The exact locked source supplies
	// both the wallet movement and the four snapshots of the transfer.
	legacyPayer := payer("0")
	exec("INSERT INTO user_affiliates(user_id,aff_code,aff_quota) VALUES($1,$2,900000000000.00000001)", legacyPayer, uuid.NewString()[:20])
	_, _, err = repository.NewAffiliateRepository(client, db).TransferQuotaToBalance(ctx, legacyPayer)
	require.NoError(t, err)
	require.Equal(t, "900000000000.00000001", balance(legacyPayer))
	require.Equal(t, balance(legacyPayer), value("SELECT amount FROM user_affiliate_ledger WHERE user_id=$1 AND action='transfer'", legacyPayer))
	// Use a real BE-16 adjustment and two different project roles.
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member')", w, uid, viewer)
	exec("INSERT INTO project_grants(workspace_id,project_id,user_id,action) VALUES($1,$2,$3,'billing.read')", w, p, viewer)
	adj := uuid.New()
	receipt := id(`INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state)
 VALUES($1,1,$2,1,'be19','{}','{}','priced','','resolved','complete','pending') RETURNING id`, uuid.NewString(), uid)
	exec("INSERT INTO billing_commands(id,workspace_id,project_id,actor_id,route,key_hash,fingerprint) VALUES($1,$2,$3,$4,'adjustments','a','b')", adj, w, p, uid)
	orig := id("SELECT wallet_event_id FROM payment_wallet_links WHERE entitlement_event_id=$1", source)
	delta := id("SELECT max(wallet_event_id) FROM payment_wallet_evidence WHERE payer_id=$1", uid)
	exec("INSERT INTO billing_adjustment_effects(id,workspace_id,project_id,request_id,receipt_id,original_event_id,event_id,currency,reason,note) VALUES($1,$2,$3,'fixture',$4,$5,$6,'USD','billing_correction','fixture')", adj, w, p, receipt, orig, delta)
	g := gate{actor: uid, sid: "verified"}
	router := gin.New()
	control.Register(router.Group("/api/v2"), control.Handler{Store: s, Access: postgres.New(db), Step: &g}, func(c *gin.Context) {
		u, _ := strconv.ParseInt(c.GetHeader("actor"), 10, 64)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: u})
		c.Set(middleware.ContextKeySessionID, c.GetHeader("session"))
	})
	call := func(method, sid, path string, actor int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("actor", fmt.Sprint(actor))
		req.Header.Set("session", sid)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	base := fmt.Sprintf("/api/v2/workspaces/%d/projects/%d/billing/adjustments/%s/payment-evidence", w, p, adj)
	link := base + "/" + fmt.Sprint(source)
	require.Equal(t, 403, call(http.MethodPut, "other", link, uid).Code)
	require.Equal(t, 403, call(http.MethodPut, "verified", link, viewer).Code)
	resp := call(http.MethodPut, "verified", link, uid)
	require.Equal(t, 204, resp.Code, resp.Body.String())
	require.Equal(t, 204, call(http.MethodPut, "verified", link, uid).Code)
	resp = call(http.MethodGet, "", base, viewer)
	require.Equal(t, 200, resp.Code, resp.Body.String())
	require.NotContains(t, resp.Body.String(), "references")
	require.NotContains(t, resp.Body.String(), cmd.String())
	resp = call(http.MethodGet, "", base, uid)
	require.Equal(t, 200, resp.Code)
	require.Contains(t, resp.Body.String(), cmd.String())
	require.Contains(t, resp.Body.String(), adj.String())
	require.ErrorIs(t, s.LinkAdjustment(ctx, payments.EvidenceScope{Workspace: w, Project: p + 1, Actor: uid}, adj, source, true), sql.ErrNoRows)
	require.ErrorIs(t, s.LinkAdjustment(ctx, payments.EvidenceScope{Workspace: w, Project: p, Actor: viewer}, adj, source, false), sql.ErrNoRows)
}
