-- Existing v1 builders submit pay_amount during INSERT. Keep their third
-- decimal before typmod coercion and validate it against the frozen rail.
ALTER TABLE payment_orders ALTER COLUMN pay_amount TYPE numeric,
    ALTER COLUMN amount TYPE numeric(26,8), ALTER COLUMN refund_amount TYPE numeric(26,8);

CREATE FUNCTION payment_minor(amount numeric, currency text) RETURNS numeric
LANGUAGE sql IMMUTABLE STRICT AS $$
 WITH unit AS (SELECT CASE
  WHEN currency IN ('BIF','CLP','DJF','GNF','JPY','KMF','KRW','MGA','PYG','RWF','VND','VUV','XAF','XOF','XPF','ISK','UGX') THEN 0
  WHEN currency IN ('BHD','IQD','JOD','KWD','LYD','OMR','TND') THEN 3 ELSE 2 END AS digits)
 SELECT CASE WHEN currency ~ '^[A-Z]{3}$' AND amount>0 AND amount=trunc(amount,digits)
   AND amount<'Infinity'::numeric
 THEN trunc(amount * power(10::numeric, CASE WHEN currency IN ('ISK','UGX') THEN 2 ELSE digits END)) END FROM unit
$$;

CREATE TABLE payment_order_terms (
 order_id bigint PRIMARY KEY, payer_id bigint NOT NULL, provider text NOT NULL, instance text NOT NULL,
 currency text, provider_minor numeric(24,0), source_amount numeric(26,8) NOT NULL,
 fee_rate numeric(10,4) NOT NULL, fee_rule text, plan_id bigint, group_id bigint, days integer,
 historical boolean NOT NULL
);

CREATE FUNCTION payment_freeze_order(o payment_orders, historical boolean) RETURNS void LANGUAGE plpgsql AS $$
DECLARE rail text := coalesce(nullif(o.provider_snapshot->>'provider_key',''),o.provider_key,o.payment_type,'');
 cur text := upper(nullif(btrim(o.provider_snapshot->>'currency'),''));
 minor numeric;
BEGIN
 IF cur IS NULL AND (rail IN ('alipay','easypay','wxpay') OR NOT historical) THEN cur := 'CNY'; END IF;
 minor := payment_minor(o.pay_amount,cur);
 -- Legacy NUMERIC(20,2) cannot prove the submitted third fractional digit.
 IF historical AND cur IN ('BHD','IQD','JOD','KWD','LYD','OMR','TND') THEN minor := NULL; END IF;
 IF NOT historical AND (minor IS NULL OR minor>9223372036854775807) THEN
   RAISE EXCEPTION 'invalid provider amount for currency %',cur;
 END IF;
 INSERT INTO payment_order_terms VALUES(o.id,o.user_id,rail,coalesce(o.provider_instance_id,''),cur,minor,
   o.amount,o.fee_rate,CASE WHEN NOT historical THEN 'fee-ceiling-v1' END,
   o.plan_id,o.subscription_group_id,o.subscription_days,historical);
END $$;

SELECT payment_freeze_order(o,true) FROM payment_orders o;
CREATE FUNCTION payment_freeze_new_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM payment_freeze_order(NEW,false); RETURN NEW; END $$;
CREATE TRIGGER payment_terms_created AFTER INSERT ON payment_orders FOR EACH ROW EXECUTE FUNCTION payment_freeze_new_order();

