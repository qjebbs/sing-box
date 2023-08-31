### Structure

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

    The selector can only be controlled through the [Clash API](/configuration/experimental#clash-api-fields) currently.

### Fields

#### outbounds

List of outbound tags to select.

#### outbounds

List of [Provider](/configuration/provider) tags to select.

#### default

The default outbound tag. The first outbound will be used if empty.