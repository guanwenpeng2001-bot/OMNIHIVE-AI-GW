//go:build unit

package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// gatewayResourceMemoryRepo extends the shared in-memory job repository with the
// resource store extension so list, in-use and ownership paths are exercised
// without the shared database.
type gatewayResourceMemoryRepo struct {
	*fileBatchMemoryRepo
	fileInUse bool
}

func newGatewayResourceMemoryRepo() *gatewayResourceMemoryRepo {
	return &gatewayResourceMemoryRepo{fileBatchMemoryRepo: &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}}
}

func (r *gatewayResourceMemoryRepo) GatewayResourceFileInUse(context.Context, string) (bool, error) {
	return r.fileInUse, nil
}

func (r *gatewayResourceMemoryRepo) ListGatewayResources(_ context.Context, platform string, user, key, group int64, family, purpose, after string, limit int) ([]*UpstreamJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]*UpstreamJob, 0, limit)
	for _, job := range r.jobs {
		if job.Platform != platform || job.UserID != user || job.APIKeyID != key || job.GroupID == nil || *job.GroupID != group || job.ResourceState.Deleted {
			continue
		}
		switch family {
		case "files":
			if job.Family != "files" {
				continue
			}
			if purpose != "" && (job.Billing.ResourceFile == nil || job.Billing.ResourceFile.Purpose != purpose) {
				continue
			}
		case "batch", "batch_output":
			if job.Family != "batch" {
				continue
			}
			if family == "batch_output" && (job.Status != "succeeded" || job.ResultRef == "" || job.ResourceState.OutputDeleted) {
				continue
			}
		default:
			continue
		}
		if after != "" && job.PublicRequestID >= after {
			continue
		}
		jobs = append(jobs, cloneFileBatchJob(job))
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].PublicRequestID > jobs[j].PublicRequestID })
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func gatewayResourceTestPricing() *ModelPricing {
	return &ModelPricing{InputPricePerToken: 0.000003, OutputPricePerToken: 0.000015, CacheReadPricePerToken: 0.0000003}
}

const gatewayResourceValidJSONLLine = `{"custom_id":"row-1","method":"POST","url":"/v1/chat/completions","body":{"model":"gpt-4o","messages":[{"role":"user","content":"secret prompt"}]}}`

func TestGatewayResourceFileAdmissionValidatesBatchInputs(t *testing.T) {
	valid := gatewayResourceValidJSONLLine + "\n" + strings.ReplaceAll(gatewayResourceValidJSONLLine, "row-1", "row-2") + "\n"
	snapshot, err := ValidateGatewayResourceFile("batch", "input.jsonl", []byte(valid))
	require.NoError(t, err)
	require.Equal(t, 2, snapshot.Lines)
	require.Equal(t, "gpt-4o", snapshot.BatchModel)
	require.Equal(t, "/v1/chat/completions", snapshot.BatchEndpoint)
	require.Len(t, snapshot.CustomIDHashes, 2)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret prompt", "prompts never reach the durable snapshot")
	require.NotContains(t, string(encoded), "row-1", "custom ids are stored hashed")

	plain, err := ValidateGatewayResourceFile("assistants", "notes.txt", []byte("not jsonl"))
	require.NoError(t, err)
	require.Empty(t, plain.Lines)

	rejections := map[string]string{
		"missing purpose":         "",
		"blank line":              gatewayResourceValidJSONLLine + "\n\n",
		"duplicate custom id":     gatewayResourceValidJSONLLine + "\n" + gatewayResourceValidJSONLLine,
		"streaming body":          `{"custom_id":"row-1","method":"POST","url":"/v1/chat/completions","body":{"model":"gpt-4o","stream":true}}`,
		"unknown endpoint":        `{"custom_id":"row-1","method":"POST","url":"/v1/unknown","body":{"model":"gpt-4o"}}`,
		"missing model":           `{"custom_id":"row-1","method":"POST","url":"/v1/chat/completions","body":{}}`,
		"mixed model":             gatewayResourceValidJSONLLine + "\n" + `{"custom_id":"row-2","method":"POST","url":"/v1/chat/completions","body":{"model":"gpt-4o-mini"}}`,
		"mixed endpoint":          gatewayResourceValidJSONLLine + "\n" + `{"custom_id":"row-2","method":"POST","url":"/v1/responses","body":{"model":"gpt-4o"}}`,
		"overlong custom id":      `{"custom_id":"` + strings.Repeat("a", 65) + `","method":"POST","url":"/v1/chat/completions","body":{"model":"gpt-4o"}}`,
		"unsupported http method": `{"custom_id":"row-1","method":"GET","url":"/v1/chat/completions","body":{"model":"gpt-4o"}}`,
	}
	for name, body := range rejections {
		t.Run(name, func(t *testing.T) {
			purpose := "batch"
			if name == "missing purpose" {
				purpose, body = "", gatewayResourceValidJSONLLine
			}
			_, err := ValidateGatewayResourceFile(purpose, "input.jsonl", []byte(body))
			require.Error(t, err)
			require.ErrorIs(t, err, ErrGatewayResourceFileInvalid)
		})
	}
}

