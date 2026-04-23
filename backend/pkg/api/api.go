package api

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"strconv"
	"time"

	//register "pgx" sql driver
	"github.com/doug-martin/goqu/v9"
	"github.com/jmoiron/sqlx"
	migrate "github.com/rubenv/sql-migrate"

	"github.com/flatcar/nebraska/backend/pkg/logger"

	// PostgreSQL Driver and Toolkit
	_ "github.com/jackc/pgx/v4/stdlib"

	// Postgresql driver
	_ "github.com/lib/pq"
)

var (
	//go:embed db/*.sql
	sqlFolder embed.FS
	//go:embed db/migrations/*.sql
	migrationsFolder embed.FS
)

const (
	defaultDbURL          = "postgres://postgres:nebraska@127.0.0.1:5432/nebraska?sslmode=disable&connect_timeout=10"
	maxOpenAndIdleDbConns = 25
	dBConnMaxLifetime     = 5 * 60 // seconds
)

func nowUTC() time.Time {
	return time.Now().UTC()
}

var (
	l = logger.New("api")

	// ErrNoRowsAffected indicates that no rows were affected in an update or
	// delete database operation.
	ErrNoRowsAffected = errors.New("nebraska: no rows affected")

	// ErrInvalidSemver indicates that the provided semver version is not valid.
	ErrInvalidSemver = errors.New("nebraska: invalid semver")

	// ErrInvalidArch indicates that the provided architecture is not valid/supported
	ErrInvalidArch = errors.New("nebraska: invalid/unsupported arch")

	// ErrArchMismatch indicates that arches of two objects didn't
	// match (for example, for a package and channel)
	ErrArchMismatch = errors.New("nebraska: mismatched arches")
)

const migrationsTable = "database_migrations"

// runtimeTables are the tables that subscriber instances can write to.
// All other tables are admin-only (read-only on subscribers).
var runtimeTables = []string{
	"instance", "instance_application", "instance_status_history",
	"event", "activity", "instance_stats", "group_state",
}

// API represents an api instance used to interact with Nebraska entities.
type API struct {
	db           *sqlx.DB
	dbDriver     string
	dbURL        string
	instanceMode string

	// disableUpdatesOnFailedRollout defines wether to disable updates
	// after a first rollout attempt failed (ResultFailed)
	disableUpdatesOnFailedRollout bool
}

// New creates a new API instance, creates the underlying db connection.
func New(options ...func(*API) error) (*API, error) {
	api := &API{
		dbDriver:     "pgx",
		dbURL:        os.Getenv("NEBRASKA_DB_URL"),
		instanceMode: os.Getenv("NEBRASKA_INSTANCE_MODE"),
	}

	if api.dbURL == "" {
		api.dbURL = defaultDbURL
	}

	var err error
	api.db, err = sqlx.Open(api.dbDriver, api.dbURL)
	if err != nil {
		return nil, err
	}
	if err := api.db.Ping(); err != nil {
		return nil, err
	}

	var (
		maxOpenConns    int
		maxIdleConns    int
		connMaxLifetime int
	)

	maxOpenConns, err = strconv.Atoi(os.Getenv("NEBRASKA_DB_MAX_OPEN_CONNS"))
	if err != nil {
		maxOpenConns = maxOpenAndIdleDbConns
	}
	maxIdleConns, err = strconv.Atoi(os.Getenv("NEBRASKA_DB_MAX_IDLE_CONNS"))
	if err != nil {
		maxIdleConns = maxOpenConns
	}

	connMaxLifetime, err = strconv.Atoi(os.Getenv("NEBRASKA_DB_CONN_MAX_LIFETIME"))
	if err != nil {
		connMaxLifetime = dBConnMaxLifetime
	}

	api.db.SetMaxOpenConns(maxOpenConns)
	api.db.SetMaxIdleConns(maxIdleConns)
	api.db.SetConnMaxLifetime(time.Duration(connMaxLifetime) * time.Second)

	for _, option := range options {
		err := option(api)
		if err != nil {
			return nil, err
		}
	}
	return api, nil
}

