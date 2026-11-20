package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
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
	previousLogDB, previousColdDB, previousConfigured, previousDSN := LOG_DB, logColdDB.Load(), logColdConfigured, logColdDSN
	previousRetention, previousCleanup := logHotRetentionDays, logHotCleanupEnabled
	previousSyncBatch, previousCleanupBatch, previousPause := logColdSyncBatchSize, logHotCleanupBatchSize, logHotCleanupPause
	previousMemoryCache := common.MemoryCacheEnabled
	previousReconnect, previousReconnectAt := logColdReconnect, logColdReconnectAt.Load()
	t.Cleanup(func() {
		LOG_DB, logColdConfigured, logColdDSN = previousLogDB, previousConfigured, previousDSN
		logColdDB.Store(previousColdDB)
		logColdReconnect = previousReconnect
		logColdReconnectAt.Store(previousReconnectAt)
		logHotRetentionDays, logHotCleanupEnabled = previousRetention, previousCleanup
		logColdSyncBatchSize, logHotCleanupBatchSize, logHotCleanupPause = previousSyncBatch, previousCleanupBatch, previousPause
		common.MemoryCacheEnabled = previousMemoryCache
	})
	LOG_DB, logColdConfigured = hot, cold != nil
	logColdDB.Store(cold)
	logHotRetentionDays, logHotCleanupEnabled = retentionDays, cleanup
	logHotCleanupPause = 0
	common.MemoryCacheEnabled = false
	// Searches on a store that is not connected start no real connect unless a
	// test asks for one.
	logColdReconnect = func() (*gorm.DB, error) { return nil, errors.New("reconnect is off in this test") }
	logColdReconnectAt.Store(0)
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
				logColdConfigured = false
				logColdDB.Store(nil)
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
	addHotLogs := func(t *testing.T, from, to int) {
		for id := from; id <= to; id++ {
			require.NoError(t, hot.Create(&Log{Id: id, CreatedAt: now - 1000, RequestId: fmt.Sprintf("r%d", id)}).Error)
		}
	}
	syncedState := func(t *testing.T) LogColdSyncState {
		state, found, err := coldLogSyncState(ctx)
		require.NoError(t, err)
		require.True(t, found)
		return state
	}

	result, err := SyncColdLogs(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, ColdLogSyncResult{SyncedId: 4, SyncedCreatedAt: now - 1000, Copied: 4, Lag: 1, LagSeconds: 1000}, result)
	assert.Equal(t, []int{1, 2, 3, 4}, logIds(t, cold), "id 5 is inside the safety window")

	result, err = SyncColdLogs(ctx, now+400)
	require.NoError(t, err)
	assert.Equal(t, ColdLogSyncResult{SyncedId: 5, SyncedCreatedAt: now - 100, Copied: 1, Lag: 0, LagSeconds: 500}, result)

	t.Run("rows left above the progress are rewritten", func(t *testing.T) {
		addHotLogs(t, 6, 8)
		// A run that wrote id 6 and part of id 7 but never advanced the progress.
		require.NoError(t, cold.Create(&[]Log{{Id: 6, CreatedAt: now - 1000, RequestId: "r6"}, {Id: 7, CreatedAt: now - 1000, RequestId: "partial"}}).Error)

		result, err := SyncColdLogs(ctx, now+400)
		require.NoError(t, err)
		assert.Equal(t, ColdLogSyncResult{SyncedId: 8, SyncedCreatedAt: now - 1000, Copied: 3, Lag: 0, LagSeconds: 1400}, result)
		assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8}, logIds(t, cold))
		var archived Log
		require.NoError(t, cold.Where("id = ?", 7).First(&archived).Error)
		assert.Equal(t, "r7", archived.RequestId)
	})

	t.Run("a failed write keeps the progress and a rerun completes", func(t *testing.T) {
		addHotLogs(t, 9, 12)
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
		assert.Equal(t, int64(10), syncedState(t).SyncedId)

		result, err = SyncColdLogs(ctx, now+400)
		require.NoError(t, err)
		assert.EqualValues(t, 12, result.SyncedId)
		assert.EqualValues(t, 2, result.Copied)
		assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, logIds(t, cold))
	})

	t.Run("a write the archive silently drops does not advance the progress", func(t *testing.T) {
		addHotLogs(t, 13, 14)
		// Stands in for an archive that discards a block as a duplicate.
		require.NoError(t, cold.Callback().Create().After("gorm:create").Register("test:drop_cold_row", func(db *gorm.DB) {
			_, err := db.Statement.ConnPool.ExecContext(db.Statement.Context, "DELETE FROM logs WHERE id = ?", 14)
			db.AddError(err)
		}))
		_, err := SyncColdLogs(ctx, now+400)
		require.ErrorContains(t, err, "holds 1 logs for ids 13 to 14 after writing 2")
		assert.EqualValues(t, 12, syncedState(t).SyncedId)
		require.NoError(t, cold.Callback().Create().Remove("test:drop_cold_row"))

		result, err := SyncColdLogs(ctx, now+400)
		require.NoError(t, err)
		assert.EqualValues(t, 14, result.SyncedId)
		assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}, logIds(t, cold))
	})

	t.Run("a run gives up when another run moves the progress", func(t *testing.T) {
		addHotLogs(t, 15, 16)
		require.NoError(t, cold.Create(&Log{Id: 15, CreatedAt: now - 1000, RequestId: "r15"}).Error)
		moved := false
		// Another runner advances the progress right after this run finds the leftover.
		require.NoError(t, cold.Callback().Query().After("gorm:query").Register("test:move_progress", func(db *gorm.DB) {
			if moved || !strings.Contains(db.Statement.SQL.String(), "id > ") {
				return
			}
			moved = true
			db.AddError(hot.Model(&LogColdSyncState{}).Where("name = ?", logColdSyncStateName).Update("synced_id", 15).Error)
		}))
		t.Cleanup(func() { assert.NoError(t, cold.Callback().Query().Remove("test:move_progress")) })

		_, err := SyncColdLogs(ctx, now+400)
		require.ErrorContains(t, err, "progress changed during the run")
		assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, logIds(t, cold), "the other run's rows are kept")
		assert.EqualValues(t, 15, syncedState(t).SyncedId)
	})

	t.Run("an archive without progress is left alone", func(t *testing.T) {
		require.NoError(t, hot.Where("1 = 1").Delete(&LogColdSyncState{}).Error)
		_, err := SyncColdLogs(ctx, now+400)
		require.Error(t, err)
		assert.Len(t, logIds(t, cold), 15)
	})
}

