### 结构

```json
{
  "type": "loadbalance",
  "tag": "loadbalance",
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
  "pick": {
    "objective": "leastload",
    "strategy": "random",
    "max_fail": 0,
    "max_rtt": "1000ms",
    "tolerance": 0.2,
    "min_nodes": 1,
    "max_nodes": 0,
    "jitter_weight": 1,
    "biases": [
      {
        "contains": "keyword",
        "prefix": "",
        "suffix": "",
        "regexp": "",
        "rtt_scale": 10
      }
    ]
  },
  "interrupt_exist_connections": false
}
```

### 字段

#### outbounds

出站标签列表。

#### all_providers

当 `all_providers` 为 `true` 时，将使用所有订阅，而不只是 `providers` 列表中的订阅。默认为 `false`。

#### providers

订阅标签列表。

#### exclude

排除 `providers` 节点的正则表达式。排除表达式的优先级高于包含表达式。

#### include

包含 `providers` 节点的正则表达式。

#### health_check

用于测试节点的[健康检查](/zh/configuration/shared/health-check/)。可以是顶层 `health_checks` 条目的字符串标签，也可以是内联的健康检查对象。

必填，不存在隐式的默认健康检查。

#### pick

参见“节点挑选字段”

### 节点挑选字段

#### objective

负载均衡的目标。默认为 `alive`。

| 目标        | 描述                                      |
| ----------- | ----------------------------------------- |
| `alive`     | 选用存活节点                              |
| `qualified` | 选用合格节点 (符合 `max_rtt`, `max_fail`) |
| `leastload` | 选用低负载节点 (历次检查中表现更稳定的)   |
| `leastping` | 选用低延时节点                            |

负载均衡将节点分为三类:

1. 失败节点: 无法连接的节点
2. 存活节点: 通过健康检查的节点
3. 合格节点: 存活且满足限制条件 (`max_rtt`, `max_fail`)

正常情况下，负载均衡将从当前目标面向的分类中挑选（见上表）。没有合适节点时，负载均衡将从次一级分类中选择。举例来说，`leastload` 实际执行的策略可能为：

- 从合格节点中选择低负载节点
- 从存活节点中选择低负载节点
- 从失败节点(可能是临时失败)中选择低负载节点

一般而言，使用 `leastload`，`leastping` 可以获得更好的网络质量；`alive` 适用于追求出口数量、对网络质量不敏感的场合。

`leastload` 和 `leastping` 目标都以分数排序，越低越好。计算公式为：

```
leastload_score = STD
leastping_score = MED + jitter_weight * STD
```

#### strategy

负载均衡的策略。默认为 `random`。

| 策略             | 描述                             |
| ---------------- | -------------------------------- |
| `random`         | 从符合目标的节点中，随机挑选     |
| `roundrobin`     | 从符合目标的节点中，轮流选择     |
| `consistenthash` | 使用同一节点处理同源站点的请求。 |

注意：`consistenthash` 要求出口数量相对稳定，仅当目标为 `alive` 时可用。

#### max_rtt

合格节点可接受的健康检查最大往返时间。比较的是往返时间中位数，因此单次尖刺不会淘汰一个本来不错的节点。默认为 `0`，即接受任何往返时间。

#### max_fail

合格节点健康检查最大失败次。默认为 `0`，即不允许任何失败。

#### tolerance

> 仅适用于 `least*` 目标

与最优节点的最大相对差距，默认为 `0.2`，即最多比最优节点慢 20% 的节点会与它一起被选中。

比较的是[分数](#objective)：`leastping` 的分数包含抖动，两个目标的分数都包含失败的检查，因为统计时它按超时时长计入。

容差以选择中最优的节点为基准计算，因此不会为了凑数而选中明显更差的节点。
同时始终应用 `10ms` 的绝对下限，避免过小的数值（例如始终稳定节点的标准差）被噪声拆开。

例如 `tolerance: 0.2` 时，节点为 `174ms`、`504ms`、`505ms`，只会选中 `174ms` 的节点，因为其余节点慢了接近三倍。

#### min_nodes

> 仅适用于 `least*` 目标

最少选出的节点数量，默认为 `1`。

仅在容差范围内的节点少于 `min_nodes` 时生效，例如 `min_nodes: 2` 会在容差之外再选中次优节点，用质量换取冗余。

#### max_nodes

> 仅适用于 `least*` 目标

最多选出的节点数量，默认为 `0`，即不限制。

仅在节点质量接近时生效，例如所有节点都在容差范围内时，只从中选出 `max_nodes` 个，避免将连接分散到每一个节点。

#### jitter_weight

> 仅适用于 `least*` 目标

叠加到[分数](#objective)上的标准差权重，默认为 `1`，即往返时间波动 300ms 的节点，分数相当于比其中位数慢 300ms。

如果你更愿意接受“低延时但不稳定”的节点，而不是“稍慢但稳定”的节点，可以把它调低。该值对 `leastload` 无效，因为它的分数本身就是标准差。

#### biases

负载均衡的挑选偏好。默认为空。

当节点标签匹配 `biases` 中的条件时，挑选时会将该节点的往返时间(或标准差)乘以 `rtt_scale` 进行比较。
`rtt_scale` 默认为 `1`，越大表示越不偏好该节点。
举例来说，`rtt_scale: 10` 表示当节点的往返时间为 `100ms` 时，比较时将其视为 `1000ms`。

- `contains` 表示节点标签包含某个关键词时匹配。
- `prefix` 表示节点标签以某个关键词开头时匹配。
- `suffix` 表示节点标签以某个关键词结尾时匹配。
- `regexp` 表示节点标签匹配某个正则表达式时匹配。

如果配置了多个条件，满足任一条件即可匹配。

#### interrupt_exist_connections

当选定的出站发生更改时，中断现有连接。

仅入站连接受此设置影响，内部连接将始终被中断。

对于 `least*` 目标，若刚离开选择范围的节点与最优节点的差距仍在健康检查统计误差之内，则保留其已有连接，避免因为一次可高可低的测量而断开连接。新连接仍然只使用选择范围内的节点。保留的余量最多为容差本身，因此节点存活的宽度最多不超过两倍容差。

该保留仅作用于入站连接，且仅在启用本选项时生效：内部连接无论如何都会被中断。
