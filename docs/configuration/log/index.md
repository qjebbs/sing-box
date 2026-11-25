# Log

### Structure

```json
{
  "log": {
    "disabled": false,
    "level": "info",
    "output": "box.log",
    "timestamp": true,
    "disable_color": false
  }
}

```

### Fields

#### disabled

Disable logging, no output after start.

#### level

Log level. One of: `trace` `debug` `info` `warn` `error` `fatal` `panic`.

#### output

Output file path. Will not write log to console after enable.

`stdout` and `stderr` are accepted to write all log levels to the standard output or standard error.

If empty, the stream is chosen by level: `warn`, `error`, `fatal` and `panic` are written to `stderr`, `info` and below are written to `stdout`. This allows a service supervisor that only captures `stderr`, such as procd, to record serious messages without the routine log filling its buffer. To keep the previous behavior of writing everything to `stderr`, set it to `stderr` explicitly.

#### timestamp

Add time to each line.

#### disable_color

Disable colored log output.

Colors are also disabled when [`output`](#output) is a file.