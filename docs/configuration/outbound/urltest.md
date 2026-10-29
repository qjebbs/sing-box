### Structure

```json
{
  "type": "urltest",
  "tag": "auto",
  
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
  "objective": "leastping",
  "tolerance": 0.2,
  "interrupt_exist_connections": false
}
```

### Selection

The group is a special case of the
[load balance](/configuration/outbound/loadbalance/) group:

`objective` defaults to `leastping`, `leastload` is also available;
`min_nodes` and `max_nodes` are both `1`, it always uses only the node with the best score.

### Fields

#### outbounds

List of outbound tags to test.

#### all_providers

When `all_providers` is `true`, all providers will be used instead of just those in the `providers` list. The default value is `false`.

#### providers

List of [Provider](/configuration/provider) tags to test.

#### exclude

Exclude regular expression to filter `providers` nodes. The priority of the exclude expression is higher than the include expression.

#### include

Include regular expression to filter `providers` nodes.

#### health_check

The [Health Check](/configuration/shared/health-check/) used to test the nodes. A string tag of a top-level `health_checks` entry, or an inline health check object.

Required. There is no implicit default health check.

#### objective

The objective of the choice. Default is `leastping`.

| Objective   | Description                    |
| ----------- | ------------------------------ |
| `leastping` | the node with the lowest delay |
| `leastload` | the most stable node           |

#### tolerance

The relative score band around the best node, default is `0.2`, i.e. 20% of its score.

This group always uses the node with the best score, so the value doesn't delay a
switch. It is the band within which the node that was just replaced keeps the connections
it already carries: a difference that cannot be told apart from noise by the statistics
is no reason to break them. The allowance is at most the band itself, so a node survives
at most twice the tolerance.

#### interrupt_exist_connections

Interrupt existing connections when the selected outbound has changed.

Only inbound connections are affected by this setting, internal connections will always be interrupted.

The connections of the node that was just replaced are kept while its score is still
within the [tolerance](#tolerance), so they are not broken by an ambiguous measurement.
New connections always use the node with the best score.
