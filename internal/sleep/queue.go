package sleep

import (
	"sync"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

// PeerQueue holds queued state for a single sleeping peer.
type PeerQueue struct {
	Routes    []*protocol.RouteAdvertise
	Withdraws []*protocol.RouteWithdraw
	NodeInfos []*protocol.NodeInfoAdvertise
}

// StateQueue manages queued state for all sleeping peers.
type StateQueue struct {
	mu       sync.RWMutex
	queues   map[identity.AgentID]*PeerQueue
	maxItems int // Maximum items per type per peer
}

// NewStateQueue creates a new state queue with the given maximum items per peer.
func NewStateQueue(maxItems int) *StateQueue {
	if maxItems <= 0 {
		maxItems = 1000
	}
	return &StateQueue{
		queues:   make(map[identity.AgentID]*PeerQueue),
		maxItems: maxItems,
	}
}

// getOrCreate returns the queue for a peer, creating it if necessary.
// Must be called with mu held for writing.
func (q *StateQueue) getOrCreate(peerID identity.AgentID) *PeerQueue {
	pq, ok := q.queues[peerID]
	if !ok {
		pq = &PeerQueue{}
		q.queues[peerID] = pq
	}
	return pq
}

// AddRoute queues a route advertisement for a peer.
func (q *StateQueue) AddRoute(peerID identity.AgentID, adv *protocol.RouteAdvertise) {
	if adv == nil {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	pq := q.getOrCreate(peerID)

	// Check for duplicate (same origin + sequence)
	for _, existing := range pq.Routes {
		if existing.OriginAgent == adv.OriginAgent && existing.Sequence == adv.Sequence {
			return // Already have this one
		}
	}

	// Evict oldest if at capacity
	if len(pq.Routes) >= q.maxItems {
		pq.Routes = pq.Routes[1:]
	}

	pq.Routes = append(pq.Routes, adv)
}

// AddWithdraw queues a route withdrawal for a peer.
func (q *StateQueue) AddWithdraw(peerID identity.AgentID, withdraw *protocol.RouteWithdraw) {
	if withdraw == nil {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	pq := q.getOrCreate(peerID)

	// Check for duplicate
	for _, existing := range pq.Withdraws {
		if existing.OriginAgent == withdraw.OriginAgent && existing.Sequence == withdraw.Sequence {
			return
		}
	}

	// Evict oldest if at capacity
	if len(pq.Withdraws) >= q.maxItems {
		pq.Withdraws = pq.Withdraws[1:]
	}

	pq.Withdraws = append(pq.Withdraws, withdraw)
}

// AddNodeInfo queues a node info advertisement for a peer.
func (q *StateQueue) AddNodeInfo(peerID identity.AgentID, info *protocol.NodeInfoAdvertise) {
	if info == nil {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	pq := q.getOrCreate(peerID)

	// For node info, we keep only the latest per origin agent
	// since older ones are superseded
	for i, existing := range pq.NodeInfos {
		if existing.OriginAgent == info.OriginAgent {
			// Replace with newer
			if info.Sequence > existing.Sequence {
				pq.NodeInfos[i] = info
			}
			return
		}
	}

	// Evict oldest if at capacity
	if len(pq.NodeInfos) >= q.maxItems {
		pq.NodeInfos = pq.NodeInfos[1:]
	}

	pq.NodeInfos = append(pq.NodeInfos, info)
}

// GetAndClear retrieves and clears all queued state for a peer.
// Returns nil if no state is queued.
func (q *StateQueue) GetAndClear(peerID identity.AgentID) *protocol.QueuedState {
	q.mu.Lock()
	defer q.mu.Unlock()

	pq, ok := q.queues[peerID]
	if !ok {
		return nil
	}

	// Convert to protocol.QueuedState
	state := &protocol.QueuedState{
		Routes:    make([]protocol.RouteAdvertise, len(pq.Routes)),
		Withdraws: make([]protocol.RouteWithdraw, len(pq.Withdraws)),
		NodeInfos: make([]protocol.NodeInfoAdvertise, len(pq.NodeInfos)),
	}

	for i, r := range pq.Routes {
		state.Routes[i] = *r
	}
	for i, w := range pq.Withdraws {
		state.Withdraws[i] = *w
	}
	for i, n := range pq.NodeInfos {
		state.NodeInfos[i] = *n
	}

	// Clear the queue for this peer
	delete(q.queues, peerID)

	// Return nil if everything was empty
	if len(state.Routes) == 0 && len(state.Withdraws) == 0 && len(state.NodeInfos) == 0 {
		return nil
	}

	return state
}

// PeerCount returns the number of peers with queued state.
func (q *StateQueue) PeerCount() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queues)
}

// Clear removes all queued state for all peers.
func (q *StateQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queues = make(map[identity.AgentID]*PeerQueue)
}

// HasStateFor returns true if there is queued state for the given peer.
func (q *StateQueue) HasStateFor(peerID identity.AgentID) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	pq, ok := q.queues[peerID]
	if !ok {
		return false
	}

	return len(pq.Routes) > 0 || len(pq.Withdraws) > 0 || len(pq.NodeInfos) > 0
}

// Stats returns statistics about the queue.
func (q *StateQueue) Stats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	stats := QueueStats{
		PeerCount: len(q.queues),
	}

	for _, pq := range q.queues {
		stats.TotalRoutes += len(pq.Routes)
		stats.TotalWithdraws += len(pq.Withdraws)
		stats.TotalNodeInfos += len(pq.NodeInfos)
	}

	return stats
}

// QueueStats contains statistics about the state queue.
type QueueStats struct {
	PeerCount      int `json:"peer_count"`
	TotalRoutes    int `json:"total_routes"`
	TotalWithdraws int `json:"total_withdraws"`
	TotalNodeInfos int `json:"total_node_infos"`
}
