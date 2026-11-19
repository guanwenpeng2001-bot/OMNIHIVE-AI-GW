package service

import (
	"context"
	"strconv"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks/media"
)

// MediaTaskBinding exists only on a server-admitted Task Center execution.
type MediaTaskBinding struct {
	Request, Model, Kind string
	Account              int64
}
type mediaTaskKey struct{}

func WithMediaTaskBinding(ctx context.Context, b MediaTaskBinding) context.Context {
	return context.WithValue(ctx, mediaTaskKey{}, b)
}
func ValidateMediaTaskAccount(ctx context.Context, account *Account, model string) error {
	b, ok := ctx.Value(mediaTaskKey{}).(MediaTaskBinding)
	if !ok {
		return nil
	}
	if account == nil || account.Platform != PlatformQianwen || account.Type != AccountTypeAPIKey || account.ID != b.Account || model != b.Model || account.GetMappedModel(model) != b.Model {
		return media.Unsupported
	}
	return nil
}

// Preserve ordinary scheduler eligibility and slot acquisition while pinning
// the paid generation to its verified endpoint. Dispatch rechecks the binding.
func (s *OpenAIGatewayService) MediaTaskExclusions(ctx context.Context, group *int64) (map[int64]struct{}, error) {
	b, ok := ctx.Value(mediaTaskKey{}).(MediaTaskBinding)
	if !ok {
		return nil, nil
	}
	accounts, err := s.listSchedulableAccounts(ctx, group, PlatformQianwen)
	if err != nil {
		return nil, err
	}
	excluded := make(map[int64]struct{}, len(accounts))
	for _, a := range accounts {
		if a.ID != b.Account {
			excluded[a.ID] = struct{}{}
		}
	}
	return excluded, nil
}

// QianwenTaskRate uses the same group/model tariff resolver as native settlement.
func (s *OpenAIGatewayService) QianwenTaskRate(ctx context.Context, k *APIKey, p media.Profile) (money.Rate, error) {
	if k == nil || k.Group == nil {
		return money.Rate{}, ErrUpstreamJobTariff
	}
	var amount float64
	per := "1"
	switch p.Kind {
	case "audio":
		cfg, err := s.qianwenNativeAudioPrice(ctx, k, p.Model)
		if err != nil || cfg == nil || cfg.TTSPerMChars == nil {
			return money.Rate{}, ErrUpstreamJobTariff
		}
		amount = *cfg.TTSPerMChars
		per = "1000000"
	case "video":
		price := LookupVideoModelPrice(k.Group.VideoModelPrices, p.Model, "720p")
		if price == nil || !finitePositive(*price) {
			return money.Rate{}, ErrUpstreamJobTariff
		}
		amount = *price
	default:
		return money.Rate{}, ErrUpstreamJobTariff
	}
	return money.NewRate("USD", strconv.FormatFloat(amount, 'f', -1, 64), money.Unit(p.Unit), per)
}
