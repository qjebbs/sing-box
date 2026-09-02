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
  "tolerance": 50,
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

#### tolerance

以毫秒为单位的测试容差。 默认使用 `50`。

#### interrupt_exist_connections

当选定的出站发生更改时，中断现有连接。

仅入站连接受此设置影响，内部连接将始终被中断。
