package ai

import (
	"coinsphere/backend/plugin/sdk"
	"context"
	"gorm.io/gorm"
	"time"
)

var (
	ErrPermission = sdk.ErrSession
	ErrNotFound   = sdk.ErrNotFound
	ErrConflict   = sdk.ErrConflict
)

type M = map[string]any
type runtime struct {
	DB   *gorm.DB
	host sdk.Host
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	return "********"
}
func (a *runtime) validateSession(ctx context.Context) error {
	return a.host.Sessions.ValidateSession(ctx, nil)
}

// Session validation locks out logout/password changes before plugin row locks.
func (a *runtime) write(ctx context.Context, execute func(*runtime) error) error {
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := a.host.Sessions.ValidateSession(ctx, tx); err != nil {
			return err
		}
		copy := *a
		copy.DB = tx
		return execute(&copy)
	})
}
