package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormPostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type invoiceRestoreHarness struct {
	db      *gorm.DB
	restore func(*testing.T) *gorm.DB
}

func openInvoiceIntegrationSQLiteHarness(t *testing.T) invoiceRestoreHarness {
	t.Helper()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "invoice-source.db")
	restoredPath := filepath.Join(directory, "invoice-restored.db")
	db := openInvoiceIntegrationSQLite(t, sourcePath)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return invoiceRestoreHarness{
		db: db,
		restore: func(t *testing.T) *gorm.DB {
			t.Helper()
			closeInvoiceIntegrationSQL(t, db)
			copyInvoiceIntegrationDatabase(t, sourcePath, restoredPath)
			restored := openInvoiceIntegrationSQLite(t, restoredPath)
			t.Cleanup(func() { closeInvoiceIntegrationSQL(t, restored) })
			return restored
		},
	}
}

func openInvoiceIntegrationSQLite(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path+"?_journal_mode=DELETE&_busy_timeout=30000"), &gorm.Config{})
	require.NoError(t, err)
	var journalMode string
	require.NoError(t, db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error)
	require.Equal(t, "delete", strings.ToLower(journalMode), "backup fixture must not use WAL")
	migrateInvoiceIntegrationTables(t, db)
	return db
}

func copyInvoiceIntegrationDatabase(t *testing.T, sourcePath, restoredPath string) {
	t.Helper()
	source, err := os.Open(sourcePath)
	require.NoError(t, err)
	defer source.Close()
	destination, err := os.OpenFile(restoredPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	require.NoError(t, copyErr)
	require.NoError(t, closeErr)
}

func openInvoiceIntegrationPostgreSQLHarness(t *testing.T) (invoiceRestoreHarness, bool) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		return invoiceRestoreHarness{}, false
	}
	pgDump, dumpErr := exec.LookPath("pg_dump")
	require.NoError(t, dumpErr, "declared invoice PostgreSQL runtime requires pg_dump before database creation")
	pgRestore, restoreErr := exec.LookPath("pg_restore")
	require.NoError(t, restoreErr, "declared invoice PostgreSQL runtime requires pg_restore before database creation")

	adminConfig, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	adminSQL := stdlib.OpenDB(*adminConfig)
	admin, err := gorm.Open(gormPostgres.New(gormPostgres.Config{Conn: adminSQL, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)

	runID := invoiceIntegrationRandomHex(t, 6)
	prefix := "newapi_invoice_ihc_" + runID + "_"
	sourceName := prefix + "source"
	restoredName := prefix + "restored"
	created := make([]string, 0, 2)
	connections := make([]*sql.DB, 0, 2)
	t.Cleanup(func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
		for index := len(created) - 1; index >= 0; index-- {
			require.NoError(t, admin.Exec(`DROP DATABASE "`+created[index]+`"`).Error)
		}
		for _, databaseName := range []string{sourceName, restoredName} {
			var exact int64
			require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", databaseName).Scan(&exact).Error)
			assert.Zero(t, exact, "invoice restore cleanup left exact database %s", databaseName)
		}
		var prefixed int64
		require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE LEFT(datname, LENGTH(?)) = ?", prefix, prefix).Scan(&prefixed).Error)
		assert.Zero(t, prefixed, "invoice restore cleanup left a generated-prefix database")
		require.NoError(t, adminSQL.Close())
	})

	createInvoiceIntegrationPostgreSQLDatabase(t, admin, sourceName)
	created = append(created, sourceName)
	sourceDB, sourceSQL := openBoundInvoiceIntegrationPostgreSQL(t, adminConfig, sourceName)
	connections = append(connections, sourceSQL)
	migrateInvoiceIntegrationTables(t, sourceDB)

	return invoiceRestoreHarness{
		db: sourceDB,
		restore: func(t *testing.T) *gorm.DB {
			t.Helper()
			require.NoError(t, sourceSQL.Close(), "source handles must close before pg_dump")
			dumpPath := filepath.Join(t.TempDir(), "invoice.dump")
			runInvoicePostgreSQLTool(t, adminConfig, sourceName, pgDump,
				"--format=custom", "--file", dumpPath, sourceName)
			createInvoiceIntegrationPostgreSQLDatabase(t, admin, restoredName)
			created = append(created, restoredName)
			runInvoicePostgreSQLTool(t, adminConfig, restoredName, pgRestore,
				"--exit-on-error", "--no-owner", "--no-privileges", "--dbname", restoredName, dumpPath)
			restoredDB, restoredSQL := openBoundInvoiceIntegrationPostgreSQL(t, adminConfig, restoredName)
			connections = append(connections, restoredSQL)
			migrateInvoiceIntegrationTables(t, restoredDB)
			return restoredDB
		},
	}, true
}

