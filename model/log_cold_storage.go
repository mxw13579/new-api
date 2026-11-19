package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
)

// Cold log storage keeps every log in a ClickHouse archive while the hot log
// database keeps only recent ones. Three switches go live one after another:
//
//   - LOG_COLD_SQL_DSN copies hot logs to the archive.
//   - LOG_HOT_RETENTION_DAYS > 0 sends searches starting before the hot
//     retention to the archive.
//   - LOG_HOT_CLEANUP_ENABLED=true deletes hot logs past the retention that
//     the archive already holds.
//
// With none of them set nothing here runs and LOG_COLD_DB stays nil.

// LOG_COLD_DB is the cold ClickHouse archive, or nil when none is configured.
var LOG_COLD_DB *gorm.DB

var (
	logHotRetentionDays    int
	logHotCleanupEnabled   bool
	logColdSyncBatchSize   = 10000
	logHotCleanupBatchSize = 5000
	logHotCleanupPause     = 200 * time.Millisecond
)

const (
	SystemTaskTypeColdLogSync   = "cold_log_sync"
	SystemTaskTypeHotLogCleanup = "hot_log_cleanup"

	// logColdSyncSafetySeconds keeps the sync behind the newest logs. A
	// PostgreSQL id becomes visible when its insert commits, not in id order, so
	// only ids up to the newest log at least this old are copied.
	logColdSyncSafetySeconds = 300
	logColdSyncStateName     = "logs"
)

// LogColdSyncState stores, in the hot log database, the highest log id copied
// to the cold archive. Every hot log with a smaller or equal id is archived.
type LogColdSyncState struct {
	Name      string `gorm:"type:varchar(32);primaryKey"`
	SyncedId  int64  `gorm:"not null;default:0"`
	UpdatedAt int64  `gorm:"bigint"`
}

func (LogColdSyncState) TableName() string { return "log_cold_sync_states" }

// ColdLogSyncResult reports one sync run.
type ColdLogSyncResult struct {
	SyncedId int64 `json:"synced_id"`
	Copied   int64 `json:"copied"`
	Lag      int64 `json:"lag"`
}

// InitLogColdDB connects the cold archive named by LOG_COLD_SQL_DSN. Call it
// after InitLogDB. The master node creates the archive table and the sync
// progress table.
func InitLogColdDB() error {
	dsn := os.Getenv("LOG_COLD_SQL_DSN")
	if dsn == "" {
		return nil
	}
	if os.Getenv("LOG_SQL_DSN") != "" {
		return errors.New("LOG_COLD_SQL_DSN cannot be used together with LOG_SQL_DSN: the cold log archive keeps the hot logs in the main database")
	}
	if !isClickHouseDSN(dsn) {
		return errors.New("LOG_COLD_SQL_DSN only supports ClickHouse")
	}
	db, err := gorm.Open(clickhouse.Open(normalizeClickHouseDSN(dsn)), newGormConfig(false))
	if err != nil {
		return err
	}
	if common.DebugEnabled {
		db = db.Debug()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
	sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))
	sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

	logHotRetentionDays = max(common.GetEnvOrDefault("LOG_HOT_RETENTION_DAYS", 0), 0)
	logHotCleanupEnabled = common.GetEnvOrDefaultBool("LOG_HOT_CLEANUP_ENABLED", false)
	logColdSyncBatchSize = max(common.GetEnvOrDefault("LOG_COLD_SYNC_BATCH_SIZE", logColdSyncBatchSize), 1)
	logHotCleanupBatchSize = max(common.GetEnvOrDefault("LOG_HOT_CLEANUP_BATCH_SIZE", logHotCleanupBatchSize), 1)
	if logHotCleanupEnabled && logHotRetentionDays == 0 {
		common.SysError(common.LogText("LOG_HOT_CLEANUP_ENABLED is ignored because LOG_HOT_RETENTION_DAYS is not set"))
		logHotCleanupEnabled = false
	}

	if common.IsMasterNode {
		if err := db.Exec(clickHouseLogCreateTableSQL(0)).Error; err != nil {
			return err
		}
		if err := LOG_DB.AutoMigrate(&LogColdSyncState{}); err != nil {
			return err
		}
	}
	LOG_COLD_DB = db
	common.SysLog(common.LogText("using ClickHouse as cold log archive: hot retention %d days, hot cleanup %t", logHotRetentionDays, logHotCleanupEnabled))
	return nil
}

func coldLogStore() logStore {
	return logStore{db: LOG_COLD_DB, clickHouse: LOG_COLD_DB.Dialector.Name() == "clickhouse", groupCol: "`group`", cold: true}
}

func coldLogRoutingEnabled() bool {
	return LOG_COLD_DB != nil && logHotRetentionDays > 0
}

// logStoreForStart picks the store for a search starting at startTimestamp,
// the start the caller gave. A start before the hot retention reads the cold
// archive; any other search, including one without a start, reads the hot
// store. Results of the two stores are never merged.
func logStoreForStart(startTimestamp int64) logStore {
	if !coldLogRoutingEnabled() || startTimestamp == 0 {
		return hotLogStore()
	}
	if startTimestamp >= common.GetTimestamp()-int64(logHotRetentionDays)*24*60*60 {
		return hotLogStore()
	}
	return coldLogStore()
}

// ColdLogSyncEnabled reports whether hot logs are copied to a cold archive.
func ColdLogSyncEnabled() bool {
	return LOG_COLD_DB != nil
}

// HotLogCleanupEnabled reports whether archived hot logs past the retention
// are deleted.
func HotLogCleanupEnabled() bool {
	return coldLogRoutingEnabled() && logHotCleanupEnabled
}

