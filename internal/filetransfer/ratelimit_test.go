package filetransfer

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func TestRateLimitedReader_Unlimited(t *testing.T) {
	data := []byte("hello world")
	r := bytes.NewReader(data)

	// With 0 rate limit, should return unwrapped reader
	limited := NewRateLimitedReader(context.Background(), r, 0)
	if limited != r {
		t.Error("expected unwrapped reader for 0 rate limit")
	}

	// With negative rate limit, should return unwrapped reader
	r2 := bytes.NewReader(data)
	limited2 := NewRateLimitedReader(context.Background(), r2, -100)
	if limited2 != r2 {
		t.Error("expected unwrapped reader for negative rate limit")
	}
}

func TestRateLimitedReader_Read(t *testing.T) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	r := bytes.NewReader(data)

	// Use a high rate limit so test completes quickly
	limited := NewRateLimitedReader(context.Background(), r, 1024*1024)

	result, err := io.ReadAll(limited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(result, data) {
		t.Error("data mismatch")
	}
}

func TestRateLimitedReader_RateLimiting(t *testing.T) {
	// Create 32KB of data (larger than the 16KB burst)
	// This ensures we actually hit the rate limit after the initial burst
	data := make([]byte, 32*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	r := bytes.NewReader(data)

	// Limit to 8KB/s
	// After the 16KB burst, reading the remaining 16KB should take ~2 seconds
	rateLimit := int64(8 * 1024)
	limited := NewRateLimitedReader(context.Background(), r, rateLimit)

	start := time.Now()
	result, err := io.ReadAll(limited)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(result, data) {
		t.Error("data mismatch")
	}

	// After the 16KB burst, we have 16KB remaining at 8KB/s = 2 seconds
	// Use 1 second threshold to avoid flaky tests
	if elapsed < 1*time.Second {
		t.Errorf("expected at least 1s for rate limiting, got %v", elapsed)
	}
}

func TestRateLimitedReader_ContextCancellation(t *testing.T) {
	data := make([]byte, 1024)
	r := bytes.NewReader(data)

	ctx, cancel := context.WithCancel(context.Background())
	limited := NewRateLimitedReader(ctx, r, 100) // Very slow rate

	// Cancel context immediately
	cancel()

	// Read should fail with context error
	_, err := io.ReadAll(limited)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRateLimitedWriter_Unlimited(t *testing.T) {
	var buf bytes.Buffer

	// With 0 rate limit, should return unwrapped writer
	limited := NewRateLimitedWriter(context.Background(), &buf, 0)
	if limited != &buf {
		t.Error("expected unwrapped writer for 0 rate limit")
	}
}

func TestRateLimitedWriter_Write(t *testing.T) {
	var buf bytes.Buffer
	data := []byte("hello world test data")

	// Use a high rate limit so test completes quickly
	limited := NewRateLimitedWriter(context.Background(), &buf, 1024*1024)

	n, err := limited.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Error("data mismatch")
	}
}

func TestRateLimitedWriter_ContextCancellation(t *testing.T) {
	var buf bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	limited := NewRateLimitedWriter(ctx, &buf, 100) // Very slow rate

	// Cancel context immediately
	cancel()

	// Write should fail with context error
	data := make([]byte, 1024)
	_, err := limited.Write(data)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRateLimitedWriter_RateLimiting(t *testing.T) {
	var buf bytes.Buffer

	// Create 32KB of data (larger than the 16KB burst)
	data := make([]byte, 32*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Limit to 8KB/s
	rateLimit := int64(8 * 1024)
	limited := NewRateLimitedWriter(context.Background(), &buf, rateLimit)

	start := time.Now()
	// Write directly - the writer handles chunking internally
	n, err := limited.Write(data)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Error("data mismatch")
	}

	// After the 16KB burst, we have 16KB remaining at 8KB/s = 2 seconds
	// Use 1 second threshold to avoid flaky tests
	if elapsed < 1*time.Second {
		t.Errorf("expected at least 1s for rate limiting, got %v", elapsed)
	}
}
