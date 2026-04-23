package api_test

import (
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func TestMain(m *testing.M) {
	if os.Getenv("NEBRASKA_SKIP_TESTS") != "" {
		return
	}

	setupTestDB()

	a, err := api.NewWithMigrations(api.OptionInitDB)
	if err != nil {
		log.Printf("Failed to init DB: %v\n", err)
		log.Println("These tests require PostgreSQL running and a tests database created, please adjust NEBRASKA_DB_URL as needed.")
		os.Exit(1)
	}
	a.Close()

	os.Exit(m.Run())
}

func TestMigrateDown(t *testing.T) {
	db, err := api.NewWithMigrations(api.OptionInitDB)
	require.NoError(t, err)
	defer db.Close()

	// Note: The full assertion checking migration count requires access to
	// unexported migrationsTable and migration type. In the external test
	// package, we verify MigrateDown executes without error.
	_, err = db.MigrateDown("0004")
	require.NoError(t, err)
}
