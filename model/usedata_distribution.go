package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	QuotaDistributionMetricQuota    = "quota"
	QuotaDistributionMetricTokens   = "tokens"
	QuotaDistributionMetricRequests = "requests"

	QuotaDistributionDimensionUser  = "user"
	QuotaDistributionDimensionKey   = "key"
	QuotaDistributionDimensionGroup = "group"
)

// QuotaDistributionQuery defines the authenticated scope and aggregation axes for a distribution query.
type QuotaDistributionQuery struct {
	AuthUserID int
	Role       int
	StartTime  int64
	EndTime    int64
	Metric     string
	Dimension  string
	Username   string
}

// QuotaDistributionRow is one stable-identity distribution bucket returned to the dashboard.
type QuotaDistributionRow struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Dimension string `json:"dimension"`
	Quota     int64  `json:"quota"`
	Tokens    int64  `json:"tokens"`
	Requests  int64  `json:"requests"`
}

type quotaDistributionAggregateRow struct {
	Identity    int64  `gorm:"column:identity"`
	Label       string `gorm:"column:label"`
	Quota       int64  `gorm:"column:quota"`
	Tokens      int64  `gorm:"column:tokens"`
	Requests    int64  `gorm:"column:requests"`
	HasNegative int64  `gorm:"column:has_negative"`
}

// IsValidQuotaDistributionMetric reports whether metric is supported by the distribution endpoint.
func IsValidQuotaDistributionMetric(metric string) bool {
	return metric == QuotaDistributionMetricQuota ||
		metric == QuotaDistributionMetricTokens ||
		metric == QuotaDistributionMetricRequests
}

// IsValidQuotaDistributionDimension reports whether dimension is supported by the distribution endpoint.
func IsValidQuotaDistributionDimension(dimension string) bool {
	return dimension == QuotaDistributionDimensionUser ||
		dimension == QuotaDistributionDimensionKey ||
		dimension == QuotaDistributionDimensionGroup
}

