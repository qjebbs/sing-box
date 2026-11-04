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

The implementation adds the resolution margin only when at least two checks succeeded; otherwise
`band = tolerance`.

`tolerance` describes the **true** difference, while the comparison only sees **observed** values.
The more samples, the smaller the noise and the closer the band to the configured value — but it is
**never narrower than it**.

!!! note "Why added rather than the larger of the two"

    With `max`, once `tolerance` (50ms, say) is larger than the margin (7ms), the margin does not
    exist, and a node sitting on the cut flips in and out with the measurement noise.

The scale of the noise is `JIT_best`, and that choice says two things:

- **Not `DEV_best`**: `DEV` is **how much the node paid in total**, and `steady` ranks by it, so a
  single spike must count in full ("a node that stalls is not a steady node"). The margin needs
  "how far this reading can be off", and only the median-based `JIT` is not moved by one slow
  check.
- **Not the standard error `SE` of the score either**: a full error estimate can be widened by a
  sum of squares, so one slow check inflates it just the same, and the band decides who counts as
  equally good — inflating it lets a clearly worse node in (see [Noise](#noise)). The price is that
  the margin is a **heuristic**, not a mathematical lower bound on the actual noise or a 95%
  resolution guarantee. On measured windows, the score error divided by `JIT_best/√n` had a median
  of about 2.8; that ratio depends on the data and is not universal. `tolerance` should be sized
  with this limitation in mind.

### Retention

With `interrupt_exist_connections` enabled, when the node in use is replaced, its existing
connections are not necessarily interrupted. A node that just left the selection is kept while its
excess over the band is still within the noise of the two estimates, so a connection is not broken
by a measurement that is not representative:

```
allowance = band + min(band, 2 × √(SE_node² + SE_best²))
```

The multiplier `2` is an engineering heuristic, not a 95% confidence guarantee: the noise values
are estimates, and the selection band uses a robust proxy rather than the full score error.
The allowance is capped at the band itself, so a node never survives more than **twice the band**
away from the best node — hysteresis is there to ride out noise, not to keep a node that is
clearly worse.

The two objectives differ in practice by the size of their estimated score noise, see
[Noise](#noise).

- `quick` defaults to 50ms, while its noise is usually some tens of milliseconds at a `sampling`
  of 10 (`sd(x)` and `sd(deviation)` each contribute a term), so the allowance is essentially
  "band plus a noise allowance" and the cap sometimes binds.
- `steady` defaults to 10ms, while its estimated spread term `sd(|xᵢ − MED|)/√n` reaches tens of milliseconds at a
  `sampling` of 10, so the cap often binds and the allowance falls back to `2 × band`. The noise
  here is the spread of the samples rather than `JIT`, because retention asks "is it worth
  breaking": one slow check makes the spread larger and keeps the connections longer, which is the
  outcome wanted. The band, by contrast, uses the robust `JIT` — the two sources differ on
  purpose.

A `urltest` group has no second layer: it uses one whole node at a time, so the hysteresis lives in
the **selection** — the node in use keeps its place while its score stays within `Limit(best)` of
the best node, that is **one** band rather than two, and it is measured against the best node for
the same reason.

This is hysteresis for connections, and it is a different question from the selection: the
selection asks which node new connections should use, the retention asks whether it is worth
breaking the connections a node already has.

### Noise

Every statistic here is an estimate from a small window, so a score is an estimate, and a
comparison of two scores is an estimate too. There are two levels of noise: of one score, and of
the difference between two. The selection and the retention use the same model, with different
nodes substituted into it.

**The noise of one score.** A score is computed from `n` samples; how much it would move if the
checks ran again is its standard error. For a sample mean of independent observations, the
standard error is estimated by the sample spread divided by `√n`; for medians and statistics
around an estimated center, it depends on the distribution and the statistic. The formulas below
are the implementation's noise proxies and rely on the approximations stated with them:

```
SE(mean) = sd(observations) / √n
```

Under a stable IID model, `1/√n` often describes how estimation error shrinks as samples are added;
it does not make the error scale or coefficient the same for every statistic or node.
When fewer than two checks succeeded, the implementation uses the base score as its noise proxy
instead of applying the formulas below.

`steady` scores by `DEV`, the average distance to the median, so its spread is the one of **those
distances**:

```
SE_proxy(DEV) ≈ sd(d) / √n, where dᵢ = |xᵢ − MED|
```

This is an asymptotic plug-in estimate under approximately independent, identically distributed
samples, not an exact finite-sample standard error: all `dᵢ` share the sample median. With only two
successful checks the two absolute deviations are equal, so `sd(d)` is zero even though `DEV` would
vary across repeated samples. Trends and changing sample distributions can also invalidate the
approximation.

`quick` scores by a sum of two estimates. The median of `n` samples has asymptotic standard error
`1/(2f(m)√n)`, where `f(m)` is the density at the population median; `1.2533σ/√n` is its
normal-distribution approximation. The jitter is a **median of absolute deviations** scaled by
`1.4826`. The implementation uses the following noise proxy:

```
SE_proxy(MED) = 1.2533 · sd(x) / √n
SE_proxy(JIT) = 1.858 · sd(d) / √n
SE_proxy      = √( SE_proxy(MED)² + SE_proxy(JIT)² )
```

The `1.858` factor is an implementation heuristic that applies a normal-median coefficient to
`d`; however, even for normal `x`, absolute deviations are half-normal, not normal. It should not be
read as a rigorously derived standard error. For a symmetric distribution, if `q` is the median of
`|X−m|` and `g(q)` its density, the known-center asymptotic standard error of its median is
`1/(2g(q)√n)`; estimating the center and non-normal shapes require corresponding adjustments.
The two score terms are asymptotically independent under a symmetric IID model, but that is not an
exact identity for arbitrary distributions or finite windows.

**The noise of the difference of two scores.** If the two nodes' measurement errors are independent,
their estimated variances add:

```
SD(score_node − score_best) ≈ √(SE_proxy_node² + SE_proxy_best²)
```

If the checks share a target or network conditions, their errors may be correlated. The difference
variance then also contains `−2·Cov(score_node, score_best)`; the implementation ignores this term.
With independent errors, variances are added before taking the square root.

**Why another 2.** The multiplier 2 is a convenient engineering choice. Because the score errors
are approximated and may not be normally distributed, it does not by itself imply 95% coverage.

**The same model substituted two ways, in the selection and in the retention:**

| | compared with | margin | why |
| --- | --- | --- | --- |
| Selection: the band | the best vs an **unknown candidate** | `2√2 × JIT_best/√n` | the cut applies to every candidate, so it can only assume the candidate is measured as well as the best; using each node's own noise estimate would give a noisier node a wider margin, the wrong direction. The robust `JIT` is used here on purpose instead of the full score-noise proxy, see below |
| Retention: the allowance | the best vs **this one node** | `2 × √(SE_proxy_node² + SE_proxy_best²)` | the node is known, so its own noise proxy is used; and the allowance is capped at the band, so a noisy node cannot stay forever |

Both score-noise proxies already carry the `1/√n`, there is nothing else to divide by: more samples
shrink them under the stated approximation. The selection is written as `2√2 × JIT_best/√n`
because that is the policy's "two equally measured estimates" substitution, with 2 and `√2`
folded into one constant; it does not make the proxy an exact standard error.

**Why the selection does not use the full score-noise proxy directly.** The band decides who counts
as equally good; if its margin followed that proxy, one slow check could widen the band and let a
clearly worse node in — the opposite of "a single probe must not decide the selection" (see
[Tolerance](#tolerance)). On measured windows, the score-noise estimate divided by
`JIT_best/√n` had a median of about **2.8**; that ratio depends on the data and is not universal.
The selection margin is therefore a heuristic, not a mathematical lower bound on actual noise or
a 95% resolution. Retention uses each known node's own noise proxy, and caps the allowance at the
band so a noisy node cannot stay indefinitely.

For a fixed statistic and a stable IID distribution, its standard error typically decreases on the
order of `1/√n`. The scale and coefficient depend on the node's distribution and the statistic; the
relative noise is not necessarily the same across nodes.

The band is a configured value that does not shrink with the sample count, and the margin only
widens it, so there is no regime where too few samples turn the band into noise-chasing. But the
longer the window the older what it reads: **lowering the noise and following a change are two
ends of one knob**.

!!! note ""

    One intuition worth correcting: **the noise cannot be subtracted away.** Noise is not a bias
    that can be subtracted; it is variation around the underlying value.
    Since the relative noise can differ across nodes, subtracting an estimated amount from each
    score would not generally preserve the ranking. The band is a policy for tolerating that
    uncertainty, not a way to remove it.

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
