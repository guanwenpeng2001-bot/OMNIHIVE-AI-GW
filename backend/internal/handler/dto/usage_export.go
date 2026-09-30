package dto

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Usage export scope selects the column whitelist. The self scope is the owner's
// own view of the same rows: it must never expose administrator-only columns
// (account identity, account cost, upstream request identifiers, user agent).
type UsageExportScope string

const (
	UsageExportScopeAdmin UsageExportScope = "admin"
	UsageExportScopeSelf  UsageExportScope = "self"
)

// UsageExportFormat selects the wire shape of one export response.
const (
	// UsageExportFormatCSV streams rows as RFC4180 CSV with a canonical header row.
	UsageExportFormatCSV = "csv"
	// UsageExportFormatJSON returns the same rows as the list DTO payload. It exists
	// for the Excel writer, which needs typed values and client-side localized labels.
	UsageExportFormatJSON = "json"
)

// Canonical export column keys, in export order. The keys are stable ASCII
// identifiers: clients map them to localized labels and the CSV header row is
// replaced by the client before the file is written.
var (
	usageExportEvidenceColumns = []string{
		"evidence_pricing_state",
		"evidence_settlement_state",
		"evidence_completion_state",
		"evidence_source",
		"evidence_version",
		"evidence_conditions",
		"evidence_quantity",
		"evidence_unit",
		"evidence_unit_price",
		"evidence_charged",
		"evidence_request_id",
		"evidence_job_id",
		"evidence_session_id",
		"evidence_turn_id",
		"evidence_snapshot",
	}

	usageExportColumnsAdmin = append([]string{
		"time",
		"user",
		"api_key",
		"account",
		"model",
		"upstream_model",
		"upstream_response_model",
		"upstream_model_mismatch",
		"requested_reasoning_effort",
		"reasoning_effort",
		"group",
		"inbound_endpoint",
		"upstream_endpoint",
		"request_type",
		"input_tokens",
		"output_tokens",
		"cache_read_tokens",
		"cache_creation_tokens",
		"input_cost",
		"output_cost",
		"cache_read_cost",
		"cache_creation_cost",
		"rate_multiplier",
		"account_rate_multiplier",
		"total_cost",
		"actual_cost",
		"account_billed_cost",
		"first_token_ms",
		"duration_ms",
		"request_id",
		"upstream_request_id",
		"user_agent",
		"ip_address",
	}, usageExportEvidenceColumns...)

	usageExportColumnsSelf = append([]string{
		"time",
		"api_key",
		"model",
		"upstream_response_model",
		"reasoning_effort",
		"inbound_endpoint",
		"ip_address",
		"request_type",
		"billing_mode",
		"input_tokens",
		"output_tokens",
		"cache_read_tokens",
		"cache_creation_tokens",
		"rate_multiplier",
		"actual_cost",
		"total_cost",
		"first_token_ms",
		"duration_ms",
	}, usageExportEvidenceColumns...)
)

// UsageExportColumns returns the canonical export column order for one scope.
// The slice is a copy: callers cannot corrupt the shared contract.
func UsageExportColumns(scope UsageExportScope) []string {
	if scope == UsageExportScopeSelf {
		return append([]string(nil), usageExportColumnsSelf...)
	}
	return append([]string(nil), usageExportColumnsAdmin...)
}

// usageExportUnknown marks a value the export cannot prove. Unknown values are
// never reported as zero.
const usageExportUnknown = "unknown"

