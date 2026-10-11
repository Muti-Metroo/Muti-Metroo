---
title: Routing
description: Configuration reference for the routing section, covering route advertisement interval, node info interval, route expiry, and the max_hops setting.
sidebar_position: 13
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-plumbing.png" alt="Mole configuring routing" style={{maxWidth: '180px'}} />
</div>

# Routing

The `routing` section sets how often the agent re-advertises its routes and node information to the mesh, and how long it keeps routes learned from other agents. The defaults suit most meshes.

## Configuration

```yaml
routing:
  advertise_interval: 2m   # Re-advertise local routes
  node_info_interval: 2m   # Re-advertise node info (defaults to advertise_interval)
  route_ttl: 5m            # Drop learned routes not refreshed within this time
  max_hops: 16
```

## Keys

| Key                  | Type     | Default                | Description                                                                                                                            |
| -------------------- | -------- | ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `advertise_interval` | duration | `2m`                   | How often the agent floods its routes (CIDR, domain, and forward keys). `0` uses `2m`.                                                 |
| `node_info_interval` | duration | `advertise_interval`   | How often the agent floods its node information (display name, system details, peers, advertised listeners). Unset or `0` uses `advertise_interval`. |
| `route_ttl`          | duration | `5m`                   | A learned route that is not refreshed within this time is removed. `0` uses five times `advertise_interval`.                           |
| `max_hops`           | int      | `16`                   | Must be between 1 and 255. Validated, but the current release does not limit route propagation by it.                                  |

## Advertisement and expiry

Agents re-advertise their routes every `advertise_interval`, and each agent drops routes it has not heard about for `route_ttl`. Keep `route_ttl` at two or more advertisement intervals so a single lost advertisement does not remove a route.

Changes made at runtime (route, forward, and display name changes) are advertised immediately. To trigger an advertisement by hand:

```bash
curl -X POST http://localhost:8080/routes/advertise
```

Shorter intervals make the mesh notice a lost exit sooner, at the cost of more control traffic:

| `advertise_interval` | `route_ttl` | Effect                          |
| -------------------- | ----------- | ------------------------------- |
| `30s`                | `90s`       | Faster failover, more traffic   |
| `2m`                 | `5m`        | Default                         |
| `5m`                 | `12m`       | Less traffic, slower failover   |

`node_info_interval` only affects how quickly dashboards see updated agent details.

## Examples

### Faster convergence

```yaml
routing:
  advertise_interval: 30s
  route_ttl: 90s
```

### Low-bandwidth links

```yaml
routing:
  advertise_interval: 5m
  node_info_interval: 10m
  route_ttl: 12m
```

## See Also

- [Routing concepts](/concepts/routing) for route selection and flood propagation
- [Connections](/configuration/connections) for keepalive and reconnection timing
- [Limits](/configuration/limits) for stream limits and the stream open timeout
- [Exit](/configuration/exit) for the routes an agent advertises
- [Route Management API](/api/route-management) for the advertise trigger and route table endpoints
