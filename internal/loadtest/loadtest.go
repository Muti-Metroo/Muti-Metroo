// Package loadtest provides load testing utilities for Muti Metroo.
package loadtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/routing"
)

// StreamMetrics contains metrics from stream load testing.
type StreamMetrics struct {
	TotalStreams      int64
	SuccessfulStreams int64
	FailedStreams     int64
	TotalBytesWritten int64
	TotalBytesRead    int64
	AvgLatencyMs      float64
	MaxLatencyMs      float64
	MinLatencyMs      float64
	Duration          time.Duration
	StreamsPerSecond  float64
	ThroughputMBps    float64
}

// RouteMetrics contains metrics from route table load testing.
type RouteMetrics struct {
	TotalRoutes      int
	InsertionTimeMs  float64
	LookupTimeNs     float64
	MemoryUsageBytes int64
	LookupsPerSecond float64
}

// ChurnMetrics contains metrics from connection churn testing.
type ChurnMetrics struct {
	TotalConnections    int64
	SuccessfulConnects  int64
	FailedConnects      int64
	TotalDisconnects    int64
	AvgConnectTimeMs    float64
	AvgDisconnectTimeMs float64
	Duration            time.Duration
	ChurnRate           float64
}

// StreamLoadGenerator generates load on streams.
type StreamLoadGenerator struct {
	concurrency int
	dataSize    int
	duration    time.Duration

	metrics StreamMetrics
	mu      sync.Mutex
}

// NewStreamLoadGenerator creates a new stream load generator.
func NewStreamLoadGenerator(concurrency, dataSize int, duration time.Duration) *StreamLoadGenerator {
	return &StreamLoadGenerator{
		concurrency: concurrency,
		dataSize:    dataSize,
		duration:    duration,
		metrics: StreamMetrics{
			MinLatencyMs: float64(^uint64(0) >> 1), // Max float64
		},
	}
}

// Run executes the stream load test using the provided stream factory.
// streamFactory should return a pair of connected streams (reader, writer).
func (g *StreamLoadGenerator) Run(ctx context.Context, streamFactory func() (io.ReadWriteCloser, io.ReadWriteCloser, error)) (*StreamMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, g.duration)
	defer cancel()

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < g.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.runWorker(ctx, streamFactory)
		}()
	}

	wg.Wait()
	g.metrics.Duration = time.Since(startTime)

	// Calculate derived metrics
	if g.metrics.Duration > 0 {
		seconds := g.metrics.Duration.Seconds()
		g.metrics.StreamsPerSecond = float64(g.metrics.SuccessfulStreams) / seconds
		totalBytes := float64(g.metrics.TotalBytesWritten + g.metrics.TotalBytesRead)
		g.metrics.ThroughputMBps = totalBytes / (1024 * 1024) / seconds
	}

	if g.metrics.SuccessfulStreams > 0 {
		g.metrics.AvgLatencyMs = g.metrics.AvgLatencyMs / float64(g.metrics.SuccessfulStreams)
	}

	return &g.metrics, nil
}

func (g *StreamLoadGenerator) runWorker(ctx context.Context, streamFactory func() (io.ReadWriteCloser, io.ReadWriteCloser, error)) {
	data := make([]byte, g.dataSize)
	rand.Read(data)
	readBuf := make([]byte, g.dataSize)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		start := time.Now()
		reader, writer, err := streamFactory()
		if err != nil {
			atomic.AddInt64(&g.metrics.FailedStreams, 1)
			atomic.AddInt64(&g.metrics.TotalStreams, 1)
			continue
		}

		// Write data
		n, err := writer.Write(data)
		if err != nil {
			writer.Close()
			reader.Close()
			atomic.AddInt64(&g.metrics.FailedStreams, 1)
			atomic.AddInt64(&g.metrics.TotalStreams, 1)
			continue
		}
		atomic.AddInt64(&g.metrics.TotalBytesWritten, int64(n))

		// Read data back
		n, err = io.ReadFull(reader, readBuf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			writer.Close()
			reader.Close()
			atomic.AddInt64(&g.metrics.FailedStreams, 1)
			atomic.AddInt64(&g.metrics.TotalStreams, 1)
			continue
		}
		atomic.AddInt64(&g.metrics.TotalBytesRead, int64(n))

		writer.Close()
		reader.Close()

		latency := float64(time.Since(start).Milliseconds())

		g.mu.Lock()
		g.metrics.AvgLatencyMs += latency
		if latency > g.metrics.MaxLatencyMs {
			g.metrics.MaxLatencyMs = latency
		}
		if latency < g.metrics.MinLatencyMs {
			g.metrics.MinLatencyMs = latency
		}
		g.mu.Unlock()

		atomic.AddInt64(&g.metrics.SuccessfulStreams, 1)
		atomic.AddInt64(&g.metrics.TotalStreams, 1)
	}
}

// RouteTableLoadTester tests route table performance.
type RouteTableLoadTester struct {
	routeCount int
}

// NewRouteTableLoadTester creates a new route table load tester.
func NewRouteTableLoadTester(routeCount int) *RouteTableLoadTester {
	return &RouteTableLoadTester{
		routeCount: routeCount,
	}
}

