### 结构

```json
{
  "type": "urltest",
  "tag": "auto",
  
  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ],
  "all_providers": false,
  "providers": [
    "provider-a",
    "provider-b",
  ],
  "exclude": "",
  "include": "",
  "health_check": "",
  "tolerance": 0.2,
  "interrupt_exist_connections": false
}
```

### 选择

节点按下述分数排序，并使用其中最好的一个：

```
score = (ALL - FAIL) / ALL × MED + FAIL / ALL × TIMEOUT
```

`MED` 是**成功**检查的中位数延迟，`FAIL / ALL` 是失败检查的占比，`TIMEOUT` 是失败检查放弃前等待的超时（15s）。
一次尝试要么以 `MED` 成功，要么等到 `TIMEOUT` 失败，因此分数就是一次尝试的平均开销：
十次里失败一次的节点，分数比中位数多 1.5s。

失败不参与 `MED` 的计算，否则会被计算两次。因此分数不会超过超时——全部失败的节点分数即为超时，
而没有成功检查的节点没有延迟可报告。

单次异常的检查结果不会改变选择，只有当另一个节点的分数比当前节点好出 [`tolerance`](#tolerance) 以上时才会替换。

节点按健康检查的结论分为三类：

1. 最近一次检查通过的节点，按分数排序；
2. 从未检查过、或统计已过期的节点；
3. 最近一次检查失败的节点。

类别的优先级高于分数，因此失败的节点只有在没有其它类别节点时才会被使用。后两类内部不做比较，按配置顺序选择第一个，
所以还没有任何测量结果的组不会随机选择。

### 字段

#### outbounds

用于测试的出站标签列表。

#### all_providers

当 `all_providers` 为 `true` 时，将使用所有订阅，而不只是 `providers` 列表中的订阅。默认为 `false`。

#### providers

用于测试的[订阅](/zh/configuration/provider)标签列表。

#### exclude

排除 `providers` 节点的正则表达式。排除表达式的优先级高于包含表达式。

#### include

包含 `providers` 节点的正则表达式。

#### health_check

用于测试节点的[健康检查](/zh/configuration/shared/health-check/)。可以是顶层 `health_checks` 条目的字符串标签，也可以是内联的健康检查对象。

必填，不存在隐式的默认健康检查。

#### tolerance

替换当前节点所需的相对分数优势，默认为 `0.2`，即最好节点分数的 20%。该值必须小于 `1`。

比较的是[选择](#选择)中的分数，即中位数延迟加上失败惩罚，而不是单次检查结果。

最近一次检查失败的节点不受该值影响，因为它与其它节点不属于同一类别。

#### interrupt_exist_connections

当选定的出站发生更改时，中断现有连接。

仅入站连接受此设置影响，内部连接将始终被中断。