// BuildAdminUsageExportRow formats one admin usage record into the canonical
// admin column order. Formatting is locale independent: timestamps keep the
// requested report zone with an explicit offset, money keeps ledger precision.
func BuildAdminUsageExportRow(log *AdminUsageLog, zone string) []string {
	if log == nil {
		return nil
	}
	values := []string{
		formatUsageExportTime(log.CreatedAt, zone),
		usageExportUserName(log.User),
		usageExportAPIKeyName(log.APIKey),
		usageExportAccountName(log.Account),
		log.Model,
		usageExportFirstNonEmpty(log.UpstreamModel, &log.Model),
		usageExportString(log.UpstreamResponseModel),
		usageExportBool(log.UpstreamModelMismatch),
		formatUsageExportReasoningEffort(log.ReasoningEffort),
		formatUsageExportReasoningEffort(usageExportFirstNonEmptyPtr(log.UpstreamReasoningEffort, log.ReasoningEffort)),
		usageExportGroupName(log.Group),
		usageExportString(log.InboundEndpoint),
		usageExportString(log.UpstreamEndpoint),
		usageExportRequestType(log.UsageLog),
		strconv.Itoa(log.InputTokens),
		strconv.Itoa(log.OutputTokens),
		strconv.Itoa(log.CacheReadTokens),
		strconv.Itoa(log.CacheCreationTokens),
		formatUsageExportLedgerAmount(&log.InputCost),
		formatUsageExportLedgerAmount(&log.OutputCost),
		formatUsageExportLedgerAmount(&log.CacheReadCost),
		formatUsageExportLedgerAmount(&log.CacheCreationCost),
		strconv.FormatFloat(log.RateMultiplier, 'f', -1, 64),
		strconv.FormatFloat(usageExportAccountRate(log.AccountRateMultiplier), 'f', -1, 64),
		formatUsageExportLedgerAmount(&log.TotalCost),
		formatUsageExportLedgerAmount(&log.ActualCost),
		formatUsageExportLedgerAmount(usageExportAccountBilledCost(log)),
		usageExportInt(log.FirstTokenMs),
		usageExportInt(log.DurationMs),
		log.RequestID,
		usageExportString(log.UpstreamRequestID),
		usageExportString(log.UserAgent),
		usageExportString(log.IPAddress),
	}
	return append(values, usageExportEvidenceValues(log.BillingExplanation, log.SessionID)...)
}

// BuildSelfUsageExportRow formats one owner-visible usage record into the
// canonical self column order.
func BuildSelfUsageExportRow(log *UsageLog, zone string) []string {
	if log == nil {
		return nil
	}
	values := []string{
		formatUsageExportTime(log.CreatedAt, zone),
		usageExportAPIKeyName(log.APIKey),
		log.Model,
		usageExportOrUnknown(log.UpstreamResponseModel),
		formatUsageExportReasoningEffort(log.ReasoningEffort),
		usageExportString(log.InboundEndpoint),
		usageExportString(log.IPAddress),
		usageExportRequestType(*log),
		usageExportDisplayBillingMode(log),
		strconv.Itoa(log.InputTokens),
		strconv.Itoa(log.OutputTokens),
		strconv.Itoa(log.CacheReadTokens),
		strconv.Itoa(log.CacheCreationTokens),
		strconv.FormatFloat(log.RateMultiplier, 'f', -1, 64),
		formatUsageExportLedgerAmount(&log.ActualCost),
		formatUsageExportLedgerAmount(&log.TotalCost),
		usageExportInt(log.FirstTokenMs),
		usageExportInt(log.DurationMs),
	}
	return append(values, usageExportEvidenceValues(log.BillingExplanation, log.SessionID)...)
}

// UsageExportCSVWriter streams export rows as RFC4180 CSV. Every cell is
// sanitized against spreadsheet formula injection before it is quoted, and the
// stream starts with a UTF-8 BOM so Excel reads non-ASCII text correctly.
type UsageExportCSVWriter struct {
	writer *csv.Writer
}

// NewUsageExportCSVWriter writes the BOM and the canonical header row.
func NewUsageExportCSVWriter(w io.Writer, columns []string) (*UsageExportCSVWriter, error) {
	if _, err := io.WriteString(w, "\uFEFF"); err != nil {
		return nil, err
	}
	writer := csv.NewWriter(w)
	header := make([]string, 0, len(columns))
	for _, column := range columns {
		header = append(header, sanitizeUsageExportCell(column))
	}
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	return &UsageExportCSVWriter{writer: writer}, nil
}

// WriteRows sanitizes and writes rows, then flushes so a streaming response
// reaches the client without buffering the whole export.
func (e *UsageExportCSVWriter) WriteRows(rows [][]string) error {
	sanitized := make([]string, 0, len(rows))
	for _, row := range rows {
		sanitized = sanitized[:0]
		for _, cell := range row {
			sanitized = append(sanitized, sanitizeUsageExportCell(cell))
		}
		if err := e.writer.Write(sanitized); err != nil {
			return err
		}
	}
	e.writer.Flush()
	return e.writer.Error()
}