func TestHotLogCleanupKeepsUnsyncedAndRecentLogs(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	seed := func(t *testing.T, cleanup bool) (*gorm.DB, *gorm.DB) {
		hot, cold := newSQLiteColdLogStores(t, 30, cleanup)
		logs := []Log{
			{Id: 1, CreatedAt: now - 40*testDay},           // archived, past retention + 1 day
			{Id: 2, CreatedAt: now - 30*testDay - 3600},    // archived, inside the extra day
			{Id: 3, CreatedAt: now - 40*testDay},           // past retention, not archived yet
			{Id: 4, CreatedAt: now - 40*testDay - testDay}, // past retention, not archived yet
		}
		require.NoError(t, hot.Create(&logs).Error)
		require.NoError(t, cold.Create(logs[:2]).Error)
		require.NoError(t, hot.Create(&LogColdSyncState{Name: logColdSyncStateName, SyncedId: 2, SyncedCreatedAt: logs[1].CreatedAt}).Error)
		return hot, cold
	}

	t.Run("scheduled cleanup", func(t *testing.T) {
		hot, _ := seed(t, true)
		logHotCleanupBatchSize = 1
		deleted, err := CleanupHotLogs(ctx, now)
		require.NoError(t, err)
		assert.EqualValues(t, 1, deleted)
		assert.Equal(t, []int{2, 3, 4}, logIds(t, hot))
	})

	t.Run("scheduled cleanup off", func(t *testing.T) {
		hot, _ := seed(t, false)
		deleted, err := CleanupHotLogs(ctx, now)
		require.NoError(t, err)
		assert.Zero(t, deleted)
		assert.Equal(t, []int{1, 2, 3, 4}, logIds(t, hot))
	})

	t.Run("manual cleanup stops at the hot retention boundary", func(t *testing.T) {
		hot, _ := seed(t, false)
		assert.Equal(t, now-31*testDay, LogCleanupTarget(now, now))
		assert.Equal(t, now-40*testDay, LogCleanupTarget(now-40*testDay, now))
		remaining, err := CountOldLog(ctx, now)
		require.NoError(t, err)
		assert.EqualValues(t, 1, remaining)
		deleted, err := DeleteOldLogBatch(ctx, now, 1)
		require.NoError(t, err)
		assert.EqualValues(t, 1, deleted)
		assert.Equal(t, []int{2, 3, 4}, logIds(t, hot), "logs inside the retention and unarchived logs survive")
	})

	t.Run("manual cleanup without routing keeps the target", func(t *testing.T) {
		hot, _ := seed(t, false)
		logHotRetentionDays = 0
		assert.Equal(t, now, LogCleanupTarget(now, now))
		remaining, err := CountOldLog(ctx, now)
		require.NoError(t, err)
		assert.EqualValues(t, 2, remaining)
		deleted, err := DeleteOldLogBatch(ctx, now, 100)
		require.NoError(t, err)
		assert.EqualValues(t, 2, deleted)
		assert.Equal(t, []int{3, 4}, logIds(t, hot), "unarchived logs survive the manual cleanup")
	})

	t.Run("an emptied archive stops the sync and every deletion", func(t *testing.T) {
		hot, cold := seed(t, true)
		require.NoError(t, cold.Where("1 = 1").Delete(&Log{}).Error)
		_, err := SyncColdLogs(ctx, now)
		require.ErrorContains(t, err, "does not hold synced log id 2")
		_, err = CleanupHotLogs(ctx, now)
		require.ErrorContains(t, err, "does not hold synced log id 2")
		_, err = DeleteOldLogBatch(ctx, now, 100)
		require.ErrorContains(t, err, "does not hold synced log id 2")
		assert.Equal(t, []int{1, 2, 3, 4}, logIds(t, hot))
		assert.Empty(t, logIds(t, cold), "nothing is copied over the gap")
	})
}

