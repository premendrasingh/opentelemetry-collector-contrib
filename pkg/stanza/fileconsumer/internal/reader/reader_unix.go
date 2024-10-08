// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package reader // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/fileconsumer/internal/reader"

import (
	"errors"

	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

func (r *Reader) tryLockFile() bool {
	if err := unix.Flock(int(r.file.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		if !errors.Is(err, unix.EWOULDBLOCK) {
			r.set.Logger.Error("Failed to lock", zap.Error(err))
		}
		return false
	}

	return true
}

func (r *Reader) unlockFile() {
	if err := unix.Flock(int(r.file.Fd()), unix.LOCK_UN); err != nil {
		r.set.Logger.Error("Failed to unlock", zap.Error(err))
	}
}
