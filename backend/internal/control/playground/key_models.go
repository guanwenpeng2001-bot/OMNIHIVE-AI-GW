package playground

import (
	"cmp"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

var selectionChanged = &fault.Fault{Status: 409, Reason: "KEY_MODEL_SELECTION_CHANGED", Message: "The selected key or catalog changed. Restart model selection."}

type keyModel struct {
	ID          string      `json:"id"`
	Model       string      `json:"model"`
	DisplayName string      `json:"display_name"`
	Provider    string      `json:"provider"`
	Pairs       []modelPair `json:"pairs"`
}

type keyModelPage struct {
	Key          int64      `json:"key_id,string"`
	Workspace    int64      `json:"workspace_id,string"`
	Project      int64      `json:"project_id,string"`
	ScopeVersion int64      `json:"scope_version,string"`
	AsOf         time.Time  `json:"as_of"`
	Next         *string    `json:"next_cursor"`
	Items        []keyModel `json:"items"`
}

type keyModelsCursor struct {
	Actor, Workspace, Project, Key, ScopeVersion int64
	CatalogVersion, Revision, After              string
	AsOf                                         time.Time
}

func newKeyModelCursorKey(secret [][]byte) []byte {
	if len(secret) > 0 && len(secret[0]) > 0 {
		mac := hmac.New(sha256.New, secret[0])
		mac.Write([]byte("omnihive.playground.key-models.v1"))
		return mac.Sum(nil)
	}
	key := make([]byte, sha256.Size)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}

func (h *Handler) keyModelMAC(body []byte) []byte {
	mac := hmac.New(sha256.New, h.keyModelCursorKey)
	mac.Write(body)
	return mac.Sum(nil)
}

func (h *Handler) sealKeyModelCursor(cursor keyModelsCursor) string {
	body, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(h.keyModelMAC(body))
}

func (h *Handler) openKeyModelCursor(token string) (keyModelsCursor, error) {
	var cursor keyModelsCursor
	if len(token) > 2048 {
		return cursor, fault.Invalid("/cursor")
	}
	payload, tag, ok := strings.Cut(token, ".")
	body, bodyErr := base64.RawURLEncoding.DecodeString(payload)
	mac, macErr := base64.RawURLEncoding.DecodeString(tag)
	if !ok || bodyErr != nil || macErr != nil || !hmac.Equal(h.keyModelMAC(body), mac) ||
		json.Unmarshal(body, &cursor) != nil || cursor.After == "" || cursor.AsOf.IsZero() {
		return cursor, fault.Invalid("/cursor")
	}
	return cursor, nil
}

func (h *Handler) listKeyModels(c *gin.Context, a identity.Access) error {
	if a.Require("keys.read") != nil || a.Require("models.read") != nil {
		return fault.Forbidden
	}
	id, err := strconv.ParseInt(c.Param("key_id"), 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != c.Param("key_id") {
		return fault.NotFound
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return fault.Invalid("/query")
	}
	for field, values := range query {
		if (field != "limit" && field != "cursor") || len(values) != 1 || values[0] == "" {
			return fault.Invalid("/" + field)
		}
	}
	limit := 25
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return fault.Invalid("/limit")
		}
	}
	ctx := c.Request.Context()
	now := time.Now().UTC()
	out := keyModelPage{Key: id, Workspace: a.WorkspaceID, Project: a.ProjectID, ScopeVersion: a.Version, AsOf: now, Items: []keyModel{}}
	key, err := h.Gateway.checkSelectedKey(ctx, a, id, 0, false)
	if err == nil && (key.models.DeniesAllKeyModels() || !slices.Contains(key.actions, "text.chat")) {
		err = keyIneligible
	}
	token := query.Get("cursor")
	var after keyModelsCursor
	if errors.Is(err, keyIneligible) {
		if token != "" {
			return selectionChanged
		}
		c.Header("Cache-Control", "private, no-store")
		success(c, 200, out)
		return nil
	}
	if err != nil {
		return err
	}
	if token != "" {
		after, err = h.openKeyModelCursor(token)
		if err != nil {
			return err
		}
		if after.Actor != a.ActorID || after.Workspace != a.WorkspaceID || after.Project != a.ProjectID ||
			after.Key != id || after.ScopeVersion != a.Version || now.Sub(after.AsOf) > catalog.SnapshotTTL || after.AsOf.After(now) {
			return selectionChanged
		}
		out.AsOf = after.AsOf
	}
	models, err := h.Gateway.selectedCatalog(ctx, a, key, out.AsOf)
	if err != nil {
		return err
	}
	items := models.keyModelItems(key, now)
	if err := h.Gateway.verifySelectionRevision(ctx, a, key); err != nil {
		return err
	}
	if !models.snapshot.AsOf.IsZero() {
		out.AsOf = models.snapshot.AsOf.UTC()
	}
	revision := catalog.Digest(struct {
		Credential string
		Policy     service.ModelAccessSnapshot
		Actions    []string
		Items      []keyModel
	}{base64.RawURLEncoding.EncodeToString(h.keyModelMAC([]byte("credential\x00" + key.digest))), key.models, key.actions, items})
	if token != "" {
		if after.CatalogVersion != models.snapshot.Version || after.Revision != revision {
			return selectionChanged
		}
		start := slices.IndexFunc(items, func(m keyModel) bool { return m.ID == after.After })
		if start < 0 {
			return selectionChanged
		}
		items = items[start+1:]
	}
	if len(items) > limit {
		items = items[:limit]
		next := h.sealKeyModelCursor(keyModelsCursor{
			Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID, Key: id, ScopeVersion: a.Version,
			CatalogVersion: models.snapshot.Version, Revision: revision, After: items[limit-1].ID, AsOf: out.AsOf,
		})
		out.Next = &next
	}
	out.Items = items
	c.Header("Cache-Control", "private, no-store")
	success(c, 200, out)
	return nil
}

func (models groupCatalog) keyModelItems(key selectedKey, now time.Time) []keyModel {
	items := []keyModel{}
	for _, model := range models.snapshot.Details {
		item := keyModel{ID: model.Model.ID, Model: model.Model.Model, DisplayName: model.Model.DisplayName, Provider: model.Model.Provider}
		for _, protocol := range []string{"anthropic_messages", "gemini_generate", "openai_chat", "openai_responses"} {
			pair := modelPair{"text.chat", protocol}
			if len(models.endpoints(key, model, pair, now, true)) != 0 {
				item.Pairs = append(item.Pairs, pair)
			}
		}
		if len(item.Pairs) != 0 {
			items = append(items, item)
		}
	}
	slices.SortFunc(items, func(a, b keyModel) int { return cmp.Compare(a.ID, b.ID) })
	return items
}
