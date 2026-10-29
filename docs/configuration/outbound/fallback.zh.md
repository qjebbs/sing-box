---
icon: material/new-box
---

### 结构

```json
{
  "type": "fallback",
  "tag": "fallback",
  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ],
  "all_providers": false,
  "providers": [
    "provider-a",
    "provider-b"
  ],
  "exclude": "",
  "include": "",
  "health_check": ""
}
```

### 字段

#### outbounds

出站标签列表，按声明顺序依次尝试。

#### all_providers

当 `all_providers` 为 `true` 时，将使用所有提供商，而不只是 `providers` 列表中的提供商。默认值为 `false`。

#### providers

提供商标签列表，在 `outbounds` 之后尝试。

#### exclude

排除与提供商节点匹配的正则表达式。排除表达式的优先级高于包含表达式。

#### include

包含与提供商节点匹配的正则表达式。

#### health_check

可选。用于测试出站的[健康检查](/zh/configuration/shared/health-check/)，为顶层 `health_checks` 中的标签，或内联的健康检查对象。

与 `loadbalance` 不同，它不是必填项：未配置时**不会挂载任何检查器**，不会启动任何检查轮次，因此该出站本身不产生任何流量，出站按声明顺序依次尝试。

### 行为

连接首先使用第一个出站，失败时尝试下一个，直到某个出站接受连接。 UDP 连接的行为相同。
顺序为：先是 `outbounds`，然后是 `all_providers` 的节点，最后是 `providers` 的节点。配置了 `health_check` 时，最近一次检查通过的出站会被提到前面（两组内部仍保持声明顺序），未通过的出站排在后面但仍会尝试，因此即使所有检查都失败，该组依然可用。

不支持所请求网络的出站会被跳过，因此仅支持 TCP 的出站不会被用于 UDP 连接。

它不做节点选择：每条连接都自己按顺序拨号，所以某个出站恢复后，下一条连接就会重新用上它——无论是否配置了健康检查。

只有拨号本身的失败会被重试。连接已被接受之后才出现的失败（例如上层协议的握手错误）属于已经返回的连接，无法在此重试。

失败的尝试以 `debug` 级别记录。当所有出站都失败时，错误会列出每次尝试的出站标签和错误。
