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
  "rtt_tolerance": 0.2,
  "jitter_tolerance": "10ms",
  "interrupt_exist_connections": false
}
```

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

不同目标的评价方法同[负载均衡 objective 字段](/zh/configuration/outbound/loadbalance/#objective)，但没有合格节点的概念。

统计量、分数与容差的设计理由见[节点评分](/zh/configuration/shared/node-rating/)。

#### rtt_tolerance

> 仅适用于 `leastping` 目标

与最好节点的最大**相对**差距，取值范围 `0-1`，默认为 `0.2`，即其分数的 20%。

当前节点的分数处在最好节点的该区间之内时保持原位，因此不值得切换的差距会保持连接不动。

#### jitter_tolerance

> 仅适用于 `leastload` 目标

与最好节点的最大**绝对**差距，默认为 `10ms`，即抖动最多比最优节点高 10ms 的节点仍算同样好。

#### interrupt_exist_connections

当选定的出站发生更改时，中断现有连接。

仅入站连接受此设置影响，内部连接将始终被中断。

刚被替换的节点的连接，在其分数仍处于 `rtt_tolerance` 区间内时会被保留，因此不会被
一个两可的测量结果断开。新连接始终使用分数最好的节点。