// NewWithMigrations creates a new API instance, creates the underlying db connection and
// applies all available db migrations. In distributed mode (NEBRASKA_INSTANCE_MODE is set),
// it also creates a least-privilege runtime or admin role and reconnects using that role.
func NewWithMigrations(options ...func(*API) error) (*API, error) {
	api, err := New(options...)
	if err != nil {
		return nil, err
	}

	migrate.SetTable(migrationsTable)
	migrations := migrationAssets()

	if _, err := migrate.Exec(api.db.DB, "postgres", migrations, migrate.Up); err != nil {
		return nil, err
	}

	// In distributed mode, create a least-privilege role and reconnect.
	// The bootstrap connection (NEBRASKA_DB_URL) is only used for migrations
	// and role management. The runtime connection uses a restricted role.
	if api.instanceMode != "" {
		if err := api.setupAndReconnectAsRole(); err != nil {
			return nil, fmt.Errorf("failed to setup runtime role: %w", err)
		}
	}

	return api, nil
}

type migration struct {
	ID        string    `db:"id"`
	AppliedAt time.Time `db:"applied_at"`
}

func (api *API) MigrateDown(version string) (int, error) {
	migrate.SetTable(migrationsTable)
	migrations := migrationAssets()

	// find version based on input string
	query, _, err := goqu.Select("*").From(migrationsTable).Where(goqu.C("id").Like(fmt.Sprintf("%s%%", version))).ToSQL()
	if err != nil {
		return 0, err
	}

	var mig migration
	err = api.db.QueryRowx(query).StructScan(&mig)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("no migrations found for: %s, err: %v", version, err)
		}
		return 0, err
	}

	// find  count of migrations that have been applied after the version
	query, _, err = goqu.Select(goqu.COUNT("*")).From(migrationsTable).Where(goqu.C("applied_at").Gt(mig.AppliedAt)).ToSQL()
	if err != nil {
		return 0, err
	}

	countMap := make(map[string]interface{})

	err = api.db.QueryRowx(query).MapScan(countMap)
	if err != nil {
		return 0, err
	}

	levels := countMap["count"].(int64)
	l.Info().Msgf("migrating down %d levels", levels)
	count, err := migrate.ExecMax(api.db.DB, "postgres", migrations, migrate.Down, int(levels))
	if err != nil {
		return 0, err
	}
	l.Info().Msg("successfully migrated down")
	return count, nil
}

func migrationAssets() *migrate.EmbedFileSystemMigrationSource {
	return &migrate.EmbedFileSystemMigrationSource{
		FileSystem: migrationsFolder,
		Root:       "db/migrations",
	}
}

// OptionInitDB will initialize the database during the API instance creation,
// dropping all existing tables, which will force all migration scripts to be
// re-executed. Use with caution, this will DESTROY ALL YOUR DATA.
func OptionInitDB(api *API) error {
	sqlFile, err := sqlFolder.ReadFile("db/drop_all_tables.sql")
	if err != nil {
		return err
	}

	if _, err := api.db.Exec(string(sqlFile)); err != nil {
		return err
	}
	return nil
}

// OptionDisableUpdatesOnFailedRollout will modify API to disable
// updates on failed rollout.
func OptionDisableUpdatesOnFailedRollout(api *API) error {
	api.disableUpdatesOnFailedRollout = true

	return nil
}

