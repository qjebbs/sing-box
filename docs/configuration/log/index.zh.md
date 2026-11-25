# 日志

### 结构

```json
{
  "log": {
    "disabled": false,
    "level": "info",
    "output": "box.log",
    "timestamp": true
  }
}

```

### 字段

#### disabled

禁用日志，启动后不输出日志。

#### level

日志等级，可选值：`trace` `debug` `info` `warn` `error` `fatal` `panic`。

#### output

输出文件路径，启动后将不输出到控制台。

接受 `stdout` 和 `stderr`，将所有等级的日志写入标准输出或标准错误。

为空时按等级选择输出流：`warn`、`error`、`fatal`、`panic` 写入 `stderr`，`info` 及以下写入 `stdout`。这样仅采集 `stderr` 的服务监管程序（如 procd）可以只记录严重日志，而不被日常日志刷屏。如需保持全部输出到 `stderr` 的旧行为，请显式指定为 `stderr`。

#### timestamp

添加时间到每行。