### 结构

字符串或对象。

当为字符串时，为顶层 [`health_checks`](/zh/configuration/#health_checks) 选项中定义的共享健康检查的标签。

当为对象时：

```json
{
  "interval": "5m",
  "sampling": 10,
  "destination": "https://www.gstatic.com/generate_204",
  "idle_timeout": "30m",
  "detour_of": [
    "proxy-a",
    "proxy-b"
  ]
}
```

健康检查用于检查并为出站组提供节点的健康状态信息，可被多个出站组共享，避免重复的健康检查开销。

出站组承载的连接也会被计入，因此通过检查却丢掉一部分流量的节点同样会被扣分。
保留的连接条数与 `sampling` 个检查结果相同，使两者描述同一时间窗口的等长片段。

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

!!! note ""

    检查只在该组被使用时进行：从未被使用的健康检查不会执行任何检查。
    首次使用会启动检查循环，并立刻执行一轮。

#### idle_timeout

没有任何出站组使用此健康检查后，暂停健康检查的时长，`0` 表示持续检查。

当一个共享健康检查被多个出站组使用时，只有在所有组都空闲该时长后才会暂停检查。

暂停期间不会测试任何节点。只要有组再次被使用就会立即恢复检查，并立刻进行一次检查以刷新统计，
因为早于 `(sampling+1) * interval` 的结果会被丢弃。

小于 `interval` 的值会被提升为 `interval`。

#### detour_of

链式出站按连接经过的顺序排列节点，且只有第一个节点可为出组。假设你配置有经过该出组的链式出站：

```json
{
  "tag": "chain",
  "type": "chain",
  "outbounds": ["A", "B"]
}
```

实际链路为：

```
LoadBalance (A) ---> Shadowsocks (B)
```

并且你希望 `A` 节点的健康检查链路与上图一致，那么只需列出组之后需要经过的节点：

```json
"detour_of": ["B"]
```

实际检查链路为：

```
Trojan [A.Node] ---> Shadowsocks (B)
```

若非如此，几乎不可能检测出这样的节点，它们直接使用没问题，但作为链式代理上游时，却由于审计规则等原因，无法正常工作。
配置此项还可以避免服务器对测试请求的劫持，提高检测的准确性。

限制：此配置不支持添加出站组，如 `selector`, `loadbalance`, `chain`。