// GetQuotaDistribution returns role-scoped distribution buckets for the requested time range.
func GetQuotaDistribution(query QuotaDistributionQuery) ([]QuotaDistributionRow, error) {
	if query.AuthUserID <= 0 {
		return nil, errors.New("invalid user id")
	}
	if !IsValidQuotaDistributionMetric(query.Metric) {
		return nil, errors.New("invalid metric")
	}
	if !IsValidQuotaDistributionDimension(query.Dimension) {
		return nil, errors.New("invalid dimension")
	}
	if !quotaDistributionDimensionAllowed(query.Role, query.Dimension) {
		return nil, errors.New("dimension not allowed")
	}

	dbQuery := quotaDistributionBaseQuery(query)
	dbQuery = applyQuotaDistributionDimensionFilter(dbQuery, query.Dimension)

	aggregateRows := make([]quotaDistributionAggregateRow, 0)
	err := applyQuotaDistributionAggregate(dbQuery, query.Dimension, query.Metric).Find(&aggregateRows).Error
	if err != nil {
		return nil, err
	}
	for _, aggregateRow := range aggregateRows {
		if aggregateRow.HasNegative > 0 {
			return nil, errors.New("negative quota distribution source metrics")
		}
	}
	rows := buildQuotaDistributionRows(query.Dimension, aggregateRows)
	switch query.Dimension {
	case QuotaDistributionDimensionUser:
		if err := fillQuotaDistributionUserLabels(rows); err != nil {
			return nil, err
		}
	case QuotaDistributionDimensionKey:
		if err := fillQuotaDistributionKeyLabels(rows); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func quotaDistributionDimensionAllowed(role int, dimension string) bool {
	if role >= common.RoleRootUser {
		return true
	}
	if role >= common.RoleAdminUser {
		return dimension == QuotaDistributionDimensionUser || dimension == QuotaDistributionDimensionGroup
	}
	return dimension == QuotaDistributionDimensionKey || dimension == QuotaDistributionDimensionGroup
}

func quotaDistributionBaseQuery(query QuotaDistributionQuery) *gorm.DB {
	dbQuery := DB.Table("quota_data").
		Where("created_at >= ? and created_at <= ?", query.StartTime, query.EndTime)
	if query.Role < common.RoleAdminUser {
		return dbQuery.Where("user_id = ?", query.AuthUserID)
	}
	if query.Username != "" {
		dbQuery = dbQuery.Where("username = ?", query.Username)
	}
	return dbQuery
}

func applyQuotaDistributionDimensionFilter(dbQuery *gorm.DB, dimension string) *gorm.DB {
	switch dimension {
	case QuotaDistributionDimensionKey:
		return dbQuery.Where("token_id > 0")
	case QuotaDistributionDimensionGroup:
		return dbQuery.Where("use_group <> ''")
	default:
		return dbQuery
	}
}

func applyQuotaDistributionAggregate(dbQuery *gorm.DB, dimension string, metric string) *gorm.DB {
	metricSelect := "COALESCE(SUM(quota), 0) as quota, COALESCE(SUM(token_used), 0) as tokens, COALESCE(SUM(count), 0) as requests, COALESCE(SUM(CASE WHEN quota < 0 OR token_used < 0 OR count < 0 THEN 1 ELSE 0 END), 0) as has_negative"
	switch dimension {
	case QuotaDistributionDimensionUser:
		return dbQuery.
			Select("user_id as identity, '' as label, " + metricSelect).
			Group("user_id").
			Order(quotaDistributionMetricOrder(metric)).
			Order("user_id ASC")
	case QuotaDistributionDimensionKey:
		return dbQuery.
			Select("token_id as identity, '' as label, " + metricSelect).
			Group("token_id").
			Order(quotaDistributionMetricOrder(metric)).
			Order("token_id ASC")
	default:
		return dbQuery.
			Select("0 as identity, use_group as label, " + metricSelect).
			Group("use_group").
			Order(quotaDistributionMetricOrder(metric)).
			Order("use_group ASC")
	}
}

func quotaDistributionMetricOrder(metric string) string {
	switch metric {
	case QuotaDistributionMetricTokens:
		return "tokens DESC"
	case QuotaDistributionMetricRequests:
		return "requests DESC"
	default:
		return "quota DESC"
	}
}

func buildQuotaDistributionRows(dimension string, aggregateRows []quotaDistributionAggregateRow) []QuotaDistributionRow {
	rows := make([]QuotaDistributionRow, 0, len(aggregateRows))
	for _, aggregateRow := range aggregateRows {
		row := QuotaDistributionRow{
			Label:     aggregateRow.Label,
			Dimension: dimension,
			Quota:     aggregateRow.Quota,
			Tokens:    aggregateRow.Tokens,
			Requests:  aggregateRow.Requests,
		}
		switch dimension {
		case QuotaDistributionDimensionUser:
			row.ID = fmt.Sprintf("user:%d", aggregateRow.Identity)
			row.Label = row.ID
		case QuotaDistributionDimensionKey:
			row.ID = fmt.Sprintf("key:%d", aggregateRow.Identity)
		default:
			row.ID = "group:" + aggregateRow.Label
		}
		rows = append(rows, row)
	}
	return rows
}

func fillQuotaDistributionUserLabels(rows []QuotaDistributionRow) error {
	userIDs := make([]int, 0)
	seen := make(map[int]struct{})
	for _, row := range rows {
		userID, ok := strings.CutPrefix(row.ID, "user:")
		if !ok {
			continue
		}
		id, err := strconv.Atoi(userID)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		userIDs = append(userIDs, id)
	}
	if len(userIDs) == 0 {
		return nil
	}

	var users []struct {
		Id       int    `gorm:"column:id"`
		Username string `gorm:"column:username"`
	}
	if err := DB.Model(&User{}).Select("id, username").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return err
	}
	usernameByID := make(map[int]string, len(users))
	for _, user := range users {
		if user.Username != "" {
			usernameByID[user.Id] = user.Username
		}
	}
	for index := range rows {
		userID, ok := strings.CutPrefix(rows[index].ID, "user:")
		if !ok {
			continue
		}
		id, err := strconv.Atoi(userID)
		if err != nil {
			continue
		}
		if username := usernameByID[id]; username != "" {
			rows[index].Label = username
		}
	}
	return nil
}

func fillQuotaDistributionKeyLabels(rows []QuotaDistributionRow) error {
	tokenIDs := make([]int, 0)
	seen := make(map[int]struct{})
	for _, row := range rows {
		tokenID, ok := strings.CutPrefix(row.ID, "key:")
		if !ok {
			continue
		}
		id, err := strconv.Atoi(tokenID)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		tokenIDs = append(tokenIDs, id)
	}
	if len(tokenIDs) == 0 {
		return nil
	}

	var tokens []struct {
		Id   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := DB.Model(&Token{}).Select("id, name").Where("id IN ?", tokenIDs).Find(&tokens).Error; err != nil {
		return err
	}
	tokenNameByID := make(map[int]string, len(tokens))
	for _, token := range tokens {
		tokenNameByID[token.Id] = token.Name
	}
	for index := range rows {
		tokenID, ok := strings.CutPrefix(rows[index].ID, "key:")
		if !ok {
			continue
		}
		id, err := strconv.Atoi(tokenID)
		if err != nil {
			continue
		}
		rows[index].Label = tokenNameByID[id]
	}
	return nil
}