// Flush flushes the underlying writer.
func (e *UsageExportCSVWriter) Flush() {
	e.writer.Flush()
}

// UsageExportCSVStream writes the CSV body lazily: the header row is only
// emitted when the first batch arrives, so a failed query can still answer with
// a normal error response instead of a half written file.
type UsageExportCSVStream struct {
	target  io.Writer
	columns []string
	writer  *UsageExportCSVWriter
	started bool
}

// NewUsageExportCSVStream prepares a lazy CSV stream for one export response.
func NewUsageExportCSVStream(target io.Writer, columns []string) *UsageExportCSVStream {
	return &UsageExportCSVStream{target: target, columns: columns}
}

// Started reports whether any byte of the body has been written.
func (s *UsageExportCSVStream) Started() bool {
	return s.started
}

// WriteRows writes one batch, creating the CSV writer on first use.
func (s *UsageExportCSVStream) WriteRows(rows [][]string) error {
	if s.writer == nil {
		writer, err := NewUsageExportCSVWriter(s, s.columns)
		if err != nil {
			return err
		}
		s.writer = writer
	}
	return s.writer.WriteRows(rows)
}

// Close emits an empty (header only) export when no batch was produced.
func (s *UsageExportCSVStream) Close() error {
	if s.writer == nil {
		writer, err := NewUsageExportCSVWriter(s, s.columns)
		if err != nil {
			return err
		}
		s.writer = writer
	}
	s.writer.Flush()
	return s.writer.writer.Error()
}

// Write tracks actual output even when initializing the BOM/header only partly
// succeeds. HTTP callers must also check whether their response was committed.
func (s *UsageExportCSVStream) Write(data []byte) (int, error) {
	written, err := s.target.Write(data)
	if written > 0 {
		s.started = true
	}
	if written < len(data) && err == nil {
		err = io.ErrShortWrite
	}
	return written, err
}

// sanitizeUsageExportCell neutralizes spreadsheet formula injection. Values that
// begin with a formula trigger are prefixed with a single quote, which both
// Excel and LibreOffice treat as text.
func sanitizeUsageExportCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

// SanitizeUsageExportCell exposes the CSV injection guard for tests and for
// callers that build export cells outside this package.
func SanitizeUsageExportCell(value string) string {
	return sanitizeUsageExportCell(value)
}

// UsageExportFilename builds the stable attachment name for one export.
func UsageExportFilename(prefix, startDate, endDate, format string) string {
	extension := "csv"
	if format == UsageExportFormatJSON {
		extension = "json"
	}
	name := prefix
	if startDate != "" || endDate != "" {
		name = prefix + "_" + startDate + "_to_" + endDate
	}
	return name + "." + extension
}

func formatUsageExportTime(value time.Time, zone string) string {
	if value.IsZero() {
		return ""
	}
	location, err := time.LoadLocation(strings.TrimSpace(zone))
	if err != nil || location == nil {
		location = time.UTC
	}
	local := value.In(location)
	offset := local.Format("-07:00")
	if offset == "+00:00" {
		offset = "GMT+00:00"
	} else {
		offset = "GMT" + offset
	}
	return local.Format("2006-01-02 15:04:05") + " (" + local.Location().String() + ", " + offset + ")"
}

func formatUsageExportLedgerAmount(value *float64) string {
	if value == nil {
		return usageExportUnknown
	}
	amount := *value
	if amount != amount || amount > 1e308 || amount < -1e308 {
		return usageExportUnknown
	}
	formatted := strconv.FormatFloat(amount, 'f', 8, 64)
	if formatted == "-0.00000000" {
		return "0.00000000"
	}
	return formatted
}

func formatUsageExportReasoningEffort(effort *string) string {
	raw := strings.TrimSpace(usageExportString(effort))
	if raw == "" {
		return "-"
	}
	switch strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(raw)) {
	case "low":
		return "Low"
	case "medium":
		return "Medium"
	case "high":
		return "High"
	case "xhigh", "extrahigh":
		return "XHigh"
	case "max":
		return "Max"
	case "none", "minimal":
		return "-"
	}
	if len(raw) > 1 {
		return strings.ToUpper(raw[:1]) + raw[1:]
	}
	return strings.ToUpper(raw)
}

