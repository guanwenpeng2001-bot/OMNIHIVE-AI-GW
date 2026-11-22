package legacy

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
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

type authRequiredSettings struct{ service.SettingRepository }

func (authRequiredSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{
		service.SettingKeyModelPlazaEnabled:     "true",
		service.SettingKeyModelPlazaRequireAuth: "true",
	}, nil
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
		mock.ExpectQuery("INSERT INTO catalog_registry_versions").WithArgs(registry.Version() + ":" + catalog.ReferenceInventoryVersion()).WillReturnRows(sqlmock.NewRows([]string{"version"}))
		mock.ExpectRollback()
		var groups []service.Group
		if configured {
			groups = []service.Group{{ID: 10, Platform: service.PlatformOpenAI, AccountType: service.AccountTypeOAuth}}
			mock.ExpectQuery("FROM catalog_compatibility_publications").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		}
		plaza := service.NewModelPlazaService(configuredChannels{}, configuredGroups{groups: groups}, nil, nil, nil)
		source := NewSource(catalogpg.New(db), plaza, service.NewSettingService(configuredSettings{}, nil), nil, nil)
		perspective := catalog.Perspective{Workspace: 1, Audience: "public"}
		in, err := source.Load(service.WithModelRegistrySnapshot(context.Background(), registry), perspective, catalog.Window{}, now)
		require.NoError(t, err)
		details := catalog.Project(perspective, in, catalog.Window{}, now).Details
		if configured {
			require.Len(t, details, len(want))
			for _, detail := range details {
				require.True(t, want[detail.Model.Model])
				require.Equal(t, service.PlatformOpenAI, detail.Model.Provider)
				require.Equal(t, "requires_scope", detail.Model.AvailabilityState)
				require.Equal(t, "requires_scope", detail.Model.AccessState)
				require.Equal(t, "draft", detail.Model.PublicationState, "reviewed inventory alone is not an operator publication")
				require.Equal(t, "unpriced", detail.Model.PriceState)
				require.Equal(t, "requires_scope", detail.Model.RouteState)
				require.Empty(t, detail.Endpoints)
				for _, fact := range detail.Facts {
					require.Contains(t, []string{catalog.SourcePreset, catalog.SourceBuiltin}, fact.Source)
				}
			}
		} else {
			require.Empty(t, details)
		}
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestConfiguredCatalogSourceHonorsRequireAuthForAuthenticatedPublicViewer(t *testing.T) {
	now := time.Now()
	registry := service.NewModelRegistry().Snapshot()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	plaza := service.NewModelPlazaService(configuredChannels{}, configuredGroups{}, nil, nil, nil)
	source := NewSource(catalogpg.New(db), plaza, service.NewSettingService(authRequiredSettings{}, nil), nil, nil)
	ctx := service.WithModelRegistrySnapshot(context.Background(), registry)

	perspective := catalog.Perspective{Workspace: 1, Audience: "public"}
	_, err = source.Load(ctx, perspective, catalog.Window{}, now)
	require.ErrorIs(t, err, fault.Unauthenticated)

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO catalog_registry_versions").WithArgs(registry.Version() + ":" + catalog.ReferenceInventoryVersion()).WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectRollback()
	_, err = source.Load(catalog.WithPublicViewerAuthenticated(ctx), perspective, catalog.Window{}, now)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScopedBindingFactsKeepDeclarationSeparateFromDiscovery(t *testing.T) {
	now := time.Now().UTC()
	yes := true
	for _, row := range []struct{ source, rule, want string }{
		{"upstream", "native_id", "account_discovery"},
		{"account-declaration", "identity_bound_exact", "account_declaration"},
	} {
		t.Run(row.source, func(t *testing.T) {
			binding := service.PlazaCatalogBinding{Metadata: service.UpstreamModelMetadata{ID: "private", ProviderProfile: "openai", SupportsFunctionCalling: &yes,
				FieldEvidence: map[string]service.ModelCapabilityEvidence{"supports_function_calling": {Source: row.source, MatchRule: row.rule, ProviderProfile: "openai", MatchedModelID: "private"}}}}
			binding.SnapshotRevision = 7
			facts := scopedBindingFacts(binding, now.Add(time.Hour), now)
			require.Len(t, facts, 1)
			require.Equal(t, row.want, facts[0].Source)
			require.Equal(t, row.want, facts[0].Provenance["tools"].SourceKind)
			require.Equal(t, "7", facts[0].Provenance["tools"].SourceRevision)
			require.False(t, catalog.PublicEvidence(facts[0].Evidence))
		})
	}
}
