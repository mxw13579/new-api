package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const testDay = int64(24 * 60 * 60)

func openColdLogTestSQLite(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/"+name), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	return db
}

// useColdLogStores installs hot and cold log stores and the cold storage
// settings for one test, restoring the previous ones afterwards.
func useColdLogStores(t *testing.T, hot *gorm.DB, cold *gorm.DB, retentionDays int, cleanup bool) {
	t.Helper()
	previousLogDB, previousColdDB := LOG_DB, LOG_COLD_DB
	previousRetention, previousCleanup := logHotRetentionDays, logHotCleanupEnabled
	previousSyncBatch, previousCleanupBatch, previousPause := logColdSyncBatchSize, logHotCleanupBatchSize, logHotCleanupPause
	previousMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		LOG_DB, LOG_COLD_DB = previousLogDB, previousColdDB
		logHotRetentionDays, logHotCleanupEnabled = previousRetention, previousCleanup
		logColdSyncBatchSize, logHotCleanupBatchSize, logHotCleanupPause = previousSyncBatch, previousCleanupBatch, previousPause
		common.MemoryCacheEnabled = previousMemoryCache
	})
	LOG_DB, LOG_COLD_DB = hot, cold
	logHotRetentionDays, logHotCleanupEnabled = retentionDays, cleanup
	logHotCleanupPause = 0
	common.MemoryCacheEnabled = false
}

func newSQLiteColdLogStores(t *testing.T, retentionDays int, cleanup bool) (*gorm.DB, *gorm.DB) {
	t.Helper()
	hot := openColdLogTestSQLite(t, "hot.db")
	cold := openColdLogTestSQLite(t, "cold.db")
	require.NoError(t, hot.AutoMigrate(&Log{}, &LogColdSyncState{}))
	require.NoError(t, cold.AutoMigrate(&Log{}))
	useColdLogStores(t, hot, cold, retentionDays, cleanup)
	return hot, cold
}

func logIds(t *testing.T, db *gorm.DB) []int {
	t.Helper()
	var ids []int
	require.NoError(t, db.Model(&Log{}).Order("id").Pluck("id", &ids).Error)
	return ids
}

func requestIdsOf(logs []*Log) []string {
	ids := make([]string, 0, len(logs))
	for _, log := range logs {
		ids = append(ids, log.RequestId)
	}
	return ids
}

func TestLogSearchRoutesByCallerStart(t *testing.T) {
	now := time.Now().Unix()
	for _, tc := range []struct {
		name         string
		noCold       bool
		retention    int
		filter       LogFilter
		wantRequests []string
		wantQuota    int
	}{
		{name: "no start reads hot", retention: 30, filter: LogFilter{}, wantRequests: []string{"hot-recent"}, wantQuota: 10},
		{name: "start inside retention reads hot", retention: 30, filter: LogFilter{StartTimestamp: now - 10*testDay}, wantRequests: []string{"hot-recent"}, wantQuota: 10},
		{name: "start before retention reads cold", retention: 30, filter: LogFilter{StartTimestamp: now - 60*testDay}, wantRequests: []string{"cold-recent", "cold-old"}, wantQuota: 1100},
		{name: "start filled by the range limit reads hot", retention: 30, filter: LogFilter{StartTimestamp: now - 365*testDay, StartFilledByRange: true}, wantRequests: []string{"hot-recent"}, wantQuota: 10},
		{name: "routing off reads hot", retention: 0, filter: LogFilter{StartTimestamp: now - 60*testDay}, wantRequests: []string{"hot-recent"}, wantQuota: 10},
		{name: "no cold archive reads hot", noCold: true, retention: 30, filter: LogFilter{StartTimestamp: now - 60*testDay}, wantRequests: []string{"hot-recent"}, wantQuota: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hot, cold := newSQLiteColdLogStores(t, tc.retention, false)
			require.NoError(t, hot.Create(&Log{UserId: 1, Username: "alice", CreatedAt: now - 30, Type: LogTypeConsume, Quota: 10, RequestId: "hot-recent"}).Error)
			require.NoError(t, cold.Create(&[]Log{
				{Id: 1, UserId: 1, Username: "alice", CreatedAt: now - 40*testDay, Type: LogTypeConsume, Quota: 1000, RequestId: "cold-old"},
				{Id: 2, UserId: 1, Username: "alice", CreatedAt: now - 120, Type: LogTypeConsume, Quota: 100, RequestId: "cold-recent"},
			}).Error)
			if tc.noCold {
				LOG_COLD_DB = nil
			}

			logs, total, err := GetAllLogs(tc.filter, 0, 10)
			require.NoError(t, err)
			assert.Equal(t, tc.wantRequests, requestIdsOf(logs))
			assert.EqualValues(t, len(tc.wantRequests), total)

			userLogs, _, err := GetUserLogs(1, tc.filter, 0, 10)
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.wantRequests, requestIdsOf(userLogs))

			stat, err := SumUsedQuota(LogTypeUnknown, tc.filter.StartTimestamp, 0, "", "", "", 0, "", tc.filter.StartFilledByRange)
			require.NoError(t, err)
			assert.Equal(t, tc.wantQuota, stat.Quota)
			assert.Equal(t, 1, stat.Rpm, "rpm always counts the hot store")
		})
	}
}

