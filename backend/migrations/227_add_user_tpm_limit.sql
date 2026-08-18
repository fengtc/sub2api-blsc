-- Add a user-level global tokens-per-minute limit.
-- 0 means unlimited. Usage is aggregated across all API keys, models, and groups.
ALTER TABLE users ADD COLUMN IF NOT EXISTS tpm_limit integer NOT NULL DEFAULT 0;

COMMENT ON COLUMN users.tpm_limit IS '用户级全局 TPM 上限；0 表示不限制；按所有 API Key、模型和分组合并统计。';
