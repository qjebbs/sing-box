---
icon: material/new-box
---

!!! question "自 sing-box 1.12.0 起"

# 服务

### 结构

```json
{
  "services": [
    {
      "type": "",
      "tag": ""
    }
  ]
}
```

### 字段

| 类型       | 格式                   |
|-----------|------------------------|
| `ccm`     | [CCM](./ccm)           |
| `derp`    | [DERP](./derp)         |
| `ocm`     | [OCM](./ocm)           |
| `resolved`| [Resolved](./resolved) |
| `ssm-api` | [SSM API](./ssm-api)   |
| `health-checker` | [健康检查](./health-checker) |

#### tag

端点的标签。