func createInvoiceIntegrationPostgreSQLDatabase(t *testing.T, admin *gorm.DB, databaseName string) {
	t.Helper()
	var existing int64
	require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", databaseName).Scan(&existing).Error)
	require.Zero(t, existing)
	require.NoError(t, admin.Exec(`CREATE DATABASE "`+databaseName+`" TEMPLATE template0`).Error)
}

func openBoundInvoiceIntegrationPostgreSQL(t *testing.T, adminConfig *pgx.ConnConfig, databaseName string) (*gorm.DB, *sql.DB) {
	t.Helper()
	config := adminConfig.Copy()
	config.Database = databaseName
	sqlDB := stdlib.OpenDB(*config)
	db, err := gorm.Open(gormPostgres.New(gormPostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	var currentDatabase string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&currentDatabase).Error)
	require.Equal(t, databaseName, currentDatabase, "database binding must be proven before writes")
	return db, sqlDB
}

func runInvoicePostgreSQLTool(t *testing.T, config *pgx.ConnConfig, databaseName, executable string, arguments ...string) {
	t.Helper()
	command := exec.CommandContext(context.Background(), executable, arguments...)
	command.Env = append(os.Environ(),
		"PGHOST="+config.Host,
		"PGPORT="+strconv.Itoa(int(config.Port)),
		"PGUSER="+config.User,
		"PGPASSWORD="+config.Password,
		"PGDATABASE="+databaseName,
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s failed: %s", filepath.Base(executable), strings.TrimSpace(string(output)))
}

func migrateInvoiceIntegrationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.TopUp{}, &model.SubscriptionOrder{}, &model.InvoicePaymentEvidenceBackfillRun{},
		&model.InvoicePaymentEvidenceBackfillItem{},
	))
	require.NoError(t, model.MigratePersonalInvoiceStructures(db))
}

func closeInvoiceIntegrationSQL(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func invoiceIntegrationRandomHex(t *testing.T, size int) string {
	t.Helper()
	value := make([]byte, size)
	_, err := rand.Read(value)
	require.NoError(t, err)
	return hex.EncodeToString(value)
}

func bindInvoiceIntegrationDatabase(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	model.DB = db
	common.SetMainDatabaseType(databaseType)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
	})
}

func waitInvoiceIntegrationDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sqlDB.PingContext(ctx), "restored database must be reachable")
}

type invoiceIntegrationStore struct {
	objects   map[string][]byte
	checksums map[string]string
}

func newInvoiceIntegrationStore() *invoiceIntegrationStore {
	return &invoiceIntegrationStore{objects: make(map[string][]byte), checksums: make(map[string]string)}
}
func (*invoiceIntegrationStore) AuthorityID() string { return invoiceIntegrationAuthorityID }
func (*invoiceIntegrationStore) Bucket() string      { return invoiceIntegrationBucket }
func (store *invoiceIntegrationStore) Put(_ context.Context, key string, body io.Reader, _ int64, checksum string) error {
	value, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	store.objects[key] = value
	store.checksums[key] = checksum
	return nil
}

func (store *invoiceIntegrationStore) Copy(_ context.Context, sourceKey, destinationKey string) error {
	value, ok := store.objects[sourceKey]
	if !ok {
		return ErrInvoiceObjectNotFound
	}
	store.objects[destinationKey] = append([]byte(nil), value...)
	store.checksums[destinationKey] = store.checksums[sourceKey]
	return nil
}

func (store *invoiceIntegrationStore) Head(_ context.Context, key string) (InvoiceObjectHead, error) {
	value, ok := store.objects[key]
	if !ok {
		return InvoiceObjectHead{}, ErrInvoiceObjectNotFound
	}
	return InvoiceObjectHead{SizeBytes: int64(len(value)), ChecksumSHA256: store.checksums[key], ETag: store.etag(key)}, nil
}

func (store *invoiceIntegrationStore) Get(_ context.Context, key, ifMatch string) (InvoiceObjectGet, error) {
	value, ok := store.objects[key]
	if !ok {
		return InvoiceObjectGet{}, ErrInvoiceObjectNotFound
	}
	if ifMatch != store.etag(key) {
		return InvoiceObjectGet{}, ErrInvoiceObjectIntegrityUnavailable
	}
	return InvoiceObjectGet{
		Body: io.NopCloser(bytes.NewReader(value)), SizeBytes: int64(len(value)),
		ChecksumSHA256: store.checksums[key], ETag: store.etag(key),
	}, nil
}

func (store *invoiceIntegrationStore) Delete(_ context.Context, key string) error {
	if _, ok := store.objects[key]; !ok {
		return ErrInvoiceObjectNotFound
	}
	delete(store.objects, key)
	delete(store.checksums, key)
	return nil
}

func (*invoiceIntegrationStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (store *invoiceIntegrationStore) etag(key string) string {
	digest := sha256.Sum256(store.objects[key])
	return fmt.Sprintf("\"%x\"", digest[:16])
}
