-- Add first-class Kilo platform identity. No data mutation.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN (
        'anthropic', 'openai', 'gemini', 'antigravity', 'grok',
        'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go',
        'cursor', 'qianwen', 'commandcode', 'qianfan', 'hunyuan',
        'mistral', 'openrouter', 'bedrock', 'volcengine', 'vertex',
        'a6api', 'aicodemirror', 'aicodewith', 'aicoding', 'aigocode',
        'aihubmix', 'amux', 'apikey_fun', 'apinebula', 'astron_coding',
        'atlascloud', 'azure_openai', 'bailing', 'byteplus', 'ccsub',
        'cherryin', 'claudeapi', 'claudecn', 'code0', 'code_9527',
        'compshare', 'compshare_coding', 'crazyrouter', 'crazyrouter_intl', 'cubence',
        'dmxapi', 'dmxapi_com', 'dmxapi_ssvip', 'eflowcode', 'fennoai',
        'fluxa', 'jiekou', 'kat_coder', 'longcat', 'micu',
        'mimo', 'mimo_token_cn', 'modelscope', 'novita', 'nvidia',
        'packycode', 'pateway', 'pipellm', 'ppio', 'qiniu',
        'qwencloud', 'qwencloud_maas_token', 'qwencloud_token', 'relaxycode', 'rightcode',
        'runapi', 'shengsuanyun', 'siliconflow_cn', 'siliconflow_global', 'soleapi',
        'soshow', 'sssaicode', 'stepfun_api', 'stepfun_api_intl', 'stepfun_plan',
        'stepfun_plan_intl', 'subrouter', 'sudocode_chat', 'sudocode_us', 'teamorouter',
        'teamorouter_com', 'therouter', 'together', 'xycai', 'zetaapi', 'github_copilot', 'nous_research', 'astron_token', 'mimo_token_sg', 'mimo_token_eu', 'kaggle', 'workbuddy', 'kilo'
    ));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN (
        'anthropic', 'openai', 'gemini', 'antigravity', 'grok',
        'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go',
        'cursor', 'qianwen', 'commandcode', 'qianfan', 'hunyuan',
        'mistral', 'openrouter', 'bedrock', 'volcengine', 'a6api',
        'aicodemirror', 'aicodewith', 'aicoding', 'aigocode', 'aihubmix',
        'amux', 'apikey_fun', 'apinebula', 'astron_coding', 'atlascloud',
        'azure_openai', 'bailing', 'byteplus', 'ccsub', 'cherryin',
        'claudeapi', 'claudecn', 'code0', 'code_9527', 'compshare',
        'compshare_coding', 'crazyrouter', 'crazyrouter_intl', 'cubence', 'dmxapi',
        'dmxapi_com', 'dmxapi_ssvip', 'eflowcode', 'fennoai', 'fluxa',
        'jiekou', 'kat_coder', 'longcat', 'micu', 'mimo',
        'mimo_token_cn', 'modelscope', 'novita', 'nvidia', 'packycode',
        'pateway', 'pipellm', 'ppio', 'qiniu', 'qwencloud',
        'qwencloud_maas_token', 'qwencloud_token', 'relaxycode', 'rightcode', 'runapi',
        'shengsuanyun', 'siliconflow_cn', 'siliconflow_global', 'soleapi', 'soshow',
        'sssaicode', 'stepfun_api', 'stepfun_api_intl', 'stepfun_plan', 'stepfun_plan_intl',
        'subrouter', 'sudocode_chat', 'sudocode_us', 'teamorouter', 'teamorouter_com',
        'therouter', 'together', 'xycai', 'zetaapi', 'github_copilot', 'nous_research', 'astron_token', 'mimo_token_sg', 'mimo_token_eu', 'kaggle', 'workbuddy', 'kilo'
    ));

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN (
        'openai', 'anthropic', 'gemini', 'grok', 'antigravity',
        'kimi', 'zhipu', 'deepseek', 'qianwen', 'commandcode',
        'minimax', 'opencode_go', 'cursor', 'volcengine', 'qianfan',
        'hunyuan', 'mistral', 'openrouter', 'bedrock', 'a6api',
        'aicodemirror', 'aicodewith', 'aicoding', 'aigocode', 'aihubmix',
        'amux', 'apikey_fun', 'apinebula', 'astron_coding', 'atlascloud',
        'azure_openai', 'bailing', 'byteplus', 'ccsub', 'cherryin',
        'claudeapi', 'claudecn', 'code0', 'code_9527', 'compshare',
        'compshare_coding', 'crazyrouter', 'crazyrouter_intl', 'cubence', 'dmxapi',
        'dmxapi_com', 'dmxapi_ssvip', 'eflowcode', 'fennoai', 'fluxa',
        'jiekou', 'kat_coder', 'longcat', 'micu', 'mimo',
        'mimo_token_cn', 'modelscope', 'novita', 'nvidia', 'packycode',
        'pateway', 'pipellm', 'ppio', 'qiniu', 'qwencloud',
        'qwencloud_maas_token', 'qwencloud_token', 'relaxycode', 'rightcode', 'runapi',
        'shengsuanyun', 'siliconflow_cn', 'siliconflow_global', 'soleapi', 'soshow',
        'sssaicode', 'stepfun_api', 'stepfun_api_intl', 'stepfun_plan', 'stepfun_plan_intl',
        'subrouter', 'sudocode_chat', 'sudocode_us', 'teamorouter', 'teamorouter_com',
        'therouter', 'together', 'xycai', 'zetaapi', 'github_copilot', 'nous_research', 'astron_token', 'mimo_token_sg', 'mimo_token_eu', 'kaggle', 'workbuddy', 'kilo'
    ));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN (
        'openai', 'anthropic', 'gemini', 'grok', 'antigravity',
        'kimi', 'zhipu', 'deepseek', 'qianwen', 'commandcode',
        'minimax', 'opencode_go', 'cursor', 'volcengine', 'qianfan',
        'hunyuan', 'mistral', 'openrouter', 'bedrock', 'a6api',
        'aicodemirror', 'aicodewith', 'aicoding', 'aigocode', 'aihubmix',
        'amux', 'apikey_fun', 'apinebula', 'astron_coding', 'atlascloud',
        'azure_openai', 'bailing', 'byteplus', 'ccsub', 'cherryin',
        'claudeapi', 'claudecn', 'code0', 'code_9527', 'compshare',
        'compshare_coding', 'crazyrouter', 'crazyrouter_intl', 'cubence', 'dmxapi',
        'dmxapi_com', 'dmxapi_ssvip', 'eflowcode', 'fennoai', 'fluxa',
        'jiekou', 'kat_coder', 'longcat', 'micu', 'mimo',
        'mimo_token_cn', 'modelscope', 'novita', 'nvidia', 'packycode',
        'pateway', 'pipellm', 'ppio', 'qiniu', 'qwencloud',
        'qwencloud_maas_token', 'qwencloud_token', 'relaxycode', 'rightcode', 'runapi',
        'shengsuanyun', 'siliconflow_cn', 'siliconflow_global', 'soleapi', 'soshow',
        'sssaicode', 'stepfun_api', 'stepfun_api_intl', 'stepfun_plan', 'stepfun_plan_intl',
        'subrouter', 'sudocode_chat', 'sudocode_us', 'teamorouter', 'teamorouter_com',
        'therouter', 'together', 'xycai', 'zetaapi', 'github_copilot', 'nous_research', 'astron_token', 'mimo_token_sg', 'mimo_token_eu', 'kaggle', 'workbuddy', 'kilo'
    ));
