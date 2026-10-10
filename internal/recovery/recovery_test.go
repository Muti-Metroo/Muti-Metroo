package recovery

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestRecoverWithLog_RecoversPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		defer RecoverWithLog(logger, "testGoroutine")
		panic("test panic")
	}()

	wg.Wait()

	output := buf.String()
	if !strings.Contains(output, "panic recovered") {
		t.Errorf("expected 'panic recovered' in output, got: %s", output)
	}
	if !strings.Contains(output, "testGoroutine") {
		t.Errorf("expected goroutine name in output, got: %s", output)
	}
	if !strings.Contains(output, "test panic") {
		t.Errorf("expected panic message in output, got: %s", output)
	}
	if !strings.Contains(output, "stack=") {
		t.Errorf("expected stack trace in output, got: %s", output)
	}
}

func TestRecoverWithLog_NoopOnNoPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		defer RecoverWithLog(logger, "normalGoroutine")
		// No panic
	}()

	wg.Wait()

	if buf.Len() > 0 {
		t.Errorf("expected no output when no panic, got: %s", buf.String())
	}
}

func TestRecoverWithCallback_CallsCallback(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	var wg sync.WaitGroup
	wg.Add(1)

	var callbackCalled bool
	var recoveredValue interface{}

	go func() {
		defer wg.Done()
		defer RecoverWithCallback(logger, "callbackGoroutine", func(r interface{}) {
			callbackCalled = true
			recoveredValue = r
		})
		panic("callback test")
	}()

	wg.Wait()

	if !callbackCalled {
		t.Error("expected callback to be called")
	}
	if recoveredValue != "callback test" {
		t.Errorf("expected recovered value 'callback test', got: %v", recoveredValue)
	}
}

func TestRecoverWithCallback_NoCallbackOnNoPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	var wg sync.WaitGroup
	wg.Add(1)

	callbackCalled := false

	go func() {
		defer wg.Done()
		defer RecoverWithCallback(logger, "normalGoroutine", func(r interface{}) {
			callbackCalled = true
		})
		// No panic
	}()

	wg.Wait()

	if callbackCalled {
		t.Error("expected callback not to be called when no panic")
	}
}

func TestRecoverWithCallback_NilCallback(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	var wg sync.WaitGroup
	wg.Add(1)

	// Should not panic when callback is nil
	go func() {
		defer wg.Done()
		defer RecoverWithCallback(logger, "nilCallbackGoroutine", nil)
		panic("nil callback test")
	}()

	wg.Wait()

	output := buf.String()
	if !strings.Contains(output, "panic recovered") {
		t.Errorf("expected panic to be logged, got: %s", output)
	}
}

func TestRecoverNoop(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	completed := false

	go func() {
		defer wg.Done()
		defer RecoverNoop()
		defer func() { completed = true }()
		panic("should be silently recovered")
	}()

	wg.Wait()

	if !completed {
		t.Error("expected goroutine to complete after recovery")
	}
}
