### 结构

字符串或对象。

当为字符串时，为顶层 [`health_checks`](/zh/configuration/#health_checks) 选项中定义的共享健康检查的标签。

当为对象时：

```json
{
  "interval": "5m",
  "sampling": 10,
  "destination": "https://www.gstatic.com/generate_204",
  "detour_of": [
    "proxy-a",
    "proxy-b"
  ]
}
```

健康检查用于检查并为出站组提供节点的健康状态信息，可被多个出站组共享，避免重复的健康检查开销。

顶层 `health_checks` 的每一项使用相同的对象结构，并额外带有 `tag` 字段：

```json
{
  "health_checks": [
    {
      "tag": "default",
      "interval": "5m",
      "sampling": 10,
      "destination": "https://www.gstatic.com/generate_204"
    }
  ]
}
```

!!! warning ""

    每个出站组都必须显式配置健康检查，不存在隐式的默认健康检查。

### 字段

#### interval

每个节点的健康检查间隔。不小于 `10s`，默认为 `5m`。

#### sampling

对最近的多少次检查结果进行采样。大于 `0`，默认为 `10`。

#### destination

用于健康检查的链接。默认使用 `https://www.gstatic.com/generate_204`。

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
