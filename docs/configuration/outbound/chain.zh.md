### 结构

```json
{
  "type": "chain",
  "tag": "chain",
  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ]
}
```

### 字段

#### outbounds

组成链式代理的出站标签列表。

限制：代理链末端节点（示例中的 `proxy-c`）可为任意出站，其余节点不能为出站组，如 `selector`, `loadbalance`, `chain`。

末端节点以外节点的[拨号字段](/zh/configuration/shared/dial/)设置将被覆盖。