package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestMigrateDBCreatesRuntimeTables guards the full master migration: task
// plugin sync and password login encryption query these tables at startup, and
// a merge once dropped them from the migration list without any build failure.
func TestMigrateDBCreatesRuntimeTables(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open("file:" + t.Name() + "?mode=memory&cache=shared")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)

			previousDB, previousLogDB := DB, LOG_DB
			previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			common.SetLogDatabaseType(common.DatabaseType(dialect))
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetMainDatabaseType(previousMain)
				common.SetLogDatabaseType(previousLog)
				initCol()
				assert.NoError(t, sqlDB.Close())
			})

			// The second run is the restart of an already migrated database.
			for range 2 {
				require.NoError(t, migrateDB())
			}
			for _, table := range []any{&TaskPlugin{}, &LoginEncryptionKey{}, &AffiliateLog{}, &Token{}} {
				assert.True(t, db.Migrator().HasTable(table), "%T table must exist after migration", table)
			}
		})
	}
}
