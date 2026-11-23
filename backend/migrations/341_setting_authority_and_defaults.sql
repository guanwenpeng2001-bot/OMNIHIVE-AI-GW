-- Provenance is metadata only: setting values and secrets remain in settings.
CREATE TABLE IF NOT EXISTS setting_overrides (
    setting_key TEXT PRIMARY KEY,
    source TEXT NOT NULL CHECK (source IN ('install_seed', 'environment', 'yaml', 'default', 'admin', 'unknown')),
    seeded_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Existing explicit values, including false, zero, and empty strings, are immutable.
-- Older installations without a plaza row remain disabled; new setup seeds true.
WITH defaults(setting_key, setting_value) AS (
    VALUES
        ('model_plaza_enabled', 'false'),
        ('model_plaza_require_auth', 'false'),
        ('model_plaza_description', ''),
        ('default_balance', '0'),
        ('default_concurrency', '5'),
        ('registration_enabled', 'true'),
        ('email_verify_enabled', 'false'),
        ('registration_email_suffix_whitelist', '[]'),
        ('registration_email_domain_quota_enabled', 'false'),
        ('promo_code_enabled', 'true'),
        ('password_reset_enabled', 'false'),
        ('frontend_url', ''),
        ('invitation_code_enabled', 'false'),
        ('totp_enabled', 'false'),
        ('passkey_enabled', 'true'),
        ('session_binding_enabled', 'false'),
        ('step_up_enabled', 'false'),
        ('audit_log_retention_days', '180'),
        ('backend_mode_enabled', 'false'),
        ('admin_api_key', ''),
        ('turnstile_enabled', 'false'),
        ('turnstile_site_key', ''),
        ('turnstile_secret_key', ''),
        ('smtp_host', ''),
        ('smtp_port', '587'),
        ('smtp_username', ''),
        ('smtp_password', ''),
        ('smtp_from', ''),
        ('smtp_from_name', ''),
        ('smtp_use_tls', 'false'),
        ('openai_ttft_mode', 'semantic'),
        ('web_search_emulation_config', '{"enabled":false,"providers":[]}'),
        ('default_platform_quotas', '{}'),
        ('account_scheduling_thresholds', '{}')
)
INSERT INTO settings (key, value, updated_at)
SELECT setting_key, setting_value, NOW()
FROM defaults
ON CONFLICT (key) DO NOTHING;

WITH registered(setting_key, source) AS (
    VALUES
        ('model_plaza_enabled', 'default'),
        ('model_plaza_require_auth', 'default'),
        ('model_plaza_description', 'default'),
        ('default_balance', 'unknown'),
        ('default_concurrency', 'unknown'),
        ('registration_enabled', 'default'),
        ('email_verify_enabled', 'default'),
        ('registration_email_suffix_whitelist', 'default'),
        ('registration_email_domain_quota_enabled', 'default'),
        ('promo_code_enabled', 'default'),
        ('password_reset_enabled', 'default'),
        ('frontend_url', 'default'),
        ('invitation_code_enabled', 'default'),
        ('totp_enabled', 'default'),
        ('passkey_enabled', 'default'),
        ('session_binding_enabled', 'default'),
        ('step_up_enabled', 'default'),
        ('audit_log_retention_days', 'default'),
        ('backend_mode_enabled', 'default'),
        ('admin_api_key', 'default'),
        ('turnstile_enabled', 'default'),
        ('turnstile_site_key', 'default'),
        ('turnstile_secret_key', 'default'),
        ('smtp_host', 'default'),
        ('smtp_port', 'default'),
        ('smtp_username', 'default'),
        ('smtp_password', 'default'),
        ('smtp_from', 'default'),
        ('smtp_from_name', 'default'),
        ('smtp_use_tls', 'default'),
        ('openai_ttft_mode', 'default'),
        ('web_search_emulation_config', 'default'),
        ('default_platform_quotas', 'default'),
        ('account_scheduling_thresholds', 'default')
)
INSERT INTO setting_overrides (setting_key, source, seeded_at, updated_at)
SELECT setting_key, source, NULL, NOW()
FROM registered
ON CONFLICT (setting_key) DO NOTHING;

-- Provider provenance cannot be reconstructed safely from an existing value.
WITH provider_keys(setting_key) AS (
    SELECT setting_key
    FROM settings
    WHERE key ~ '^(linuxdo_connect|dingtalk_connect|wechat_connect|oidc_connect|github_oauth|google_oauth)_'
)
INSERT INTO setting_overrides (setting_key, source, updated_at)
SELECT setting_key, 'unknown', NOW()
FROM provider_keys
ON CONFLICT (setting_key) DO NOTHING;
