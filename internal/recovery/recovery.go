// Package recovery provides panic recovery utilities for goroutines.
package recovery

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// RecoverWithLog recovers from panics and logs them with the provided logger.
// Use this with defer at the start of goroutines to prevent crashes and log diagnostics.
//
// Example:
//
//	go func() {
//	    defer recovery.RecoverWithLog(logger, "myGoroutine")
//	    // ... goroutine work
//	}()
func RecoverWithLog(logger *slog.Logger, name string) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		logger.Error("panic recovered",
			"goroutine", name,
			"panic", fmt.Sprintf("%v", r),
			"stack", stack)
	}
}

// RecoverWithCallback recovers from panics, logs them, and calls the optional callback.
// The callback can be used for cleanup or metrics reporting.
func RecoverWithCallback(logger *slog.Logger, name string, callback func(recovered interface{})) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		logger.Error("panic recovered",
			"goroutine", name,
			"panic", fmt.Sprintf("%v", r),
			"stack", stack)
		if callback != nil {
			callback(r)
		}
	}
}

// RecoverNoop silently recovers from panics without logging.
// Use only in tests or when logging is not available.
func RecoverNoop() {
	recover()
}
