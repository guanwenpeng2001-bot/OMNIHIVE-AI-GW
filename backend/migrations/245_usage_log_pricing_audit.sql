-- Nullable audit metadata: historical unknowns remain NULL.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS thinking_enabled boolean;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS price_source text;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS price_card_version text;
