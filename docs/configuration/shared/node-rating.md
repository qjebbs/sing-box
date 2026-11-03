# Node Rating

How a group turns the results of its [health checks](/configuration/shared/health-check/) into
the order it picks by, and why each part of it is shaped the way it is.

This page is about the reasoning, not the fields: see
[load balance](/configuration/outbound/loadbalance/) and
[url test](/configuration/outbound/urltest/) for what can be configured.

### The Two Questions

A node can be bad in two independent ways, and a group has to answer one of them:

- **How far is it?** A node of 170ms delays everything it carries more than a node of 40ms.
- **How steady is it?** A node that answers in 200ms most of the time and stalls for three
  seconds sometimes delays the traffic that lands on the stall.

`leastping` answers the first and `leastload` the second, which is why they are not a single
score with a weight between them: no weight is better than any other without a model of how
much a stall costs, and a weight that has to be guessed is a number no measurement supports.

### The Statistics

Every statistic is computed from the checks that **succeeded**. A failed check waited for the
timeout, so its delay says nothing about the delay the node has when it works: the share of
checks that failed is carried separately and mixed in where the score is made.

With `x₁ … xₙ` the delays of the successful checks and `n` their count:

```
MED     = median(x₁ … xₙ)
JIT     = 1.4826 × median(|xᵢ − MED|)
DEV     = (1/n) × Σ|xᵢ − MED|
```

Only `MED` says how far the node is. `JIT` and `DEV` measure the same thing in two ways: how
far each check strays from that node's own median. Both are measured from the node's own
median, so neither knows how far the node is — a node of 4s that stays there and a node of
100ms that stays there have the same jitter.

- **`MED`** is the median rather than the average. The average is moved by a single slow
  check, and it then describes the worst moment of the window instead of the node.
