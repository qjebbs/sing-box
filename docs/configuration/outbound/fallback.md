---
icon: material/new-box
---

### Structure

```json
{
  "type": "fallback",
  "tag": "fallback",
  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ],
  "all_providers": false,
  "providers": [
    "provider-a",
    "provider-b"
  ],
  "exclude": "",
  "include": "",
  "health_check": ""
}
```

### Fields

#### outbounds

List of outbound tags, tried in the declared order.

#### all_providers

When `all_providers` is `true`, all providers will be used instead of just those in the `providers` list. The default value is `false`.

#### providers

List of provider tags, tried after the `outbounds`.

#### exclude

Exclude regular expression to filter `providers` nodes. The priority of the exclude expression is higher than the include expression.

#### include

Include regular expression to filter `providers` nodes.

#### health_check

Optional. The [Health Check](/configuration/shared/health-check/) used to test the outbounds, a string tag of a top-level `health_checks` entry or an inline health check object.

Unlike `loadbalance`, it is not required: when it is absent, **no health checker is attached at all**. No check round is ever started, so the outbound generates no traffic of its own, and the outbounds are tried in the declared order.

### Behavior

A connection uses the first outbound, and when it fails, the next one is tried, until one of them accepts the connection. UDP connections behave the same way.

The order is: the `outbounds` first, then the nodes of `all_providers`, then the nodes of `providers`. With `health_check`, the outbounds that passed their latest check are moved ahead (the declared order is kept inside both groups), and the failed ones are put behind but still tried, so the group keeps working even when all the checks fail.

Outbounds that don't support the requested network are skipped, so a TCP only outbound is not used for UDP connections.

There is no node selection: every connection dials the outbounds in order on its own, so an outbound that recovered is used again by the next connection, with or without a health check.

Only the failure of the dial itself is retried. A failure that surfaces after the connection was accepted (such as a handshake error of an upper layer) belongs to the connection that was already returned and cannot be retried here.

Failed attempts are logged at the `debug` level. When all outbounds fail, the error lists the outbound tag and the error of each attempt.
