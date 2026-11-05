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
    "objective": "alive",
    "strategy": "random",
    "max_fail": 0,
    "max_rtt": "1000ms",
    "tolerance": "50ms",
    "min_nodes": 1,
    "max_nodes": 0,
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

| Objective   | Description                                                |
| ----------- | ---------------------------------------------------------- |
| `alive`     | prefer alive nodes                                         |
| `qualified` | prefer qualified nodes (`max_rtt`, `max_fail`)             |
| `quick`     | the nodes that typically cost the least: delay plus jitter |
| `idle`      | the nodes that queue the least         |

Load balancing divides nodes into three classes:

1. Failed Nodes, that cannot be connected
2. Alive Nodes, that pass the health check
3. Qualified Nodes, that are alive and meet the constraints (`max_rtt`, `max_fail`)

It tries to pick from the class that the objective is targeting (see the table above). If there is no node for current class, it falls back to the next class. For example, the behavior of `idle` could be:

- Apply `idle` to the qualified nodes
- Apply `idle` to the alive nodes
- Apply `idle` to the failed nodes (could be temporarily failed)

Generally speaking, use `idle`, `quick` for better network quality; use `alive` for more quantity of outbound nodes.

Both `idle` and `quick` rank the nodes by a score, lower is better:

```
MED     = median(x₁ … xₙ)
JIT     = 1.4826 * median(|xᵢ − MED|)
DEV     = (1/n) * Σ|xᵢ − MED|
cost    = MED + JIT        # quick
failure = min(TIMEOUT, 3 * cost)
standing= MED - BASELINE   # idle: over the node's own best

score   = (ALL - FAIL) / ALL * cost + FAIL / ALL * failure   # quick
score   = min(TIMEOUT, standing)                             # idle
```

`MED`, `JIT` and `DEV` describe the **successful** checks only, so the successes and the
failures are mixed in by their shares.

- `MED` is how far the node is: the median delay, what a check usually costs.
- `JIT` is the **typical** deviation of the checks from that median, which one slow check
  cannot move. `1.4826` turns the median of the absolute deviations into an estimate of the
  standard deviation (`MAD ≈ 0.6745σ` for normal samples).
- `DEV` is the **average** deviation: a spike is not ignored, and it describes the window as
  a whole.
- `TIMEOUT` is the timeout a failed check waits for, 15s.

The reasoning behind the statistics, the scores and the tolerance is on the
[Node Rating](/configuration/shared/node-rating/) page.

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

The `idle` objective cannot reflect a failure in its score, so you may want to use this setting to
remove the nodes that failed during the check window.

#### tolerance

> Only used by the `quick` and `idle` objectives: `alive` and `qualified` have no score, so
> setting it has no effect

The maximum **absolute** difference to the best node, default is `50ms` for `quick` and `10ms` for
`idle`, i.e. nodes whose score is at most this far above the best one are selected with it. The
two scores are of different magnitudes, which is why the defaults differ: the score of `quick` is a
delay of hundreds of milliseconds, the one of `idle` the queueing it adds on top of its own
best, which is of the same order.

The band also carries a **resolution margin** that shrinks with the sample count and grows with the
jitter of the best node, so it is never narrower than this value.

#### min_nodes

> Available only for the `quick` and `idle` objectives

The minimum number of nodes to select, default is `1`.

This only affects the case when the nodes within the tolerance are fewer than `min_nodes`, e.g. `min_nodes: 2` selects the runner-up even when it is outside the tolerance, which trades quality for redundancy.

#### max_nodes

> Available only for the `quick` and `idle` objectives

The maximum number of nodes to select, default is `0`, which means unlimited.

This affects the case when the nodes are equally good, e.g. when all nodes are within the tolerance, only `max_nodes` of them are selected to avoid spreading connections over every node.

#### biases

The picking bias of load balancing. Default is empty.

When the node tag matches the condition in `biases`,
its round-trip time (or the spread it is ranked by) will be multiplied by `rtt_scale` for comparison when picking.
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

For `quick` and `idle` objectives the connections are kept while the node that left the selection is still within the noise of the health check statistics, so the connections are not closed by a measurement that could go either way. New connections always keep to the selection. The allowance is at most the band itself, so a node never survives more than twice its band away from the best. The `alive` and `qualified` objectives have no band, so a node stops being retained as soon as it leaves the selection.

The retention applies to the inbound connections only, and only when this option is enabled: internal connections are interrupted either way.