func TestSyncColdLogsStopsWhenProgressFallsBehindArchive(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	hot, cold := newSQLiteColdLogStores(t, 30, false)
	logColdSyncBatchSize = 2
	// Ids 1 to 4 are older than the archive scan margin before ids 5 and 6.
	createdAt := func(id int) int64 {
		if id <= 4 {
			return now - 3*testDay
		}
		return now - 1000
	}
	for id := 1; id <= 6; id++ {
		require.NoError(t, hot.Create(&Log{Id: id, CreatedAt: createdAt(id), RequestId: fmt.Sprintf("r%d", id)}).Error)
	}
	_, err := SyncColdLogs(ctx, now)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3, 4, 5, 6}, logIds(t, cold))
	setProgress := func(t *testing.T, syncedId int64, syncedCreatedAt int64) {
		require.NoError(t, hot.Model(&LogColdSyncState{}).Where("name = ?", logColdSyncStateName).
			Updates(map[string]any{"synced_id": syncedId, "synced_created_at": syncedCreatedAt}).Error)
	}

	// Ids 4 to 6 above the progress are more than one batch.
	setProgress(t, 3, createdAt(3))
	_, err = SyncColdLogs(ctx, now)
	require.ErrorContains(t, err, "need manual repair")
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6}, logIds(t, cold))

	setProgress(t, 0, 0)
	_, err = SyncColdLogs(ctx, now)
	require.ErrorContains(t, err, "need manual repair")
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6}, logIds(t, cold))

	// Only synced_id was reset: the old synced_created_at would hide ids 1 to 4
	// from a count bounded by created_at.
	setProgress(t, 0, createdAt(6))
	_, err = SyncColdLogs(ctx, now)
	require.ErrorContains(t, err, "holds 6 logs above synced id 0")
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6}, logIds(t, cold))

	// Ids 5 and 6 fit in one batch, but the hot cleanup already deleted id 6,
	// so its archived row is the only copy and is kept.
	setProgress(t, 4, createdAt(4))
	require.NoError(t, hot.Where("id = ?", 6).Delete(&Log{}).Error)
	_, err = SyncColdLogs(ctx, now)
	require.ErrorContains(t, err, "holds 2 logs above synced id 4 but the hot database keeps only 1 of them")
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6}, logIds(t, cold))
	state, _, err := coldLogSyncState(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 4, state.SyncedId)

	// With both still in the hot store they are deleted and rewritten.
	require.NoError(t, hot.Create(&Log{Id: 6, CreatedAt: createdAt(6), RequestId: "r6"}).Error)
	result, err := SyncColdLogs(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 6, result.SyncedId)
	assert.EqualValues(t, 2, result.Copied)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6}, logIds(t, cold))

	// A first run that wrote one batch without recording it is redone.
	require.NoError(t, cold.Where("id > ?", 2).Delete(&Log{}).Error)
	setProgress(t, 0, 0)
	result, err = SyncColdLogs(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 6, result.Copied)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6}, logIds(t, cold))
}