// setupAndReconnectAsRole creates a least-privilege DB role based on
// the instance mode and reconnects the API using that role. The original
// bootstrap connection (from NEBRASKA_DB_URL) is closed after the role
// is created.
func (api *API) setupAndReconnectAsRole() error {
	var roleName string
	if api.IsPrimary() {
		roleName = "nebraska_admin"
	} else {
		roleName = "nebraska_runtime"
	}

	// Generate a random password for the role
	password, err := generateRandomPassword()
	if err != nil {
		return fmt.Errorf("generating password: %w", err)
	}

	// Create or update the role using PG format() for safe identifier/literal quoting.
	createRoleSQL := fmt.Sprintf(
		`DO $body$ BEGIN
		  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN
		    EXECUTE format('CREATE ROLE %%I WITH LOGIN PASSWORD %%L', '%s', '%s');
		  ELSE
		    EXECUTE format('ALTER ROLE %%I WITH PASSWORD %%L', '%s', '%s');
		  END IF;
		END $body$`,
		roleName, roleName, password, roleName, password)
	_, err = api.db.Exec(createRoleSQL)
	if err != nil {
		return fmt.Errorf("creating role %s: %w", roleName, err)
	}

	// Grant permissions based on instance mode
	if api.IsPrimary() {
		// Admin role: read + write all tables
		_, err = api.db.Exec("GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + roleName)
		if err != nil {
			return fmt.Errorf("granting admin permissions: %w", err)
		}
	} else {
		// Runtime role: read all + write only runtime tables
		_, err = api.db.Exec("GRANT SELECT ON ALL TABLES IN SCHEMA public TO " + roleName)
		if err != nil {
			return fmt.Errorf("granting read permissions: %w", err)
		}
		for _, table := range runtimeTables {
			_, err = api.db.Exec("GRANT INSERT, UPDATE, DELETE ON TABLE " + table + " TO " + roleName)
			if err != nil {
				return fmt.Errorf("granting write on %s: %w", table, err)
			}
		}
	}

	// Grant sequence access
	_, err = api.db.Exec("GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + roleName)
	if err != nil {
		return fmt.Errorf("granting sequence permissions: %w", err)
	}

	// Build a new DB URL with the role credentials
	roleURL, err := replaceDBUserPassword(api.dbURL, roleName, password)
	if err != nil {
		return fmt.Errorf("building runtime URL: %w", err)
	}

	// Close bootstrap connection and reconnect as the role
	l.Info().Str("role", roleName).Msg("Reconnecting with least-privilege role")
	api.db.Close()

	api.db, err = sqlx.Open(api.dbDriver, roleURL)
	if err != nil {
		return fmt.Errorf("reconnecting as %s: %w", roleName, err)
	}
	if err := api.db.Ping(); err != nil {
		return fmt.Errorf("pinging as %s: %w", roleName, err)
	}

	api.dbURL = roleURL
	return nil
}

// replaceDBUserPassword parses a postgres:// URL and replaces the user and password.
func replaceDBUserPassword(dbURL, user, password string) (string, error) {
	u, err := neturl.Parse(dbURL)
	if err != nil {
		return "", err
	}
	u.User = neturl.UserPassword(user, password)
	return u.String(), nil
}

// generateRandomPassword creates a 20-character hex password.
func generateRandomPassword() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Close releases the connections to the database.
func (api *API) Close() error {
	return api.db.Close()
}

// NewForTest creates a new API instance with given options and fills
// the database with sample data for testing purposes.
func NewForTest(options ...func(*API) error) (*API, error) {
	a, err := NewWithMigrations(options...)
	if err != nil {
		return nil, err
	}

	sqlFile, err := sqlFolder.ReadFile("db/sample_data.sql")
	if err != nil {
		return nil, err
	}

	_, err = a.db.Exec(string(sqlFile))
	if err != nil {
		return nil, err
	}
	return a, nil
}

// DB returns the underlying database connection. Sub-packages use this
// for direct query access when their operations require it.
func (api *API) DB() *sqlx.DB {
	return api.db
}

// IsPrimary returns true if this instance can perform admin write operations.
// Set NEBRASKA_INSTANCE_MODE=subscriber to disable admin writes.
func (api *API) IsPrimary() bool {
	return api.instanceMode != "subscriber"
}

// DisableUpdatesOnFailedRollout returns whether the instance is configured to
// disable updates after a failed rollout attempt.
func (api *API) DisableUpdatesOnFailedRollout() bool {
	return api.disableUpdatesOnFailedRollout
}
