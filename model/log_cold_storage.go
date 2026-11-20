package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	clickhousego "github.com/ClickHouse/clickhouse-go/v2"
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
// With none of them set nothing here runs.
//
// Configuring the archive and reaching it are kept apart. logColdConfigured is
// set once at startup and decides every behavior above; logColdDB holds the
// connection, which stays nil while the archive cannot be reached. Until it is
// connected the sync retries it every run, a search that needs the archive
// starts a background connect at most once every logColdReconnectInterval, hot
// deletions refuse to run, and searches that belong on the archive fail.

var (
	// logColdConfigured reports that LOG_COLD_SQL_DSN is set. It is written at
	// startup only.
	logColdConfigured bool
	logColdDSN        string
	// logColdDB is the connected cold archive. Request goroutines read it while
	// the sync installs it, so it is only touched through Load and Store.
	logColdDB atomic.Pointer[gorm.DB]
	// logColdConnectMu keeps two connects from opening two pools.
	logColdConnectMu sync.Mutex
	// logColdReconnectAt is when a search last started a background connect, in
	// Unix nanoseconds. A compare-and-swap on it admits one search per interval,
	// so searches never wait on each other or on the dial.
	logColdReconnectAt atomic.Int64
	// logColdReconnect connects the archive for a search; tests replace it.
	logColdReconnect = connectLogColdDB

	errLogColdArchiveUnavailable = errors.New("cold log archive is configured but not connected")
)

const (
	defaultLogColdSyncBatchSize   = 10000
	defaultLogHotCleanupBatchSize = 5000
	// maxLogHotCleanupBatchSize keeps the archive check of a cleanup batch, an
	// id IN (...) list, within ClickHouse's default 256 KiB max_query_size.
	maxLogHotCleanupBatchSize = 10000
	logColdReconnectInterval  = 30 * time.Second
)

var (
	logHotRetentionDays    int
	logHotCleanupEnabled   bool
	logColdSyncBatchSize   = defaultLogColdSyncBatchSize
	logHotCleanupBatchSize = defaultLogHotCleanupBatchSize
	logHotCleanupPause     = 200 * time.Millisecond
)

const (
	SystemTaskTypeColdLogSync   = "cold_log_sync"
	SystemTaskTypeHotLogCleanup = "hot_log_cleanup"

	// logColdSyncSafetySeconds keeps the sync behind the newest logs. A
	// PostgreSQL id becomes visible when its insert commits, not in id order, so
	// only ids up to the newest log at least this old are copied.
	logColdSyncSafetySeconds = 300
	// logColdArchiveScanMarginSeconds bounds archive checks by created_at, the
	// leading ClickHouse sort key, instead of scanning the whole id column. A
	// log's created_at is taken right before its insert, so a log with a larger
	// id than the synced one is never created this much earlier.
	logColdArchiveScanMarginSeconds = 24 * 60 * 60
	logColdSyncStateName            = "logs"
	logDaySeconds                   = 24 * 60 * 60
)

// LogColdSyncState stores, in the hot log database, the highest log id copied
// to the cold archive and that log's created_at. Every hot log with a smaller
// or equal id is archived.
type LogColdSyncState struct {
	Name            string `gorm:"type:varchar(32);primaryKey"`
	SyncedId        int64  `gorm:"not null;default:0"`
	SyncedCreatedAt int64  `gorm:"bigint;not null;default:0"`
	UpdatedAt       int64  `gorm:"bigint"`
}

func (LogColdSyncState) TableName() string { return "log_cold_sync_states" }

// ColdLogSyncResult reports one sync run. Lag counts the hot logs not yet
// copied; LagSeconds is how old the newest copied log is.
type ColdLogSyncResult struct {
	SyncedId        int64 `json:"synced_id"`
	SyncedCreatedAt int64 `json:"synced_created_at"`
	Copied          int64 `json:"copied"`
	Lag             int64 `json:"lag"`
	LagSeconds      int64 `json:"lag_seconds"`
}

