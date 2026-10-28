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
  "tolerance": 0.2,
  "interrupt_exist_connections": false
}
```

### Selection

The nodes are ranked by the following score, and the best one is used:

```
score = (ALL - FAIL) / ALL * MED + FAIL / ALL * TIMEOUT
```

`MED` is the median delay of the **successful** checks, `FAIL / ALL` the share of the
checks that failed, and `TIMEOUT` the timeout a failed check waits for before it gives
up, 15s. An attempt either succeeds after `MED` or fails after `TIMEOUT`, so the score
is what an attempt costs on average: a node failing one check in ten is scored 1.5s
slower than its median.

The failures are not part of `MED`, they would be counted twice. A score therefore
never exceeds the timeout, which a node that fails every check scores, and a node with
no successful check has no delay to report.

A single bad check doesn't change the selection, and another node only replaces the
selected one when its score is better by more than [`tolerance`](#tolerance).

The nodes are grouped by what the health check says about them:

1. Nodes that passed their latest check, ordered by the score.
2. Nodes that were never checked, or whose statistics expired.
3. Nodes whose latest check failed.

The class takes priority over the score, so a failed node is only used when no node of
another class is left. Within the last two classes nothing is compared: the first one
in configuration order is used, so a group that has no measurement yet doesn't pick at
random.

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

#### tolerance

The relative score advantage another node needs to replace the selected one, default
is `0.2`, i.e. 20% of the score of the best node. The value must be less than `1`.

The score compared is the one in [Selection](#selection), the median delay plus the
failure penalty, not a single check result.

A node whose latest check failed is not affected by this value, since it doesn't
belong to the same class as the others.

#### interrupt_exist_connections

Interrupt existing connections when the selected outbound has changed.

Only inbound connections are affected by this setting, internal connections will always be interrupted.
