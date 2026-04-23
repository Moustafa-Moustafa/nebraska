package api_test

import (
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/admin"
	apiruntime "github.com/flatcar/nebraska/backend/pkg/api/runtime"
)

const (
	defaultTestDbURL string = "postgres://postgres:nebraska@127.0.0.1:5432/nebraska_tests?sslmode=disable&connect_timeout=10"
)

func newForTest(t *testing.T) *api.API {
	t.Helper()
	a, err := api.NewForTest(api.OptionInitDB, api.OptionDisableUpdatesOnFailedRollout)
	require.NoError(t, err)
	require.NotNil(t, a)
	return a
}

func adminSvc(a *api.API) *admin.Service {
	return admin.NewService(a.DB())
}

func runtimeSvc(a *api.API) *apiruntime.Service {
	return apiruntime.NewService(a.DB(), a.DisableUpdatesOnFailedRollout())
}

func checkDB(t *testing.T) {
	t.Helper()
	if _, ok := os.LookupEnv("NEBRASKA_DB_URL"); !ok {
		t.Logf("NEBRASKA_DB_URL not set, setting to default %q\n", defaultTestDbURL)
		_ = os.Setenv("NEBRASKA_DB_URL", defaultTestDbURL)
	}
}

func setupTestDB() {
	if _, ok := os.LookupEnv("NEBRASKA_DB_URL"); !ok {
		log.Printf("NEBRASKA_DB_URL not set, setting to default %q\n", defaultTestDbURL)
		_ = os.Setenv("NEBRASKA_DB_URL", defaultTestDbURL)
	}
}
