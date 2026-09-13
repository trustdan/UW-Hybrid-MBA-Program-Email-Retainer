//go:build !windows

package scheduler

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("Windows Task Scheduler is only supported on Windows")

func installTask(ctx context.Context, launcherPath string) (*TaskStatus, error) {
	return nil, errUnsupported
}

func queryStatus(ctx context.Context) (*TaskStatus, error) {
	return nil, errUnsupported
}

func runTask(ctx context.Context) error {
	return errUnsupported
}

func removeTask(ctx context.Context) error {
	return errUnsupported
}
