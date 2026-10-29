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

Restrictions: The end node of the proxy chain (`proxy-c` in the example) can be any outbound, but the other nodes must support [Dial Fields](/configuration/shared/dial/), so outbound groups, such as `selector`, `loadbalance`, `chain`, cannot be used.

The `detour` of the nodes other than the end node is overwritten with the next node of the chain. Their other dial fields (`bind_interface`, `routing_mark`, etc.) take no effect under a detour, except `domain_resolver`, which still resolves the address of the node's own server.