// InitLogColdDB configures the cold archive named by LOG_COLD_SQL_DSN. Call it
// after InitLogDB. An invalid configuration fails startup; an archive that
// cannot be reached or set up is logged and left to the sync to connect. The
// master node creates the sync progress table and the archive table.
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
	if common.IsMasterNode {
		if err := LOG_DB.AutoMigrate(&LogColdSyncState{}); err != nil {
			return err
		}
	}
	configureLogColdStorage(true, nil)
	logColdDSN = dsn
	common.SysLog(common.LogText("using ClickHouse as cold log archive: hot retention %d days, hot cleanup %t", logHotRetentionDays, logHotCleanupEnabled))
	if _, err := connectLogColdDB(); err != nil {
		common.SysError(common.LogText("cold log archive is unavailable; it is reconnected in the background, hot log cleanup and archive searches fail until then: %s", err.Error()))
	}
	return nil
}

// connectLogColdDB returns the cold archive connection, opening it from
// LOG_COLD_SQL_DSN when it is not open yet. On the master node the archive
// table is created first; a connection whose table cannot be created is closed
// and not installed, so the next sync run tries again.
func connectLogColdDB() (*gorm.DB, error) {
	logColdConnectMu.Lock()
	defer logColdConnectMu.Unlock()
	if db := logColdDB.Load(); db != nil {
		return db, nil
	}
	db, err := gorm.Open(clickhouse.Open(normalizeClickHouseDSN(logColdDSN)), newGormConfig(false))
	if err != nil {
		// gorm.Open keeps the pool it opened when the ping fails.
		if db != nil && db.ConnPool != nil {
			_ = closeDB(db)
		}
		return nil, err
	}
	if common.DebugEnabled {
		db = db.Debug()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
	sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))
	sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))
	if common.IsMasterNode {
		if err := db.Exec(clickHouseLogCreateTableSQL(0)).Error; err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
	}
	logColdDB.Store(db)
	return db, nil
}

// UseLogColdDB installs db as the connected cold log archive and reads the
// cold storage settings from the environment. A nil db turns cold storage off.
func UseLogColdDB(db *gorm.DB) {
	configureLogColdStorage(db != nil, db)
}

// configureLogColdStorage records whether a cold archive is configured,
// installs its connection (nil while it is not reachable), and reads the cold
// storage settings from the environment.
func configureLogColdStorage(configured bool, db *gorm.DB) {
	logColdConfigured = configured
	logColdDB.Store(db)
	logHotRetentionDays = max(common.GetEnvOrDefault("LOG_HOT_RETENTION_DAYS", 0), 0)
	logHotCleanupEnabled = common.GetEnvOrDefaultBool("LOG_HOT_CLEANUP_ENABLED", false)
	logColdSyncBatchSize = max(common.GetEnvOrDefault("LOG_COLD_SYNC_BATCH_SIZE", defaultLogColdSyncBatchSize), 1)
	logHotCleanupBatchSize = max(common.GetEnvOrDefault("LOG_HOT_CLEANUP_BATCH_SIZE", defaultLogHotCleanupBatchSize), 1)
	if configured && logHotCleanupEnabled && logHotRetentionDays == 0 {
		common.SysError(common.LogText("LOG_HOT_CLEANUP_ENABLED is ignored because LOG_HOT_RETENTION_DAYS is not set"))
		logHotCleanupEnabled = false
	}
}