func TestLogRequestIdSearchFallsBackToCold(t *testing.T) {
	now := time.Now().Unix()
	hot, cold := newSQLiteColdLogStores(t, 30, false)
	require.NoError(t, hot.Create(&Log{UserId: 1, CreatedAt: now - 60, Type: LogTypeConsume, RequestId: "in-both", UpstreamRequestId: "up-hot"}).Error)
	require.NoError(t, cold.Create(&[]Log{
		{Id: 1, UserId: 1, CreatedAt: now - 40*testDay, Type: LogTypeConsume, RequestId: "archived", UpstreamRequestId: "up-archived"},
		{Id: 2, UserId: 1, CreatedAt: now - 60, Type: LogTypeConsume, RequestId: "in-both", Content: "cold copy"},
	}).Error)

	logs, total, err := GetAllLogs(LogFilter{RequestId: "archived"}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"archived"}, requestIdsOf(logs))
	assert.EqualValues(t, 1, total)

	logs, _, err = GetUserLogs(1, LogFilter{UpstreamRequestId: "up-archived"}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"archived"}, requestIdsOf(logs))

	var streamed []string
	require.NoError(t, StreamUserLogs(context.Background(), 1, LogFilter{RequestId: "archived"}, func(log *Log) error {
		streamed = append(streamed, log.RequestId)
		return nil
	}))
	assert.Equal(t, []string{"archived"}, streamed)

	logs, _, err = GetAllLogs(LogFilter{RequestId: "in-both"}, 0, 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Empty(t, logs[0].Content, "a hot match is not repeated on the archive")

	logHotRetentionDays = 0
	logs, total, err = GetAllLogs(LogFilter{RequestId: "archived"}, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, logs, "without routing the archive is never searched")
	assert.Zero(t, total)
}

