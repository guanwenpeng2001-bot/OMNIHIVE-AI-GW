package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/setting"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type settingRepository struct {
	client *ent.Client
}

func NewSettingRepository(client *ent.Client) service.SettingRepository {
	return &settingRepository{client: client}
}

func (r *settingRepository) Get(ctx context.Context, key string) (*service.Setting, error) {
	m, err := r.client.Setting.Query().Where(setting.KeyEQ(key)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, service.ErrSettingNotFound
		}
		return nil, err
	}
	return &service.Setting{
		ID:        m.ID,
		Key:       m.Key,
		Value:     m.Value,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

func (r *settingRepository) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *settingRepository) Set(ctx context.Context, key, value string) error {
	now := time.Now()
	return r.client.Setting.
		Create().
		SetKey(key).
		SetValue(value).
		SetUpdatedAt(now).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func (r *settingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	settings, err := r.client.Setting.Query().Where(setting.KeyIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) SetMultiple(ctx context.Context, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}

	now := time.Now()
	builders := make([]*ent.SettingCreate, 0, len(settings))
	for key, value := range settings {
		builders = append(builders, r.client.Setting.Create().SetKey(key).SetValue(value).SetUpdatedAt(now))
	}
	return r.client.Setting.
		CreateBulk(builders...).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func (r *settingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	settings, err := r.client.Setting.Query().All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) Delete(ctx context.Context, key string) error {
	_, err := r.client.Setting.Delete().Where(setting.KeyEQ(key)).Exec(ctx)
	return err
}

func settingSnapshotVersion(rows []*ent.Setting) string {
	ordered := append([]*ent.Setting(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Key < ordered[j].Key })
	digest := sha256.New()
	for _, row := range ordered {
		// Never expose a hash of secret values. Revisions use only row identity
		// and committed update timestamps, including deletion/addition changes.
		fmt.Fprintf(digest, "%d:%s:%d\n", row.ID, row.Key, row.UpdatedAt.UnixNano())
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func (r *settingRepository) GetAllVersioned(ctx context.Context) (map[string]string, string, error) {
	rows, err := r.client.Setting.Query().All(ctx)
	if err != nil {
		return nil, "", err
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	return values, settingSnapshotVersion(rows), nil
}

func (r *settingRepository) ResetOverrides(ctx context.Context, keys []string, expectedVersion string) error {
	tx, err := r.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Setting.Query().All(ctx)
	if err != nil {
		return err
	}
	if settingSnapshotVersion(rows) != expectedVersion {
		return service.ErrSettingVersionConflict
	}
	if _, err := tx.Setting.Delete().Where(setting.KeyIn(keys...)).Exec(ctx); err != nil {
		return settingResetError(err)
	}
	return settingResetError(tx.Commit())
}

func settingResetError(err error) error {
	var state interface{ SQLState() string }
	if errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01") {
		return service.ErrSettingVersionConflict
	}
	return err
}