// Run executes the route table load test.
func (t *RouteTableLoadTester) Run() (*RouteMetrics, error) {
	localID, _ := identity.NewAgentID()
	table := routing.NewTable(localID)
	metrics := &RouteMetrics{}

	// Generate routes
	routes := make([]*routing.Route, t.routeCount)
	for i := 0; i < t.routeCount; i++ {
		agentID, _ := identity.NewAgentID()
		// Generate random CIDR
		cidr := fmt.Sprintf("%d.%d.%d.0/24", (i>>16)&255, (i>>8)&255, i&255)
		_, network, _ := net.ParseCIDR(cidr)
		routes[i] = &routing.Route{
			Network:     network,
			NextHop:     agentID,
			OriginAgent: agentID,
			Metric:      uint16(i % 16),
		}
	}

	// Measure insertion time
	insertStart := time.Now()
	for _, route := range routes {
		table.AddRoute(route)
	}
	insertDuration := time.Since(insertStart)
	metrics.InsertionTimeMs = float64(insertDuration.Milliseconds())
	metrics.TotalRoutes = t.routeCount

	// Measure lookup time
	lookupCount := 10000
	lookupStart := time.Now()
	for i := 0; i < lookupCount; i++ {
		ip := net.ParseIP(fmt.Sprintf("%d.%d.%d.1", (i>>16)&255, (i>>8)&255, i&255))
		table.Lookup(ip)
	}
	lookupDuration := time.Since(lookupStart)
	metrics.LookupTimeNs = float64(lookupDuration.Nanoseconds()) / float64(lookupCount)
	metrics.LookupsPerSecond = float64(lookupCount) / lookupDuration.Seconds()

	return metrics, nil
}

// ConnectionChurnTester tests connection churn.
type ConnectionChurnTester struct {
	concurrency int
	duration    time.Duration
	mu          sync.Mutex
}

// NewConnectionChurnTester creates a new connection churn tester.
func NewConnectionChurnTester(concurrency int, duration time.Duration) *ConnectionChurnTester {
	return &ConnectionChurnTester{
		concurrency: concurrency,
		duration:    duration,
	}
}

// ConnectFunc is a function that establishes a connection and returns a close function.
type ConnectFunc func() (closeFunc func() error, err error)

// Run executes the connection churn test.
func (t *ConnectionChurnTester) Run(ctx context.Context, connectFn ConnectFunc) (*ChurnMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, t.duration)
	defer cancel()

	var wg sync.WaitGroup
	metrics := &ChurnMetrics{}
	startTime := time.Now()

	for i := 0; i < t.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.runChurnWorker(ctx, connectFn, metrics)
		}()
	}

	wg.Wait()
	metrics.Duration = time.Since(startTime)

	// Calculate derived metrics
	if metrics.Duration > 0 {
		metrics.ChurnRate = float64(metrics.TotalConnections) / metrics.Duration.Seconds()
	}
	if metrics.SuccessfulConnects > 0 {
		metrics.AvgConnectTimeMs = metrics.AvgConnectTimeMs / float64(metrics.SuccessfulConnects)
	}
	if metrics.TotalDisconnects > 0 {
		metrics.AvgDisconnectTimeMs = metrics.AvgDisconnectTimeMs / float64(metrics.TotalDisconnects)
	}

	return metrics, nil
}

func (t *ConnectionChurnTester) runChurnWorker(ctx context.Context, connectFn ConnectFunc, metrics *ChurnMetrics) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Connect
		connectStart := time.Now()
		closeFunc, err := connectFn()
		connectDuration := time.Since(connectStart)

		atomic.AddInt64(&metrics.TotalConnections, 1)
		if err != nil {
			atomic.AddInt64(&metrics.FailedConnects, 1)
			continue
		}

		atomic.AddInt64(&metrics.SuccessfulConnects, 1)
		t.mu.Lock()
		metrics.AvgConnectTimeMs += float64(connectDuration.Milliseconds())
		t.mu.Unlock()

		// Small delay to simulate actual usage
		time.Sleep(10 * time.Millisecond)

		// Disconnect
		disconnectStart := time.Now()
		if closeFunc != nil {
			closeFunc()
		}
		disconnectDuration := time.Since(disconnectStart)

		atomic.AddInt64(&metrics.TotalDisconnects, 1)
		t.mu.Lock()
		metrics.AvgDisconnectTimeMs += float64(disconnectDuration.Milliseconds())
		t.mu.Unlock()
	}
}

// ThroughputTester tests sustained throughput.
type ThroughputTester struct {
	duration   time.Duration
	bufferSize int
}

// NewThroughputTester creates a new throughput tester.
func NewThroughputTester(duration time.Duration, bufferSize int) *ThroughputTester {
	return &ThroughputTester{
		duration:   duration,
		bufferSize: bufferSize,
	}
}

// ThroughputMetrics contains throughput test results.
type ThroughputMetrics struct {
	TotalBytes     int64
	Duration       time.Duration
	ThroughputMBps float64
	ThroughputGbps float64
}

// Run executes the throughput test.
func (t *ThroughputTester) Run(ctx context.Context, writer io.Writer) (*ThroughputMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, t.duration)
	defer cancel()

	data := make([]byte, t.bufferSize)
	rand.Read(data)

	metrics := &ThroughputMetrics{}
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			goto done
		default:
		}

		n, err := writer.Write(data)
		if err != nil {
			break
		}
		metrics.TotalBytes += int64(n)
	}

done:
	metrics.Duration = time.Since(startTime)
	if metrics.Duration > 0 {
		seconds := metrics.Duration.Seconds()
		metrics.ThroughputMBps = float64(metrics.TotalBytes) / (1024 * 1024) / seconds
		metrics.ThroughputGbps = float64(metrics.TotalBytes) * 8 / (1000 * 1000 * 1000) / seconds
	}

	return metrics, nil
}
