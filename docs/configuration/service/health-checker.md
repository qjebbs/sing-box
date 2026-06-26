# Health Checker

Health checker service is used to check and provide health status information of nodes for outbound groups. 
It can be shared by multiple outbound groups to avoid redundant health check overhead.

The program will automatically create a default health checker service with the tag `default`. You can explicitly override it or create a new service.

### Structure

```json
{
  "services": [
    {
      "type": "health-checker",
      "tag": "default",
      "interval": "5m",
      "sampling": 10,
      "destination": "https://www.gstatic.com/generate_204",
      "detour_of": [
        "proxy-a",
        "proxy-b"
      ]
    }
  ]
}
```

### Fields

#### interval

The interval of health check for each node. Must be greater than `10s`, default is `5m`.

#### sampling

The number of recent health check results to sample. Must be greater than `0`, default is `10`.

#### destination

The destination URL for health check. Default is `http://www.gstatic.com/generate_204`.

#### detour_of

Let's say you have an outbound chain:

```json
{
  "tag": "chain",
  "type": "chain",
  "outbounds": ["A", "B"]
}
```
The actual chain is:

```
Shadowsocks (A) ---> LoadBalance (B)
```

And you want the health check of each node of `B` to be exactly the same as above, just configurate

```json
"detour_of": ["A"]
```

The check chain will be:

```
Shadowsocks (A) ---> Trojan [B.Node]
```

If not, it would be almost impossible to detect such nodes, which are fine to use directly, but not when they're used as an upstream, due to audit rules and other reasons.
Configuring this item can also avoid the server from hijacking the test request, and improve the accuracy of the health check.

Restrictions: This configuration does not support adding outbound groups, such as `selector`, `loadbalance`, `chain`.
