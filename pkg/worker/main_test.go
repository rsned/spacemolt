package worker

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestMain makes every settle wait and default poll sleep instant. Tests that
// need real sleep semantics inject their own via the deps sleep fields.
func TestMain(m *testing.M) {
	sleepFunc = func(ctx context.Context, _ time.Duration) error {
		return ctx.Err()
	}
	os.Exit(m.Run())
}