func TestHotLogCleanupChecksEachBatchInArchive(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	seed := func(t *testing.T, missing []int) *gorm.DB {
		hot, cold := newSQLiteColdLogStores(t, 30, true)
		logHotCleanupBatchSize = 2
		var logs []Log
		for id := 1; id <= 6; id++ {
			logs = append(logs, Log{Id: id, CreatedAt: now - 40*testDay + int64(id)})
		}
		require.NoError(t, hot.Create(&logs).Error)
		require.NoError(t, cold.Create(&logs).Error)
		require.NoError(t, hot.Create(&LogColdSyncState{Name: logColdSyncStateName, SyncedId: 6, SyncedCreatedAt: logs[5].CreatedAt}).Error)
		// The archive lost logs of the second batch but still holds the progress.
		require.NoError(t, cold.Where("id IN ?", missing).Delete(&Log{}).Error)
		return hot
	}

	t.Run("scheduled cleanup", func(t *testing.T) {
		hot := seed(t, []int{3, 4})
		deleted, err := CleanupHotLogs(ctx, now)
		require.ErrorContains(t, err, "holds 0 of the 2 hot logs with ids 3 to 4")
		assert.EqualValues(t, 2, deleted)
		assert.Equal(t, []int{3, 4, 5, 6}, logIds(t, hot))
	})

	t.Run("manual cleanup", func(t *testing.T) {
		hot := seed(t, []int{3, 4})
		deleted, err := DeleteOldLogBatch(ctx, now, 1)
		require.ErrorContains(t, err, "holds 0 of the 2 hot logs with ids 3 to 4")
		assert.EqualValues(t, 2, deleted)
		assert.Equal(t, []int{3, 4, 5, 6}, logIds(t, hot))
	})

	t.Run("one log missing keeps its whole batch", func(t *testing.T) {
		hot := seed(t, []int{3})
		deleted, err := CleanupHotLogs(ctx, now)
		require.ErrorContains(t, err, "holds 1 of the 2 hot logs with ids 3 to 4")
		assert.EqualValues(t, 2, deleted)
		assert.Equal(t, []int{3, 4, 5, 6}, logIds(t, hot))
	})
}

func TestColdLogStoreReconnectsInBackground(t *testing.T) {
	_, cold := newSQLiteColdLogStores(t, 30, false)
	logColdDB.Store(nil)
	attempts := make(chan struct{}, 10)
	release := make(chan struct{})
	logColdReconnect = func() (*gorm.DB, error) {
		attempts <- struct{}{}
		<-release
		return nil, errors.New("archive still down")
	}

	// Searches fail at once while the connect is still dialing, and only the
	// first one in the interval starts it.
	for range 5 {
		_, err := coldLogStore()
		require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	}
	select {
	case <-attempts:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no background connect was started")
	}
	close(release)
	_, err := coldLogStore()
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	assert.Empty(t, attempts, "one connect per interval")

	// After the interval the next search tries again, and once the connect
	// installs the archive searches use it.
	logColdReconnectAt.Add(-int64(logColdReconnectInterval))
	logColdReconnect = func() (*gorm.DB, error) {
		logColdDB.Store(cold)
		attempts <- struct{}{}
		return cold, nil
	}
	_, err = coldLogStore()
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	select {
	case <-attempts:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no background connect was started after the interval")
	}
	store, err := coldLogStore()
	require.NoError(t, err)
	assert.True(t, store.cold)

	// Without a configured archive nothing is connected.
	logColdConfigured = false
	logColdDB.Store(nil)
	logColdReconnectAt.Store(0)
	_, err = coldLogStore()
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	assert.Zero(t, logColdReconnectAt.Load())
}

