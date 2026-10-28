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
  "all_providers": false,
  "providers": [
    "provider-a",
    "provider-b",
  ],
  "exclude": "",
  "include": "",
  "health_check": "",
  "pick": {
    "objective": "leastload",
    "strategy": "random",
    "max_fail": 0,
    "max_rtt": "1000ms",
    "tolerance": 0.2,
    "min_nodes": 1,
    "max_nodes": 0,
    "jitter_weight": 1,
    "biases": [
      {
        "contains": "keyword",
        "prefix": "",
        "suffix": "",
        "regexp": "",
        "rtt_scale": 10
      }
    ]
  },
  "interrupt_exist_connections": false
}
```

### Fields

#### outbounds

List of outbound tags.

#### all_providers

When `all_providers` is `true`, all providers will be used instead of just those in the `providers` list. The default value is `false`.

#### providers

List of provider tags.

#### exclude

Exclude regular expression to filter `providers` nodes. The priority of the exclude expression is higher than the include expression.

#### include

Include regular expression to filter `providers` nodes.

#### health_check

The [Health Check](/configuration/shared/health-check/) used to test the nodes. A string tag of a top-level `health_checks` entry, or an inline health check object.

Required. There is no implicit default health check.

#### pick

See "Pick Fields"

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

Both `leastload` and `leastping` rank the nodes by a score, lower is better:

```
cost = STD                          # leastload
cost = MED + jitter_weight * STD    # leastping

score = (ALL - FAIL) / ALL * cost + FAIL / ALL * TIMEOUT
```

`MED` and `STD` are the median and the standard deviation of the **successful**
checks, `FAIL / ALL` the share of the checks that failed, and `TIMEOUT` the timeout a
failed check waits for, 15s.

The delay statistics describe the checks that worked, so the failures are mixed in by
their own share: an attempt either succeeds with `cost` or fails after `TIMEOUT`. The
score is what an attempt costs on average, it never exceeds the timeout, and a node
that never worked is scored the timeout instead of the zero of a missing value.

#### strategy

The strategy of load balancing. Default is `random`.

| Strategy         | Description                                        |
| ---------------- | -------------------------------------------------- |
| `random`         | Pick randomly from nodes match the objective       |
| `roundrobin`     | Rotate from nodes match the objective              |
| `consistenthash` | Use same node for requests to same origin targets. |

Note: `consistenthash` requires a relatively stable quantity of nodes, it's available only when the objective is `alive`

#### max_rtt

The maximum round-trip time of health check that is acceptable for qulified nodes. The median RTT of the successful checks is compared, so a single spike doesn't disqualify an otherwise good node. Default is `0`, which accepts any round-trip time.

#### max_fail

The maximum number of health check failures for qulified nodes, default is `0`, i.e. no failures allowed.

#### tolerance

> Available only for `least*` objectives

The maximum relative difference to the best node, default is `0.2`, i.e. nodes that are at most 20% worse than the best one are selected with it.

The value compared is the [score](#objective): the jitter is part of it for `leastping`, and the failures of both objectives are part of it by their share of the samples.

The tolerance is applied to the best node in the selection, so a clearly worse node is never selected just to fill a count.
A minimum absolute difference of `10ms` is always used, so tiny values, such as the deviation of an always stable node, aren't split by noise.

For example, with `tolerance: 0.2` and nodes of `174ms`, `504ms`, `505ms`, only the `174ms` node is selected, as the others are nearly three times slower.

#### min_nodes

> Available only for `least*` objectives

The minimum number of nodes to select, default is `1`.

This only affects the case when the nodes within the tolerance are fewer than `min_nodes`, e.g. `min_nodes: 2` selects the runner-up even when it is outside the tolerance, which trades quality for redundancy.

#### max_nodes

> Available only for `least*` objectives

The maximum number of nodes to select, default is `0`, which means unlimited.

This affects the case when the nodes are equally good, e.g. when all nodes are within the tolerance, only `max_nodes` of them are selected to avoid spreading connections over every node.

#### jitter_weight

> Available only for `least*` objectives

Scales the standard deviation added to the [score](#objective). Default is `1`, so a node whose RTT deviates by 300ms is scored as 300ms slower than its median.

Set it lower if you prefer a low but unstable node over a slightly slower steady one. It has no effect on `leastload`, whose score is the standard deviation itself.

#### biases

The picking bias of load balancing. Default is empty.

When the node tag matches the condition in `biases`,
its round-trip time (or standard deviation) will be multiplied by `rtt_scale` for comparison when picking.
The default value of `rtt_scale` is `1`, and the larger it is, the less preferred the node is. 
For example, `rtt_scale: 10` means that when the node's round-trip time is `100ms`, it will be treated as `1000ms` for comparison.

- `contains` means matching when the node tag contains a keyword.
- `prefix` means matching when the node tag starts with a keyword.
- `suffix` means matching when the node tag ends with a keyword.
- `regexp` means matching when the node tag matches a regular expression.

If multiple conditions are configured, matching any one of them is sufficient.

#### interrupt_exist_connections

Interrupt existing connections when the selected outbound has changed.

Only inbound connections are affected by this setting, internal connections will always be interrupted.

For `least*` objectives the connections are kept while the node that left the selection is still within the noise of the health check statistics, so the connections are not closed by a measurement that could go either way. New connections always keep to the selection. The allowance is at most the tolerance itself, so a node never survives more than twice the tolerance.

The retention applies to the inbound connections only, and only when this option is enabled: internal connections are interrupted either way.