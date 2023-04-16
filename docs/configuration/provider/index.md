# Provider

### Structure

List of subscription providers.

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

      ... // Dial Options
    }
  ]
}
```

### Fields

#### tag

==Required==

Tag of the provider.

The node `node_name` from `provider` will be tagged as `provider node_name`.

#### url

==Required==

URL to the provider.

#### interval

Refresh interval. The minimum value is `1m`, the default value is `1h`.

#### exclude

Regular expression to exclude nodes. The priority of the exclude expression is higher than the include expression.

#### include

Regular expression to include nodes.

#### download_detour

The tag of the outbound used to download from the provider.

Default outbound will be used if empty.

#### cache_file

Downloaded content will be cached in this file.

> When `sing-box` is running as a system service, it may not have network access when it starts. Using cache file can avoid the fetch failing for the first time.

### Dial Fields

See [Dial Fields](/configuration/shared/dial) for details.