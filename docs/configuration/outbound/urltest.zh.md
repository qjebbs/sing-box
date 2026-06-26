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
  "checker": "default",
  "tolerance": 50
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

#### checker

健康检查服务的标签。可选，未配置时使用默认健康检查服务。

#### tolerance

以毫秒为单位的测试容差。 默认使用 `50`。