func TestGatewayResourceLimitsAreExplicit(t *testing.T) {
	oversize := make([]byte, GatewayResourceFileLimit+1)
	for i := range oversize {
		oversize[i] = 'a'
	}
	_, err := ValidateGatewayResourceFile("assistants", "big.bin", oversize)
	require.ErrorIs(t, err, ErrGatewayResourceFileTooLarge)

	tooManyLines := strings.Repeat(gatewayResourceValidJSONLLine+"\n", 2)
	_, err = ValidateGatewayResourceFile("batch", "input.jsonl", []byte(tooManyLines))
	require.Error(t, err, "a duplicated custom id is refused rather than truncated")

	body, contentType := gatewayResourceMultipartFixture(t, "input.jsonl", []byte("0123456789"))
	_, _, _, err = ParseGatewayResourceMultipart(contentType, body, 4)
	require.ErrorIs(t, err, ErrGatewayResourceFileTooLarge, "an oversized upload is refused explicitly")

	_, _, _, err = ParseGatewayResourceMultipart("application/json", []byte("{}"), 1024)
	require.Error(t, err)

	body, contentType = gatewayResourceMultipartFixture(t, "", []byte("0123456789"))
	_, _, _, err = ParseGatewayResourceMultipart(contentType, body, 1024)
	require.Error(t, err, "a file part without a filename is refused")

	line := strings.Repeat(gatewayResourceValidJSONLLine+"\n", GatewayResourceBatchLineLimit+1)
	_, err = ValidateGatewayResourceFile("batch", "input.jsonl", []byte(line))
	require.Error(t, err, "the batch line limit is enforced before any upstream call")
}

func TestGatewayResourceBatchBillingSnapshotFreezesDiscountedTariff(t *testing.T) {
	groupID := int64(7)
	group := &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 2}
	account := openAIActionAccount()
	account.Schedulable = true
	key := &APIKey{ID: 2, UserID: 1, GroupID: &groupID, Group: group}
	admission := GatewayResourceBatchAdmission{
		Platform: PlatformOpenAI, Model: "gpt-4o", UpstreamModel: "gpt-4o", Endpoint: "/v1/chat/completions",
		PricingMode: GatewayResourcePricingModeTokens, StandardPrices: gatewayResourceTestPricing(), PriceSource: "group",
		PricingAt: time.Now(), UserGroupRateMultiplier: 2, RequestCount: 1, CustomIDHashes: []string{"hashed"},
	}
	billing, err := GatewayResourceBatchBillingSnapshot(admission, group, account, key, nil)
	require.NoError(t, err)
	require.Equal(t, PricingStatePriced, billing.PricingState)
	require.NotNil(t, billing.ResourceBatch.TokenPrices)
	require.InDelta(t, 0.0000015, billing.ResourceBatch.TokenPrices.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.0000075, billing.ResourceBatch.TokenPrices.OutputPricePerToken, 1e-12)
	require.Zero(t, billing.ResourceBatch.TokenPrices.InputPricePerTokenPriority)
	require.InDelta(t, 2.0, billing.RateMultiplier, 1e-12)
	require.Equal(t, 1.0, billing.ResourceBatch.TimeMultiplier)

	anthropicAdmission := admission
	anthropicAdmission.Platform = PlatformAnthropic
	anthropicBilling, err := GatewayResourceBatchBillingSnapshot(anthropicAdmission, group, account, key, nil)
	require.NoError(t, err)
	expected := AnthropicMessageBatchTokenPricing(*gatewayResourceTestPricing())
	require.Equal(t, expected.InputPricePerToken, anthropicBilling.ResourceBatch.TokenPrices.InputPricePerToken)
	require.Equal(t, expected.CacheReadPricePerToken, anthropicBilling.ResourceBatch.TokenPrices.CacheReadPricePerToken)

	freeBilling, err := GatewayResourceBatchBillingSnapshot(GatewayResourceBatchAdmission{
		Platform: PlatformOpenAI, Model: "omni-moderation-latest", Endpoint: "/v1/moderations",
		PricingMode: GatewayResourcePricingModeFree, RequestCount: 1, CustomIDHashes: []string{"hashed"},
	}, group, account, key, nil)
	require.NoError(t, err)
	require.Equal(t, PricingStateFree, freeBilling.PricingState)
	require.Nil(t, freeBilling.ResourceBatch.TokenPrices)

	_, err = GatewayResourceBatchBillingSnapshot(admission, nil, account, key, nil)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
}