- **`JIT`** is the *typical* deviation, the median of them. One slow check cannot move it: it
  would have to cross the middle of the samples. `1.4826` turns the median of the deviations
  into an estimate of the standard deviation, so `JIT` is on the same scale as `DEV` and as the
  standard errors below (`MAD ≈ 0.6745σ` for normal samples; see
  [Why Not the Standard Deviation](#why-not-the-standard-deviation)).
- **`DEV`** is the *average* deviation. Every check counts by its own share, so a rare spike
  adds what it cost instead of being ignored, and a spike that repeats adds more.

The two answer different questions about the same deviations, and that is the whole difference
between the objectives:

| | Question | Says |
| --- | --- | --- |
| `JIT` | how much does it usually vary? | a node that stalled once is still a close node |
| `DEV` | how much did it cost in total? | a node that stalls is not a steady node |

### The Scores

```
cost  = MED + JIT    # leastping, what a node typically costs, in milliseconds
cost  = DEV          # leastload, how far it strays from its own typical delay, in ms
```

Both scores are in milliseconds, but they answer the two questions from the top:

- `leastping` is the **cost of a node**. A node that is close but unstable costs as a node that
  is simply slower does, so `JIT` is added once, at its own scale. Written as a product it is
  the same thing, and shows what the addition means:

  ```
  MED + JIT = MED × (1 + JIT / MED)
  ```

  The factor in brackets is the **relative jitter**: `leastping` ranks by how far the node is,
  corrected by how much it typically varies.

- `leastload` takes `DEV` alone, with no `MED` in it. It asks how far the node strays from its
  own typical delay, so a node steady at 4s and a node steady at 100ms are equally steady. That
  is what the name asks for: not how fast it is, but how steady it is.

The failures are mixed in by their share, in the scale of the node:

```
failure = min(TIMEOUT, 3 × cost)
score   = (ALL - FAIL) / ALL × cost + FAIL / ALL × failure
```

A failed attempt costs three attempts that worked: it had to be tried again, but it is scored in
the scale of the node rather than as the timeout it waited for. Scoring it as the timeout would
bury a fast node that failed once behind every node that never failed, whatever its delay; the
cap at `TIMEOUT` keeps a node that fails most of the time from being scored better than the
failure it is.

### Tolerance

A group does not switch for a difference it cannot trust. The band is the range around the best
node within which the nodes count as equally good, and each objective has its own, because the
**quantity** they compare is not the same — that is why the two cannot be one field:

| Objective | Score | Field | Unit | Default |
| --- | --- | --- | --- | --- |
| `leastping` | `MED + JIT` | `rtt_tolerance` | share of the best value | `0.2` |
| `leastload` | `DEV` | `jitter_tolerance` | absolute milliseconds | `10ms` |

A tolerance belongs to the objective that measures its quantity: setting one for the other
objective, or for an objective left at its default, is refused when the group is created
instead of being silently ignored, since a value that has no effect is a mistake rather
than a preference.

**The delay is relative**, because it is far from zero: 20% of the best 170ms is 34ms, a real
band.

**The jitter must be absolute**, because it approaches zero: 20% of the best 2.8ms is 0.56ms,
barely more than the millisecond the delays are stored in, while the measured 2σ resolution is
6.6ms — **a 0.56ms band selects noise, not nodes** (see [Noise](#noise)). So the jitter band is
an absolute value anyone can reason about: `jitter_tolerance: 10ms` means "within 10ms of the
steadiest node counts as equally steady".

The band of `leastload` also carries a **resolution margin**:

```
band = jitter_tolerance + 2√2 × DEV_best / √n
cut  = DEV_best + band
```

The second term is not a floor that applies only sometimes, it is always added — because
`jitter_tolerance` describes the **true** difference, while the comparison only sees **observed**
values:

| `sampling` | margin | band (`jitter_tolerance: 10ms`) |
| --- | --- | --- |
| 10 | 2.5ms | 12.5ms |
| 50 | 1.1ms | 11.1ms |
| 200 | 0.6ms | 10.6ms |

The more samples, the smaller the margin and the closer the band to the configured value — but it
is **never narrower than it**.

Why added rather than the larger of the two: with `max`, a `jitter_tolerance` of 10ms against a
2.5ms margin means the margin does not exist, and a node sitting on the cut flips in and out with
the measurement noise — measured, a node whose true `DEV` is exactly at the cut is selected 50% of
the time. Adding the margin pushes the cut out and the same node is selected 74% of the time.

The margin uses `DEV_best`, not each node's own standard error: the other way round, a noisier
node would get a larger margin and become easier to include, which is the wrong direction.

`leastping` needs no such term: with the best node at 408ms the band is 81.6ms, while the 2σ
noise of the score itself is about 1.8ms, 2.2% of it, and can be ignored.

### Retention

When the node in use is replaced, its existing connections are not necessarily interrupted.
A node that left the selection is kept while its excess over the band is within the noise of
the two estimates, so a connection is not broken by a measurement that could have gone either
way. The allowance is capped at the band itself, so a node never survives more than twice the
tolerance away from the best.

This is hysteresis for connections, and it is a different question from the selection: the
selection asks which node new connections should use, the retention asks whether it is worth
breaking the connections a node already has.

### Noise

Every statistic here is an estimate from a small window, so every comparison has an error bar.
For a mean of `n` samples the standard error is `σ/√n`, and `DEV` is a mean of deviations whose
own spread is about `DEV`:

```
SE(DEV) ≈ DEV / √n
```

**The relative noise is `1/√n`, the same for every node, and it depends only on the sample
count.** Measured with `sampling` 10 on the raw samples of ten nodes, `SE(DEV)/DEV` fell between
24% and 38%, against `1/√10 = 32%`.

The resolution needed to separate two nodes is the difference of two estimates, so it is about
`2√2/√n` at two standard errors:

| `sampling` | resolution `2√2/√n` | a 20% relative band |
| --- | --- | --- |
| 10 | 89% | 4.5× narrower than the noise |
| 50 | 40% | 2× narrower |
| 200 | 20% | exactly level |
| 500 | 13% | the band decides |

This is why **the jitter is measured in absolute milliseconds and the delay in relative ones**.
A 20% relative band on the jitter would need `sampling > 200`; at an `interval` of 2 minutes that
is 400 minutes, about 6.7 hours, and the longer the window the older what it reads —
**lowering the noise and following a change are two ends of one knob**.

With an absolute `jitter_tolerance`, the question disappears: the band is a configured value that
does not shrink with the sample count, and the resolution margin is always added and shrinks as
the window grows (see the table under [Tolerance](#tolerance)). There is no regime where too few
samples turn the band into noise-chasing.

!!! note ""

    One intuition worth correcting: **the noise cannot be subtracted away.**
    `SE(DEV)/DEV = 1/√n` is the same ratio for every node, so removing it from each `DEV`
    multiplies them all by `√(1−1/n)` — the ranking and the selection are unchanged. Noise is not
    a bias that can be subtracted, it is a spread around the true value that cannot be. The only
    place it can be accounted for is the band, which is the resolution term above.

### Why Not the Standard Deviation

The sample standard deviation is the usual measure of spread, and it is the wrong one here,
for three reasons that compound:

1. **It is measured around the average, which the outlier moves.** One slow check of ten lifts
   the average from 449ms to 897ms, and the other nine checks are then scored against a centre
   they have nothing to do with: their deviation of 457ms becomes 751ms without any of them
   changing.
2. **It is squared, so one sample can own the result.** In a window where a single check took
   3.15s, 83% of the variance comes from that check, and the reported spread describes that one
   probe rather than the node.
3. **The square root does not restore the scale.** A spike of `X` in `n` samples contributes
   `X/n` to the mean deviation, which is the share of connections it delays, but `X/√n` to the
   standard deviation — 3.2× too much at `n = 10`, and worse as the window grows.

`DEV` is the same idea without the two defects: linear, and measured from a centre the spike
cannot move. A node that alternates between 200ms and 800ms still reads 445ms, so it is not
that `DEV` ignores instability — it is that the estimate stays a statement about the node.

### Why Not Trend

A window has an order, and none of the statistics above use it: shuffle the samples and `MED`,
`JIT` and `DEV` are unchanged. So a node that has been sliding for ten minutes reads the same
as a node that has been steady, if the spreads match.

A half-window comparison (`median(second half) − median(first half)`) does recover that, and
it is cheap. It is not part of the score because a score that contains a trend would rank
differently when `interval` changes, and a ranking has to be a property of the node, not of the
cadence it was sampled at. A trend belongs to the decision, next to the tolerance, not to the
measurement.

It also needs care: with a `sampling` of 10, two consecutive bad checks out of five in the
second half do not move its median, so the trend only appears from the third one onward. That
makes it a slower signal than the last check, which is already in the statistics.

### Debugging a Rating

The full report of a group — every statistic, every node, and the checks they were computed
from — is served by the Clash API at `GET /private/health/{tag}`, so a ranking can be replayed
and explained offline instead of being inferred from the log.

The report describes one ranking, and it says enough to replay it: `best` is the node every
other one is compared to, and `limit` the highest score that still counts as equally good.
A node is as good as `best` when its `status` is the one of `best` and its `score` is at most
`limit`. A load balance group selects every such node (or at least `min_nodes` of them), while
a group that uses a single node keeps the one in use as long as it is one of them, so `best`
and `selected` can name different nodes there. All of it comes from the same round of checks,
so a report taken in the middle of a round shows that round's ranking, not a mixture of the
nodes measured so far and the previous decision.
