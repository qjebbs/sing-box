### Structure

```json
{
  "type": "chain",
  "tag": "chain",
  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ]
}
```

### Fields

#### outbounds

List of outbound tags that make up the chain of proxies. 

Restrictions: The end node of the proxy chain (`proxy-c` in the example) can be any outbound, but the other nodes cannot be outbound groups, such as `selector`, `loadbalance`, `chain`.

The [Dial Fields](/configuration/shared/dial/) settings of nodes other than the end node will be overwritten.