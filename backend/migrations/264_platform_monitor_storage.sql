-- Converge databases that already applied the historical restrictive CHECKs.
-- Preserve concrete platform labels in storage; service capabilities still gate active probes and quota polling.
ALTER TABLE channel_monitors DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitors ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'qianwen', 'commandcode', 'minimax', 'opencode_go', 'cursor', 'volcengine', 'qianfan', 'hunyuan', 'mistral', 'bedrock'));
ALTER TABLE channel_monitor_request_templates DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
ALTER TABLE channel_monitor_request_templates ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'qianwen', 'commandcode', 'minimax', 'opencode_go', 'cursor', 'volcengine', 'qianfan', 'hunyuan', 'mistral', 'bedrock'));
