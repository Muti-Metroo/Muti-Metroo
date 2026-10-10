package loadtest

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// mockStream is a simple in-memory stream for testing.
type mockStream struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func newMockStreamPair() (*mockStream, *mockStream) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	s1 := &mockStream{reader: r1, writer: w2}
	s2 := &mockStream{reader: r2, writer: w1}

	return s1, s2
}

func (s *mockStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

func (s *mockStream) Write(p []byte) (n int, err error) {
	return s.writer.Write(p)
}

func (s *mockStream) Close() error {
	s.reader.Close()
	s.writer.Close()
	return nil
}

func TestStreamLoadGenerator(t *testing.T) {
	gen := NewStreamLoadGenerator(2, 1024, 100*time.Millisecond)

	streamFactory := func() (io.ReadWriteCloser, io.ReadWriteCloser, error) {
		s1, s2 := newMockStreamPair()

		// Echo server
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := s2.Read(buf)
				if err != nil {
					return
				}
				s2.Write(buf[:n])
			}
		}()

		return s1, s1, nil
	}

	ctx := context.Background()
	metrics, err := gen.Run(ctx, streamFactory)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if metrics.TotalStreams == 0 {
		t.Error("expected at least one stream")
	}
	t.Logf("Stream metrics: total=%d, success=%d, failed=%d, throughput=%.2f MB/s",
		metrics.TotalStreams, metrics.SuccessfulStreams, metrics.FailedStreams, metrics.ThroughputMBps)
}

func TestRouteTableLoadTester(t *testing.T) {
	tester := NewRouteTableLoadTester(1000)
	metrics, err := tester.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if metrics.TotalRoutes != 1000 {
		t.Errorf("expected 1000 routes, got %d", metrics.TotalRoutes)
	}
	if metrics.LookupsPerSecond == 0 {
		t.Error("expected positive lookups per second")
	}
	t.Logf("Route metrics: routes=%d, insert=%.2fms, lookup=%.2fns, lookups/s=%.0f",
		metrics.TotalRoutes, metrics.InsertionTimeMs, metrics.LookupTimeNs, metrics.LookupsPerSecond)
}

func TestConnectionChurnTester(t *testing.T) {
	tester := NewConnectionChurnTester(2, 100*time.Millisecond)

	var mu sync.Mutex
	connections := make(map[int]bool)
	connID := 0

	connectFn := func() (func() error, error) {
		mu.Lock()
		id := connID
		connID++
		connections[id] = true
		mu.Unlock()

		closeFunc := func() error {
			mu.Lock()
			delete(connections, id)
			mu.Unlock()
			return nil
		}
		return closeFunc, nil
	}

	ctx := context.Background()
	metrics, err := tester.Run(ctx, connectFn)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if metrics.TotalConnections == 0 {
		t.Error("expected at least one connection")
	}
	t.Logf("Churn metrics: total=%d, success=%d, failed=%d, churn_rate=%.2f/s",
		metrics.TotalConnections, metrics.SuccessfulConnects, metrics.FailedConnects, metrics.ChurnRate)
}

func TestThroughputTester(t *testing.T) {
	tester := NewThroughputTester(100*time.Millisecond, 64*1024)

	// Use a discard writer that counts bytes
	var buf bytes.Buffer

	ctx := context.Background()
	metrics, err := tester.Run(ctx, &buf)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if metrics.TotalBytes == 0 {
		t.Error("expected some bytes written")
	}
	t.Logf("Throughput metrics: bytes=%d, duration=%v, throughput=%.2f MB/s (%.3f Gbps)",
		metrics.TotalBytes, metrics.Duration, metrics.ThroughputMBps, metrics.ThroughputGbps)
}

// Benchmarks

func BenchmarkRouteTableInsert100(b *testing.B) {
	benchmarkRouteTableInsert(b, 100)
}

func BenchmarkRouteTableInsert1000(b *testing.B) {
	benchmarkRouteTableInsert(b, 1000)
}

func BenchmarkRouteTableInsert10000(b *testing.B) {
	benchmarkRouteTableInsert(b, 10000)
}

func benchmarkRouteTableInsert(b *testing.B, count int) {
	for i := 0; i < b.N; i++ {
		tester := NewRouteTableLoadTester(count)
		tester.Run()
	}
}

func BenchmarkRouteTableLookup(b *testing.B) {
	tester := NewRouteTableLoadTester(10000)
	metrics, _ := tester.Run()
	b.ReportMetric(metrics.LookupTimeNs, "ns/lookup")
	b.ReportMetric(metrics.LookupsPerSecond, "lookups/sec")
}

func BenchmarkConcurrentStreams10(b *testing.B) {
	benchmarkConcurrentStreams(b, 10)
}

func BenchmarkConcurrentStreams100(b *testing.B) {
	benchmarkConcurrentStreams(b, 100)
}

func benchmarkConcurrentStreams(b *testing.B, concurrency int) {
	for i := 0; i < b.N; i++ {
		gen := NewStreamLoadGenerator(concurrency, 1024, 50*time.Millisecond)

		streamFactory := func() (io.ReadWriteCloser, io.ReadWriteCloser, error) {
			s1, s2 := newMockStreamPair()

			go func() {
				buf := make([]byte, 1024)
				for {
					n, err := s2.Read(buf)
					if err != nil {
						return
					}
					s2.Write(buf[:n])
				}
			}()

			return s1, s1, nil
		}

		ctx := context.Background()
		metrics, _ := gen.Run(ctx, streamFactory)
		b.ReportMetric(metrics.StreamsPerSecond, "streams/sec")
	}
}

func BenchmarkThroughput(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tester := NewThroughputTester(50*time.Millisecond, 64*1024)
		var buf bytes.Buffer
		ctx := context.Background()
		metrics, _ := tester.Run(ctx, &buf)
		b.ReportMetric(metrics.ThroughputMBps, "MB/s")
	}
}

// Integration-style benchmarks using TCP

func BenchmarkTCPThroughput(b *testing.B) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Server - discard all data
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go io.Copy(io.Discard, conn)
		}
	}()

	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			b.Fatalf("failed to connect: %v", err)
		}

		tester := NewThroughputTester(50*time.Millisecond, 64*1024)
		ctx := context.Background()
		metrics, _ := tester.Run(ctx, conn)
		conn.Close()

		b.ReportMetric(metrics.ThroughputMBps, "MB/s")
	}
}

func BenchmarkTCPConnectionChurn(b *testing.B) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Server - accept and immediately close
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	for i := 0; i < b.N; i++ {
		tester := NewConnectionChurnTester(10, 50*time.Millisecond)

		connectFn := func() (func() error, error) {
			conn, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				return nil, err
			}
			return func() error { return conn.Close() }, nil
		}

		ctx := context.Background()
		metrics, _ := tester.Run(ctx, connectFn)
		b.ReportMetric(metrics.ChurnRate, "conn/sec")
	}
}