func TestGatewayResourceOwnershipNeverLeaksAcrossTenants(t *testing.T) {
	repo := newGatewayResourceMemoryRepo()
	groupID := int64(3)
	repo.jobs["public-file"] = &UpstreamJob{
		PublicRequestID: "public-file", Platform: PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2,
		GroupID: &groupID, AccountID: 42, Status: "succeeded", UpstreamTaskID: "secret-upstream",
		Billing: UpstreamJobBilling{ResourceFile: &GatewayResourceFileSnapshot{Purpose: "batch"}},
	}
	repo.jobs["public-batch"] = &UpstreamJob{
		PublicRequestID: "public-batch", Platform: PlatformOpenAI, Family: "batch", UserID: 1, APIKeyID: 2,
		GroupID: &groupID, AccountID: 42, Status: "running", UpstreamTaskID: "secret-upstream-batch",
		Billing: UpstreamJobBilling{Model: "gpt-4o", RequestedModel: "gpt-4o", ResourceBatch: &GatewayResourceBatchSnapshot{Model: "gpt-4o", RequestCount: 1, CustomIDHashes: []string{"h"}}},
	}
	jobs := NewUpstreamJobService(repo, nil, nil)
	ctx := context.Background()

	job, err := jobs.GetGatewayResource(ctx, "public-file", 1, 2, PlatformOpenAI, "files")
	require.NoError(t, err)
	require.Equal(t, "public-file", job.PublicRequestID)

	for _, tenant := range []struct {
		name      string
		user, key int64
	}{{"other user", 9, 2}, {"other key", 1, 9}} {
		t.Run(tenant.name, func(t *testing.T) {
			_, err := jobs.GetGatewayResource(ctx, "public-file", tenant.user, tenant.key, PlatformOpenAI, "files")
			require.ErrorIs(t, err, ErrUpstreamJobNotFound)
		})
	}
	_, err = jobs.GetGatewayResource(ctx, "public-file", 1, 2, PlatformAnthropic, "files")
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	_, err = jobs.GetGatewayResource(ctx, "public-file", 1, 2, PlatformOpenAI, "batch")
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)

	otherGroup := int64(31)
	remoteCalls := 0
	_, err = jobs.ManageGatewayResource(ctx, "public-batch", PlatformOpenAI, "batch", 1, 2, otherGroup, "cancel", func(context.Context, *UpstreamJob, string) error {
		remoteCalls++
		return nil
	})
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	require.Zero(t, remoteCalls)

	_, err = jobs.WithGatewayResourceInput(ctx, "public-file", PlatformOpenAI, 9, 2, groupID, func() (*UpstreamJob, error) {
		return nil, nil
	})
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	require.NotContains(t, job.PublicRequestID, job.UpstreamTaskID)
}

