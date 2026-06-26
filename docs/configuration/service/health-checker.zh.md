# Health Checker

健康检查服务用于检查并为出站组提供节点的健康状态信息，可被多个出站组共享，避免重复的健康检查开销。

程序会自动创建一个默认的健康检查服务，tag 为 `default`，你可以显式覆盖它或创建新的服务。

### 结构

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

### 字段

#### interval

每个节点的健康检查间隔。不小于`10s`，默认为 `5m`。

#### sampling

对最近的多少次检查结果进行采样。大于 `0`，默认为 `10`。

#### destination

用于健康检查的链接。默认使用 `http://www.gstatic.com/generate_204`。

#### detour_of

假设你配置有链式出站：

```json
{
  "tag": "chain",
  "type": "chain",
  "outbounds": ["A", "B"]
}
```

实际链路为：

```
Shadowsocks (A) ---> LoadBalance (B)
```

并且你希望 `B` 节点的健康检查链路与上图一致。那么只需设置 

```json
"detour_of": ["A"]
```

实际检查链路为：

```
Shadowsocks (A) ---> Trojan [B.Node]
```

若非如此，几乎不可能检测出这样的节点，它们直接使用没问题，但作为链式代理上游时，却由于审计规则等原因，无法正常工作。
配置此项还可以避免服务器对测试请求的劫持，提高检测的准确性。

限制：此配置不支持添加出站组，如 `selector`, `loadbalance`, `chain`。