// usageExportRequestType keeps the request-type codes used by the panel filters
// so the exported value is stable and machine readable.
func usageExportRequestType(log UsageLog) string {
	if trimmed := strings.TrimSpace(log.RequestType); trimmed != "" && trimmed != "unknown" {
		return trimmed
	}
	if log.OpenAIWSMode {
		return "ws_v2"
	}
	if log.Stream {
		return "stream"
	}
	return "sync"
}

// usageExportDisplayBillingMode mirrors the panel rule: an explicit video/token
// mode wins, an unlabeled row with images is an image charge.
func usageExportDisplayBillingMode(log *UsageLog) string {
	mode := strings.TrimSpace(usageExportString(log.BillingMode))
	if mode == string(service.BillingModeVideo) || mode == string(service.BillingModeToken) {
		return mode
	}
	if log.ImageCount > 0 && mode == "" {
		return string(service.BillingModeImage)
	}
	return mode
}

func usageExportAccountRate(value *float64) float64 {
	if value == nil {
		return 1
	}
	return *value
}

// usageExportAccountBilledCost mirrors accountBilledAmount in the panel:
// (account_stats_cost ?? total_cost) * (account_rate_multiplier ?? 1).
func usageExportAccountBilledCost(log *AdminUsageLog) *float64 {
	base := log.AccountStatsCost
	if base == nil {
		base = &log.TotalCost
	}
	amount := *base * usageExportAccountRate(log.AccountRateMultiplier)
	return &amount
}

func usageExportEvidenceValues(explanation *UsageBillingExplanation, sessionID *string) []string {
	unknown := usageExportUnknown
	if explanation == nil {
		return []string{unknown, unknown, unknown, unknown, unknown, unknown, unknown, unknown, unknown, unknown,
			unknown, unknown, usageExportFirstNonEmpty(sessionID, nil), unknown, unknown}
	}
	state := func(value string) string {
		if strings.TrimSpace(value) == "" {
			return unknown
		}
		return value
	}
	snapshot := explanation.Snapshot
	source := explanation.PriceSource
	version := explanation.PriceCardVersion
	if snapshot != nil {
		if strings.TrimSpace(snapshot.Source) != "" {
			source = &snapshot.Source
		}
		if strings.TrimSpace(snapshot.PriceCardVersion) != "" {
			version = &snapshot.PriceCardVersion
		}
	}
	jobID := usageExportStringValue(explanation.JobID)
	turnID := usageExportStringValue(explanation.TurnID)
	resolvedSession := usageExportStringValue(explanation.SessionID)
	if resolvedSession == "" {
		resolvedSession = usageExportString(sessionID)
	}
	return []string{
		state(explanation.PricingState),
		state(explanation.SettlementState),
		state(explanation.CompletionState),
		usageExportOrUnknown(source),
		usageExportOrUnknown(version),
		usageExportConditions(snapshot),
		usageExportQuantity(snapshot),
		usageExportUnit(snapshot),
		usageExportUnitPrice(snapshot),
		usageExportCharged(snapshot),
		usageExportString(explanation.PriceSource),
		usageExportStringValue(jobID),
		usageExportStringValue(resolvedSession),
		usageExportStringValue(turnID),
		usageExportSnapshot(snapshot),
	}
}

func usageExportOrUnknown(value *string) string {
	if trimmed := strings.TrimSpace(usageExportString(value)); trimmed != "" {
		return trimmed
	}
	return usageExportUnknown
}