func TestSyncColdLogs(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	hot, cold := newSQLiteColdLogStores(t, 30, false)
	logColdSyncBatchSize = 2
	for id := 1; id <= 4; id++ {
		require.NoError(t, hot.Create(&Log{Id: id, CreatedAt: now - 1000, RequestId: fmt.Sprintf("r%d", id)}).Error)
	}
	// Id 3 is younger than the safety window but below id 4, so it is copied.
	require.NoError(t, hot.Model(&Log{}).Where("id = ?", 3).Update("created_at", now-10).Error)
	require.NoError(t, hot.Create(&Log{Id: 5, CreatedAt: now - 100, RequestId: "r5"}).Error)

	result, err := SyncColdLogs(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, ColdLogSyncResult{SyncedId: 4, Copied: 4, Lag: 1}, result)
	assert.Equal(t, []int{1, 2, 3, 4}, logIds(t, cold), "id 5 is inside the safety window")

	result, err = SyncColdLogs(ctx, now+400)
	require.NoError(t, err)
	assert.Equal(t, ColdLogSyncResult{SyncedId: 5, Copied: 1, Lag: 0}, result)

	t.Run("rows left above the progress are rewritten", func(t *testing.T) {
		for id := 6; id <= 8; id++ {
			require.NoError(t, hot.Create(&Log{Id: id, CreatedAt: now - 1000, RequestId: fmt.Sprintf("r%d", id)}).Error)
		}
		// A run that wrote id 6 and part of id 7 but never advanced the progress.
		require.NoError(t, cold.Create(&[]Log{{Id: 6, RequestId: "r6"}, {Id: 7, RequestId: "partial"}}).Error)

		result, err := SyncColdLogs(ctx, now+400)
		require.NoError(t, err)
		assert.Equal(t, ColdLogSyncResult{SyncedId: 8, Copied: 3, Lag: 0}, result)
		assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8}, logIds(t, cold))
		var archived Log
		require.NoError(t, cold.Where("id = ?", 7).First(&archived).Error)
		assert.Equal(t, "r7", archived.RequestId)
	})

	t.Run("a failed write keeps the progress and a rerun completes", func(t *testing.T) {
		for id := 9; id <= 12; id++ {
			require.NoError(t, hot.Create(&Log{Id: id, CreatedAt: now - 1000, RequestId: fmt.Sprintf("r%d", id)}).Error)
		}
		writes, failWrite := 0, 2
		require.NoError(t, cold.Callback().Create().Before("gorm:create").Register("test:fail_cold_write", func(db *gorm.DB) {
			writes++
			if writes == failWrite {
				db.AddError(errors.New("cold archive unavailable"))
			}
		}))
		t.Cleanup(func() { assert.NoError(t, cold.Callback().Create().Remove("test:fail_cold_write")) })

		result, err := SyncColdLogs(ctx, now+400)
		require.Error(t, err)
		assert.Equal(t, int64(10), result.SyncedId)
		syncedId, found, err := coldLogSyncedId(ctx)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(10), syncedId)

		result, err = SyncColdLogs(ctx, now+400)
		require.NoError(t, err)
		assert.Equal(t, ColdLogSyncResult{SyncedId: 12, Copied: 2, Lag: 0}, result)
		assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, logIds(t, cold))
	})

	t.Run("an archive without progress is left alone", func(t *testing.T) {
		require.NoError(t, hot.Where("1 = 1").Delete(&LogColdSyncState{}).Error)
		_, err := SyncColdLogs(ctx, now+400)
		require.Error(t, err)
		assert.Len(t, logIds(t, cold), 12)
	})
}

func TestHotLogCleanupKeepsUnsyncedAndRecentLogs(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	seed := func(t *testing.T, cleanup bool) *gorm.DB {
		hot, _ := newSQLiteColdLogStores(t, 30, cleanup)
		require.NoError(t, hot.Create(&[]Log{
			{Id: 1, CreatedAt: now - 40*testDay},           // archived, past retention + 1 day
			{Id: 2, CreatedAt: now - 30*testDay - 3600},    // archived, inside the extra day
			{Id: 3, CreatedAt: now - 40*testDay},           // past retention, not archived yet
			{Id: 4, CreatedAt: now - 40*testDay - testDay}, // past retention, not archived yet
		}).Error)
		require.NoError(t, hot.Create(&LogColdSyncState{Name: logColdSyncStateName, SyncedId: 2}).Error)
		return hot
	}

	t.Run("scheduled cleanup", func(t *testing.T) {
		hot := seed(t, true)
		logHotCleanupBatchSize = 1
		deleted, err := CleanupHotLogs(ctx, now)
		require.NoError(t, err)
		assert.EqualValues(t, 1, deleted)
		assert.Equal(t, []int{2, 3, 4}, logIds(t, hot))
	})

	t.Run("scheduled cleanup off", func(t *testing.T) {
		hot := seed(t, false)
		deleted, err := CleanupHotLogs(ctx, now)
		require.NoError(t, err)
		assert.Zero(t, deleted)
		assert.Equal(t, []int{1, 2, 3, 4}, logIds(t, hot))
	})

	t.Run("manual cleanup", func(t *testing.T) {
		hot := seed(t, false)
		remaining, err := CountOldLog(ctx, now)
		require.NoError(t, err)
		assert.EqualValues(t, 2, remaining)
		deleted, err := DeleteOldLogBatch(ctx, now, 1)
		require.NoError(t, err)
		assert.EqualValues(t, 1, deleted)
		deleted, err = DeleteOldLogBatch(ctx, now, 100)
		require.NoError(t, err)
		assert.EqualValues(t, 1, deleted)
		assert.Equal(t, []int{3, 4}, logIds(t, hot), "unarchived logs survive the manual cleanup")
	})
}

