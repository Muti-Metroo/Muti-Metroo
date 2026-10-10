# Integration Test Concepts

This document describes the principles and conventions for integration tests in Muti Metroo. Read this before writing new tests.

A live coverage matrix lives in [`coverage.csv`](./coverage.csv). When you add or extend a test, update the matching row in that file.

## Two test surfaces

Integration tests live in two places, and both should be used:

| Surface | Location | When to use |
|---|---|---|
| **In-process Go tests** | `internal/integration/*_test.go` | Most behavior verification. Fast iteration. Runs under `make test` and `go test -race`. Uses in-process agent fixtures (`TestAgent`, `MeshAgent`, `MeshRunner`) with real listeners on ephemeral ports. |
| **Docker e2e shell tests** | `testbed/e2e-test.sh` + `docker-compose.yml` | Whole-system smoke tests against an actual 6-agent Docker mesh. Use for validating production-like deployment, cross-container networking, real DNS, real TLS, and binary-level behavior. |

Default to in-process Go tests for new coverage. Reach for the Docker testbed when:
- The test needs a real network namespace, real DNS, or real cross-host TCP behavior
- You are validating CLI binaries (not just library code) end to end
- You need to verify behavior under real container restarts or signals
- You are testing a feature where in-process fakes would hide a real failure mode (e.g. HTTP CONNECT proxy traversal, mTLS chain validation against a real CA, behavior behind a real reverse proxy)

When in doubt, write the in-process test first because it runs in seconds, then add a Docker case only if it catches something the Go test can't.

## The three-agent minimum for tunnel tests

**Any test that exercises tunneling, relaying, or multi-hop behavior must use at least three agents.**

A two-agent setup (`A -> B`) is a direct connection. It does **not** exercise the relay code path at all, because there is no transit hop in between. A two-agent test of "streaming through the mesh" only proves that an ingress can talk to an exit when they are peered directly, which is not what production looks like.

A three-agent chain (`A -> B -> C`) is the minimum that:
- Exercises a real transit node (B) that must forward frames between two peer connections
- Validates the frame dispatcher in `internal/agent/agent.go` actually relays
- Confirms route propagation across more than one hop
- Confirms that transit (B) cannot decrypt the E2E payload (since the ephemeral keys are between A and C only)
- Surfaces stream-multiplexing bugs that only show up with relay

For most "core mesh" features, prefer a **four-agent chain** (`A -> B -> C -> D`). This is the established pattern in the existing tests. With two transit hops it catches a class of issues that only appear when the transit chain has length > 1: broken seen-by tracking, off-by-one route metrics, cascading reconnect failures, and frame ordering bugs that only manifest when frames cross more than one peer boundary.

Use more than four agents only when the test specifically needs branching topology, redundancy, or sleep/wake propagation against a wider mesh. The 5-agent sleep mesh in `sleep_test.go` and the 6-agent Docker testbed are the existing examples.

**Exceptions** (where two agents are sufficient):
- Pure peer-handshake / reconnection tests (`reconnect_test.go::TestPeerReconnection`)
- Single-transport wire tests for H2/WS (`transport_relay_test.go`)
- Local-only feature tests where the mesh is incidental (CLI tooling, embedded config, certificate generation)

## Topology choices

| Topology | Use when |
|---|---|
| **Linear chain** (`A-B-C-D`) | Default. Easiest to reason about. Catches most relay/routing bugs. |
| **Hub-and-spoke** (`A-B`, `A-C`, `A-D`) | Validating that a hub correctly fans out to many peers, route flooding to siblings, and that B/C/D can reach each other through A. |
| **Mesh with redundant paths** | Loop prevention (`SeenBy`), longest-prefix-match with metric tiebreakers, route convergence after a single peer drop. |
| **Asymmetric tree** | Domain-route tests, route propagation across different branch depths. |
| **6-agent Docker mesh** (testbed) | Whole-system smoke. Do not write a new Go test in this shape -- extend `testbed/e2e-test.sh` instead. |

Avoid inventing new topologies unless the existing ones cannot express what you are testing. Reuse `MeshRunner` / `TestAgent` fixtures rather than rolling your own.

## Real network, real protocol

Integration tests **must** use real listeners and real transports. Do not mock `transport.PeerConn` or stub the frame protocol. The point of an integration test is to catch interactions the unit tests cannot see; mocking the wire defeats the purpose.

Conventions:
- Use **ephemeral ports** in Go tests (`:0` then read back `Addr()`). Never hard-code ports inside `internal/integration/`. Fixed ports belong only in the Docker testbed.
- Use real **echo servers** (`net.Listen("tcp", ":0")`) as upstream targets. The existing tests already wire this up; reuse the helpers.
- Use real **TLS** with auto-generated self-signed certs unless the test is specifically about CA-signed validation.
- Use real **DNS** only when the test is specifically about DNS behavior. For everything else, target IPs directly so DNS flakiness cannot bury a real failure.

## State, determinism, and cleanup

- **Always** `defer` agent shutdown so a panic in the middle of a test does not leak goroutines or sockets. The race detector will catch leaks; do not let them accumulate.
- Use `t.TempDir()` for any data directory. Never write to `./testdata` or hard-coded paths.
- If a test depends on route propagation, **poll** with a deadline rather than `time.Sleep`. The polling pattern in `testbed/e2e-test.sh::wait_for_routes` is the reference -- copy it.
- For Docker tests, reset `sleep_state.json` files at the start of the run so a previous failed run does not poison the next one. The e2e shell script already does this.
- Tests that take more than ~5 seconds should respect `testing.Short()` and skip themselves under `go test -short`.

## Race detection is mandatory

All integration tests must pass under `go test -race`. The CI pipeline runs with `-race` enabled. If your test passes locally but only sometimes -- it has a race, and the right fix is to find the race, not to retry. Two recent fixes (`peer/connection.go` send-on-closed-channel, `sleep/sleep.go` callback-overlap) were caught exactly this way.

## Flakiness budget: zero

A flaky integration test is worse than no test, because it trains everyone to ignore failures. If a new test flakes even once during development, fix it before merging. Common root causes:
- Polling without a deadline -> use the deadline pattern
- Hard-coded sleeps -> use polling
- Shared global state (ports, files, env vars) -> use ephemeral resources
- Insufficient cleanup -> use `defer` and `t.Cleanup`
- Real-world DNS or external services -> avoid them; use direct IPs and local fixtures

## When you add or modify a test

1. Find the matching row in [`coverage.csv`](./coverage.csv). If none exists, add one in the right category.
2. Set `Coverage` to `Full` (or `Partial` if the test only covers some of the feature).
3. Add the test reference in `GoIntegrationTests` or `E2EShellTests` using the abbreviated form (e.g., `agent_chain::BasicConnectivity`).
4. Update the `Notes` column if the test makes a non-obvious choice that future contributors should know about.

Keeping the CSV current is what makes it useful as a planning document. A stale CSV will mislead the next person looking for gaps.