func TestGatewayResourceBatchCancelStopsUpstreamAndRecordsExplicitState(t *testing.T) {
	repo := newGatewayResourceMemoryRepo()
	groupID := int64(3)
	repo.jobs["public-batch"] = &UpstreamJob{
		PublicRequestID: "public-batch", Platform: PlatformOpenAI, Family: "batch", UserID: 1, APIKeyID: 2,
		GroupID: &groupID, AccountID: 42, Status: "running", UpstreamTaskID: "batch_upstream",
		Billing: UpstreamJobBilling{ResourceBatch: &GatewayResourceBatchSnapshot{Model: "gpt-4o", RequestCount: 1, CustomIDHashes: []string{"h"}}},
	}
	jobs := NewUpstreamJobService(repo, nil, nil)
	ctx := context.Background()
	actions := make([]string, 0, 2)
	remote := func(_ context.Context, job *UpstreamJob, action string) error {
		require.Equal(t, "batch_upstream", job.UpstreamTaskID)
		actions = append(actions, action)
		return nil
	}
	cancelled, err := jobs.ManageGatewayResource(ctx, "public-batch", PlatformOpenAI, "batch", 1, 2, groupID, "cancel", remote)
	require.NoError(t, err)
	require.Equal(t, []string{"cancel"}, actions)
	require.True(t, cancelled.ResourceState.CancelRequested)
	require.Equal(t, "cancelling", cancelled.ResourceState.ProviderStatus)

	again, err := jobs.ManageGatewayResource(ctx, "public-batch", PlatformOpenAI, "batch", 1, 2, groupID, "cancel", remote)
	require.NoError(t, err)
	require.Len(t, actions, 1, "a repeated cancel never reaches the upstream twice")
	require.True(t, again.ResourceState.CancelRequested)

	repo.jobs["public-batch"] = &UpstreamJob{
		PublicRequestID: "public-batch", Platform: PlatformOpenAI, Family: "batch", UserID: 1, APIKeyID: 2,
		GroupID: &groupID, AccountID: 42, Status: "submitted",
		Billing: UpstreamJobBilling{ResourceBatch: &GatewayResourceBatchSnapshot{Model: "gpt-4o", RequestCount: 1, CustomIDHashes: []string{"h"}}},
	}
	_, err = jobs.ManageGatewayResource(ctx, "public-batch", PlatformOpenAI, "batch", 1, 2, groupID, "cancel", remote)
	require.ErrorIs(t, err, ErrUpstreamResourceConflict, "a batch without an upstream binding cannot be cancelled")
}

func TestGatewayResourceFileDeleteRespectsBatchInputUse(t *testing.T) {
	repo := newGatewayResourceMemoryRepo()
	groupID := int64(3)
	repo.jobs["public-file"] = &UpstreamJob{
		PublicRequestID: "public-file", Platform: PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2,
		GroupID: &groupID, AccountID: 42, Status: "succeeded", UpstreamTaskID: "file_upstream",
		Billing: UpstreamJobBilling{ResourceFile: &GatewayResourceFileSnapshot{Purpose: "batch"}},
	}
	jobs := NewUpstreamJobService(repo, nil, nil)
	ctx := context.Background()
	remoteCalls := 0
	remote := func(context.Context, *UpstreamJob, string) error {
		remoteCalls++
		return nil
	}
	repo.fileInUse = true
	_, err := jobs.ManageGatewayResource(ctx, "public-file", PlatformOpenAI, "files", 1, 2, groupID, "delete", remote)
	require.ErrorIs(t, err, ErrUpstreamResourceConflict)
	require.Zero(t, remoteCalls, "an in-use input file never reaches the upstream delete")

	repo.fileInUse = false
	deleted, err := jobs.ManageGatewayResource(ctx, "public-file", PlatformOpenAI, "files", 1, 2, groupID, "delete", remote)
	require.NoError(t, err)
	require.True(t, deleted.ResourceState.Deleted)
	require.Equal(t, 1, remoteCalls)
}

