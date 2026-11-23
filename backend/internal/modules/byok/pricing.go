package byok

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type Bounds struct{ Input, Output int64 }
type Estimate struct {
	Source          string       `json:"funding_source"`
	ExternalState   string       `json:"external_state"`
	External        *money.Money `json:"byok_external_estimate"`
	Fee             *money.Money `json:"platform_service_fee"`
	Platform        *money.Money `json:"platform_estimate"`
	PriceVersion    *string      `json:"price_version"`
	IncludeExternal bool         `json:"include_external_in_budget"`
}
type frozenPrice struct {
	At   time.Time       `json:"at"`
	Card *pricing.Frozen `json:"card"`
	Fee  *money.Money    `json:"platform_service_fee"`
}

// PriceScope is also used by the trusted BE-07 tariff publisher. Tenant-specific
// upstream prices and the wallet lane are independent immutable price lineages.
func PriceScope(w, p, account int64, lane, model string) string {
	digest := sha256.Sum256([]byte(model))
	return fmt.Sprintf("byok/v1/%d/%d/%d/%s/%x", w, p, account, lane, digest)
}

func (b Bounds) usage() (pricing.Usage, error) {
	if b.Input < 0 || b.Output < 0 {
		return pricing.Usage{}, fault.Invalid("/token_bound")
	}
	i := strconv.FormatInt(b.Input, 10)
	o := strconv.FormatInt(b.Output, 10)
	size, _ := money.NewQuantity("token", i)
	input, _ := money.NewQuantity("input_token", i)
	output, _ := money.NewQuantity("output_token", o)
	one, _ := money.NewQuantity("request", "1")
	return pricing.Usage{Context: size, Dimensions: []pricing.Dimension{{Kind: "input", Quantity: input}, {Kind: "output", Quantity: output}, {Kind: "request", Quantity: one, Optional: true}}}, nil
}

func price(ctx context.Context, tx *sql.Tx, c Credential, project, account int64, source, model string, b Bounds, at time.Time) (Estimate, frozenPrice, error) {
	estimate := Estimate{Source: source, ExternalState: "unknown", IncludeExternal: c.Budget}
	f := frozenPrice{At: at}
	if c.Workspace <= 0 || project <= 0 || account <= 0 || c.Provider == "" ||
		(source != "byok" && source != "wallet") || (source == "byok" && account != c.ID) {
		return estimate, f, unfunded
	}
	usage, e := b.usage()
	if e != nil {
		return estimate, f, e
	}
	if source == "byok" {
		fee, e := enabled(ctx, tx, c.Workspace, c.Provider, c.Auth)
		if e != nil {
			return estimate, f, e
		}
		value, e := money.NewMoney("USD", fee)
		if e != nil {
			return estimate, f, e
		}
		estimate.Fee = &value
		f.Fee = &value
	} else {
		estimate.ExternalState = "not_applicable"
		estimate.IncludeExternal = false
	}
	version, e := pricing.ResolveExactTx(ctx, tx, PriceScope(c.Workspace, project, account, source, model), at)
	if e != nil {
		return estimate, f, e
	}
	if version != nil {
		card := pricing.NewExactTariff(*version, at).Freeze(usage)
		result, e := card.Calculate()
		if e != nil {
			return estimate, f, e
		}
		if result.Charge != nil {
			if result.Charge.Amount().Compare(money.Amount{}) < 0 {
				return estimate, f, unfunded
			}
			f.Card = &card
			estimate.PriceVersion = result.PriceVersion
			if source == "byok" {
				estimate.ExternalState = "estimated"
				estimate.External = result.Charge
			} else {
				estimate.Platform = result.Charge
			}
		}
	}
	if source == "wallet" && (estimate.Platform == nil || estimate.Platform.Currency() != "USD") {
		return estimate, f, unfunded
	}
	return estimate, f, nil
}
