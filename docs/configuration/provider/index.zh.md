# 订阅

### 结构

订阅源列表。

```json
{
  "providers": [
    {
      "tag": "provider",
      "url": "https://url.to/provider.txt",
      "interval": "24h",
      "exclude": "",
      "include": "",
      "download_detour": "",
      "cache_file": "provider.txt"

      ... // 拨号字段
    }
  ]
}
```

### 字段

#### tag

==必填==

订阅源的标签。

来自 `provider` 的节点 `node_name`，导入后的标签为 `provider node_name`。

#### url

==必填==

订阅源的 URL。

#### interval

刷新订阅的时间间隔。最小值为 `1m`，默认值为 `1h`。

#### exclude

排除节点的正则表达式。排除表达式的优先级高于包含表达式。

#### include

包含节点的正则表达式。

#### download_detour

用于下载订阅内容的出站的标签。

如果为空，将使用默认出站。

#### cache_file

将下载的订阅内容缓存到本地的文件名。

> 当 `sing-box` 作为系统服务运行，启动时很可能没有网络，利用缓存文件可避免初次获取订阅失败的问题。

### 拨号字段

参阅 [拨号字段](/zh/configuration/shared/dial/)。