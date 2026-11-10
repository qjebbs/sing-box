### Structure

A string or an object.

When string, the tag of a shared health check defined in the top-level [`health_checks`](/configuration/#health_checks) option.

When object:

```json
{
  "interval": "5m",
  "sampling": 10,
  "url": "https://www.gstatic.com/generate_204",
  "idle_timeout": "30m",
  "relay_path": [
    "proxy-a",
    "proxy-b"
  ]
}
```

Health checks are used to check and provide health status information of nodes for outbound groups.
They can be shared by multiple outbound groups to avoid redundant health check overhead.

The connections the groups carry are watched as well, so a node that passes its checks
and then drops the traffic is not trusted until its next scheduled check: after a few
failed connections within a short window the node is checked again. The check replaces
its latest result instead of adding one, so the results of every node stay as evenly
sampled as the interval made them.

That check is also the one that decides whether the node is used right now. A failed
connection can be the client, the destination or the network, which is why a few of them
are needed and why a failed scheduled check on its own never removes a node; but a node
whose own connections were already failing, and whose check then failed too, is suspended:
it is not selected for new connections until a check passes. A suspended node ranks with
the nodes that are alive but not qualified, so it is still used when nothing better is
left. The health report marks it with `suspended`.

Each entry of the top-level `health_checks` uses the same object structure, with an additional `tag` field:

```json
{
  "health_checks": [
    {
      "tag": "default",
      "interval": "5m",
      "sampling": 10,
      "url": "https://www.gstatic.com/generate_204"
    }
  ]
}
```

!!! warning ""

    A health check must be configured explicitly on each outbound group. There is no implicit default health check.

### Fields

#### interval

The interval of health check for each node. Must be greater than `10s`, default is `5m`.

#### sampling

The number of recent health check results to sample. Must be greater than `0`, default is `10`.

#### url

The URL for health check. Default is `https://www.gstatic.com/generate_204`.

!!! note ""

    Checks only run while the group is in use: a health check that is never used performs no checks at all.
    The first use starts the check loop, which then runs one round right away.

#### idle_timeout

The duration after which checks are paused when all outbound groups using this
health check are inactive. Checks resume as soon as a group is used again. `0`
keeps checking continuously, values smaller than `interval` are raised to
`interval`.

If this health check is used by any outbound group with an `idle` objective, idle probes continue
while regular checks are paused. At most one check is made per 30-minute slot,
providing a reliable baseline for evaluating node load.

#### relay_path

Run the health check as a relay, with the checked node as the entry.

Restrictions: The nodes other than the entry cannot be outbound groups, so `selector`, `loadbalance`,
etc. are not supported here.

Let's say the Entry is an outbound group, and you configure a relay as follows:

```json
{
  "tag": "relay",
  "type": "relay",
  "outbounds": ["Entry Group", "Hop", "Exit"]
}
```

The actual chain is: `a node of Entry Group` -> `Hop` -> `Exit`

And you want the health check chain to be the same as above, just configure the nodes other than
the entry:

```json
"relay_path": ["Hop", "Exit"]
```

The check chain will be: `the checked node of Entry Group` -> `Hop` -> `Exit`

If not, it would be almost impossible to detect such nodes, which are fine to use directly, but not when they're used as an upstream, due to audit rules and other reasons.
Configuring this item can also avoid the server from hijacking the test request, and improve the accuracy of the health check.

### Recommended configuration

The `quick` and `idle` objectives rank the nodes by the most recent `sampling` checks, over a
window of `interval × sampling`. Recommended:

| Case | `interval` | `sampling` | Span |
| --- | --- | --- | --- |
| Everyday load balancing (recommended) | `2m` | `10` | 20 minutes |
| Few nodes, cheap checks | `1m` | `15` | 15 minutes |
| Many nodes, steadier | `2m` | `15` | 30 minutes |
| The defaults, limited checks (span beyond the recommendation) | `5m` | `10` | 50 minutes |

For load balancing, at least 10 samples, and no more than 30 minutes of span.

`urltest` uses a single node, so a 10 minute window is enough and there is no reason to stretch the
span for more samples:

| Case | `interval` | `sampling` | Span |
| --- | --- | --- | --- |
| Recommended (half the checks) | `2m` | `5` | 10 minutes |
| Changes found sooner | `1m` | `10` | 10 minutes |

Both cover the same span, and the measured agreement of the pick between them is within noise.
`2m × 5` holds only five samples, so its median is coarser and the resolution margin of the band is
about 1.4 times wider, and it sees any change up to two minutes later: the interval is the reaction
granularity. Beyond 10 minutes of span, the node a window picks stops matching how it behaves
afterwards.

Keep `max_fail` at `0`: it says "not one failure in this window" and stops an unstable node
earlier and harder than a score can. Raised to `1` or more, a single failure is worth only 50
points of `idle` at ten samples, which does not move a node out of the band, and what is left to
stop it is the long-run failure rate — evidence that builds up slowly. Leave `tolerance` at its
default.
