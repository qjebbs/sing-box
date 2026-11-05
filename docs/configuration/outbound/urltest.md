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
  "objective": "quick",
  "tolerance": "50ms",
  "interrupt_exist_connections": false
}
```

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

The objective of the choice. Default is `quick`.

| Objective | Description                                     |
| --------- | ----------------------------------------------- |
| `quick`   | the node that usually costs the least           |
| `idle`    | the node that queues the least |

The objectives rank the nodes the same way as the
[load balance `objective`](/configuration/outbound/loadbalance/#objective) field, without
the notion of a qualified node.

The reasoning behind the statistics, the scores and the tolerance is on the
[Node Rating](/configuration/shared/node-rating/) page.

#### tolerance

The maximum **absolute** difference to the best node, default is `50ms` for `quick` and `10ms` for
`idle`, i.e. a node whose score is at most this far above the best one still counts as equally
good. The two scores are of different magnitudes, which is why the defaults differ.

The node in use keeps its place while its score is within this band of the best one, so a
difference that is not worth the switch keeps the connections where they are. The band also carries
a **resolution margin** that shrinks with the sample count and grows with the jitter of the best
node, so it is never narrower than this value.

#### interrupt_exist_connections

Interrupt existing connections when the selected outbound has changed.

Only inbound connections are affected by this setting, internal connections will always be interrupted.

The connections of the node that was just replaced are kept while its score is still
within the band of the tolerance in use, so they are not broken by an ambiguous measurement.
New connections always use the node with the best score.