func TestGatewayResourceChargePricesOnlyVerifiableUsage(t *testing.T) {
	groupID := int64(3)
	prices := GatewayResourceBatchTokenPricing(*gatewayResourceTestPricing())
	job := &UpstreamJob{
		PublicRequestID: "public-batch", RequestFingerprint: "fingerprint", Platform: PlatformOpenAI, Family: "batch",
		UserID: 1, APIKeyID: 2, GroupID: &groupID, AccountID: 42, Status: "running",
		Billing: UpstreamJobBilling{
			Model: "gpt-4o", RequestedModel: "gpt-4o", RateMultiplier: 2, AccountRateMultiplier: 1,
			ResourceBatch: &GatewayResourceBatchSnapshot{
				Model: "gpt-4o", UpstreamModel: "gpt-4o", Endpoint: "/v1/chat/completions", PricingMode: GatewayResourcePricingModeTokens,
				TokenPrices: &prices, PriceSource: "group", RequestCount: 2, CustomIDHashes: []string{"a", "b"},
			},
		},
	}
	usage, err := GatewayResourceBatchUsageJSON(GatewayResourceBatchUsage{
		RequestCounts: map[string]int64{"total": 2, "completed": 2, "failed": 0}, ProviderStatus: "completed",
		Lines: 2, Matched: 2, InputTokens: 1000, OutputTokens: 500, CachedTokens: 0,
	})
	require.NoError(t, err)
	job.UsageJSON = usage

	log, command, err := GatewayResourceJobCharge(job)
	require.NoError(t, err)
	expectedTotal := QuantizeUsageBillingAmount(1000*0.0000015 + 500*0.0000075)
	require.InDelta(t, expectedTotal, log.TotalCost, 1e-12)
	require.InDelta(t, expectedTotal*2, log.ActualCost, 1e-12)
	require.InDelta(t, expectedTotal*2, command.BalanceCost, 1e-12)
	require.Equal(t, PricingStatePriced, log.PricingState)
	require.NotEmpty(t, log.PricingSnapshot, "the receipt freezes the effective rates")

	uncharged := *job
	uncharged.UsageJSON = mustGatewayResourceUsage(t, GatewayResourceBatchUsage{
		RequestCounts: map[string]int64{"total": 2, "completed": 1, "failed": 0}, ProviderStatus: "cancelled",
		Lines: 1, Matched: 1, InputTokens: 10, OutputTokens: 5,
	})
	_, _, err = GatewayResourceJobCharge(&uncharged)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage, "unaccounted provider counters are never billed")

	serverTools := *job
	serverTools.UsageJSON = mustGatewayResourceUsage(t, GatewayResourceBatchUsage{
		RequestCounts: map[string]int64{"total": 2, "completed": 2, "failed": 0}, ProviderStatus: "completed",
		Lines: 2, Matched: 2, InputTokens: 10, OutputTokens: 5, ServerToolUses: 1,
	})
	_, _, err = GatewayResourceJobCharge(&serverTools)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)

	missingCounts := *job
	missingCounts.UsageJSON = mustGatewayResourceUsage(t, GatewayResourceBatchUsage{Lines: 0, Matched: 0})
	_, _, err = GatewayResourceJobCharge(&missingCounts)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)

	anthropic := *job
	anthropic.Platform = PlatformAnthropic
	anthropic.UsageJSON = mustGatewayResourceUsage(t, GatewayResourceBatchUsage{
		RequestCounts:  map[string]int64{"processing": 0, "succeeded": 2, "errored": 0, "canceled": 0, "expired": 0},
		ProviderStatus: "ended", Lines: 2, Matched: 2, InputTokens: 1000, OutputTokens: 500,
	})
	_, _, err = GatewayResourceJobCharge(&anthropic)
	require.NoError(t, err, "an ended message batch with complete counts settles")

	partial := *job
	partial.UsageJSON = mustGatewayResourceUsage(t, GatewayResourceBatchUsage{
		RequestCounts:  map[string]int64{"processing": 1, "succeeded": 1, "errored": 0, "canceled": 0, "expired": 0},
		ProviderStatus: "ended", Lines: 1, Matched: 1, InputTokens: 10, OutputTokens: 5,
	})
	_, _, err = GatewayResourceJobCharge(&partial)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
}

func TestGatewayResourceFilesSettleAsExplicitlyFreeJobs(t *testing.T) {
	groupID := int64(3)
	job := &UpstreamJob{
		PublicRequestID: "public-file", RequestFingerprint: "fingerprint", Platform: PlatformOpenAI, Family: "files",
		UserID: 1, APIKeyID: 2, GroupID: &groupID, AccountID: 42,
		Billing: UpstreamJobBilling{PricingState: PricingStateFree, RateMultiplier: 1, AccountRateMultiplier: 1,
			ResourceFile: &GatewayResourceFileSnapshot{Purpose: "batch"}},
	}
	log, command, err := GatewayResourceJobCharge(job)
	require.NoError(t, err)
	require.Zero(t, log.ActualCost)
	require.Zero(t, command.BalanceCost)
	require.Equal(t, PricingStateFree, log.PricingState)

	withoutSnapshot := *job
	withoutSnapshot.Billing.ResourceFile = nil
	_, _, err = GatewayResourceJobCharge(&withoutSnapshot)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
}

