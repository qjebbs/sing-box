# 引言

sing-box 使用 JSON 作为配置文件格式。

### 结构

```json
{
  "log": {},
  "dns": {},
  "inbounds": [],
  "outbounds": [],
  "route": {},
  "experimental": {}
}
```

### 字段

| Key            | Format                 |
|----------------|------------------------|
| `log`          | [日志](./log/)           |
| `dns`          | [DNS](./dns/)          |
| `inbounds`     | [入站](./inbound/)       |
| `outbounds`    | [出站](./outbound/)      |
| `route`        | [路由](./route/)         |
| `experimental` | [实验性](./experimental/) |

### 检查

```bash
sing-box check
```

### 格式化

```bash
sing-box format -w -c config.json -D config_directory
```

### 合并

```bash
sing-box merge output.json -c config.json -D config_directory
```

### 扩展的配置合并

此分支项目提供关于配置文件合并的扩展特性，使用 `-E` 参数启用。

```bash
sing-box run -E -c 01-base.json -c 02-provider-1.json
sing-box run -E -C config_dir
```

它支持更深入的合并规则：

- 简单字段（字符串、数字、布尔值）会被后来者覆盖，其它字段（数组、对象）会被合并。
- 数组会按 `_order` 字段值进行排序，小的排在前面。
- 数组内拥有相同 `tag` 或 `_tag` 的对象会被合并。

不存在上游合并逻辑的一些限制：

- 无法合并扩充数组内对象。比如扩充前序文件中 `selector` 的 `outbounds` 字段。
- 要求合并前的每个配置文件必须是合法可用的。所以你必须重复写 `"type": "selector"`，即使从合并的角度来看这是多余的。
- 不支持精细调整合并后对象顺序。

支持更多的文件格式：

- `JSON`: *.json, *.jsonc
- `YAML`: *.yaml, *.yml
- `TOML`: *.toml

假设我们有以下配置文件：

`01-base.json`:

```json
{
  "log": {"level": "debug"},
  "outbounds": [
    {"tag": "selected",  "outbounds": ["direct"]},
    {"tag": "direct"},
    {"tag": "block"},
  ]
}
```

`02-provider-1.json`:

```json
{
  "outbounds": [
    {"tag": "selected", "providers": ["provider-1"]},
  ],
  "providers": [{
    "tag": "provider-1",
    "url": "https://url.to/provider-1"
  }],
}
```

合并后的配置文件:

```jsonc
// sing-box check -v -E -c 01-base.json -c 02-provider-1.json
{
  "log": {"level": "debug"},
  "outbounds": [
    {"tag": "selected", "outbounds": ["direct"], "providers": ["provider-1"]},
    {"tag": "direct"},
    {"tag": "block"},
  ],
  "providers": [{
    "tag": "provider-1",
    "url": "https://url.to/provider-1"
  }]
}
```

可以看到，`02-provider-1.json` 是可插拔的，不需要时，可以简单地移除整个文件，而不破坏剩余配置文件的可用性。

注意：扩展合并逻辑与 `format` 命令在设计层面冲突，以下情况 `format` 命令不能正确工作：

1. `*.json` 格式，但使用了扩展字段 `_order` 或 `_tag`。
1. `*.json` 以外的所有格式。

若你不依赖于 `format`，则无需担心。