CREATE TABLE payment_commands (
 id uuid PRIMARY KEY, kind text NOT NULL, payer_id bigint NOT NULL,
 subject text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION payment_current_command() RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT nullif(current_setting('omnihive.payment_command',true),'')::uuid
$$;

CREATE TABLE payment_order_history (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, order_id bigint NOT NULL REFERENCES payment_order_terms,
 command_id uuid NOT NULL, prior_state text, state text NOT NULL, provider_trade text NOT NULL,
 refund_amount numeric(26,8) NOT NULL, observed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_order_history_order ON payment_order_history(order_id,id);
INSERT INTO payment_order_history(order_id,command_id,state,provider_trade,refund_amount)
 SELECT id,gen_random_uuid(),status,payment_trade_no,refund_amount FROM payment_orders;

CREATE TABLE payment_trade_claims (
 provider text NOT NULL, instance text NOT NULL, trade text NOT NULL,
 order_id bigint UNIQUE NOT NULL REFERENCES payment_order_terms, PRIMARY KEY(provider,instance,trade)
);
-- Freeze ambiguous historical trade IDs too. A future order must never reuse one.
CREATE TABLE payment_historical_trades AS
 SELECT t.provider,t.instance,o.payment_trade_no AS trade,min(o.id) AS first_order,count(*) AS orders
 FROM payment_orders o JOIN payment_order_terms t ON t.order_id=o.id
 WHERE o.paid_at IS NOT NULL AND o.payment_trade_no<>'' GROUP BY 1,2,3;
CREATE UNIQUE INDEX payment_historical_trade ON payment_historical_trades(provider,instance,trade);

CREATE FUNCTION payment_record_order() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE t payment_order_terms; prior text;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF (OLD.user_id,OLD.amount,OLD.pay_amount,OLD.fee_rate,OLD.provider_instance_id,OLD.provider_key,
      OLD.provider_snapshot,OLD.plan_id,OLD.subscription_group_id,OLD.subscription_days,OLD.order_type,OLD.out_trade_no)
   IS DISTINCT FROM (NEW.user_id,NEW.amount,NEW.pay_amount,NEW.fee_rate,NEW.provider_instance_id,NEW.provider_key,
      NEW.provider_snapshot,NEW.plan_id,NEW.subscription_group_id,NEW.subscription_days,NEW.order_type,NEW.out_trade_no)
  THEN RAISE EXCEPTION 'payment order terms or callback amount mismatch'; END IF;
  IF OLD.paid_at IS NOT NULL AND (NEW.paid_at IS DISTINCT FROM OLD.paid_at OR NEW.payment_trade_no<>OLD.payment_trade_no)
   THEN RAISE EXCEPTION 'captured payment identity is immutable'; END IF;
  IF OLD.status=NEW.status THEN RETURN NEW; END IF;
  prior := OLD.status;
 END IF;
 IF NEW.status='PAID' THEN
   SELECT * INTO STRICT t FROM payment_order_terms WHERE order_id=NEW.id;
   IF NEW.paid_at IS NULL OR btrim(NEW.payment_trade_no)='' OR EXISTS (SELECT 1 FROM payment_historical_trades WHERE
      provider=t.provider AND instance=t.instance AND trade=NEW.payment_trade_no AND (first_order<>NEW.id OR orders>1))
   THEN RAISE EXCEPTION 'missing or reused provider trade identity'; END IF;
   INSERT INTO payment_trade_claims VALUES(t.provider,t.instance,NEW.payment_trade_no,NEW.id);
 END IF;
 IF NEW.status='RECHARGING' AND NEW.paid_at IS NULL THEN RAISE EXCEPTION 'fulfillment requires captured payment'; END IF;
 INSERT INTO payment_order_history(order_id,command_id,prior_state,state,provider_trade,refund_amount)
 VALUES(NEW.id,coalesce(payment_current_command(),gen_random_uuid()),prior,NEW.status,NEW.payment_trade_no,NEW.refund_amount);
 RETURN NEW;
END $$;
-- AFTER INSERT trigger names run alphabetically following the terms snapshot.
CREATE TRIGGER payment_transition AFTER INSERT OR UPDATE ON payment_orders FOR EACH ROW EXECUTE FUNCTION payment_record_order();

CREATE TABLE payment_provider_events (
 provider text NOT NULL, instance text NOT NULL, event_key text NOT NULL, kind text NOT NULL,
 order_id bigint NOT NULL REFERENCES payment_order_terms, trade text NOT NULL, effect_key text NOT NULL,
 currency text NOT NULL, minor bigint NOT NULL CHECK(minor>0), command_id uuid UNIQUE NOT NULL,
 received_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(provider,instance,event_key)
);
CREATE INDEX payment_events_order ON payment_provider_events(order_id);
CREATE TABLE payment_entitlement_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, command_id uuid NOT NULL,
 kind text NOT NULL, source_id bigint NOT NULL, payer_id bigint NOT NULL, order_id bigint,
 amount numeric(26,8), group_id bigint, detail jsonb NOT NULL DEFAULT '{}',
 transaction_id xid8, observed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_entitlement_tx ON payment_entitlement_events(transaction_id,payer_id);
CREATE INDEX payment_entitlement_order ON payment_entitlement_events(order_id,kind);
CREATE UNIQUE INDEX payment_once_redeemed ON payment_entitlement_events(source_id) WHERE kind='redeem';
CREATE UNIQUE INDEX payment_once_affiliate ON payment_entitlement_events(source_id) WHERE kind IN ('affiliate_transfer','affiliate_accrue');
CREATE UNIQUE INDEX payment_once_promotion ON payment_entitlement_events(source_id) WHERE kind='promotion';

CREATE TABLE payment_wallet_links (
 entitlement_event_id bigint PRIMARY KEY REFERENCES payment_entitlement_events,
 wallet_event_id bigint UNIQUE NOT NULL REFERENCES billing_wallet_events
);
CREATE TABLE payment_reversals (
 provider text NOT NULL, instance text NOT NULL, effect_key text NOT NULL,
 order_id bigint NOT NULL REFERENCES payment_order_terms, command_id uuid UNIQUE NOT NULL REFERENCES payment_provider_events(command_id),
 kind text NOT NULL CHECK(kind IN ('refund','chargeback')), minor bigint NOT NULL CHECK(minor>0),
 source_amount numeric(26,8) NOT NULL, original_entitlement_id bigint NOT NULL REFERENCES payment_entitlement_events,
 original_wallet_event_id bigint REFERENCES billing_wallet_events, wallet_event_id bigint UNIQUE REFERENCES billing_wallet_events,
 PRIMARY KEY(provider,instance,effect_key)
);
CREATE INDEX payment_reversals_order ON payment_reversals(order_id);

-- Source rows that existed at cutoff retain their observed values. They carry
-- no transaction ID and cannot claim an earlier wallet event.
INSERT INTO payment_entitlement_events(command_id,kind,source_id,payer_id,amount,group_id,detail)
 SELECT gen_random_uuid(),'redeem',id,used_by,value,group_id,jsonb_build_object('type',type)
 FROM redeem_codes WHERE status='used' AND used_by IS NOT NULL;
INSERT INTO payment_entitlement_events(command_id,kind,source_id,payer_id,order_id,amount)
 SELECT gen_random_uuid(),'affiliate_'||action,id,user_id,source_order_id,amount
 FROM user_affiliate_ledger WHERE action IN ('transfer','accrue');
INSERT INTO payment_entitlement_events(command_id,kind,source_id,payer_id,amount)
 SELECT gen_random_uuid(),'promotion',id,user_id,bonus_amount FROM promo_code_usages;
INSERT INTO payment_entitlement_events(command_id,kind,source_id,payer_id,group_id,detail)
 SELECT gen_random_uuid(),'subscription',id,user_id,group_id,jsonb_build_object('starts_at',starts_at,'expires_at',expires_at,'status',status)
 FROM user_subscriptions;

CREATE FUNCTION payment_capture_source() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE k text; payer bigint; oid bigint; qty numeric; gid bigint; facts jsonb:='{}'; sid bigint:=NEW.id;
BEGIN
 CASE TG_TABLE_NAME
 WHEN 'redeem_codes' THEN
   IF NEW.status<>'used' OR NEW.used_by IS NULL THEN RETURN NEW; END IF;
   IF TG_OP='UPDATE' AND OLD.status='used' THEN RETURN NEW; END IF;
   k:='redeem'; payer:=NEW.used_by; qty:=NEW.value; gid:=NEW.group_id;
   facts:=jsonb_build_object('type',NEW.type,'days',NEW.validity_days);
   SELECT CASE WHEN count(*)=1 THEN min(id) END INTO oid FROM payment_orders
     WHERE recharge_code=NEW.code AND user_id=payer AND order_type='balance' AND amount=qty AND paid_at IS NOT NULL;
 WHEN 'user_affiliate_ledger' THEN
   k:='affiliate_'||NEW.action; payer:=NEW.user_id; qty:=NEW.amount; oid:=NEW.source_order_id;
 WHEN 'promo_code_usages' THEN
   k:='promotion'; payer:=NEW.user_id; qty:=NEW.bonus_amount;
 WHEN 'user_subscriptions' THEN
   k:='subscription'; payer:=NEW.user_id; gid:=NEW.group_id;
   facts:=jsonb_build_object('starts_at',NEW.starts_at,'expires_at',NEW.expires_at,'status',NEW.status,'deleted_at',NEW.deleted_at);
 WHEN 'payment_audit_logs' THEN
   IF NEW.action<>'SUBSCRIPTION_ASSIGNED' THEN RETURN NEW; END IF;
   SELECT o.id,o.user_id,o.subscription_group_id,s.id INTO STRICT oid,payer,gid,sid
   FROM payment_orders o JOIN user_subscriptions s ON s.user_id=o.user_id AND s.group_id=o.subscription_group_id
   WHERE o.id::text=NEW.order_id AND s.deleted_at IS NULL;
   k:='plan_assignment';
 END CASE;
 INSERT INTO payment_entitlement_events(command_id,kind,source_id,payer_id,order_id,amount,group_id,detail,transaction_id)
 VALUES(coalesce(payment_current_command(),gen_random_uuid()),k,sid,payer,oid,qty,gid,facts,pg_current_xact_id());
 RETURN NEW;
END $$;
CREATE INDEX payment_order_redeem_lookup ON payment_orders(recharge_code,user_id) WHERE order_type='balance' AND paid_at IS NOT NULL;
CREATE TRIGGER payment_redeemed AFTER INSERT OR UPDATE OF status ON redeem_codes FOR EACH ROW EXECUTE FUNCTION payment_capture_source();
CREATE TRIGGER payment_promotion AFTER INSERT ON promo_code_usages FOR EACH ROW EXECUTE FUNCTION payment_capture_source();
CREATE TRIGGER payment_affiliate AFTER INSERT ON user_affiliate_ledger FOR EACH ROW EXECUTE FUNCTION payment_capture_source();
CREATE TRIGGER payment_subscription_created AFTER INSERT ON user_subscriptions FOR EACH ROW EXECUTE FUNCTION payment_capture_source();
CREATE TRIGGER payment_subscription_changed AFTER UPDATE OF starts_at,expires_at,status,deleted_at ON user_subscriptions
 FOR EACH ROW WHEN ((OLD.starts_at,OLD.expires_at,OLD.status,OLD.deleted_at) IS DISTINCT FROM
 (NEW.starts_at,NEW.expires_at,NEW.status,NEW.deleted_at)) EXECUTE FUNCTION payment_capture_source();
CREATE TRIGGER payment_plan_assigned AFTER INSERT ON payment_audit_logs FOR EACH ROW WHEN (NEW.action='SUBSCRIPTION_ASSIGNED') EXECUTE FUNCTION payment_capture_source();

CREATE FUNCTION payment_link_wallet() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE eid bigint; delta numeric; movements integer;
BEGIN
 IF NEW.transaction_id IS NULL OR NEW.kind NOT IN ('redeem','affiliate_transfer','promotion')
    OR (NEW.kind='redeem' AND NEW.detail->>'type'<>'balance') THEN RETURN NULL; END IF;
 -- One source and one movement in a payer transaction constitute proof.
 IF (SELECT count(*) FROM payment_entitlement_events WHERE transaction_id=NEW.transaction_id
  AND payer_id=NEW.payer_id AND (kind IN ('affiliate_transfer','promotion') OR (kind='redeem' AND detail->>'type'='balance')))<>1 THEN RETURN NULL; END IF;
 SELECT count(*),min(e.id),min(e.amount) INTO movements,eid,delta FROM billing_wallet_events e
 JOIN billing_funding_accounts f ON f.id=e.funding_account_id AND f.user_id=NEW.payer_id AND f.currency='USD'
 WHERE e.transaction_id=NEW.transaction_id AND e.kind='balance_change';
 IF movements=1 THEN
   IF NEW.amount>0 AND delta<>NEW.amount THEN RAISE EXCEPTION 'payment wallet movement differs from source'; END IF;
   INSERT INTO payment_wallet_links VALUES(NEW.id,eid);
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER payment_link_at_commit AFTER INSERT ON payment_entitlement_events
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION payment_link_wallet();

CREATE TABLE payment_adjustment_links (
 entitlement_event_id bigint NOT NULL REFERENCES payment_entitlement_events,
 adjustment_command_id uuid NOT NULL REFERENCES billing_adjustment_effects(id),
 actor_id bigint NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(entitlement_event_id,adjustment_command_id)
);

DO $$ DECLARE tbl text; BEGIN
 FOREACH tbl IN ARRAY ARRAY['payment_order_terms','payment_order_history','payment_trade_claims','payment_historical_trades',
 'payment_provider_events','payment_entitlement_events','payment_wallet_links','payment_reversals','payment_commands','payment_adjustment_links'] LOOP
 EXECUTE format('CREATE TRIGGER payment_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change()',tbl);
 END LOOP;
END $$;

-- Dispatches all BE-19 monetary commands to the existing locked payer row.
-- BE-11 alone emits signed wallet postings, including for legacy callers.
CREATE TABLE payment_command_results (
 command_id uuid PRIMARY KEY REFERENCES payment_commands(id),
 balance_before numeric(20,8) NOT NULL, balance_after numeric(20,8) NOT NULL
);
CREATE TRIGGER payment_command_results_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON payment_command_results
 FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change();

CREATE FUNCTION payment_write_balance(payer bigint, operation text, qty numeric)
RETURNS numeric LANGUAGE plpgsql AS $$
DECLARE result numeric; previous numeric;
BEGIN
 IF qty IS NULL OR qty<>trunc(qty,8) OR abs(qty)>=1000000000000 OR
    operation IS NULL OR operation NOT IN ('redeem','transfer','promotion','reversal','set','adjust') OR
    (operation IN ('transfer','promotion','reversal','set') AND qty<0) THEN
   RAISE EXCEPTION 'invalid exact wallet command';
 END IF;
 SELECT balance INTO STRICT previous FROM users WHERE id=payer AND deleted_at IS NULL FOR NO KEY UPDATE;
 UPDATE users SET balance=CASE operation
  WHEN 'set' THEN qty
  WHEN 'reversal' THEN balance-least(greatest(balance,0),qty)
  WHEN 'redeem' THEN CASE WHEN qty<0 THEN greatest(balance+qty,0) ELSE balance+qty END
  ELSE balance+qty END,
  total_recharged=total_recharged+CASE WHEN operation IN ('redeem','transfer','promotion') THEN greatest(qty,0) ELSE 0 END,
  updated_at=now() WHERE id=payer AND deleted_at IS NULL AND (operation<>'adjust' OR balance+qty>=0)
 RETURNING balance INTO STRICT result;
 INSERT INTO payment_command_results(command_id,balance_before,balance_after)
 SELECT id,previous,result FROM payment_commands WHERE id=payment_current_command() AND payer_id=payer;
 RETURN result;
END $$;

-- A command reference for every entry, including old binary writes and
-- historical openings. Historical tokens identify imported entries only.
CREATE TABLE payment_wallet_command_ids (
 wallet_event_id bigint PRIMARY KEY REFERENCES billing_wallet_events, command_id uuid NOT NULL,
 historical boolean NOT NULL DEFAULT false
);
LOCK TABLE billing_wallet_events IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO payment_wallet_command_ids SELECT id,gen_random_uuid(),true FROM billing_wallet_events;
CREATE FUNCTION payment_capture_wallet_command() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO payment_wallet_command_ids(wallet_event_id,command_id)
 VALUES(NEW.id,coalesce(payment_current_command(),gen_random_uuid()));
 RETURN NEW;
END $$;
CREATE TRIGGER payment_command_captured AFTER INSERT ON billing_wallet_events
 FOR EACH ROW EXECUTE FUNCTION payment_capture_wallet_command();
CREATE TRIGGER payment_wallet_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON payment_wallet_command_ids
 FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change();

CREATE VIEW payment_wallet_evidence AS
 SELECT e.id AS wallet_event_id,f.user_id AS payer_id,e.currency,e.amount,e.kind,e.posted_at,
 coalesce(a.id,s.command_id,c.command_id) AS command_id,s.id AS entitlement_event_id,
 coalesce(a.original_event_id,r.original_wallet_event_id) AS original_wallet_event_id,
 a.event_id AS adjustment_event_id,a.id AS adjustment_command_id,m.receipt_id
 FROM billing_wallet_events e JOIN payment_wallet_command_ids c ON c.wallet_event_id=e.id
 JOIN billing_funding_accounts f ON f.id=e.funding_account_id
 LEFT JOIN payment_wallet_links l ON l.wallet_event_id=e.id
 LEFT JOIN payment_entitlement_events s ON s.id=l.entitlement_event_id
 LEFT JOIN payment_reversals r ON r.wallet_event_id=e.id
 LEFT JOIN billing_adjustment_effects a ON a.event_id=e.id
 LEFT JOIN billing_movement_links m ON m.event_id=e.id;

-- The legacy transfer scans the locked quota as double precision before
-- clearing it. Freeze the exact claim while OLD.aff_quota is still available.
CREATE FUNCTION payment_capture_affiliate_claim() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO payment_entitlement_events(command_id,kind,source_id,payer_id,amount,transaction_id)
 VALUES(coalesce(payment_current_command(),gen_random_uuid()),'affiliate_claim',OLD.user_id,OLD.user_id,OLD.aff_quota,pg_current_xact_id());
 RETURN NEW;
END $$;
CREATE TRIGGER payment_affiliate_claim AFTER UPDATE OF aff_quota ON user_affiliates
 FOR EACH ROW WHEN (OLD.aff_quota>0 AND NEW.aff_quota=0) EXECUTE FUNCTION payment_capture_affiliate_claim();

-- Restore a proven source amount before BE-11 observes the balance movement.
-- A transaction with multiple candidate sources retains explicit attribution.
CREATE FUNCTION payment_restore_exact_credit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE qty numeric; candidates integer;
BEGIN
 SELECT count(*),min(s.amount) INTO candidates,qty FROM payment_entitlement_events s
 WHERE s.payer_id=NEW.id AND s.transaction_id=pg_current_xact_id()
 AND (s.kind='affiliate_claim' OR (s.kind='redeem' AND s.detail->>'type'='balance')) AND s.amount>0;
 IF candidates=1 AND NEW.balance<>OLD.balance+qty
   AND (NEW.balance-OLD.balance)::double precision=qty::double precision
   AND (NEW.total_recharged-OLD.total_recharged)::double precision=qty::double precision
   AND NOT EXISTS(SELECT 1 FROM billing_wallet_events e JOIN billing_funding_accounts f ON f.id=e.funding_account_id
    WHERE f.user_id=NEW.id AND e.kind='balance_change' AND e.transaction_id=pg_current_xact_id())
 THEN
   NEW.balance:=OLD.balance+qty; NEW.total_recharged:=OLD.total_recharged+qty;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER payment_restore_credit BEFORE UPDATE OF balance,total_recharged ON users
 FOR EACH ROW WHEN (NEW.total_recharged>OLD.total_recharged) EXECUTE FUNCTION payment_restore_exact_credit();

CREATE FUNCTION payment_complete_transfer_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE claimed numeric;
BEGIN
 SELECT CASE WHEN count(*)=1 THEN min(amount) END INTO claimed FROM payment_entitlement_events
 WHERE kind='affiliate_claim' AND payer_id=NEW.user_id AND transaction_id=pg_current_xact_id();
 IF claimed IS NOT NULL THEN
  IF NEW.amount::double precision<>claimed::double precision THEN RAISE EXCEPTION 'affiliate claim amount mismatch'; END IF;
  NEW.amount:=claimed;
  SELECT u.balance,a.aff_quota,a.aff_frozen_quota,a.aff_history_quota
    INTO STRICT NEW.balance_after,NEW.aff_quota_after,NEW.aff_frozen_quota_after,NEW.aff_history_quota_after
    FROM users u JOIN user_affiliates a ON a.user_id=u.id WHERE u.id=NEW.user_id;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER payment_transfer_exact BEFORE INSERT ON user_affiliate_ledger
 FOR EACH ROW WHEN (NEW.action='transfer') EXECUTE FUNCTION payment_complete_transfer_snapshot();
