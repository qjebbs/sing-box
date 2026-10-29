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

限制：代理链末端节点（示例中的 `proxy-c`）可为任意出站，其余节点必须支持拨号字段，不能为出站组，如 `selector`, `loadbalance`, `chain`。

末端节点以外节点的 `detour` 会被覆盖为链中的下一个节点，其余拨号字段（如 `bind_interface`, `routing_mark`）在 detour 下不生效；`domain_resolver` 除外，它仍用于解析该节点自身的服务器地址。