// coldLogSyncedId returns the highest log id already copied to the archive and
// whether a sync progress row exists.
func coldLogSyncedId(ctx context.Context) (int64, bool, error) {
	var states []LogColdSyncState
	if err := LOG_DB.WithContext(ctx).Where("name = ?", logColdSyncStateName).Limit(1).Find(&states).Error; err != nil {
		return 0, false, err
	}
	if len(states) == 0 {
		return 0, false, nil
	}
	return states[0].SyncedId, true, nil
}

// SyncColdLogs copies hot logs to the cold archive in id order, batch after
// batch, up to the newest log created at least logColdSyncSafetySeconds before
// now. The progress advances only after a batch is written. Archive rows above
// the progress are left over from a run that failed after writing, and they
// are deleted before copying so a rerun neither duplicates nor skips a log.
func SyncColdLogs(ctx context.Context, now int64) (ColdLogSyncResult, error) {
	result := ColdLogSyncResult{}
	if LOG_COLD_DB == nil {
		return result, nil
	}
	cold := coldLogStore()
	syncedId, found, err := coldLogSyncedId(ctx)
	if err != nil {
		return result, err
	}
	result.SyncedId = syncedId

	var leftover int64
	if err := cold.db.WithContext(ctx).Model(&Log{}).Where("id > ?", syncedId).Count(&leftover).Error; err != nil {
		return result, err
	}
	if !found {
		// Without a progress row the archive must be empty: deleting rows above
		// id 0 would destroy archived logs the hot store may no longer have.
		if leftover > 0 {
			return result, fmt.Errorf("cold log archive holds %d logs but the hot database has no sync progress in %s", leftover, LogColdSyncState{}.TableName())
		}
		state := LogColdSyncState{Name: logColdSyncStateName, UpdatedAt: now}
		if err := LOG_DB.WithContext(ctx).Create(&state).Error; err != nil {
			return result, err
		}
	}
	if leftover > 0 {
		common.SysLog(common.LogText("cold log sync: deleting %d archived logs above synced id %d left by a failed run", leftover, syncedId))
		if cold.clickHouse {
			err = cold.db.WithContext(ctx).Exec("ALTER TABLE logs DELETE WHERE id > ? SETTINGS mutations_sync = 1", syncedId).Error
		} else {
			err = cold.db.WithContext(ctx).Where("id > ?", syncedId).Delete(&Log{}).Error
		}
		if err != nil {
			return result, err
		}
	}

	var bound int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Select("COALESCE(MAX(id), 0)").
		Where("id > ? AND created_at <= ?", syncedId, now-logColdSyncSafetySeconds).Scan(&bound).Error; err != nil {
		return result, err
	}
	for syncedId < bound {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var logs []*Log
		if err := LOG_DB.WithContext(ctx).Where("id > ? AND id <= ?", syncedId, bound).Order("id").Limit(logColdSyncBatchSize).Find(&logs).Error; err != nil {
			return result, err
		}
		if len(logs) == 0 {
			break
		}
		if err := cold.db.WithContext(ctx).Create(&logs).Error; err != nil {
			return result, err
		}
		lastId := int64(logs[len(logs)-1].Id)
		update := LOG_DB.WithContext(ctx).Model(&LogColdSyncState{}).
			Where("name = ? AND synced_id = ?", logColdSyncStateName, syncedId).
			Updates(map[string]any{"synced_id": lastId, "updated_at": now})
		if update.Error != nil {
			return result, update.Error
		}
		if update.RowsAffected != 1 {
			return result, errors.New("cold log sync progress changed during the run")
		}
		syncedId = lastId
		result.SyncedId = syncedId
		result.Copied += int64(len(logs))
	}

	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("id > ?", syncedId).Count(&result.Lag).Error; err != nil {
		return result, err
	}
	common.SysLog(common.LogText("cold log sync: synced id %d, copied %d, behind %d", result.SyncedId, result.Copied, result.Lag))
	return result, nil
}

// CleanupHotLogs deletes, in small batches, hot logs created more than the hot
// retention plus one day before now that the cold archive already holds. The
// extra day keeps every log a hot search can reach in the hot store.
func CleanupHotLogs(ctx context.Context, now int64) (int64, error) {
	if !HotLogCleanupEnabled() {
		return 0, nil
	}
	syncedId, _, err := coldLogSyncedId(ctx)
	if err != nil {
		return 0, err
	}
	before := now - int64(logHotRetentionDays+1)*24*60*60
	var deleted int64
	for {
		count, err := deleteSyncedHotLogBatch(ctx, before, syncedId, logHotCleanupBatchSize)
		deleted += count
		if err != nil {
			return deleted, err
		}
		if count < int64(logHotCleanupBatchSize) {
			break
		}
		select {
		case <-ctx.Done():
			return deleted, ctx.Err()
		case <-time.After(logHotCleanupPause):
		}
	}
	common.SysLog(common.LogText("hot log cleanup: deleted %d logs created before %d up to synced id %d", deleted, before, syncedId))
	return deleted, nil
}

// deleteSyncedHotLogBatch deletes up to limit of the oldest hot logs created
// before the given time with an id no greater than syncedId. Ids are read
// first so the delete works on every database; DELETE ... LIMIT is MySQL-only
// and GORM drops Limit from deletes on PostgreSQL and SQLite.
func deleteSyncedHotLogBatch(ctx context.Context, before int64, syncedId int64, limit int) (int64, error) {
	var ids []int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ? AND id <= ?", before, syncedId).
		Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := LOG_DB.WithContext(ctx).Where("id IN ?", ids).Delete(&Log{})
	return result.RowsAffected, result.Error
}
