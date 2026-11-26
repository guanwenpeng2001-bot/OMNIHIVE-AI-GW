package service

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

const PlazaConfirmationConfigKey = "model_plaza_confirmation"

var ErrPlazaConfirmationConflict = infraerrors.Conflict("PLAZA_CONFIRMATION_CONFLICT", "Candidates changed; reload and confirm again")
var ErrPlazaConfirmationInvalid = infraerrors.BadRequest("PLAZA_CONFIRMATION_INVALID", "Select concrete candidates from the associated group")

type PlazaModelIdentity struct {
	Platform string `json:"platform"`
	Name     string `json:"name"`
}

type PlazaConfirmedModel struct {
	PlazaModelIdentity
	ConfirmedBy int64  `json:"confirmed_by,omitempty"`
	ConfirmedAt string `json:"confirmed_at,omitempty"`
}

// The existing channel owns both its group associations and display decisions.
// This metadata never participates in routing, eligibility or pricing.
type PlazaConfirmationConfig struct {
	Groups map[string][]PlazaConfirmedModel `json:"groups"`
}

type PlazaCandidateView struct {
	GroupID    int64                 `json:"group_id"`
	Revision   string                `json:"revision"`
	Candidates []PlazaModelIdentity  `json:"candidates"`
	Selected   []PlazaConfirmedModel `json:"selected"`
}

type PlazaConfirmationInput struct {
	GroupID  int64                `json:"group_id"`
	Revision string               `json:"revision"`
	Models   []PlazaModelIdentity `json:"models"`
}

type PlazaConfirmationRepository interface {
	PlazaCandidates(context.Context, int64, int64) (*PlazaCandidateView, error)
	ConfirmPlazaModels(context.Context, int64, PlazaConfirmationInput, int64) error
}

func (service *ChannelService) PlazaCandidates(ctx context.Context, channelID, groupID int64) (*PlazaCandidateView, error) {
	repository, ok := service.repo.(PlazaConfirmationRepository)
	if !ok {
		return nil, ErrPlazaConfirmationInvalid
	}
	return repository.PlazaCandidates(ctx, channelID, groupID)
}

func (service *ChannelService) ConfirmPlazaModels(ctx context.Context, channelID int64, input PlazaConfirmationInput, actorID int64) error {
	repository, ok := service.repo.(PlazaConfirmationRepository)
	if !ok || actorID <= 0 {
		return ErrPlazaConfirmationInvalid
	}
	if err := repository.ConfirmPlazaModels(ctx, channelID, input, actorID); err != nil {
		return err
	}
	service.invalidateCache()
	return nil
}

// Missing metadata means historical explicit configuration, without invented stamps.
func ChannelPlazaConfirmation(channel *Channel) PlazaConfirmationConfig {
	config := PlazaConfirmationConfig{Groups: map[string][]PlazaConfirmedModel{}}
	if raw, exists := channel.FeaturesConfig[PlazaConfirmationConfigKey]; exists {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &config)
		if config.Groups == nil {
			config.Groups = map[string][]PlazaConfirmedModel{}
		}
		return config
	}
	for _, groupID := range channel.GroupIDs {
		models := []PlazaConfirmedModel{}
		for _, model := range channel.SupportedModels() {
			models = append(models, PlazaConfirmedModel{PlazaModelIdentity: PlazaModelIdentity{Platform: model.Platform, Name: model.Name}})
		}
		config.Groups[strconv.FormatInt(groupID, 10)] = models
	}
	return config
}

func PlazaVisibleModels(channel *Channel, groupID int64) []SupportedModel {
	selected := ChannelPlazaConfirmation(channel).Groups[strconv.FormatInt(groupID, 10)]
	configured := map[PlazaModelIdentity]*ChannelModelPricing{}
	for _, model := range channel.SupportedModels() {
		configured[PlazaModelIdentity{model.Platform, model.Name}] = model.Pricing
	}
	models := make([]SupportedModel, 0, len(selected))
	for _, model := range selected {
		models = append(models, SupportedModel{Name: model.Name, Platform: model.Platform, Pricing: configured[model.PlazaModelIdentity]})
	}
	return models
}

