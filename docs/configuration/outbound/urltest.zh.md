### 结构

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
  "objective": "leastping",
  "tolerance": 0.2,
  "interrupt_exist_connections": false
}
```

### 选择

本组是[负载均衡](/zh/configuration/outbound/loadbalance/)组的一种特例配置：
`min_nodes` 与 `max_nodes` 均为 `1`。它始终只使用分数最好的一个节点。

### 字段

#### outbounds

用于测试的出站标签列表。

#### all_providers

当 `all_providers` 为 `true` 时，将使用所有订阅，而不只是 `providers` 列表中的订阅。默认为 `false`。

#### providers

用于测试的[订阅](/zh/configuration/provider)标签列表。

#### exclude

排除 `providers` 节点的正则表达式。排除表达式的优先级高于包含表达式。

#### include

包含 `providers` 节点的正则表达式。

#### health_check

用于测试节点的[健康检查](/zh/configuration/shared/health-check/)。可以是顶层 `health_checks` 条目的字符串标签，也可以是内联的健康检查对象。

必填，不存在隐式的默认健康检查。

#### objective

选择的目标，默认为 `leastping`。

| 目标        | 说明                     |
| ----------- | ------------------------ |
| `leastping` | 延时最低的节点 |
| `leastload` | 最稳定的节点 |

#### tolerance

最好节点周围的相对分数区间，默认为 `0.2`，即其分数的 20%。

本组始终使用分数最好的节点，因此该值不会推迟切换。它是刚被替换的节点保有既有连接的区间：
统计上无法与噪声区分的差距，不足以断开这些连接。容许量最多为区间本身，因此一个节点最多存活
两倍容差。

#### interrupt_exist_connections

当选定的出站发生更改时，中断现有连接。

仅入站连接受此设置影响，内部连接将始终被中断。

刚被替换的节点的连接，在其分数仍处于 tolerance 区间内时会被保留，因此不会被
一个两可的测量结果断开。新连接始终使用分数最好的节点。
