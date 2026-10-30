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

The connections the groups carry are counted as well, so a node that passes its checks
and then drops a share of the traffic is scored for it. As many of them are kept as
`sampling` health check results, so the two describe the same length of the same window.

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
