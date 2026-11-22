package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PriceContext is the immutable identity shared by projections, quotes and
// execution plans. Funding separates wallet sales from BYOK external estimates.
// Policy/catalog revisions constrain admission; they are not price lineages.
type PriceContext struct {
	WorkspaceID    int64     `json:"workspace_id,string,omitempty"`
	ProjectID      int64     `json:"project_id,string,omitempty"`
	ActorID        int64     `json:"actor_id,string,omitempty"`
	PayerID        int64     `json:"payer_id,string,omitempty"`
	KeyID          int64     `json:"key_id,string,omitempty"`
	GroupID        int64     `json:"group_id,string,omitempty"`
	AccountID      int64     `json:"account_id,string,omitempty"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	ModelID        string    `json:"model_id,omitempty"`
	Funding        string    `json:"funding"`
	PolicyVersion  int64     `json:"policy_version,string,omitempty"`
	CatalogVersion string    `json:"catalog_version,omitempty"`
	PricingAt      time.Time `json:"pricing_at"`
}

func (c PriceContext) Validate() error {
	if strings.TrimSpace(c.Provider) == "" || strings.TrimSpace(c.Model) == "" || c.PricingAt.IsZero() {
		return errors.New("pricing context requires provider, model and pricing instant")
	}
	switch c.Funding {
	case "wallet", "byok", "platform_fee":
		return nil
	default:
		return errors.New("invalid pricing funding lane")
	}
}

// ScopeKey never drops a provider prefix or incorporates mutable policy versions.
func (c PriceContext) ScopeKey() string {
	identity, _ := json.Marshal([2]string{c.Provider, c.Model})
	digest := sha256.Sum256(identity)
	return fmt.Sprintf("price/v1/%d/%d/%d/%d/%d/%s/%s", c.WorkspaceID, c.ProjectID,
		c.GroupID, c.AccountID, c.PayerID, c.Funding, hex.EncodeToString(digest[:]))
}

// IsReferenceSource is intentionally narrow: legacy *plaza_reference values
// described operator sales projections. pi reference prices never did.
func IsReferenceSource(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "pi-ai", "pi_ai", "reference", "community_reference":
		return true
	default:
		return false
	}
}

var ErrReferencePrice = errors.New("reference prices cannot authorize a charge")

func (v Version) ValidateExecutable() error {
	if v.Kind == "reference" || IsReferenceSource(v.Source) {
		return ErrReferencePrice
	}
	if v.Kind != "" && v.Kind != "sale" && v.Kind != "external_cost" && v.Kind != "platform_fee" {
		return errors.New("unknown price kind")
	}
	return nil
}