// coldLogStore returns the cold archive, or errLogColdArchiveUnavailable while
// the configured archive is not connected. A slave node runs no sync, so while
// the archive is not connected the first call in each logColdReconnectInterval
// starts a connect in the background; the call itself still fails at once, and
// calls after the connect succeeds use the archive.
func coldLogStore() (logStore, error) {
	db := logColdDB.Load()
	if db == nil {
		now := time.Now().UnixNano()
		last := logColdReconnectAt.Load()
		if logColdConfigured && now-last >= int64(logColdReconnectInterval) && logColdReconnectAt.CompareAndSwap(last, now) {
			connect := logColdReconnect
			go func() {
				if _, err := connect(); err != nil {
					common.SysError(common.LogText("cold log archive reconnect failed: %s", err.Error()))
					return
				}
				common.SysLog(common.LogText("cold log archive reconnected"))
			}()
		}
		return logStore{}, errLogColdArchiveUnavailable
	}
	return logStore{db: db, clickHouse: db.Dialector.Name() == "clickhouse", groupCol: "`group`", cold: true}, nil
}

func coldLogRoutingEnabled() bool {
	return logColdConfigured && logHotRetentionDays > 0
}

// hotLogRetentionBoundary is the oldest created_at the hot store keeps while
// searches are routed: the hot retention plus one day, so every log a hot
// search can reach stays in the hot store.
func hotLogRetentionBoundary(now int64) int64 {
	return now - int64(logHotRetentionDays+1)*logDaySeconds
}

// logStoreForStart picks the store for a search starting at startTimestamp,
// the start the caller gave. A start before the hot retention reads the cold
// archive; any other search, including one without a start, reads the hot
// store. Results of the two stores are never merged, and a search for the
// archive fails rather than reading the hot store while the archive is not
// connected.
func logStoreForStart(startTimestamp int64) (logStore, error) {
	if !coldLogRoutingEnabled() || startTimestamp == 0 {
		return hotLogStore(), nil
	}
	if startTimestamp >= common.GetTimestamp()-int64(logHotRetentionDays)*logDaySeconds {
		return hotLogStore(), nil
	}
	return coldLogStore()
}

// ColdLogSyncEnabled reports whether hot logs are copied to a cold archive.
// It stays true while the configured archive is not connected, so the sync
// keeps trying to connect it.
func ColdLogSyncEnabled() bool {
	return logColdConfigured
}

// HotLogCleanupEnabled reports whether archived hot logs past the retention
// are deleted.
func HotLogCleanupEnabled() bool {
	return coldLogRoutingEnabled() && logHotCleanupEnabled
}

// LogCleanupTarget returns the cutoff the manual log cleanup actually uses.
// While searches are routed to the cold archive a later target is moved back
// to the hot retention boundary, because a search inside the retention reads
// only the hot store.
func LogCleanupTarget(targetTimestamp int64, now int64) int64 {
	if !coldLogRoutingEnabled() {
		return targetTimestamp
	}
	return min(targetTimestamp, hotLogRetentionBoundary(now))
}

// coldLogSyncState returns the sync progress and whether its row exists.
func coldLogSyncState(ctx context.Context) (LogColdSyncState, bool, error) {
	var states []LogColdSyncState
	if err := LOG_DB.WithContext(ctx).Where("name = ?", logColdSyncStateName).Limit(1).Find(&states).Error; err != nil {
		return LogColdSyncState{}, false, err
	}
	if len(states) == 0 {
		return LogColdSyncState{}, false, nil
	}
	return states[0], true, nil
}

// verifyColdArchiveHoldsProgress fails unless the archive still holds the
// synced log. An archive that was emptied, dropped, or swapped for another one
// must stop both the sync and every hot deletion, or hot logs would be deleted
// while their only other copy is gone.
func verifyColdArchiveHoldsProgress(ctx context.Context, cold logStore, state LogColdSyncState) error {
	if state.SyncedId == 0 {
		return nil
	}
	var count int64
	if err := cold.db.WithContext(ctx).Model(&Log{}).
		Where("created_at = ? AND id = ?", state.SyncedCreatedAt, state.SyncedId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("cold log archive does not hold synced log id %d; nothing is copied or deleted until the archive and %s agree", state.SyncedId, LogColdSyncState{}.TableName())
	}
	return nil
}

