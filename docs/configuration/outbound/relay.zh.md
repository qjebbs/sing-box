### 结构

```json
{
  "type": "relay",
  "tag": "relay",
  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ]
}
```

### 字段

#### outbounds

组成链式代理的出站标签列表，按连接经过的顺序排列：

- 第一个节点是**入口**：如示例中的 `proxy-a`；
- 中间节点依次作为**中转**：如示例中的 `proxy-b`；
- 最后一个节点是**出口**，连接由此处离开：如示例中的 `proxy-c`。

链式代理至少包含**入口**和**出口**两个节点。

限制：代理链的**入口**可为任意出站，其余节点必须支持拨号字段，不能为出站组，
如 `selector`, `loadbalance`, `relay`。
**入口**以外节点的 `detour` 会被覆盖为链中的上一个节点，其余拨号字段（
如 `bind_interface`,`routing_mark`）在 detour 下不生效；
`domain_resolver` 除外，它仍用于解析该节点自身的服务器地址。