func TestGatewayResourceListScopesToOwnerAndPlatform(t *testing.T) {
	repo := newGatewayResourceMemoryRepo()
	groupID := int64(3)
	repo.jobs["a-file"] = &UpstreamJob{PublicRequestID: "a-file", Platform: PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2, GroupID: &groupID, Status: "succeeded", Billing: UpstreamJobBilling{ResourceFile: &GatewayResourceFileSnapshot{Purpose: "batch"}}}
	repo.jobs["b-file"] = &UpstreamJob{PublicRequestID: "b-file", Platform: PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2, GroupID: &groupID, Status: "succeeded", Billing: UpstreamJobBilling{ResourceFile: &GatewayResourceFileSnapshot{Purpose: "assistants"}}}
	repo.jobs["c-batch"] = &UpstreamJob{PublicRequestID: "c-batch", Platform: PlatformOpenAI, Family: "batch", UserID: 1, APIKeyID: 2, GroupID: &groupID, Status: "succeeded", ResultRef: "output-id", Billing: UpstreamJobBilling{Model: "gpt-4o", RequestedModel: "gpt-4o"}}
	repo.jobs["d-other"] = &UpstreamJob{PublicRequestID: "d-other", Platform: PlatformQianwen, Family: "files", UserID: 1, APIKeyID: 2, GroupID: &groupID, Status: "succeeded"}
	jobs := NewUpstreamJobService(repo, nil, nil)
	ctx := context.Background()

	all, err := jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "files", "", "", 20)
	require.NoError(t, err)
	require.Len(t, all, 2)

	batches, err := jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "files", "batch", "", 20)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.Equal(t, "a-file", batches[0].PublicRequestID)

	outputs, err := jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "batch_output", "", "", 20)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	require.Equal(t, "c-batch", outputs[0].PublicRequestID)

	_, err = jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "files", "unknown", "", 20)
	require.ErrorIs(t, err, ErrUpstreamResourceConflict)
	_, err = jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "files", "", "", 0)
	require.ErrorIs(t, err, ErrUpstreamResourceConflict)
}

