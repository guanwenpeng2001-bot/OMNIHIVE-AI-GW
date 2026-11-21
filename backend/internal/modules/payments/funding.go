package payments

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
)

// RedeemBalance consumes the bearer code and its stored USD value in one
// transaction. The raw code is excluded from command history.
func (s Store) RedeemBalance(ctx context.Context, id uuid.UUID, payer int64, code string) error {
	return s.command(ctx, id, payer, "redeem", fmt.Sprintf("%x", sha256.Sum256([]byte(code))), func(tx *sql.Tx) error {
		var amount string
		err := tx.QueryRowContext(ctx, `UPDATE redeem_codes SET status='used',used_at=now(),used_by=$2
 WHERE code=$1 AND status='unused' AND type='balance' AND (expires_at IS NULL OR expires_at>now())
 RETURNING value`, code, payer).Scan(&amount)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "SELECT payment_write_balance($1,'redeem',$2)", payer, amount)
		return err
	})
}

// TransferAffiliate thaws matured entries, claims the available award and
// credits its exact numeric value. Wallet and legacy audit snapshots agree.
func (s Store) TransferAffiliate(ctx context.Context, id uuid.UUID, payer int64) error {
	return s.command(ctx, id, payer, "affiliate_transfer", "available", func(tx *sql.Tx) error {
		var uid int64
		if err := tx.QueryRowContext(ctx, "SELECT user_id FROM user_affiliates WHERE user_id=$1 FOR UPDATE", payer).Scan(&uid); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `WITH matured AS (UPDATE user_affiliate_ledger SET frozen_until=NULL,updated_at=now()
 WHERE user_id=$1 AND frozen_until<=now() RETURNING amount), thaw AS (SELECT coalesce(sum(amount),0) a FROM matured)
 UPDATE user_affiliates SET aff_quota=aff_quota+thaw.a,aff_frozen_quota=greatest(aff_frozen_quota-thaw.a,0),updated_at=now()
 FROM thaw WHERE user_id=$1`, payer)
		if err != nil {
			return err
		}
		var amount string
		err = tx.QueryRowContext(ctx, "SELECT aff_quota FROM user_affiliates WHERE user_id=$1 AND aff_quota>0", payer).Scan(&amount)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE user_affiliates SET aff_quota=0,updated_at=now() WHERE user_id=$1", payer)
		if err != nil {
			return err
		}
		var balance string
		err = tx.QueryRowContext(ctx, "SELECT payment_write_balance($1,'transfer',$2)", payer, amount).Scan(&balance)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO user_affiliate_ledger(user_id,action,amount,balance_after,aff_quota_after,aff_frozen_quota_after,aff_history_quota_after)
 SELECT user_id,'transfer',$2,$3,aff_quota,aff_frozen_quota,aff_history_quota FROM user_affiliates WHERE user_id=$1`, payer, amount, balance)
		return err
	})
}

// ApplyPromotion enforces the same per-user uniqueness and usage cap as v1.
// The bonus is read from the locked promotion, without crossing a float boundary.
func (s Store) ApplyPromotion(ctx context.Context, id uuid.UUID, payer int64, code string) error {
	return s.command(ctx, id, payer, "promotion", fmt.Sprintf("%x", sha256.Sum256([]byte(code))), func(tx *sql.Tx) error {
		var promo int64
		var amount string
		err := tx.QueryRowContext(ctx, `UPDATE promo_codes SET used_count=used_count+1,updated_at=now()
 WHERE code=$1 AND status='active' AND (expires_at IS NULL OR expires_at>now()) AND (max_uses=0 OR used_count<max_uses)
 RETURNING id,bonus_amount`, code).Scan(&promo, &amount)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO promo_code_usages(promo_code_id,user_id,bonus_amount) VALUES($1,$2,$3)", promo, payer, amount)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "SELECT payment_write_balance($1,'promotion',$2)", payer, amount)
		return err
	})
}

// SetWallet and AdjustWallet are internal ports for the authorized admin
// mutation service. The caller retains audit, step-up and cache invalidation.
func (s Store) SetWallet(ctx context.Context, id uuid.UUID, payer int64, value money.Money) error {
	return s.adminWallet(ctx, id, payer, "set", value)
}
func (s Store) AdjustWallet(ctx context.Context, id uuid.UUID, payer int64, delta money.Money) error {
	return s.adminWallet(ctx, id, payer, "adjust", delta)
}
func (s Store) adminWallet(ctx context.Context, id uuid.UUID, payer int64, operation string, amount money.Money) error {
	if amount.Currency() != "USD" {
		return ErrMismatch
	}
	if _, err := money.Wallet20_8.Bind(amount.Amount()); err != nil {
		return err
	}
	return s.command(ctx, id, payer, operation, amount.String(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "SELECT payment_write_balance($1,$2,$3)", payer, operation, amount.String())
		return err
	})
}

// FundingResult returns the original committed before/after amounts, including
// a no-op. Legacy response adapters can project these into their existing DTO
// even if another operation has since changed the wallet.
type FundingResult struct {
	Before string
	After  string
	Delta  string
}

func (s Store) FundingResult(ctx context.Context, id uuid.UUID, payer int64) (FundingResult, error) {
	var r FundingResult
	err := s.DB.QueryRowContext(ctx, `SELECT balance_before,balance_after,balance_after-balance_before
 FROM payment_command_results r JOIN payment_commands c ON c.id=r.command_id WHERE c.id=$1 AND c.payer_id=$2`, id, payer).Scan(&r.Before, &r.After, &r.Delta)
	return r, err
}
