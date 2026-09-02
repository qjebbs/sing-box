# 订阅

### 结构

订阅源列表。

```jsonc
{
  "providers": [
    {
      "tag": "provider",
      "type": "http",
      "url": "https://url.to/provider.txt",
      "interval": "24h",
      "exclude": "",
      "include": "",
      "http_client": "",
      "disable_user_agent": false,
      "cache_file": "provider.txt",
      "outbounds_default": {
        ... // 拨号字段
      }
    }
  ],
  {
    // 通过出站组引用，否则订阅不起作用。
    "type": "selector", // selector, loadbalance, urltest...
    "exclude": "",
    "include": "",
    "providers": [
      "provider"
    ]
  }
}
```

### 字段

#### type

==必填==

订阅源的类型。目前仅支持 `http`。

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

#### dedup_host

是否去重协议和主机名相同的节点。默认值为 `false`。

#### dedup_host_port

是否去重协议、主机名和端口相同的节点。默认值为 `false`。

#### http_client

!!! question "自 sing-box 1.14.0 起"

用于下载订阅内容的 HTTP 客户端。

参阅 [HTTP 客户端字段](/zh/configuration/shared/http-client/)。

留空时使用默认 HTTP 客户端：即由 [`default_http_client`](/zh/configuration/route/#default_http_client)
指定的客户端，或当 `default_http_client` 为空时使用顶级 `http_clients` 的第一项。

!!! failure "隐式默认已在 sing-box 1.14.0 废弃"

    当 `http_clients` 与 `default_http_client` 均未配置时，将使用通过默认出站连接的隐式 HTTP 客户端。
    该隐式默认已在 sing-box 1.14.0 废弃，并将在 sing-box 1.16.0 移除；请改为定义 `http_clients`。

#### download_detour

!!! failure "已在 sing-box 1.14.0 废弃"

    `download_detour` 已在 sing-box 1.14.0 废弃且将在 sing-box 1.16.0 中被移除，请使用 `http_client` 代替。

用于下载订阅内容的出站的标签。

如果为空，将使用默认出站。

#### disable_user_agent

下载订阅内容时禁用 User-Agent。禁用时，服务器可能不会提供用量信息。

#### cache_file

将下载的订阅内容缓存到本地的文件名。

> 当 `sing-box` 作为系统服务运行，启动时很可能没有网络，利用缓存文件可避免初次获取订阅失败的问题。

#### outbounds_default

所有节点的默认配置，可能会被节点自身的配置覆盖。

参阅 [拨号字段](/zh/configuration/shared/dial/)。
