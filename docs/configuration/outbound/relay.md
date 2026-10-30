### Structure

```json
{
  "type": "relay",
  "tag": "relay",
  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ]
}
```

### Fields

#### outbounds

List of outbound tags that make up the relay of proxies, in the order the connection passes
through them:

- The first node is the **Entry**, e.g. `proxy-a`;
- The following nodes are the **Hops**, e.g. `proxy-b`;
- The last node is the **Exit**, where the connection leaves, e.g. `proxy-c`.

A relay contains at least an **Entry** and an **Exit**.

Restrictions: The **Entry** of a relay can be any outbound, but the other nodes must support dial
fields, so outbound groups, such as `selector`, `loadbalance`, `relay`, cannot be used.
The `detour` of the nodes other than the **Entry** is overwritten with the previous node of the
relay, and their other dial fields (such as `bind_interface`, `routing_mark`) take no effect under
a detour; `domain_resolver` is the exception, it still resolves the address of the node's own
server.
