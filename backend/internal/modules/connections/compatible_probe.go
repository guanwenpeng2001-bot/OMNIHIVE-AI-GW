package connections

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"
)

const compatibleProbeTimeout = 12 * time.Second

type compatibleEndpointProber struct {
	native Prober
	policy config.URLAllowlistConfig
}

func (p compatibleEndpointProber) ProbeConnection(ctx context.Context, account *service.Account, proxy string) service.ConnectionProbe {
	base := account.GetCredential("base_url")
	key := strings.TrimSpace(account.GetCredential("api_key"))
	provider, compatible := domain.LookupCompatibleProvider(account.Platform)
	if !compatible || account.Type != service.AccountTypeAPIKey || base == "" || key == "" ||
		account.Platform == "github_copilot" || account.Platform == "nous_research" {
		return p.native.ProbeConnection(ctx, account, proxy)
	}

	ctx, cancel := context.WithTimeout(ctx, compatibleProbeTimeout)
	defer cancel()
	mode, ok := provider.ResolveMode(strings.TrimSpace(account.GetCredential("account_mode")))
	if !ok {
		return p.native.ProbeConnection(ctx, account, proxy)
	}
	failed := service.ConnectionProbe{Network: "failed", Authorization: "unknown", Discovery: "unknown", Models: []string{}}
	if !ValidEndpoint(base) {
		return failed
	}
	if mode.Discovery == "openai_models" {
		native := p.native.ProbeConnection(ctx, account, proxy)
		if native.Discovery == "passed" || native.Network == "failed" || native.Authorization == "failed" {
			return native
		}
	}

	opts := urlvalidator.ValidationOptions{RequireAllowlist: p.policy.Enabled, AllowPrivate: p.policy.AllowPrivateHosts}
	if p.policy.Enabled {
		opts.AllowedHosts = p.policy.UpstreamHosts
	}
	allowHTTP := !p.policy.Enabled && p.policy.AllowInsecureHTTP
	base, err := urlvalidator.ValidateHTTPURL(base, allowHTTP, opts)
	if err != nil {
		return failed
	}
	shared, err := httpclient.GetClient(httpclient.Options{
		Timeout: compatibleProbeTimeout, ProxyURL: proxy,
		ValidateResolvedIP: true, AllowPrivateHosts: p.policy.AllowPrivateHosts,
	})
	if err != nil {
		return failed
	}
	client := *shared
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	protocol := mode.DiscoveryProtocol
	if protocol == "" {
		protocol = strings.TrimSpace(account.GetCredential("api_protocol"))
		if protocol == "" || protocol == domain.APIProtocolAdaptive {
			protocol = mode.DefaultProtocol
		}
	}
	endpoint, ok := mode.LookupEndpoint(protocol)
	if !ok {
		return p.native.ProbeConnection(ctx, account, proxy)
	}
	if endpoint.Path != "" && !strings.HasSuffix(base, endpoint.Path) {
		base += endpoint.Path
	}
	var targets []string
	switch {
	case strings.HasSuffix(base, "/models"):
		targets = []string{base}
	case strings.HasSuffix(base, "/v1"):
		targets = []string{base + "/models"}
	default:
		targets = []string{base + "/models", base + "/v1/models"}
	}

	result := failed
	for _, target := range targets {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return result
		}
		req.Header.Set("Accept", "application/json")
		check, tryNext := probeCompatibleModels(&client, req, endpoint.Auth, key)
		if !tryNext {
			return check
		}
		if check.Network == "passed" {
			result = check
		}
	}
	return result
}

func probeCompatibleModels(client *http.Client, anon *http.Request, auth, key string) (service.ConnectionProbe, bool) {
	result := service.ConnectionProbe{Network: "failed", Authorization: "unknown", Discovery: "unknown", Models: []string{}}
	req := anon.Clone(anon.Context())
	switch auth {
	case "x_api_key":
		req.Header.Set("X-Api-Key", key)
	case "api_key_header":
		req.Header.Set("Api-Key", key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return result, false
	}
	result.Network = "passed"
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		result.Authorization = "failed"
		return result, false
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return result, true
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, false
	}
	const maxCatalogBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil || len(body) > maxCatalogBytes {
		return result, false
	}
	var catalog struct {
		Data []struct{ ID string } `json:"data"`
	}
	if json.Unmarshal(body, &catalog) != nil || catalog.Data == nil {
		return result, true
	}
	seen := map[string]bool{}
	for _, entry := range catalog.Data {
		id := strings.TrimSpace(entry.ID)
		if ValidText(id, 256) && !strings.ContainsAny(id, "?#") && !strings.Contains(id, key) && !seen[id] {
			result.Models = append(result.Models, id)
			seen[id] = true
			if len(result.Models) == 1024 {
				break
			}
		}
	}
	result.Discovery = "passed"

	// A public catalog alone does not establish that the supplied key works.
	control, err := client.Do(anon)
	if err == nil {
		control.Body.Close()
		if control.StatusCode == http.StatusUnauthorized || control.StatusCode == http.StatusForbidden {
			result.Authorization = "passed"
		}
	}
	return result, false
}
