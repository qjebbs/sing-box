### Structure

```json
{
  "type": "loadbalance",
  "tag": "loadbalance",
  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ],
  "providers": [
    "provider-a",
    "provider-b",
  ],
  "exclude": "",
  "include": "",
  "check": {
    "interval": "5m",
    "sampling": 10,
    "destination": "https://www.gstatic.com/generate_204",
    "detour_of": [
      "proxy-a",
      "proxy-b"
    ]
  },
  "pick": {
    "objective": "leastload",
    "strategy": "random",
    "max_fail": 0,
    "max_rtt": "1000ms",
    "expected": 3,
    "baselines": [
      "30ms",
      "50ms",
      "100ms",
      "150ms",
      "200ms",
      "250ms",
      "350ms"
    ]
  }
}
```

### Fields

#### outbounds

List of outbound tags.

#### providers

List of provider tags.

#### exclude

Exclude regular expression to filter `providers` nodes. The priority of the exclude expression is higher than the include expression.

#### include

Include regular expression to filter `providers` nodes.

#### check

See "Check Fields"

#### pick

See "Pick Fields"

### Check Fields

#### interval

The interval of health check for each node. Must be greater than `10s`, default is `5m`.

#### sampling

The number of recent health check results to sample. Must be greater than `0`, default is `10`.

#### destination

The destination URL for health check. Default is `http://www.gstatic.com/generate_204`.

#### detour_of

Let's say you have an outbound chain:


```
                 (detour="B")                 (detour="C")
Shadowsocks (A) -------------> ShadowTLS (B) --------------> LoadBalance (C)
```

And you want the chain of the health check of each node to be exactly the same as above, just set the configuration of `C` to `detour_of: ["A", "B"]`, the check chain will be:

```
Shadowsocks (A) ---> ShadowTLS (B) ---> [Node]
```

> If not, it would be almost impossible to detect such nodes, which are fine to use directly, but not when they're used as an upstream, due to audit rules and other reasons.

### Pick Fields

#### objective

The objective of load balancing. Default is `alive`.

| Objective   | Description                                    |
| ----------- | ---------------------------------------------- |
| `alive`     | prefer alive nodes                             |
| `qualified` | prefer qualified nodes (`max_rtt`, `max_fail`) |
| `leastload` | least load nodes from qualified                |
| `leastping` | least latency nodes from qualified             |

Load balancing divides nodes into three classes:

1. Failed Nodes, that cannot be connected
2. Alive Nodes, that pass the health check
3. Qualified Nodes, that are alive and meet the constraints (`max_rtt`, `max_fail`)

It tries to pick from the class that the objective is targeting (see the table above). If there is no node for current class, it falls back to the next class. For example, the behavior of `leastload` could be:

- Pick least load nodes from qualified ones
- Pick least load nodes from alives ones
- Pick least load nodes from failed ones (could be temporarily failed)

Generally speaking, use `leastload`, `leastping` for better network quality; use `alive` for more quantity of outbound nodes.

#### strategy

The strategy of load balancing. Default is `random`.

| Strategy         | Description                                        |
| ---------------- | -------------------------------------------------- |
| `random`         | Pick randomly from nodes match the objective       |
| `roundrobin`     | Rotate from nodes match the objective              |
| `consistenthash` | Use same node for requests to same origin targets. |

Note: `consistenthash` requires a relatively stable quantity of nodes, it's available only when the objective is `alive`

#### max_rtt

The maximum round-trip time of health check that is acceptable for qulified nodes. Default is `0`, which accepts any round-trip time.

#### max_fail

The maximum number of health check failures for qulified nodes, default is `0`, i.e. no failures allowed.

#### expected / baselines

> Available only for `least*` objectives

`expected` is the expected number of nodes to be selected. The default value is 1.

`baselines` divide the nodes into different ranges. The default value is empty. For `leastload`, it divides according to the standard deviation (STD) of RTTs; For `leastping`, it divides according to the average of RTTs.

Here are typical configuration for `leastload`:

1. `expected: 3`, select 3 nodes with the smallest STD.

1. `expected:3, baselines =["50ms"]`, If there are more than 3 nodes with sufficient stability (STD<50ms), select as many as there are. Otherwise, select the top 3 nodes with the smallest STD.

1. `expected:3, baselines =["30ms","50ms","100ms"]`, try different baselines until we find at least 3 nodes. Otherwise select the top 3 nodes with the smallest STD. The advantage is that it can find a proper quantity of nodes without wasting nodes with similar qualities.

1. `baselines: ["30ms","50ms","100ms"]`, try to select nodes by different baselines. If there is no node matching any baseline, return the one with the smallest STD.
