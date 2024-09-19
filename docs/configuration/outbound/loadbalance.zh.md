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
  "providers": [
    "provider-a",
    "provider-b",
  ],
  "exclude": "",
  "include": "",
  "check": {
    "interval": "5m",
    "sampling": 10,
    "destination": "https://www.gstatic.com/generate_204",
    "detour_of": [
      "proxy-a",
      "proxy-b"
    ]
  },
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
    ]
  }
}
```

### 字段

#### outbounds

出站标签列表。

#### providers

订阅标签列表。

#### exclude

排除 `providers` 节点的正则表达式。排除表达式的优先级高于包含表达式。

#### include

包含 `providers` 节点的正则表达式。

#### check

参见“健康检查字段”

#### pick

参见“节点挑选字段”

### 健康检查字段

#### interval

每个节点的健康检查间隔。不小于`10s`，默认为 `5m`。

#### sampling

对最近的多少次检查结果进行采样。大于 `0`，默认为 `10`。

#### destination

用于健康检查的链接。默认使用 `http://www.gstatic.com/generate_204`。

#### detour_of

假设你配置有链式出站：

```
                 (detour="B")                 (detour="C")
Shadowsocks (A) -------------> ShadowTLS (B) --------------> LoadBalance (C)
```

并且你希望每个节点的健康检查链路与上图一致。那么只需将 `C` 设置为 `detour_of: ["A", "B"]`，实际检查链路为：

```
Shadowsocks (A) ---> ShadowTLS (B) ---> [Node]
```

> 若非如此，几乎不可能检测出这样的节点，它们直接使用没问题，但作为链式代理上游时，却由于审计规则等原因，无法正常工作。

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