// coldLogInsertContext turns off ClickHouse insert deduplication for archive
// writes. A batch rewritten after a failed run carries the same block as the
// first write; a replicated or cloud table would otherwise drop it as a
// duplicate even though the first copy was deleted.
func coldLogInsertContext(ctx context.Context) context.Context {
	return clickhousego.Context(ctx, clickhousego.WithSettings(clickhousego.Settings{"insert_deduplicate": 0}))
}

// SyncColdLogs copies hot logs to the cold archive in id order, batch after
// batch, up to the newest log created at least logColdSyncSafetySeconds before
// now. A batch advances the progress only after the archive is checked to hold
// exactly its rows. Archive rows above the progress are left over from a run
// that failed after writing; they are deleted before copying, unless another
// run moved the progress meanwhile, so a rerun neither duplicates nor skips a
// log. A failed run leaves at most one batch above the progress, and the hot
// store still holds every log of that batch, so more leftover rows than that,
// or a leftover log the hot store no longer has, mean the progress went back
// and the sync stops instead of deleting archived logs. A run first connects
// the archive when it is not connected yet.
func SyncColdLogs(ctx context.Context, now int64) (ColdLogSyncResult, error) {
	result := ColdLogSyncResult{}
	if !logColdConfigured {
		return result, nil
	}
	if _, err := connectLogColdDB(); err != nil {
		return result, fmt.Errorf("%w: %w", errLogColdArchiveUnavailable, err)
	}
	cold, err := coldLogStore()
	if err != nil {
		return result, err
	}
	state, found, err := coldLogSyncState(ctx)
	if err != nil {
		return result, err
	}
	if !found {
		// Without a progress row the archive must be empty: deleting rows above
		// id 0 would destroy archived logs the hot store may no longer have.
		var archived int64
		if err := cold.db.WithContext(ctx).Model(&Log{}).Count(&archived).Error; err != nil {
			return result, err
		}
		if archived > 0 {
			return result, fmt.Errorf("cold log archive holds %d logs but the hot database has no sync progress in %s", archived, LogColdSyncState{}.TableName())
		}
		state = LogColdSyncState{Name: logColdSyncStateName, UpdatedAt: now}
		if err := LOG_DB.WithContext(ctx).Create(&state).Error; err != nil {
			return result, err
		}
	}
	result.SyncedId, result.SyncedCreatedAt = state.SyncedId, state.SyncedCreatedAt
	if err := verifyColdArchiveHoldsProgress(ctx, cold, state); err != nil {
		return result, err
	}

	leftoverQuery := "created_at >= ? AND id > ?"
	leftoverArgs := []any{state.SyncedCreatedAt - logColdArchiveScanMarginSeconds, state.SyncedId}
	if state.SyncedId == 0 {
		// A progress reset to 0 can keep an old synced_created_at, which would
		// hide most of the archive from the count, so the whole archive is counted.
		leftoverQuery, leftoverArgs = "id > ?", []any{int64(0)}
	}
	var leftover int64
	if err := cold.db.WithContext(ctx).Model(&Log{}).Where(leftoverQuery, leftoverArgs...).Count(&leftover).Error; err != nil {
		return result, err
	}
	if leftover > int64(logColdSyncBatchSize) {
		return result, fmt.Errorf("cold log archive holds %d logs above synced id %d, more than one sync batch of %d; %s and the archive disagree and need manual repair, nothing is deleted or copied", leftover, state.SyncedId, logColdSyncBatchSize, LogColdSyncState{}.TableName())
	}
	if leftover > 0 {
		// A progress moved back by less than a batch can cover logs the hot
		// cleanup already deleted; their archived rows are the only copy left.
		var leftoverIds []int64
		if err := cold.db.WithContext(ctx).Model(&Log{}).Distinct("id").Where(leftoverQuery, leftoverArgs...).Pluck("id", &leftoverIds).Error; err != nil {
			return result, err
		}
		var hotHeld int64
		if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("id IN ?", leftoverIds).Count(&hotHeld).Error; err != nil {
			return result, err
		}
		if hotHeld != int64(len(leftoverIds)) {
			return result, fmt.Errorf("cold log archive holds %d logs above synced id %d but the hot database keeps only %d of them; deleting them would lose the only copy, so %s and the archive need manual repair, nothing is deleted or copied", len(leftoverIds), state.SyncedId, hotHeld, LogColdSyncState{}.TableName())
		}
		current, _, err := coldLogSyncState(ctx)
		if err != nil {
			return result, err
		}
		if current.SyncedId != state.SyncedId {
			return result, errors.New("cold log sync progress changed during the run")
		}
		common.SysLog(common.LogText("cold log sync: deleting %d archived logs above synced id %d left by a failed run", leftover, state.SyncedId))
		if cold.clickHouse {
			err = cold.db.WithContext(ctx).Exec("ALTER TABLE logs DELETE WHERE "+leftoverQuery+" SETTINGS mutations_sync = 1", leftoverArgs...).Error
		} else {
			err = cold.db.WithContext(ctx).Where(leftoverQuery, leftoverArgs...).Delete(&Log{}).Error
		}
		if err != nil {
			return result, err
		}
	}

	var bound int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Select("COALESCE(MAX(id), 0)").
		Where("id > ? AND created_at <= ?", state.SyncedId, now-logColdSyncSafetySeconds).Scan(&bound).Error; err != nil {
		return result, err
	}
	insertCtx := ctx
	if cold.clickHouse {
		insertCtx = coldLogInsertContext(ctx)
	}
	for state.SyncedId < bound {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var logs []*Log
		if err := LOG_DB.WithContext(ctx).Where("id > ? AND id <= ?", state.SyncedId, bound).Order("id").Limit(logColdSyncBatchSize).Find(&logs).Error; err != nil {
			return result, err
		}
		if len(logs) == 0 {
			break
		}
		if err := cold.db.WithContext(insertCtx).Create(&logs).Error; err != nil {
			return result, err
		}
		first, last := logs[0], logs[len(logs)-1]
		oldest, newest := first.CreatedAt, first.CreatedAt
		for _, log := range logs {
			oldest, newest = min(oldest, log.CreatedAt), max(newest, log.CreatedAt)
		}
		var written int64
		if err := cold.db.WithContext(ctx).Model(&Log{}).
			Where("created_at >= ? AND created_at <= ? AND id >= ? AND id <= ?", oldest, newest, first.Id, last.Id).
			Count(&written).Error; err != nil {
			return result, err
		}
		if written != int64(len(logs)) {
			return result, fmt.Errorf("cold log archive holds %d logs for ids %d to %d after writing %d", written, first.Id, last.Id, len(logs))
		}
		update := LOG_DB.WithContext(ctx).Model(&LogColdSyncState{}).
			Where("name = ? AND synced_id = ?", logColdSyncStateName, state.SyncedId).
			Updates(map[string]any{"synced_id": last.Id, "synced_created_at": last.CreatedAt, "updated_at": common.GetTimestamp()})
		if update.Error != nil {
			return result, update.Error
		}
		if update.RowsAffected != 1 {
			return result, errors.New("cold log sync progress changed during the run")
		}
		state.SyncedId, state.SyncedCreatedAt = int64(last.Id), last.CreatedAt
		result.SyncedId, result.SyncedCreatedAt = state.SyncedId, state.SyncedCreatedAt
		result.Copied += int64(len(logs))
	}

	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("id > ?", state.SyncedId).Count(&result.Lag).Error; err != nil {
		return result, err
	}
	if state.SyncedId > 0 {
		result.LagSeconds = max(now-state.SyncedCreatedAt, 0)
	}
	common.SysLog(common.LogText("cold log sync: synced id %d, copied %d, behind %d logs and %d seconds", result.SyncedId, result.Copied, result.Lag, result.LagSeconds))
	return result, nil
}

