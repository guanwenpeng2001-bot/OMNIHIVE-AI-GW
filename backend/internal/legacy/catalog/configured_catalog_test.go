package legacy

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	catalogpg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type configuredGroups struct {
	service.GroupRepository
	groups []service.Group
}

func (r configuredGroups) ListActive(context.Context) ([]service.Group, error) { return r.groups, nil }

type configuredChannels struct{ service.ChannelRepository }

func (configuredChannels) ListAll(context.Context) ([]service.Channel, error) { return nil, nil }

type configuredSettings struct{ service.SettingRepository }

func (configuredSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyModelPlazaEnabled: "true"}, nil
}

func TestConfiguredCatalogSourceIncludesFullPlatformWithoutLiveAccounts(t *testing.T) {
	now := time.Now()
	registry := service.NewModelRegistry().Snapshot()
	want := map[string]bool{}
	for _, fact := range registry.CatalogFacts(now) {
		if fact.Provider == service.PlatformOpenAI {
			want[fact.Model] = true
		}
	}
	require.GreaterOrEqual(t, len(want), 119)
	for _, configured := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		mock.ExpectBegin()
		mock.ExpectQuery("INSERT INTO catalog_registry_versions").WithArgs(registry.Version()).WillReturnRows(sqlmock.NewRows([]string{"version"}))
		mock.ExpectRollback()
		var groups []service.Group
		if configured {
			groups = []service.Group{{ID: 10, Platform: service.PlatformOpenAI, AccountType: service.AccountTypeOAuth}}
			mock.ExpectQuery("FROM catalog_compatibility_publications").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		}
		plaza := service.NewModelPlazaService(configuredChannels{}, configuredGroups{groups: groups}, nil, nil, nil)
		source := NewSource(catalogpg.New(db), plaza, service.NewSettingService(configuredSettings{}, nil), nil, nil)
		in, err := source.Load(service.WithModelRegistrySnapshot(context.Background(), registry), catalog.Perspective{Workspace: 1}, catalog.Window{}, now)
		require.NoError(t, err)
		details := catalog.Project(catalog.Perspective{Workspace: 1}, in, catalog.Window{}, now).Details
		if configured {
			require.Len(t, details, len(want))
			for _, detail := range details {
				require.True(t, want[detail.Model.Model])
				require.Equal(t, service.PlatformOpenAI, detail.Model.Provider)
				require.Equal(t, "catalog_only", detail.Model.AvailabilityState)
				require.Equal(t, "unpriced", detail.Model.PriceState)
				require.Equal(t, "requires_scope", detail.Model.RouteState)
				require.Empty(t, detail.Endpoints)
				for _, fact := range detail.Facts {
					require.Equal(t, "approved_registry", fact.Source)
				}
			}
		} else {
			require.Empty(t, details)
		}
		require.NoError(t, mock.ExpectationsWereMet())
	}
}
