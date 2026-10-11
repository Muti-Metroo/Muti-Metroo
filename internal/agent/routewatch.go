package agent

import (
	"sync"

	"github.com/Muti-Metroo/Muti-Metroo/internal/health"
	"github.com/Muti-Metroo/Muti-Metroo/internal/routing"
)

// routeWatchProvider adapts the agent to socks5.RouteWatchProvider. It serves
// dashboard snapshots (the same JSON as GET /api/dashboard) and fans a
// coalesced change signal out to every active watch connection. The upstream
// signal is driven by routing-table changes and mesh-host/suffix edits.
type routeWatchProvider struct {
	snapshot func() ([]byte, error)

	mu     sync.Mutex
	subs   map[int]chan struct{}
	nextID int
}

func newRouteWatchProvider(snapshot func() ([]byte, error)) *routeWatchProvider {
	return &routeWatchProvider{
		snapshot: snapshot,
		subs:     make(map[int]chan struct{}),
	}
}

// Snapshot returns the current dashboard JSON.
func (p *routeWatchProvider) Snapshot() ([]byte, error) {
	return p.snapshot()
}

// Subscribe registers a new watch connection and returns its signal channel
// plus an unsubscribe func. The channel is buffered (depth 1) and pulsed
// non-blocking, so a slow watcher only ever sees a coalesced "something
// changed" rather than a backlog.
func (p *routeWatchProvider) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	p.mu.Lock()
	id := p.nextID
	p.nextID++
	p.subs[id] = ch
	p.mu.Unlock()

	return ch, func() {
		p.mu.Lock()
		delete(p.subs, id)
		p.mu.Unlock()
	}
}

// signal pulses every subscriber (non-blocking). Called from the upstream
// change sources.
func (p *routeWatchProvider) signal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ch := range p.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// snapshotServerForRouteWatch returns a health.Server wired with the providers
// BuildDashboardSnapshot needs. It reuses the HTTP health server when one
// exists; otherwise it builds a listener-less instance so route-watch works
// even when the HTTP API is disabled (single-endpoint deployments).
func (a *Agent) snapshotServerForRouteWatch() *health.Server {
	if a.healthServer != nil {
		return a.healthServer
	}
	if a.snapshotServer == nil {
		srv := health.NewServer(health.ServerConfig{}, &agentStatsProvider{agent: a})
		srv.SetRemoteProvider(a)
		srv.SetSealedBox(a.sealedBox)
		srv.SetMeshHostProvider(a)
		a.snapshotServer = srv
	}
	return a.snapshotServer
}

// setupRouteWatch builds the route-watch provider, wires its upstream change
// sources (routing manager + mesh-host store), starts the coalescing drain
// goroutine, and returns the provider for installation on the SOCKS5 server.
func (a *Agent) setupRouteWatch() *routeWatchProvider {
	snapSrv := a.snapshotServerForRouteWatch()
	prov := newRouteWatchProvider(snapSrv.BuildDashboardSnapshotJSON)

	// Mesh-host and suffix edits do not flow through the routing change bus,
	// so hook the store directly.
	if a.meshHosts != nil {
		a.meshHosts.SetOnChange(prov.signal)
	}

	// Route changes (local + flood-learned) arrive on a buffered channel; the
	// manager's notify is non-blocking and may drop, which is fine -- a missed
	// signal only delays the next snapshot, and the full state is always
	// re-sent.
	changes := make(chan routing.RouteChange, 64)
	a.routeMgr.Subscribe(changes)

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			select {
			case <-a.stopCh:
				return
			case <-changes:
				prov.signal()
			}
		}
	}()

	return prov
}
