package model

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func seedDistributionQuotaData(t *testing.T, quotaData QuotaData) {
	t.Helper()
	require.NoError(t, DB.Create(&quotaData).Error)
}

func TestGetQuotaDistributionAppliesRoleScopesAndMetrics(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "bob", Password: "password", AffCode: "aff-bob"}).Error)
	seedDistributionQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		TokenID:   11,
		UseGroup:  "default",
		ModelName: "gpt-a",
		CreatedAt: 1000,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	})
	seedDistributionQuotaData(t, QuotaData{
		UserID:    2,
		Username:  "bob",
		TokenID:   22,
		UseGroup:  "vip",
		ModelName: "gpt-b",
		CreatedAt: 1000,
		Count:     5,
		Quota:     300,
		TokenUsed: 80,
	})

	userRows, err := GetQuotaDistribution(QuotaDistributionQuery{
		AuthUserID: 1,
		Role:       common.RoleCommonUser,
		StartTime:  900,
		EndTime:    1100,
		Metric:     QuotaDistributionMetricQuota,
		Dimension:  QuotaDistributionDimensionGroup,
		Username:   "bob",
	})
	require.NoError(t, err)
	require.Len(t, userRows, 1)
	assert.Equal(t, "group:default", userRows[0].ID)
	assert.Equal(t, int64(100), userRows[0].Quota)

	adminKeyRows, err := GetQuotaDistribution(QuotaDistributionQuery{
		AuthUserID: 9,
		Role:       common.RoleAdminUser,
		StartTime:  900,
		EndTime:    1100,
		Metric:     QuotaDistributionMetricQuota,
		Dimension:  QuotaDistributionDimensionKey,
	})
	require.Error(t, err)
	assert.Nil(t, adminKeyRows)

	adminGroupRows, err := GetQuotaDistribution(QuotaDistributionQuery{
		AuthUserID: 9,
		Role:       common.RoleAdminUser,
		StartTime:  900,
		EndTime:    1100,
		Metric:     QuotaDistributionMetricQuota,
		Dimension:  QuotaDistributionDimensionGroup,
	})
	require.NoError(t, err)
	require.Len(t, adminGroupRows, 2)
	assert.Equal(t, "group:vip", adminGroupRows[0].ID)
	assert.Equal(t, int64(300), adminGroupRows[0].Quota)
	assert.Equal(t, "group:default", adminGroupRows[1].ID)
	assert.Equal(t, int64(100), adminGroupRows[1].Quota)

	rootUserRows, err := GetQuotaDistribution(QuotaDistributionQuery{
		AuthUserID: 9,
		Role:       common.RoleRootUser,
		StartTime:  900,
		EndTime:    1100,
		Metric:     QuotaDistributionMetricTokens,
		Dimension:  QuotaDistributionDimensionUser,
		Username:   "bob",
	})
	require.NoError(t, err)
	require.Len(t, rootUserRows, 1)
	assert.Equal(t, "user:2", rootUserRows[0].ID)
	assert.Equal(t, "bob", rootUserRows[0].Label)
	assert.Equal(t, int64(80), rootUserRows[0].Tokens)
}