func BuildPlazaCandidates(channel *Channel, groupID int64, groupPlatform string, accounts []Account) (*PlazaCandidateView, error) {
	associated := false
	for _, associatedID := range channel.GroupIDs {
		associated = associated || associatedID == groupID
	}
	if !associated {
		return nil, ErrPlazaConfirmationInvalid
	}
	identities := map[PlazaModelIdentity]bool{}
	add := func(platform, name string) {
		if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, "*?") {
			return
		}
		identities[PlazaModelIdentity{Platform: platform, Name: name}] = true
	}
	for _, account := range accounts {
		if groupPlatform != PlatformComposite && !account.MatchesPlatform(groupPlatform, "") {
			continue
		}
		if snapshot := account.GetUpstreamModelCatalog(); snapshot != nil && snapshot.IdentityRevision == account.ModelCatalogBaseline().IdentityRevision {
			for _, name := range snapshot.DiscoveredModels {
				add(account.Platform, name)
			}
		}
		for _, target := range configuredConcreteModelIDs(&account) {
			add(account.Platform, target)
		}
	}
	view := &PlazaCandidateView{GroupID: groupID, Candidates: []PlazaModelIdentity{}, Selected: ChannelPlazaConfirmation(channel).Groups[strconv.FormatInt(groupID, 10)]}
	if view.Selected == nil {
		view.Selected = []PlazaConfirmedModel{}
	}
	for identity := range identities {
		view.Candidates = append(view.Candidates, identity)
	}
	sort.Slice(view.Candidates, func(left, right int) bool {
		if view.Candidates[left].Platform != view.Candidates[right].Platform {
			return view.Candidates[left].Platform < view.Candidates[right].Platform
		}
		return view.Candidates[left].Name < view.Candidates[right].Name
	})
	// Include the account evidence revisions, even when membership did not change.
	type evidence struct {
		ID       int64
		Baseline AccountModelCatalogBaseline
	}
	versions := make([]evidence, 0, len(accounts))
	for _, account := range accounts {
		versions = append(versions, evidence{account.ID, account.ModelCatalogBaseline()})
	}
	sort.Slice(versions, func(left, right int) bool { return versions[left].ID < versions[right].ID })
	encoded, err := json.Marshal([]any{channel.ID, channel.UpdatedAt, groupID, groupPlatform, versions})
	if err != nil {
		return nil, err
	}
	view.Revision = string(encoded)
	return view, nil
}

func ApplyPlazaConfirmation(channel *Channel, view *PlazaCandidateView, input PlazaConfirmationInput, actorID int64, now time.Time) error {
	if view.Revision != input.Revision {
		return ErrPlazaConfirmationConflict
	}
	if actorID <= 0 || input.GroupID != view.GroupID {
		return ErrPlazaConfirmationInvalid
	}
	allowed := map[PlazaModelIdentity]bool{}
	for _, identity := range view.Candidates {
		allowed[identity] = true
	}
	previous := map[PlazaModelIdentity]PlazaConfirmedModel{}
	for _, model := range view.Selected {
		previous[model.PlazaModelIdentity] = model
	}
	selected := make([]PlazaConfirmedModel, 0, len(input.Models))
	seen := map[PlazaModelIdentity]bool{}
	for _, identity := range input.Models {
		if seen[identity] {
			return ErrPlazaConfirmationInvalid
		}
		seen[identity] = true
		if !allowed[identity] {
			// Existing historical selections may remain, but are never re-stamped without evidence.
			if old, exists := previous[identity]; exists {
				selected = append(selected, old)
				continue
			}
			return ErrPlazaConfirmationInvalid
		}
		selected = append(selected, PlazaConfirmedModel{identity, actorID, now.UTC().Format(time.RFC3339Nano)})
	}
	config := ChannelPlazaConfirmation(channel)
	config.Groups[strconv.FormatInt(input.GroupID, 10)] = selected
	if channel.FeaturesConfig == nil {
		channel.FeaturesConfig = map[string]any{}
	}
	channel.FeaturesConfig[PlazaConfirmationConfigKey] = config
	return nil
}