func usageExportConditions(snapshot *service.PricingSnapshot) string {
	if snapshot == nil {
		return usageExportUnknown
	}
	conditions := struct {
		Tier          *string                        `json:"tier"`
		Size          *string                        `json:"size"`
		Thinking      *bool                          `json:"thinking"`
		LongContext   bool                           `json:"long_context"`
		Rate          float64                        `json:"rate"`
		Time          float64                        `json:"time"`
		Reasoning     float64                        `json:"reasoning"`
		Adjustment    *float64                       `json:"adjustment"`
		FX            *service.QianwenPricingOptions `json:"fx"`
		ExplicitCache bool                           `json:"explicit_cache"`
		Batch         bool                           `json:"batch"`
	}{
		Tier:          usageExportOptionalString(snapshot.ServiceTier),
		Size:          usageExportOptionalString(snapshot.SizeTier),
		Thinking:      snapshot.ThinkingEnabled,
		LongContext:   snapshot.LongContextApplied,
		Rate:          snapshot.RateMultiplier,
		Time:          snapshot.TimeMultiplier,
		Reasoning:     snapshot.ReasoningMultiplier,
		Adjustment:    usageExportOptionalFloat(snapshot.AdjustmentMultiplier),
		FX:            snapshot.FX,
		ExplicitCache: snapshot.Tokens.QianwenExplicitCache,
		Batch:         snapshot.Tokens.QianwenBatchChat,
	}
	return usageExportJSON(conditions)
}

func usageExportQuantity(snapshot *service.PricingSnapshot) string {
	if snapshot == nil {
		return usageExportUnknown
	}
	if snapshot.TokenPrices != nil {
		return usageExportJSON(snapshot.Tokens)
	}
	if snapshot.Units == 0 {
		return usageExportUnknown
	}
	return strconv.FormatFloat(snapshot.Units, 'f', -1, 64)
}

// usageExportUnit keeps the frozen calculator contract; units are never inferred
// from cost.
func usageExportUnit(snapshot *service.PricingSnapshot) string {
	if snapshot == nil {
		return usageExportUnknown
	}
	if snapshot.TokenPrices != nil {
		return "tokens"
	}
	switch snapshot.Mode {
	case "audio":
		switch snapshot.SizeTier {
		case "tts":
			return "million_characters"
		case "stt":
			return "hours"
		}
		return usageExportUnknown
	case "realtime":
		return "characters"
	case "asr", "video":
		return "seconds"
	case "3d":
		return "models"
	case "image":
		return "images"
	case "per_request":
		if snapshot.SizeTier == "calls" || snapshot.SizeTier == "thousand_calls" {
			return snapshot.SizeTier
		}
	}
	return usageExportUnknown
}

func usageExportUnitPrice(snapshot *service.PricingSnapshot) string {
	if snapshot == nil {
		return usageExportUnknown
	}
	if snapshot.UnitPrice != nil {
		return strconv.FormatFloat(*snapshot.UnitPrice, 'f', -1, 64)
	}
	if snapshot.TokenPrices != nil {
		return usageExportJSON(snapshot.TokenPrices)
	}
	return usageExportUnknown
}

func usageExportCharged(snapshot *service.PricingSnapshot) string {
	if snapshot == nil || snapshot.ChargedAmount == 0 {
		return usageExportUnknown
	}
	return formatUsageExportLedgerAmount(&snapshot.ChargedAmount)
}

func usageExportSnapshot(snapshot *service.PricingSnapshot) string {
	if snapshot == nil {
		return usageExportUnknown
	}
	return usageExportJSON(snapshot)
}

func usageExportJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return usageExportUnknown
	}
	return string(raw)
}

func usageExportOptionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func usageExportOptionalFloat(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

func usageExportString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func usageExportStringValue(value string) string {
	return strings.TrimSpace(value)
}

func usageExportInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func usageExportBool(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}

func usageExportFirstNonEmpty(primary, fallback *string) string {
	if trimmed := strings.TrimSpace(usageExportString(primary)); trimmed != "" {
		return trimmed
	}
	return usageExportString(fallback)
}

func usageExportFirstNonEmptyPtr(primary, fallback *string) *string {
	if trimmed := strings.TrimSpace(usageExportString(primary)); trimmed != "" {
		return &trimmed
	}
	return fallback
}

func usageExportUserName(user *User) string {
	if user == nil {
		return ""
	}
	return user.Email
}

func usageExportAPIKeyName(key *APIKey) string {
	if key == nil {
		return ""
	}
	return key.Name
}

func usageExportAccountName(account *AccountSummary) string {
	if account == nil {
		return ""
	}
	return account.Name
}

func usageExportGroupName(group *Group) string {
	if group == nil {
		return ""
	}
	return group.Name
}