func TestGetQuotaDistributionGroupsKeysByTokenIDNotLabel(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Token{Id: 11, UserId: 1, Key: "sk-11", Name: "shared"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 12, UserId: 1, Key: "sk-12", Name: "shared"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 13, UserId: 1, Key: "sk-13", Name: ""}).Error)
	require.NoError(t, DB.Create(&Token{Id: 14, UserId: 1, Key: "sk-14", Name: "old"}).Error)
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", 14).Update("name", "renamed").Error)
	require.NoError(t, DB.Create(&Token{Id: 15, UserId: 1, Key: "sk-15", Name: "deleted"}).Error)
	require.NoError(t, DB.Delete(&Token{Id: 15}).Error)

	for _, row := range []QuotaData{
		{UserID: 1, Username: "alice", TokenID: 11, UseGroup: "default", CreatedAt: 1000, Count: 1, Quota: 10, TokenUsed: 10},
		{UserID: 1, Username: "alice", TokenID: 12, UseGroup: "default", CreatedAt: 1000, Count: 1, Quota: 20, TokenUsed: 20},
		{UserID: 1, Username: "alice", TokenID: 13, UseGroup: "default", CreatedAt: 1000, Count: 1, Quota: 30, TokenUsed: 30},
		{UserID: 1, Username: "alice", TokenID: 14, UseGroup: "default", CreatedAt: 1000, Count: 1, Quota: 40, TokenUsed: 40},
		{UserID: 1, Username: "alice", TokenID: 15, UseGroup: "default", CreatedAt: 1000, Count: 1, Quota: 50, TokenUsed: 50},
	} {
		seedDistributionQuotaData(t, row)
	}

	rows, err := GetQuotaDistribution(QuotaDistributionQuery{
		AuthUserID: 1,
		Role:       common.RoleRootUser,
		StartTime:  900,
		EndTime:    1100,
		Metric:     QuotaDistributionMetricQuota,
		Dimension:  QuotaDistributionDimensionKey,
	})
	require.NoError(t, err)
	require.Len(t, rows, 5)

	assert.Equal(t, "key:15", rows[0].ID)
	assert.Empty(t, rows[0].Label)
	assert.Equal(t, "key:14", rows[1].ID)
	assert.Equal(t, "renamed", rows[1].Label)
	assert.Equal(t, "key:13", rows[2].ID)
	assert.Empty(t, rows[2].Label)
	assert.Equal(t, "key:12", rows[3].ID)
	assert.Equal(t, "shared", rows[3].Label)
	assert.Equal(t, "key:11", rows[4].ID)
	assert.Equal(t, "shared", rows[4].Label)
}

func TestGetQuotaDistributionGroupsUsersByIDAndUsesCurrentUsername(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice-current", Password: "password", AffCode: "aff-1"}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "", Password: "password", AffCode: "aff-2"}).Error)
	require.NoError(t, DB.Create(&User{Id: 3, Username: "deleted", Password: "password", AffCode: "aff-3"}).Error)
	require.NoError(t, DB.Delete(&User{Id: 3}).Error)

	for _, row := range []QuotaData{
		{UserID: 1, Username: "alice-old", TokenID: 11, UseGroup: "default", CreatedAt: 1000, Count: 1, Quota: 10, TokenUsed: 20},
		{UserID: 1, Username: "alice-renamed", TokenID: 12, UseGroup: "default", CreatedAt: 1000, Count: 2, Quota: 30, TokenUsed: 40},
		{UserID: 2, Username: "blank-old", TokenID: 21, UseGroup: "default", CreatedAt: 1000, Count: 3, Quota: 100, TokenUsed: 200},
		{UserID: 3, Username: "deleted-old", TokenID: 31, UseGroup: "default", CreatedAt: 1000, Count: 4, Quota: 200, TokenUsed: 300},
		{UserID: 4, Username: "missing-old", TokenID: 41, UseGroup: "default", CreatedAt: 1000, Count: 5, Quota: 300, TokenUsed: 400},
	} {
		seedDistributionQuotaData(t, row)
	}

	rows, err := GetQuotaDistribution(QuotaDistributionQuery{
		AuthUserID: 9,
		Role:       common.RoleRootUser,
		StartTime:  900,
		EndTime:    1100,
		Metric:     QuotaDistributionMetricQuota,
		Dimension:  QuotaDistributionDimensionUser,
	})
	require.NoError(t, err)
	require.Len(t, rows, 4)

	rowByID := make(map[string]QuotaDistributionRow, len(rows))
	for _, row := range rows {
		rowByID[row.ID] = row
	}
	assert.Equal(t, int64(40), rowByID["user:1"].Quota)
	assert.Equal(t, int64(3), rowByID["user:1"].Requests)
	assert.Equal(t, "alice-current", rowByID["user:1"].Label)
	assert.Equal(t, "user:2", rowByID["user:2"].Label)
	assert.Equal(t, "user:3", rowByID["user:3"].Label)
	assert.Equal(t, "user:4", rowByID["user:4"].Label)
}

