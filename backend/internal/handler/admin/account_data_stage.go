package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"reflect"
	"strings"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// The legacy decoder remains permissive. Opting in makes unknown fields (in
// particular groups, proxy IDs, source keys and references) an error, not data
// silently discarded during a supposedly safe migration.
func (req *DataImportRequest) UnmarshalJSON(data []byte) error {
	type plain DataImportRequest
	staged, err := importStageMode(data)
	if err != nil {
		return err
	}
	if !staged {
		return json.Unmarshal(data, (*plain)(req))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode((*plain)(req)); err != nil {
		return errors.New("invalid staged import schema; only accounts and approved metadata are accepted")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid staged import document")
	}
	return nil
}

// Inspect security switches before ordinary decoding can overwrite a previous
// value. JSON escaping and Unicode case folding follow the JSON struct decoder.
func importStageMode(data []byte) (bool, error) {
	invalid := errors.New("invalid or ambiguous account import security switches")
	if err := rejectImportStructAliases(data, reflect.TypeOf(DataImportRequest{})); err != nil {
		return false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return false, invalid
	}
	seen := make(map[string]bool)
	staged := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return false, invalid
		}
		name, ok := key.(string)
		if !ok {
			return false, invalid
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return false, invalid
		}
		for _, field := range []string{"stage_inactive", "skip_default_group_bind", "confirm_mixed_channel_risk"} {
			if !strings.EqualFold(name, field) {
				continue
			}
			if seen[field] {
				return false, invalid
			}
			seen[field] = true
			if field == "stage_inactive" {
				switch string(bytes.TrimSpace(raw)) {
				case "true":
					staged = true
				case "false":
					staged = false
				default:
					return false, invalid
				}
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return false, invalid
	}
	return staged, nil
}

// Follow the actual request schema, rather than case-folding every JSON object.
// Struct field aliases are ambiguous at every nesting level. Map keys (notably
// case-sensitive model IDs in credentials.model_mapping) remain dictionaries.
func rejectImportStructAliases(raw []byte, schema reflect.Type) error {
	for schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	if schema.Kind() != reflect.Struct && schema.Kind() != reflect.Slice && schema.Kind() != reflect.Array {
		return nil
	}
	invalid := errors.New("ambiguous account import fields")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return invalid
	}
	if schema.Kind() == reflect.Slice || schema.Kind() == reflect.Array {
		if opening != json.Delim('[') {
			return nil
		} // ordinary decoding validates types/null
		for decoder.More() {
			var item json.RawMessage
			if decoder.Decode(&item) != nil {
				return invalid
			}
			if err := rejectImportStructAliases(item, schema.Elem()); err != nil {
				return err
			}
		}
		return nil
	}
	if opening != json.Delim('{') {
		return nil
	}
	seen := make(map[int]bool)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return invalid
		}
		name, ok := key.(string)
		if !ok {
			return invalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return invalid
		}
		for i := 0; i < schema.NumField(); i++ {
			field := schema.Field(i)
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if tag == "" {
				tag = field.Name
			}
			if !strings.EqualFold(name, tag) {
				continue
			}
			if seen[i] {
				return invalid
			}
			seen[i] = true
			if err := rejectImportStructAliases(value, field.Type); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func invalidStagedImport(message string) error {
	return infraerrors.BadRequest("INVALID_STAGED_ACCOUNT_IMPORT", message)
}

func validateStagedDataImport(req DataImportRequest) error {
	if validateDataHeader(req.Data) != nil || len(req.Data.Accounts) == 0 || len(req.Data.Accounts) > dataPageCap {
		return invalidStagedImport("staging requires 1 to 1000 accounts and an empty proxies array")
	}
	if len(req.Data.Proxies) != 0 {
		return invalidStagedImport("staging does not accept proxies or external proxy keys")
	}
	seenIdentities := make(map[[32]byte]bool)
	for _, item := range req.Data.Accounts {
		if validateDataAccount(item) != nil || item.TPMLimit.Value < 0 {
			return invalidStagedImport("invalid staged account metadata")
		}
		if item.ProxyKey != nil {
			return invalidStagedImport("staging does not accept proxy_key")
		}
		if err := service.ValidateStagedImportExtra(item.Extra); err != nil {
			return err
		}
		switch item.ProxyDependency {
		case "none", "required", "unknown":
		default:
			return invalidStagedImport("each staged account must declare proxy_dependency: none, required, or unknown")
		}
		digest, err := service.StagedAccountImportFingerprint(item.Platform, item.Type, item.Credentials)
		if err != nil {
			return err
		}
		if seenIdentities[digest] {
			return invalidStagedImport("duplicate accounts; deduplicate the migration before staging")
		}
		seenIdentities[digest] = true
	}
	return nil
}

func (h *AccountHandler) stageDataAccounts(ctx context.Context, req DataImportRequest) (DataImportResult, error) {
	if dbent.TxFromContext(ctx) == nil || !service.IsStagedAccountImport(ctx) {
		return DataImportResult{}, invalidStagedImport("staging requires the encrypted account import transaction")
	}
	result := DataImportResult{StagedInactive: true}
	for _, item := range req.Data.Accounts {
		extra := maps.Clone(item.Extra)
		if extra == nil {
			extra = make(map[string]any)
		}
		extra[service.StagedAccountImportExtraKey] = true
		extra[service.StagedProxyDependencyExtraKey] = item.ProxyDependency
		_, err := h.adminService.CreateAccount(ctx, &service.CreateAccountInput{
			Name: item.Name, Notes: item.Notes, Platform: item.Platform, Type: item.Type,
			Status: "inactive", Credentials: maps.Clone(item.Credentials),
			Extra:    extra,
			TPMLimit: item.TPMLimit.Value, Concurrency: item.Concurrency, Priority: item.Priority,
			RateMultiplier: item.RateMultiplier, ExpiresAt: item.ExpiresAt,
			AutoPauseOnExpired: item.AutoPauseOnExpired, SkipDefaultGroupBind: true,
		})
		if err != nil {
			return DataImportResult{}, err
		}
		result.AccountCreated++
	}
	// No proxy lookup/write, enrichment, privacy, probe or after-commit callback.
	return result, nil
}
