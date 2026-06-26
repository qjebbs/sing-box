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
  "checker": "health-checker",
  "pick": {
    "objective": "leastload",
    "strategy": "random",
    "max_fail": 0,
    "max_rtt": "1000ms",
    "expected": 3,
    "baselines": [
      "30ms",
      "50ms",
      "100ms",
      "150ms",
      "200ms",
      "250ms",
      "350ms"
    ],
    "biases": [
      {
        "contains": "keyword",
        "prefix": "",
        "suffix": "",
        "regexp": "",
        "rtt_scale": 10
      }
    ]
  }
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

#### checker

健康检查服务的标签。必须添加健康检查服务才能使用 Loadbalance 出站组。

参阅 [健康检查](/zh/configuration/service/health-checker/) 了解详情。

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

#### strategy

负载均衡的策略。默认为 `random`。

| 策略             | 描述                             |
| ---------------- | -------------------------------- |
| `random`         | 从符合目标的节点中，随机挑选     |
| `roundrobin`     | 从符合目标的节点中，轮流选择     |
| `consistenthash` | 使用同一节点处理同源站点的请求。 |

注意：`consistenthash` 要求出口数量相对稳定，仅当目标为 `alive` 时可用。

#### max_rtt

合格节点可接受的健康检查最大往返时间。 默认为 `0`，即接受任何往返时间。

#### max_fail

合格节点健康检查最大失败次。默认为 `0`，即不允许任何失败。

#### expected / baselines

> 仅适用于 `least*` 目标

`expected` 是期望选出的节点数量。默认为 `1`。

`baselines` 将节点划分为不同的档位。默认为空。对于 `leastload`，它根据往返时间标准差划分；对于 `leastping`，它根据往返时间平均值划分。

以 `leastload` 为例，几种典型配置为：

1. `expected: 3`，选出标准差最小的 3 个节点。

1. `expected:3, baselines =["50ms"]`，如果有3个以上稳定性足够好的节点（标准差<50ms），有多少选多少；否则取标准差最小的前3个。

1. `expected:3, baselines =["30ms","50ms","100ms"]`，依次尝试不同基准线，直到找到至少3个节点。否则返回标准差最小的3个。这种配置的好处是，既找到合适数量的节点，又不浪费素质相近的更多节点。
1. `baselines: ["30ms","50ms","100ms"]`，依次尝试不同基准线，若没有符合任何基准线的节点，返回标准差最小的一个。

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