func TestGetQuotaDistributionRejectsNegativeSourceMetrics(t *testing.T) {
	truncateTables(t)
	seedDistributionQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		TokenID:   11,
		UseGroup:  "default",
		CreatedAt: 1000,
		Count:     -1,
		Quota:     10,
		TokenUsed: 10,
	})

	rows, err := GetQuotaDistribution(QuotaDistributionQuery{
		AuthUserID: 1,
		Role:       common.RoleCommonUser,
		StartTime:  900,
		EndTime:    1100,
		Metric:     QuotaDistributionMetricRequests,
		Dimension:  QuotaDistributionDimensionGroup,
	})

	require.Error(t, err)
	assert.Nil(t, rows)
	assert.Contains(t, err.Error(), "negative")
}

type quotaDistributionSQLRecorder struct {
	logger.Interface
	statements []string
}

func (recorder *quotaDistributionSQLRecorder) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sqlStatement, _ := fc()
	recorder.statements = append(recorder.statements, strings.ToLower(sqlStatement))
}

func TestGetQuotaDistributionRejectsNegativeMetricsWithoutSeparateCountScan(t *testing.T) {
	truncateTables(t)
	seedDistributionQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		TokenID:   11,
		UseGroup:  "default",
		CreatedAt: 1000,
		Count:     1,
		Quota:     -1,
		TokenUsed: 10,
	})
	recorder := &quotaDistributionSQLRecorder{Interface: logger.Discard}
	originalDB := DB
	DB = DB.Session(&gorm.Session{Logger: recorder})
	t.Cleanup(func() {
		DB = originalDB
	})

	rows, err := GetQuotaDistribution(QuotaDistributionQuery{
		AuthUserID: 9,
		Role:       common.RoleAdminUser,
		StartTime:  900,
		EndTime:    1100,
		Metric:     QuotaDistributionMetricQuota,
		Dimension:  QuotaDistributionDimensionGroup,
	})

	require.Error(t, err)
	assert.Nil(t, rows)
	assert.Contains(t, err.Error(), "negative")
	quotaDataSelects := 0
	for _, statement := range recorder.statements {
		assert.NotContains(t, statement, "count(*)")
		if strings.Contains(statement, "from `quota_data`") || strings.Contains(statement, "from quota_data") {
			quotaDataSelects++
		}
	}
	assert.Equal(t, 1, quotaDataSelects)
}

func TestQuotaDistributionAggregateSQLIsPortableAcrossDialects(t *testing.T) {
	cases := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{name: "sqlite", dialector: sqlite.Open(":memory:")},
		{name: "mysql", dialector: mysql.New(mysql.Config{DSN: "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", SkipInitializeWithVersion: true})},
		{name: "postgres", dialector: postgres.New(postgres.Config{Conn: &sql.DB{}, PreferSimpleProtocol: true})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(tc.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			require.NoError(t, err)
			query := QuotaDistributionQuery{
				AuthUserID: 9,
				Role:       common.RoleRootUser,
				StartTime:  900,
				EndTime:    1100,
				Metric:     QuotaDistributionMetricQuota,
				Dimension:  QuotaDistributionDimensionUser,
			}
			dbQuery := db.Table("quota_data").Where("created_at >= ? and created_at <= ?", query.StartTime, query.EndTime)
			var aggregateRows []quotaDistributionAggregateRow
			statement := applyQuotaDistributionAggregate(dbQuery, query.Dimension, query.Metric).Find(&aggregateRows).Statement.SQL.String()
			normalized := strings.ToLower(statement)
			unquoted := strings.NewReplacer("`", "", "\"", "").Replace(normalized)

			assert.Contains(t, normalized, "sum(case when")
			assert.Contains(t, normalized, "then 1 else 0 end")
			assert.Contains(t, unquoted, "group by user_id")
			assert.NotContains(t, unquoted, "group by user_id, username")
			assert.NotContains(t, normalized, "count(*)")
		})
	}
}
