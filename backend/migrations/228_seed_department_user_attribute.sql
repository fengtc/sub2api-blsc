-- Add the enterprise department attribute used by the admin user forms.
-- Keep this idempotent because an administrator may already have created it
-- through User Management -> Attribute Configuration.
INSERT INTO user_attribute_definitions (
    key,
    name,
    description,
    type,
    options,
    required,
    validation,
    placeholder,
    display_order,
    enabled,
    created_at,
    updated_at
)
SELECT
    'department',
    '部门',
    '用户所属部门，支持手工录入和批量导入',
    'text',
    '[]'::jsonb,
    FALSE,
    '{}'::jsonb,
    '请输入部门名称',
    COALESCE((
        SELECT MAX(display_order) + 1
        FROM user_attribute_definitions
        WHERE deleted_at IS NULL
    ), 0),
    TRUE,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1
    FROM user_attribute_definitions
    WHERE key = 'department'
      AND deleted_at IS NULL
);

-- Normalize an existing department definition to the text-based form used by
-- bulk imports. Existing values remain valid because they are stored as text.
UPDATE user_attribute_definitions
SET type = 'text',
    options = '[]'::jsonb,
    description = '用户所属部门，支持手工录入和批量导入',
    placeholder = '请输入部门名称',
    enabled = TRUE,
    updated_at = NOW()
WHERE key = 'department'
  AND deleted_at IS NULL;