func TestGatewayResourceSettleReusesTheSharedJobService(t *testing.T) {
	repo := newGatewayResourceMemoryRepo()
	groupID := int64(3)
	prices := GatewayResourceBatchTokenPricing(*gatewayResourceTestPricing())
	settled := GatewayResourceBatchUsage{
		RequestCounts: map[string]int64{"total": 1, "completed": 1, "failed": 0}, ProviderStatus: "completed",
		Lines: 1, Matched: 1, InputTokens: 2000, OutputTokens: 1000,
	}
	encoded, err := GatewayResourceBatchUsageJSON(settled)
	require.NoError(t, err)
	poll := func(_ context.Context, job *UpstreamJob) (UpstreamJobUpdate, error) {
		return UpstreamJobUpdate{TaskID: job.UpstreamTaskID, Status: "succeeded", ProviderStatus: "completed", Usage: encoded}, nil
	}
	jobs := NewUpstreamJobService(repo, poll, GatewayResourceJobCharge)
	ctx := context.Background()
	template := &UpstreamJob{
		Platform: PlatformOpenAI, Family: "batch", AccountID: 42, UserID: 1, APIKeyID: 2, GroupID: &groupID,
		Billing: UpstreamJobBilling{Model: "gpt-4o", RequestedModel: "gpt-4o", RateMultiplier: 1, AccountRateMultiplier: 1,
			ResourceBatch: &GatewayResourceBatchSnapshot{Model: "gpt-4o", UpstreamModel: "gpt-4o", Endpoint: "/v1/chat/completions",
				PricingMode: GatewayResourcePricingModeTokens, TokenPrices: &prices, RequestCount: 1, CustomIDHashes: []string{"a"}}},
	}
	submissions := 0
	submit := func(context.Context) (UpstreamJobUpdate, error) {
		submissions++
		return UpstreamJobUpdate{TaskID: "batch_upstream", Status: "running", ProviderStatus: "validating"}, nil
	}
	first, err := jobs.Submit(ctx, template, []byte(`{"input_file_id":"public-file","endpoint":"/v1/chat/completions","completion_window":"24h"}`), submit)
	require.NoError(t, err)
	require.Equal(t, "running", first.Status)
	second, err := jobs.Submit(ctx, template, []byte(`{"input_file_id":"public-file","endpoint":"/v1/chat/completions","completion_window":"24h"}`), submit)
	require.NoError(t, err)
	require.Equal(t, first.PublicRequestID, second.PublicRequestID)
	require.Equal(t, 1, submissions, "a retried batch create is served from the durable claim")

	jobs.now = func() time.Time { return time.Now().Add(time.Minute) }
	settledJob, err := jobs.Poll(ctx, first.PublicRequestID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", settledJob.Status)
	require.NotNil(t, settledJob.ChargedUsageLogID)
	require.Equal(t, 1, repo.charges)

	_, err = jobs.Poll(ctx, first.PublicRequestID)
	require.NoError(t, err)
	require.Equal(t, 1, repo.charges, "a settled job is never charged twice")

	otherTenant, err := repo.Get(ctx, first.PublicRequestID, 9, 9)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	require.Nil(t, otherTenant)
}

func TestGatewayResourceBatchUpstreamBodyKeepsProviderFileIdentity(t *testing.T) {
	body := []byte(`{"input_file_id":"public-file","endpoint":"/v1/chat/completions","completion_window":"24h","metadata":{"team":"research"}}`)
	rewritten, err := GatewayResourceBatchUpstreamBody(body, "file_upstream")
	require.NoError(t, err)
	require.JSONEq(t, `{"input_file_id":"file_upstream","endpoint":"/v1/chat/completions","completion_window":"24h","metadata":{"team":"research"}}`, string(rewritten))
	require.Contains(t, string(body), "public-file", "the client body is never rewritten in place")

	unchanged, err := GatewayResourceBatchUpstreamBody(rewritten, "file_upstream")
	require.NoError(t, err)
	require.JSONEq(t, string(rewritten), string(unchanged))

	_, err = GatewayResourceBatchUpstreamBody(body, "")
	require.ErrorIs(t, err, ErrUpstreamResourceConflict, "a file without an upstream binding never becomes a batch input")
	_, err = GatewayResourceBatchUpstreamBody([]byte("not json"), "file_upstream")
	require.Error(t, err)
}

func TestGatewayResourceListCursorIsOwnerScoped(t *testing.T) {
	repo := newGatewayResourceMemoryRepo()
	groupID := int64(3)
	repo.jobs["a-file"] = &UpstreamJob{PublicRequestID: "a-file", Platform: PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2, GroupID: &groupID, Status: "succeeded", Billing: UpstreamJobBilling{ResourceFile: &GatewayResourceFileSnapshot{Purpose: "batch"}}}
	repo.jobs["b-file"] = &UpstreamJob{PublicRequestID: "b-file", Platform: PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2, GroupID: &groupID, Status: "succeeded", Billing: UpstreamJobBilling{ResourceFile: &GatewayResourceFileSnapshot{Purpose: "batch"}}}
	repo.jobs["c-batch"] = &UpstreamJob{PublicRequestID: "c-batch", Platform: PlatformOpenAI, Family: "batch", UserID: 1, APIKeyID: 2, GroupID: &groupID, Status: "succeeded", ResultRef: "output-id", Billing: UpstreamJobBilling{Model: "gpt-4o", RequestedModel: "gpt-4o"}}
	repo.jobs["d-foreign"] = &UpstreamJob{PublicRequestID: "d-foreign", Platform: PlatformOpenAI, Family: "files", UserID: 9, APIKeyID: 9, GroupID: &groupID, Status: "succeeded", Billing: UpstreamJobBilling{ResourceFile: &GatewayResourceFileSnapshot{Purpose: "batch"}}}
	jobs := NewUpstreamJobService(repo, nil, nil)
	ctx := context.Background()

	page, err := jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "files", "batch", "b-file", 20)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, "a-file", page[0].PublicRequestID)

	// A batch output is addressed by its virtual id, so the cursor resolves
	// through the owning batch row.
	outputs, err := jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "batch_output", "", GatewayResourceOutputPrefix+"c-batch", 20)
	require.NoError(t, err)
	require.Empty(t, outputs)

	for _, cursor := range []string{"d-foreign", "missing-file"} {
		t.Run(cursor, func(t *testing.T) {
			_, err := jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "files", "", cursor, 20)
			require.ErrorIs(t, err, ErrGatewayResourcePagination, "an unknown or foreign cursor is reported, not silently empty")
		})
	}
	_, err = jobs.ListGatewayResources(ctx, PlatformOpenAI, 1, 2, groupID, "files", "", "c-batch", 20)
	require.ErrorIs(t, err, ErrGatewayResourcePagination, "a batch id is not a file cursor")
}

func mustGatewayResourceUsage(t *testing.T, usage GatewayResourceBatchUsage) []byte {
	t.Helper()
	encoded, err := GatewayResourceBatchUsageJSON(usage)
	require.NoError(t, err)
	return encoded
}
