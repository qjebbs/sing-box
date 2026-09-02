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
  "tolerance": 50,
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

#### tolerance

The test tolerance in milliseconds. `50` will be used if empty.

#### interrupt_exist_connections

Interrupt existing connections when the selected outbound has changed.

Only inbound connections are affected by this setting, internal connections will always be interrupted.
