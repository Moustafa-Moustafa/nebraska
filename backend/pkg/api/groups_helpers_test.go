package api_test

import (
	"time"

	"github.com/flatcar/nebraska/backend/pkg/api/dbreads"
)

// setCacheLifespanForTest temporarily sets the cache lifespan for testing.
func setCacheLifespanForTest(lifespan time.Duration) time.Duration {
	return dbreads.SetCacheLifespanForTest(lifespan)
}