// CleanupHotLogs deletes, in small batches, hot logs created before the hot
// retention boundary that the cold archive already holds.
func CleanupHotLogs(ctx context.Context, now int64) (int64, error) {
	if !HotLogCleanupEnabled() {
		return 0, nil
	}
	before := hotLogRetentionBoundary(now)
	deleted, err := deleteArchivedHotLogs(ctx, before, logHotCleanupBatchSize)
	if err != nil {
		return deleted, err
	}
	common.SysLog(common.LogText("hot log cleanup: deleted %d archived logs created before %d", deleted, before))
	return deleted, nil
}

// deleteArchivedHotLogs deletes hot logs created before the given time whose
// id is no greater than the sync progress, after checking the archive still
// holds the progress. Batches walk up the id from the last deleted one, so a
// long cleanup does not rescan deleted rows. The id of the newest log before
// the cutoff, read through the created_at index, caps the walk, so a run with
// nothing to delete ends after one index lookup; a log with a larger id but an
// older created_at is left for the next run. Ids are read before the delete
// because DELETE ... LIMIT is MySQL-only and GORM drops Limit from deletes on
// PostgreSQL and SQLite. Every batch is checked against the archive before it
// is deleted: if the archive lacks any of its logs, the run stops with the
// batch intact. Batches hold at most maxLogHotCleanupBatchSize logs. It fails
// while the archive is not connected.
func deleteArchivedHotLogs(ctx context.Context, before int64, batchSize int) (int64, error) {
	batchSize = min(batchSize, maxLogHotCleanupBatchSize)
	cold, err := coldLogStore()
	if err != nil {
		return 0, err
	}
	state, _, err := coldLogSyncState(ctx)
	if err != nil || state.SyncedId == 0 {
		return 0, err
	}
	if err := verifyColdArchiveHoldsProgress(ctx, cold, state); err != nil {
		return 0, err
	}
	distinctIds := "COUNT(DISTINCT id)"
	if cold.clickHouse {
		distinctIds = "uniqExact(id)"
	}
	var newest []int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ?", before).
		Order("created_at desc").Limit(1).Pluck("id", &newest).Error; err != nil {
		return 0, err
	}
	if len(newest) == 0 {
		return 0, nil
	}
	upper := min(newest[0], state.SyncedId)
	var deleted, lastId int64
	for {
		var batch []struct {
			Id        int64
			CreatedAt int64
		}
		if err := LOG_DB.WithContext(ctx).Model(&Log{}).Select("id, created_at").Where("id > ? AND id <= ? AND created_at < ?", lastId, upper, before).
			Order("id").Limit(batchSize).Find(&batch).Error; err != nil {
			return deleted, err
		}
		if len(batch) == 0 {
			return deleted, nil
		}
		ids := make([]int64, len(batch))
		oldest, newest := batch[0].CreatedAt, batch[0].CreatedAt
		for i, log := range batch {
			ids[i] = log.Id
			oldest, newest = min(oldest, log.CreatedAt), max(newest, log.CreatedAt)
		}
		var archived int64
		if err := cold.db.WithContext(ctx).Model(&Log{}).Select(distinctIds).
			Where("created_at >= ? AND created_at <= ? AND id IN ?", oldest, newest, ids).Scan(&archived).Error; err != nil {
			return deleted, err
		}
		if archived != int64(len(ids)) {
			return deleted, fmt.Errorf("cold log archive holds %d of the %d hot logs with ids %d to %d; they are kept and the cleanup stops", archived, len(ids), ids[0], ids[len(ids)-1])
		}
		result := LOG_DB.WithContext(ctx).Where("id IN ?", ids).Delete(&Log{})
		deleted += result.RowsAffected
		if result.Error != nil {
			return deleted, result.Error
		}
		lastId = ids[len(ids)-1]
		if len(ids) < batchSize {
			return deleted, nil
		}
		select {
		case <-ctx.Done():
			return deleted, ctx.Err()
		case <-time.After(logHotCleanupPause):
		}
	}
}
