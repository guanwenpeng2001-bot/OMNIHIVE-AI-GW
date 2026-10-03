package service

import "net/http"

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if account != nil {
		if err := applyCompatibleRequestAuth(account, request); err != nil {
			return nil, err
		}
	}
	if account != nil && account.ProxyGroupID != nil {
		resolved, err := account.EgressProxyURL(request.Context())
		if err != nil {
			return nil, err
		}
		proxyURL = resolved
	}
	if err := validateOpenAIForwardRequest(s.accountRepo, account, request); err != nil {
		return nil, err
	}
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	response, err := generationDo(accountHTTPUpstream(s.httpUpstream, account), request, proxyURL, account.ID, account.Concurrency)
	return compatibleCheckedResponse(account, response, resolveUpstreamResponseReadLimit(s.cfg)), err
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if account != nil && account.ProxyGroupID != nil {
		resolved, err := account.EgressProxyURL(request.Context())
		if err != nil {
			return nil, err
		}
		proxyURL = resolved
	}
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return accountHTTPUpstream(s.httpUpstream, account).DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return accountHTTPUpstream(s.httpUpstream, account).Do(request, proxyURL, account.ID, account.Concurrency)
}
