### 结构

```json
{
  "type": "selector",
  "tag": "select",

  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ],
  "providers": [
    "provider-a",
    "provider-b",
  ],
  "default": "proxy-c"
}
```

!!! error ""

    选择器目前只能通过 [Clash API](/zh/configuration/experimental#clash-api) 来控制。

### 字段

#### outbounds

用于选择的出站标签列表。

#### providers

用于选择的[订阅](/zh/configuration/provider)标签列表。

#### default

默认的出站标签。默认使用第一个出站。