func TestColdLogArchiveUnavailable(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	hot, cold := newSQLiteColdLogStores(t, 30, true)
	previousMaster := common.IsMasterNode
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	common.IsMasterNode = true
	t.Setenv("LOG_SQL_DSN", "")
	t.Setenv("LOG_COLD_SQL_DSN", "clickhouse://127.0.0.1:1/default?dial_timeout=1s")
	t.Setenv("LOG_HOT_RETENTION_DAYS", "30")
	t.Setenv("LOG_HOT_CLEANUP_ENABLED", "true")
	t.Setenv("LOG_HOT_CLEANUP_BATCH_SIZE", "1")

	// An unreachable archive does not fail startup, but it stays not connected.
	require.NoError(t, InitLogColdDB())
	assert.True(t, ColdLogSyncEnabled())
	assert.True(t, HotLogCleanupEnabled())
	assert.Nil(t, logColdDB.Load())

	logs := []Log{
		{Id: 1, UserId: 1, CreatedAt: now - 40*testDay, Type: LogTypeConsume, Quota: 7, RequestId: "old"},
		{Id: 2, UserId: 1, CreatedAt: now - 1000, Type: LogTypeConsume, Quota: 3, RequestId: "recent"},
	}
	require.NoError(t, hot.Create(&logs).Error)
	require.NoError(t, cold.Create(logs[:1]).Error)
	require.NoError(t, hot.Create(&LogColdSyncState{Name: logColdSyncStateName, SyncedId: 1, SyncedCreatedAt: logs[0].CreatedAt}).Error)

	_, err := CleanupHotLogs(ctx, now)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	_, err = CountOldLog(ctx, now)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	_, err = DeleteOldLogBatch(ctx, now, 100)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	assert.Equal(t, []int{1, 2}, logIds(t, hot), "no hot log is deleted while the archive is not connected")

	oldSearch := LogFilter{StartTimestamp: now - 60*testDay}
	_, _, err = GetAllLogs(oldSearch, 0, 10)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	_, _, err = GetAllLogs(LogFilter{RequestId: "missing"}, 0, 10)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable, "a request id fallback fails instead of reporting no match")
	// Entries a user reaches report the generic failure, not the internal one.
	_, _, err = GetUserLogs(1, oldSearch, 0, 10)
	require.EqualError(t, err, "Failed to query logs")
	err = StreamUserLogs(ctx, 1, oldSearch, func(*Log) error { return nil })
	require.EqualError(t, err, "Failed to query logs")
	err = StreamUserLogs(ctx, 1, LogFilter{RequestId: "missing"}, func(*Log) error { return nil })
	require.EqualError(t, err, "Failed to query logs")
	_, err = SumUsedQuota(LogTypeUnknown, oldSearch.StartTimestamp, 0, "", "", "", 0, "", false)
	require.EqualError(t, err, "Failed to query statistics")
	require.ErrorIs(t, StreamAllLogs(ctx, oldSearch, common.RoleRootUser, func(*Log) error { return nil }), errLogColdArchiveUnavailable)
	recent, _, err := GetAllLogs(LogFilter{}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"recent", "old"}, requestIdsOf(recent), "hot searches keep working")

	_, err = SyncColdLogs(ctx, now)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)

	// Once the archive is connected the sync and the cleanup resume.
	logColdDB.Store(cold)
	result, err := SyncColdLogs(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 2, result.SyncedId)
	assert.Equal(t, []int{1, 2}, logIds(t, cold))
	deleted, err := CleanupHotLogs(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	assert.Equal(t, []int{2}, logIds(t, hot))

	t.Setenv("LOG_SQL_DSN", "clickhouse://127.0.0.1:1/default")
	require.ErrorContains(t, InitLogColdDB(), "cannot be used together with LOG_SQL_DSN")
	t.Setenv("LOG_SQL_DSN", "")
	t.Setenv("LOG_COLD_SQL_DSN", "postgres://127.0.0.1:1/logs")
	require.ErrorContains(t, InitLogColdDB(), "only supports ClickHouse")
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
		if coldDB := logColdDB.Load(); coldDB != nil {
			assert.NoError(t, closeDB(coldDB))
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
	require.NoError(t, closeDB(logColdDB.Load()))
	logColdDB.Store(nil)
	require.NoError(t, InitLogColdDB())
	logHotCleanupPause = 0
	coldDB := logColdDB.Load()
	require.NotNil(t, coldDB)

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
	assert.EqualValues(t, 1000, result.LagSeconds)
	var archived []Log
	require.NoError(t, coldDB.Order("id").Find(&archived).Error)
	require.Len(t, archived, 3)
	assert.Equal(t, "old-1", archived[0].RequestId)
	assert.True(t, archived[0].IsStream)
	assert.Equal(t, "vip", archived[0].Group)

	// A leftover row above the progress is removed with a ClickHouse mutation.
	require.NoError(t, coldDB.Create(&Log{Id: archived[2].Id + 1, CreatedAt: now - 10, RequestId: "fresh"}).Error)
	result, err = SyncColdLogs(ctx, now+400)
	require.NoError(t, err)
	assert.EqualValues(t, 1, result.Copied)
	var coldCount int64
	require.NoError(t, coldDB.Model(&Log{}).Count(&coldCount).Error)
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

	// The manual cleanup cannot reach into the hot retention.
	remaining, err := CountOldLog(ctx, now+1)
	require.NoError(t, err)
	assert.Zero(t, remaining)
	deleted, err = DeleteOldLogBatch(ctx, now+1, 1)
	require.NoError(t, err)
	assert.Zero(t, deleted)
	var hotCount int64
	require.NoError(t, hot.Model(&Log{}).Count(&hotCount).Error)
	assert.EqualValues(t, 2, hotCount)

	// Make the plain MergeTree archive deduplicate inserts like a replicated or
	// cloud table, and show identical blocks are dropped unless the archive
	// write turns deduplication off.
	type dedupProbe struct{ Id int64 }
	require.NoError(t, coldDB.Exec("CREATE TABLE dedup_probe (id Int64) ENGINE = MergeTree ORDER BY id SETTINGS non_replicated_deduplication_window = 100").Error)
	probeCount := func() int64 {
		var count int64
		require.NoError(t, coldDB.Table("dedup_probe").Count(&count).Error)
		return count
	}
	for range 2 {
		require.NoError(t, coldDB.Table("dedup_probe").Create(&[]dedupProbe{{Id: 1}, {Id: 2}}).Error)
	}
	assert.EqualValues(t, 2, probeCount(), "the second identical block is deduplicated")
	require.NoError(t, coldDB.WithContext(coldLogInsertContext(ctx)).Table("dedup_probe").Create(&[]dedupProbe{{Id: 1}, {Id: 2}}).Error)
	assert.EqualValues(t, 4, probeCount(), "the archive write context turns deduplication off")

	// A batch the server stored while the client saw an error is deleted and
	// rewritten as the same block; the rewrite must not be dropped. A plain
	// MergeTree forgets a block's hash once its part is dropped, so this cannot
	// replay a replicated table keeping the hash; the probe above shows the
	// archive write turns deduplication off on the server.
	require.NoError(t, coldDB.Exec("ALTER TABLE logs MODIFY SETTING non_replicated_deduplication_window = 1000").Error)
	require.NoError(t, hot.Create(&[]Log{
		{UserId: 3, Username: "carol", CreatedAt: now - 1000, Type: LogTypeConsume, RequestId: "late-1"},
		{UserId: 3, Username: "carol", CreatedAt: now - 1000, Type: LogTypeConsume, RequestId: "late-2"},
	}).Error)
	state, _, err := coldLogSyncState(ctx)
	require.NoError(t, err)
	var batch []*Log
	require.NoError(t, hot.Where("id > ?", state.SyncedId).Order("id").Limit(2).Find(&batch).Error)
	require.Len(t, batch, 2)
	for range 2 {
		require.NoError(t, coldDB.Create(&batch).Error)
	}
	require.NoError(t, coldDB.Model(&Log{}).Count(&coldCount).Error)
	assert.EqualValues(t, 6, coldCount, "the archive now deduplicates an identical batch")
	result, err = SyncColdLogs(ctx, now+400)
	require.NoError(t, err)
	assert.EqualValues(t, 2, result.Copied)
	require.NoError(t, coldDB.Model(&Log{}).Count(&coldCount).Error)
	assert.EqualValues(t, 6, coldCount)

	// A lost connection fails cleanups and archive searches until the sync
	// connects the archive again.
	require.NoError(t, closeDB(coldDB))
	logColdDB.Store(nil)
	_, err = CleanupHotLogs(ctx, now+400*testDay)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	_, _, err = GetAllLogs(LogFilter{StartTimestamp: oldStart}, 0, 10)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	// A slave node runs no sync; a search reconnects the archive in the
	// background without creating the table.
	common.IsMasterNode = false
	logColdReconnect = connectLogColdDB
	logColdReconnectAt.Store(0)
	_, _, err = GetAllLogs(LogFilter{StartTimestamp: oldStart}, 0, 10)
	require.ErrorIs(t, err, errLogColdArchiveUnavailable)
	require.Eventually(t, func() bool { return logColdDB.Load() != nil }, 30*time.Second, 50*time.Millisecond)
	logs, total, err = GetAllLogs(LogFilter{StartTimestamp: oldStart, ModelName: "gpt_4%", Group: "vip"}, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, []string{"old-1"}, requestIdsOf(logs))
	common.IsMasterNode = true

	// On the master the sync reconnects it as well.
	require.NoError(t, closeDB(logColdDB.Load()))
	logColdDB.Store(nil)
	_, err = SyncColdLogs(ctx, now+400)
	require.NoError(t, err)
	coldDB = logColdDB.Load()
	require.NotNil(t, coldDB)

	// A progress moved back by more than one batch stops the sync without
	// deleting archived logs.
	state, _, err = coldLogSyncState(ctx)
	require.NoError(t, err)
	var rewound Log
	require.NoError(t, hot.Where("id < ?", state.SyncedId-2).Order("id desc").First(&rewound).Error)
	require.NoError(t, hot.Model(&LogColdSyncState{}).Where("name = ?", logColdSyncStateName).
		Updates(map[string]any{"synced_id": rewound.Id, "synced_created_at": rewound.CreatedAt}).Error)
	_, err = SyncColdLogs(ctx, now+400)
	require.ErrorContains(t, err, "need manual repair")
	require.NoError(t, coldDB.Model(&Log{}).Count(&coldCount).Error)
	assert.EqualValues(t, 6, coldCount)
	require.NoError(t, hot.Model(&LogColdSyncState{}).Where("name = ?", logColdSyncStateName).
		Updates(map[string]any{"synced_id": state.SyncedId, "synced_created_at": state.SyncedCreatedAt}).Error)
	result, err = SyncColdLogs(ctx, now+400)
	require.NoError(t, err)
	assert.Zero(t, result.Copied)

	// A hot log missing from the archive keeps its whole batch in the hot store.
	var firstHot Log
	require.NoError(t, hot.Order("id").First(&firstHot).Error)
	require.NoError(t, coldDB.Exec("ALTER TABLE logs DELETE WHERE id = ? SETTINGS mutations_sync = 1", firstHot.Id).Error)
	deleted, err = CleanupHotLogs(ctx, now+400*testDay)
	require.ErrorContains(t, err, "they are kept and the cleanup stops")
	assert.Zero(t, deleted)
	// Without routing the manual cleanup target is not held back to the hot
	// retention, so it reaches these recent logs.
	logHotRetentionDays = 0
	deleted, err = DeleteOldLogBatch(ctx, now+400*testDay, 100)
	logHotRetentionDays = 30
	require.ErrorContains(t, err, "they are kept and the cleanup stops")
	assert.Zero(t, deleted)
	require.NoError(t, hot.Model(&Log{}).Count(&hotCount).Error)
	assert.EqualValues(t, 4, hotCount)

	// An emptied archive stops the sync and every hot deletion.
	require.NoError(t, coldDB.Exec("TRUNCATE TABLE logs").Error)
	_, err = SyncColdLogs(ctx, now+400)
	require.ErrorContains(t, err, "does not hold synced log id")
	_, err = CleanupHotLogs(ctx, now+400*testDay)
	require.ErrorContains(t, err, "does not hold synced log id")
	_, err = DeleteOldLogBatch(ctx, now+400*testDay, 100)
	require.ErrorContains(t, err, "does not hold synced log id")
	require.NoError(t, hot.Model(&Log{}).Count(&hotCount).Error)
	assert.EqualValues(t, 4, hotCount)
}
