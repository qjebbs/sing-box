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

`quick` answers the first and `steady` the second, which is why they are not a single
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
cost    = MED + JIT  # quick, what a node typically costs, in milliseconds
cost    = DEV        # steady, how far it strays from its own typical delay, in ms
failure = min(TIMEOUT, 3 × cost)
score   = (ALL - FAIL) / ALL × cost + FAIL / ALL × failure
```

Both scores are in milliseconds, but they answer the two questions from the top:

- `quick` is the **cost of a node**. A node that is close but unstable costs as a node that
  is simply slower does, so `JIT` is added once, at its own scale.
- `steady` takes `DEV` alone, with no `MED` in it. It asks how far the node strays from its own
  typical delay, so a node steady at 4s and a node steady at 100ms are equally steady. The name
  is the question: it does not ask how quickly the node answers, but how steadily.
- The failures are mixed in by their share, in the scale of the node. A failed attempt costs
  three attempts that worked: it had to be tried again, but it is scored in the scale of the node
  rather than as the timeout it waited for. Scoring it as the timeout would bury a fast node that
  failed once behind every node that never failed, whatever its delay; the cap at `TIMEOUT` keeps
  a node that fails most of the time from being scored better than the failure it is.

### Tolerance

A group does not switch for a difference it cannot trust. The band is the range around the best
node within which the nodes count as equally good. The defaults differ, because the **quantity**
the two objectives compare is not the same:

| Objective | Score       | Default |
| --------- | ----------- | ------- |
| `quick`   | `MED + JIT` | `50ms`  |
| `steady`  | `DEV`       | `10ms`  |

The band also carries a **resolution margin** (the noise), in the same shape for both objectives:

```
band = tolerance + 2√2 × JIT_best / √n
cut  = score_best + band
```

`tolerance` describes the **true** difference, while the comparison only sees **observed** values.
The more samples, the smaller the noise and the closer the band to the configured value — but it is
**never narrower than it**.

!!! note "Why added rather than the larger of the two"

    With `max`, once `tolerance` (50ms, say) is larger than the margin (7ms), the margin does not
    exist, and a node sitting on the cut flips in and out with the measurement noise.

The scale of the noise is `JIT_best`, not `DEV_best`: the two answer different questions.

- `DEV` is **how much the node paid in total**, and `steady` ranks by it, so a single spike must
  count in full ("a node that stalls is not a steady node").
- The margin has to estimate **how far this reading can be off**, i.e. the scale of the
  fluctuation. `JIT` is the **median** of the deviations, converted to σ, and one slow check does
  not move it.

### Retention

With `interrupt_exist_connections` enabled, when the node in use is replaced, its existing
connections are not necessarily interrupted. A node that just left the selection is kept while its
excess over the band is still within the noise of the two estimates, so a connection is not broken
by a measurement that is not representative:

```
allowance = band + min(band, 2 × √(SE_node² + SE_best²))
```

The `2 ×` is the usual 95% confidence: an excess within that is not distinguishable from noise.
The allowance is capped at the band itself, so a node never survives more than **twice the band**
away from the best node — hysteresis is there to ride out noise, not to keep a node that is
clearly worse.

The two objectives share the band, and differ in the noise: the score of `quick` is made of the
median and the jitter, the one of `steady` of the mean deviation.

| Objective | Noise (`SE`) |
| --- | --- |
| `quick` | combined from the median and the jitter: `1.25·JIT/√n` and `JIT/√(2(n−1))` |
| `steady` | `DEV/√n` |

- `quick` defaults to 50ms, while its noise at `sampling` 10 with a `JIT` of 10ms is about 13ms,
  small against the band, so the allowance is essentially "band plus two standard errors" and the
  cap rarely binds.
- `steady` defaults to 10ms, while `DEV/√n` runs from 4ms on a clean node to 58ms on a node with a
  spike at `sampling` 10, so the cap often binds and the allowance falls back to `2 × band`. The
  noise here deliberately stays on `DEV` rather than moving to `JIT`: retention asks "is it worth
  breaking", and being lenient towards what may be a single transient spike is right; the band
  measures the reading, retention measures the cost the node has already paid, and the two are not
  the same quantity.

A `urltest` group has no second layer: it uses one whole node at a time, so the hysteresis lives in
the **selection** — the node in use keeps its place while its score stays within `Limit(best)` of
the best node, that is **one** band rather than two, and it is measured against the best node for
the same reason.

This is hysteresis for connections, and it is a different question from the selection: the
selection asks which node new connections should use, the retention asks whether it is worth
breaking the connections a node already has.

### Noise

Every statistic here is an estimate from a small window, so every comparison has an error bar.
For a mean of `n` samples the standard error is `σ/√n`; `DEV` is a mean of deviations, so its
standard error is the spread of **those deviations** over `√n`:

```
SE(DEV) = sd(|xᵢ − MED|) / √n
```

Only while those deviations are alike is `sd(deviation) ≈ DEV`, and only then does
`SE(DEV) ≈ DEV/√n` — which is how a clean node behaves. **One slow check breaks that premise**:
the deviations become "nine small ones and one large one", their spread is far larger than their
mean, and the true standard error grows with it while `DEV/√n` does not.

That is why the margin is not taken from `DEV_best`: `DEV` describes the node (how much it paid),
not the error of this reading. The scale of the error has to be measured by something a single
sample cannot move, which is `JIT`.

**The relative noise is `1/√n`, the same for every node, and it depends only on the sample
count** — but it is relative to the node's own scale, not to `DEV`.

The resolution needed to separate two nodes is the difference of two estimates, so it is about
`2√2/√n` at two standard errors. That is the margin, in units of `JIT_best`, and it shrinks with
the sample count:

| `sampling` | margin (in `JIT_best`) |
| --- | --- |
| 10 | 89% |
| 50 | 40% |
| 200 | 20% |
| 500 | 13% |

The band is a configured value that does not shrink with the sample count, and the margin only
widens it, so there is no regime where too few samples turn the band into noise-chasing. But the
longer the window the older what it reads: **lowering the noise and following a change are two
ends of one knob**.

!!! note ""

    One intuition worth correcting: **the noise cannot be subtracted away.** In the scale of each
    node, the relative noise is `1/√n` for every node, so removing it from each score multiplies
    them all by the same factor — the ranking and the selection are unchanged. Noise is not
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
