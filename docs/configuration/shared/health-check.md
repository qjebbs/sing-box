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

The duration after which health checks are paused when no outbound group using
this health check has been in use, 0 to keep checking permanently.

When a shared health check is used by multiple outbound groups, the checks are
only paused after every group has been idle for this duration.

While paused, no node is tested. Checking resumes as soon as a group is used
again, starting with an immediate check to refresh the statistics, since results
older than `(sampling+1) * interval` are discarded.

Values smaller than `interval` are raised to `interval`.

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
| Few nodes, cheap checks | `1m` | `15` | 15 minutes |
| Everyday load balancing (recommended) | `2m` | `10` | 20 minutes |
| Many nodes, steadier | `2m` | `15` | 30 minutes |
| The defaults, limited checks (span beyond the recommendation) | `5m` | `10` | 50 minutes |

At least 10 samples, and no more than 30 minutes of span.

`urltest` uses a single node, so sample more: `1m × 15` or `2m × 12–15`, with `tolerance` left
at its default.

#### Debug

To check this table against your own lines and load, ask the API (a non-ASCII group name has to
be URL encoded):

```bash
curl -s 'http://127.0.0.1:9090/private/health/Name/windows?ahead=2&configs=1:5,1:10,2:5'
```

Every window is priced by the same score that ranks the group, so a `rho` close to 1 means the
window orders the nodes the way they behave afterwards. `interval` is read from the checks, not
from the configuration. Right after a restart, when there are too few checks to hold a window,
`configs` is empty and `note` says what is missing.

A window needs `samples × stride` + `ahead` checks to be measurable at all, and a group keeps
only `sampling` of them: the rest is not kept. Both can be resized in place, without a restart
and without losing what was already collected:

```bash
curl -X PUT 'http://127.0.0.1:9090/private/health/Name/history?sampling=15&extra=60'
```

`sampling` is how many checks the ranking reads, leave it out to
keep the one in use. `extra` is how many checks to keep behind it, for the measurement only;
leave it out, or set it to `0`, to keep the sampling alone. The checks already collected survive
the resize.

Parameters

| Parameter | Default | Meaning |
| --- | --- | --- |
| `ahead` | `8` | how many checks after the window it is asked to predict |
| `configs` | the default grid | `stride:samples` pairs, e.g. `1:15,2:10`. `stride` is how many checks are skipped between samples, `samples` how many the window holds |

Fields

| Field | Meaning |
| --- | --- |
| `interval` | the actual interval between the checks (median), not the configured one |
| `nodes` | how many nodes were measured |
| `checks` | how many checks the longest history holds |
| `ahead` | how many checks each window was asked to predict |
| `stride` | how many checks are skipped between samples, `1` for consecutive |
| `samples` | how many checks the window holds; its span is `interval × stride × samples` |
| `span` | the actual width of the window |
| `windows` | how many positions of the history the window was measured at; the other fields are the average over them |
| `rho` | rank correlation between the order the window produces and the order the checks after it produce: `1` identical, `0` as good as guessing |
| `delta` | the standard error of `rho`; it collapses to `0` when `windows` is small, which means no precision rather than precision |
| `pool` | how many nodes the band lets in, on average, for that window |
| `note` | what was missing, when not one window could be measured |
