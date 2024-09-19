# Introduction

sing-box uses JSON for configuration files.

### Structure

```json
{
  "log": {},
  "dns": {},
  "ntp": {},
  "inbounds": [],
  "outbounds": [],
  "route": {},
  "experimental": {}
}
```

### Fields

| Key            | Format                          |
|----------------|---------------------------------|
| `log`          | [Log](./log/)                   |
| `dns`          | [DNS](./dns/)                   |
| `ntp`          | [NTP](./ntp/)                   |
| `inbounds`     | [Inbound](./inbound/)           |
| `outbounds`    | [Outbound](./outbound/)         |
| `route`        | [Route](./route/)               |
| `provider`     | [Provider](./provider/)         |
| `experimental` | [Experimental](./experimental/) |

### Check

```bash
sing-box check
```

### Format

```bash
sing-box format -w -c config.json -D config_directory
```

### Merge

```bash
sing-box merge output.json -c config.json -D config_directory
```

### Extended Configuration Merging

The fork provides an extended configuration merging mechanism which can be enabled with flag `-E`.

```bash
sing-box run -E -c 01-base.json -c 02-provider-1.json
sing-box run -E -C config_dir
```

It applies more rules:

- Simple values (string, number, boolean) are overwritten, others (array, object) are merged.
- Elements in an array will be sorted by `_order` field, the smaller ones are in the front.
- Elements with same `tag` or `_tag` in an array will be merged.

Avoids some limitations of upstream:

- Cannot be merged objects in arrays. For example, supplement the `outbounds` field of a `selector`.
- Each file must be legally available before merging. So you have to write `"type": "selector"` everywhere.
- Fine adjustment of merged object order is not supported.

It supports more formats:

- `JSON`: *.json, *.jsonc
- `YAML`: *.yaml, *.yml
- `TOML`: *.toml

Suppose we have following files:

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

Merged:

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

As you can see, `02-provider-1.json` is pluggable, you can simply remove the entire file when you don’t need it, without breaking the usability of the remaining files.

Note: The extended merging conflicts with the `format` command at the design level, the `format` command won't work correctly in the following cases:

1. `*.json` files with `_order` or `_tag` fields.
1. All files other than `*.json`.

If you don't depend on `format`, you don't need to worry about it.