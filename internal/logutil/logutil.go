// Package logutil provides small logging helpers shared across bridge packages.
package logutil

import (
	"io"
	"log"
)

// Ensure returns logger as-is when non-nil, or a discard logger otherwise.
func Ensure(logger *log.Logger) *log.Logger {
	if logger == nil {
		return log.New(io.Discard, "", 0)
	}
	return logger
}
