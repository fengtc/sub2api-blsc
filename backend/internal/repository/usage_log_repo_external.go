package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ListExternalUsageLogs returns a denormalized, read-only usage export for
// external integrations together with exact totals for the selected range.
func (r *usageLogRepository) ListExternalUsageLogs(ctx context.Context, params pagination.PaginationParams, filters service.ExternalUsageLogFilters) ([]service.ExternalUsageLog, *pagination.PaginationResult, *service.ExternalUsageLogTotals, error) {
	conditions := make([]string, 0, 8)
	args := make([]any, 0, 8)
	if filters.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.user_id = $%d", len(args)+1))
		args = append(args, filters.UserID)
	}
	if filters.APIKeyID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.api_key_id = $%d", len(args)+1))
		args = append(args, filters.APIKeyID)
	}
	if filters.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.account_id = $%d", len(args)+1))
		args = append(args, filters.AccountID)
	}
	if filters.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.group_id = $%d", len(args)+1))
		args = append(args, filters.GroupID)
	}
	if model := strings.TrimSpace(filters.Model); model != "" {
		position := len(args) + 1
		conditions = append(conditions, fmt.Sprintf("(ul.model = $%d OR ul.requested_model = $%d OR ul.upstream_model = $%d)", position, position, position))
		args = append(args, model)
	}
	if filters.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("ul.created_at >= $%d", len(args)+1))
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("ul.created_at < $%d", len(args)+1))
		args = append(args, *filters.EndTime)
	}
	whereClause := buildWhere(conditions)

	var total int64
	if err := scanSingleRow(ctx, r.sql, `SELECT COUNT(*) FROM usage_logs ul `+whereClause, args, &total); err != nil {
		return nil, nil, nil, err
	}
	totals := &service.ExternalUsageLogTotals{}
	totalsQuery := `
		SELECT COUNT(*),
			COALESCE(SUM(ul.input_tokens), 0),
			COALESCE(SUM(ul.output_tokens), 0),
			COALESCE(SUM(ul.cache_creation_tokens), 0),
			COALESCE(SUM(ul.cache_read_tokens), 0),
			COALESCE(SUM(ul.image_output_tokens), 0),
			COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens + ul.image_output_tokens), 0),
			COALESCE(SUM(ul.total_cost), 0),
			COALESCE(SUM(ul.actual_cost), 0)
		FROM usage_logs ul ` + whereClause
	if err := scanSingleRow(ctx, r.sql, totalsQuery, args,
		&totals.Requests, &totals.InputTokens, &totals.OutputTokens,
		&totals.CacheCreationTokens, &totals.CacheReadTokens,
		&totals.ImageOutputTokens, &totals.TotalTokens,
		&totals.TotalCost, &totals.ActualCost,
	); err != nil {
		return nil, nil, nil, err
	}

	orderBy := "ul.created_at DESC, ul.id DESC"
	if params.NormalizedSortOrder(pagination.SortOrderDesc) == pagination.SortOrderAsc {
		orderBy = "ul.created_at ASC, ul.id ASC"
	}
	rowArgs := append([]any(nil), args...)
	limitPos := len(rowArgs) + 1
	rowArgs = append(rowArgs, params.Limit())
	offsetPos := len(rowArgs) + 1
	rowArgs = append(rowArgs, params.Offset())
	query := fmt.Sprintf(`
		SELECT ul.id, ul.created_at, ul.user_id,
			COALESCE(u.email, ''), COALESCE(u.username, ''), COALESCE(u.notes, ''),
			ul.api_key_id, COALESCE(ak.name, ''),
			ul.account_id, COALESCE(a.name, ''), COALESCE(a.platform, COALESCE(g.platform, '')),
			ul.group_id, g.name, COALESCE(ul.request_id, ''), ul.model,
			COALESCE(NULLIF(ul.requested_model, ''), ul.model), ul.upstream_model, ul.model_mapping_chain,
			ul.input_tokens, ul.output_tokens, ul.cache_creation_tokens, ul.cache_read_tokens, ul.image_output_tokens,
			ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens + ul.image_output_tokens,
			ul.input_cost, ul.output_cost, ul.cache_creation_cost, ul.cache_read_cost, ul.image_output_cost,
			ul.total_cost, ul.actual_cost, ul.request_type, ul.stream, ul.duration_ms, ul.first_token_ms,
			ul.inbound_endpoint, ul.upstream_endpoint
		FROM usage_logs ul
		LEFT JOIN users u ON u.id = ul.user_id
		LEFT JOIN api_keys ak ON ak.id = ul.api_key_id
		LEFT JOIN accounts a ON a.id = ul.account_id
		LEFT JOIN groups g ON g.id = ul.group_id
		%s ORDER BY %s LIMIT $%d OFFSET $%d`, whereClause, orderBy, limitPos, offsetPos)
	rows, err := r.sql.QueryContext(ctx, query, rowArgs...)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.ExternalUsageLog, 0, params.Limit())
	for rows.Next() {
		var item service.ExternalUsageLog
		var groupID sql.NullInt64
		var groupName, upstreamModel, mappingChain sql.NullString
		var durationMs, firstTokenMs sql.NullInt64
		var inboundEndpoint, upstreamEndpoint sql.NullString
		var requestType int16
		if err := rows.Scan(
			&item.ID, &item.CreatedAt, &item.UserID, &item.Email, &item.Username, &item.Remark,
			&item.APIKeyID, &item.APIKeyName, &item.AccountID, &item.AccountName, &item.Platform,
			&groupID, &groupName, &item.RequestID, &item.Model, &item.RequestedModel,
			&upstreamModel, &mappingChain, &item.InputTokens, &item.OutputTokens,
			&item.CacheCreationTokens, &item.CacheReadTokens, &item.ImageOutputTokens,
			&item.TotalTokens, &item.InputCost, &item.OutputCost, &item.CacheCreationCost,
			&item.CacheReadCost, &item.ImageOutputCost, &item.TotalCost, &item.ActualCost,
			&requestType, &item.Stream, &durationMs, &firstTokenMs, &inboundEndpoint, &upstreamEndpoint,
		); err != nil {
			return nil, nil, nil, err
		}
		if groupID.Valid {
			value := groupID.Int64
			item.GroupID = &value
		}
		if groupName.Valid {
			value := groupName.String
			item.GroupName = &value
		}
		if upstreamModel.Valid {
			value := upstreamModel.String
			item.UpstreamModel = &value
		}
		if mappingChain.Valid {
			value := mappingChain.String
			item.ModelMappingChain = &value
		}
		if durationMs.Valid {
			value := int(durationMs.Int64)
			item.DurationMs = &value
		}
		if firstTokenMs.Valid {
			value := int(firstTokenMs.Int64)
			item.FirstTokenMs = &value
		}
		if inboundEndpoint.Valid {
			value := inboundEndpoint.String
			item.InboundEndpoint = &value
		}
		if upstreamEndpoint.Valid {
			value := upstreamEndpoint.String
			item.UpstreamEndpoint = &value
		}
		item.RequestType = service.RequestTypeFromInt16(requestType).String()
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	return out, paginationResultFromTotal(total, params), totals, nil
}