// TestColdLogStorageOnPostgresAndClickHouse runs the hot/cold flow on a real
// PostgreSQL hot store and a real ClickHouse archive. Set TEST_POSTGRES_DSN and
// TEST_CLICKHOUSE_DSN to loopback instances to run it.
func TestColdLogStorageOnPostgresAndClickHouse(t *testing.T) {
	postgresDSN, clickHouseDSN := os.Getenv("TEST_POSTGRES_DSN"), os.Getenv("TEST_CLICKHOUSE_DSN")
	if postgresDSN == "" || clickHouseDSN == "" {
		t.Skip("TEST_POSTGRES_DSN and TEST_CLICKHOUSE_DSN are not configured")
	}
	ctx := context.Background()
	name := fmt.Sprintf("newapi_cold_logs_%d", time.Now().UnixNano())

	pgURL, err := url.Parse(postgresDSN)
	require.NoError(t, err)
	require.True(t, net.ParseIP(pgURL.Hostname()).IsLoopback(), "database tests only permit loopback instances")
	pgAdmin, err := gorm.Open(postgres.Open(postgresDSN), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, pgAdmin.Exec("CREATE SCHEMA "+name).Error)
	t.Cleanup(func() { assert.NoError(t, pgAdmin.Exec("DROP SCHEMA "+name+" CASCADE").Error) })
	query := pgURL.Query()
	query.Set("search_path", name)
	pgURL.RawQuery = query.Encode()
	hot, err := gorm.Open(postgres.New(postgres.Config{DSN: pgURL.String(), PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, hot.AutoMigrate(&Log{}))

	chURL, err := url.Parse(clickHouseDSN)
	require.NoError(t, err)
	require.True(t, net.ParseIP(chURL.Hostname()).IsLoopback(), "database tests only permit loopback instances")
	chAdmin, err := gorm.Open(clickhouse.Open(clickHouseDSN), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, chAdmin.Exec("CREATE DATABASE "+name).Error)
	t.Cleanup(func() { assert.NoError(t, chAdmin.Exec("DROP DATABASE "+name).Error) })
	chURL.Path = "/" + name

	var pgVersion, chVersion string
	require.NoError(t, hot.Raw("SELECT version()").Scan(&pgVersion).Error)
	require.NoError(t, chAdmin.Raw("SELECT version()").Scan(&chVersion).Error)
	t.Logf("hot: %s; cold: ClickHouse %s", pgVersion, chVersion)

	previousMain, previousLog, previousMaster := common.MainDatabaseType(), common.LogDatabaseType(), common.IsMasterNode
	t.Cleanup(func() {
		common.SetDatabaseTypes(previousMain, previousLog)
		initCol()
		common.IsMasterNode = previousMaster
		if LOG_COLD_DB != nil {
			assert.NoError(t, closeDB(LOG_COLD_DB))
		}
	})
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	initCol()
	common.IsMasterNode = true
	useColdLogStores(t, hot, nil, 0, false)
	t.Setenv("LOG_SQL_DSN", "")
	t.Setenv("LOG_COLD_SQL_DSN", chURL.String())
	t.Setenv("LOG_HOT_RETENTION_DAYS", "30")
	t.Setenv("LOG_HOT_CLEANUP_ENABLED", "true")
	t.Setenv("LOG_COLD_SYNC_BATCH_SIZE", "2")
	t.Setenv("LOG_HOT_CLEANUP_BATCH_SIZE", "1")
	// Startup runs twice to show the archive and progress tables are created idempotently.
	require.NoError(t, InitLogColdDB())
	require.NoError(t, closeDB(LOG_COLD_DB))
	require.NoError(t, InitLogColdDB())
	logHotCleanupPause = 0

	now := time.Now().Unix()
	require.NoError(t, hot.Create(&[]Log{
		{UserId: 1, Username: "alice", CreatedAt: now - 40*testDay, Type: LogTypeConsume, ModelName: "gpt_4o", Group: "vip", Quota: 7, RequestId: "old-1", IsStream: true},
		{UserId: 1, Username: "alice", CreatedAt: now - 35*testDay, Type: LogTypeConsume, ModelName: "gpt-4o", Group: "default", Quota: 5, RequestId: "old-2"},
		{UserId: 2, Username: "bob", CreatedAt: now - 1000, Type: LogTypeError, ModelName: "gpt-4o", RequestId: "recent-1", UpstreamRequestId: "up-1"},
		{UserId: 1, Username: "alice", CreatedAt: now - 10, Type: LogTypeConsume, ModelName: "gpt-4o", Quota: 3, RequestId: "fresh"},
	}).Error)

	result, err := SyncColdLogs(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 3, result.Copied)
	assert.EqualValues(t, 1, result.Lag)
	var archived []Log
	require.NoError(t, LOG_COLD_DB.Order("id").Find(&archived).Error)
	require.Len(t, archived, 3)
	assert.Equal(t, "old-1", archived[0].RequestId)
	assert.True(t, archived[0].IsStream)
	assert.Equal(t, "vip", archived[0].Group)

	// A leftover row above the progress is removed with a ClickHouse mutation.
	require.NoError(t, LOG_COLD_DB.Create(&Log{Id: archived[2].Id + 1, RequestId: "fresh"}).Error)
	result, err = SyncColdLogs(ctx, now+400)
	require.NoError(t, err)
	assert.EqualValues(t, 1, result.Copied)
	var coldCount int64
	require.NoError(t, LOG_COLD_DB.Model(&Log{}).Count(&coldCount).Error)
	assert.EqualValues(t, 4, coldCount)

	oldStart := now - 60*testDay
	logs, total, err := GetAllLogs(LogFilter{StartTimestamp: oldStart, ModelName: "gpt_4%", Group: "vip"}, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, []string{"old-1"}, requestIdsOf(logs))
	assert.Equal(t, 1, logs[0].Id, "archive results carry list positions")
	stat, err := SumUsedQuota(LogTypeUnknown, oldStart, 0, "", "alice", "", 0, "vip", false)
	require.NoError(t, err)
	assert.Equal(t, 7, stat.Quota)
	var exported []string
	require.NoError(t, StreamAllLogs(ctx, LogFilter{StartTimestamp: oldStart}, common.RoleRootUser, func(log *Log) error {
		exported = append(exported, log.RequestId)
		return nil
	}))
	assert.Equal(t, []string{"fresh", "recent-1", "old-2", "old-1"}, exported)

	deleted, err := CleanupHotLogs(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)
	logs, _, err = GetAllLogs(LogFilter{RequestId: "old-2"}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"old-2"}, requestIdsOf(logs), "a request id missing from the hot store is found in the archive")
	logs, _, err = GetAllLogs(LogFilter{StartTimestamp: now - 10*testDay}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"fresh", "recent-1"}, requestIdsOf(logs))

	remaining, err := CountOldLog(ctx, now+1)
	require.NoError(t, err)
	assert.EqualValues(t, 2, remaining)
	deleted, err = DeleteOldLogBatch(ctx, now+1, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted, "the manual cleanup deletes one batch per call on PostgreSQL")
}
