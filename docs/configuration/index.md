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

| Key            | Format                         |
|----------------|--------------------------------|
| `log`          | [Log](./log)                   |
| `dns`          | [DNS](./dns)                   |
| `ntp`          | [NTP](./ntp)                   |
| `inbounds`     | [Inbound](./inbound)           |
| `outbounds`    | [Outbound](./outbound)         |
| `route`        | [Route](./route)               |
| `provider`     | [Provider](./provider)         |
| `experimental` | [Experimental](./experimental) |

### Check

```bash
$ sing-box check
```

### Format

```bash
$ sing-box format -w
```

### Multiple Configuration Files

> You can skip this section if you are using only one configuration file.

sing-box supports multiple configuration files. The latter overwrites and merges into the former, in the order in which the configuration files are loaded.

```bash
# Load by the order of parameters
sing-box run -c inbound.json -c outbound.json
# Load by the order of file names
sing-box run -r -c config_dir
```

Just remember these few rules:

- Simple values (`string`, `number`, `boolean`) are overwritten, others (`array`, `object`) are merged.
- Elements in an array will be sorted by `_order` field, the smaller ones are in the front.
- Elements with same `tag` or `_tag` in an array will be merged.

Suppose we have 2 `JSON` files:

`a.json`:

```json
{
  "log": {"level": "debug"},
  "inbounds": [{"tag": "in-1"}],
  "outbounds": [{"_order": 100, "tag": "out-1"}],
  "route": {"rules": [
    {"_tag":"rule1","inbound":["in-1"],"outbound":"out-1"}
  ]}
}
```

`b.json`:

```json
{
  "log": {"level": "error"},
  "outbounds": [{"_order": -100, "tag": "out-2"}],
  "route": {"rules": [
    {"_tag":"rule1","inbound":["in-1.1"],"outbound":"out-1.1"}
  ]}
}
```

Applied:

```jsonc
{
  // level field is overwritten by the latter value
  "log": {"level": "error"},
  "inbounds": [{"tag": "in-1"}],
  "outbounds": [
    // Although out-2 is a latecomer, but it's in 
    // the front due to the smaller "_order"
    {"tag": "out-2"},
    {"tag": "out-1"}
  ],
  "route": {"rules": [
    // 2 rules are merged into one due to the same "_tag",
    // outbound field is overwritten during the merging
    {"inbound":["in-1","in-1.1"],"outbound":"out-1.1"}
  ]}
}
```
