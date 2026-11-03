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

The scale of the noise is `JIT_best`, and that choice says two things:

- **Not `DEV_best`**: `DEV` is **how much the node paid in total**, and `steady` ranks by it, so a
  single spike must count in full ("a node that stalls is not a steady node"). The margin needs
  "how far this reading can be off", and only the median-based `JIT` is not moved by one slow
  check.
- **Not the standard error `SE` of the score either**: `SE` is the exact resolution, but it is a
  sum of squares, so one slow check inflates it just the same, and the band decides who counts as
  equally good — inflating it lets a clearly worse node in (see [Noise](#noise)). The price is that
  the margin is a **floor**: the standard error of a score is about 2.8 times it when measured, so
  the band guarantees "never narrower than the tolerance", not a 95% resolution — `tolerance`
  should be sized with that in mind.

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

The two objectives differ in practice by the size of their noise, which is the standard error of a
score, see [Noise](#noise).

- `quick` defaults to 50ms, while its noise is usually some tens of milliseconds at a `sampling`
  of 10 (`sd(x)` and `sd(deviation)` each contribute a term), so the allowance is essentially
  "band plus two standard errors" and the cap sometimes binds.
- `steady` defaults to 10ms, while `sd(|xᵢ − MED|)/√n` reaches tens of milliseconds at a
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
checks ran again is its standard error, the spread of the statistic it is made of over `√n`:

```
SE = sd(statistic) / √n
```

`√n` is "what is left after averaging `n` times": the more samples, the closer the score is to the
truth. The two objectives are made of different statistics, so their `sd` differs.

`steady` scores by `DEV`, the average distance to the median, so its spread is the one of **those
distances**:

```
SE(DEV) = sd(|xᵢ − MED|) / √n
```

`quick` scores by a sum of two terms, each an estimate with its own error, combined by their
variance. The median of `n` samples has a standard error of `1.2533σ/√n` (an asymptotic result: the
variance of a median is `1/(4n·f(m)²)`, which is `(1.2533σ)²/n` for a normal sample — it uses only
the rank of the samples, a quarter worse than the mean). The jitter is a **median of the
deviations** scaled by `1.4826`, so its standard error is `1.4826 × 1.2533` times the one of a
single deviation:

```
SE(MED) = 1.2533 · sd(x) / √n
SE(JIT) = 1.858 · sd(|xᵢ − MED|) / √n
SE      = √( SE(MED)² + SE(JIT)² )
```

**The noise of the difference of two scores.** `SE_node` and `SE_best` are how much each score
would move, and they are fixed numbers once computed; what moves is the score, so
`score_node − score_best` moves as well. The two nodes are measured separately, their errors are
independent, and so their **variances add**:

```
SD(score_node − score_best) = √(SE_node² + SE_best²)
```

Squared first and then taken back to milliseconds, not the two `SE` added or subtracted: independent
errors cancel each other out in part, and this says exactly how much.

**Why another 2.** Under a normal distribution `1.96σ` covers 95% of the probability, rounded up to
2 for the engineering version: "beyond this, it no longer looks like noise".

**The same model substituted two ways, in the selection and in the retention:**

| | compared with | margin | why |
| --- | --- | --- | --- |
| Selection: the band | the best vs an **unknown candidate** | `2√2 × JIT_best/√n` | the cut applies to every candidate, so it can only assume the candidate is measured as well as the best; using each node's own `SE` would give a noisier node a wider margin, the wrong direction. The robust `JIT` is used here on purpose instead of `SE`, see below |
| Retention: the allowance | the best vs **this one node** | `2 × √(SE_node² + SE_best²)` | the node is known, so its own real `SE` is used; and the allowance is capped at the band, so a noisy node cannot stay forever |

Both `SE` already carry the `1/√n`, there is nothing else to divide by: more samples shrink both
automatically. The selection is written as `2√2 × JIT_best/√n` because that is what the "two
equally measured estimates" substitution gives, `2√2` folding the 2 and the `√2` into one constant.

**Why the selection does not use `SE` directly.** The band decides who counts as equally good; if
its margin followed the full error of the score, one slow check would widen the band and let a
clearly worse node in — the opposite of "a single probe must not decide the selection" (see
[Tolerance](#tolerance)). The price is a margin narrower than `SE`: the measured standard error of
a score is **2.8 times** `JIT_best/√n` on median, so this margin is a **floor**, not a 95%
resolution. The retention has no such problem — it only acts on nodes that were already selected,
where a wider allowance just breaks fewer connections, so it uses the real `SE`.

**The relative noise is `1/√n`, the same for every node, and it depends only on the sample
count** — but it is relative to the node's own scale, not to `DEV`.

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
