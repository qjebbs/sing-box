# Node Rating

How a group turns the results of its [health checks](/configuration/shared/health-check/) into
the order it picks by, and why each part of it is shaped the way it is.

This page is about the reasoning, not the fields: see
[load balance](/configuration/outbound/loadbalance/) and
[url test](/configuration/outbound/urltest/) for what can be configured.

### The Two Questions

A node can be bad in two independent ways, and a group has to answer one of them:

- `quick`: **how fast is the node usually?** A node of 170ms delays everything it carries more
  than a node of 40ms.
- `idle`: **how much longer does the node wait now than it does at its best?** A node that
  answers in 200ms off-peak and sits at 600ms every evening delays every connection of that
  evening.

They are not one score with a weight between them, because they are not the same quantity:
`quick` ranks the level of the delay, `idle` ranks the part of it that **queueing** adds. No
weight is better than any other without a model of what a stalled connection costs, and a weight
that has to be guessed is a number no measurement supports.

### The Statistics

Every delay statistic is computed from the checks that **succeeded**. A failed check waited for
the timeout, so its delay says nothing about the delay the node has when it works: the failures
are carried separately, for the qualification of the node and for the failures that `quick`
prices in.

With `x₁ … xₙ` the delays of the successful checks of the current window, `n` their count, and
`dᵢ = |xᵢ − MED|` the distance of each check to the median:

```
MED     = median(x₁ … xₙ)      # the median delay of this window
JIT     = 1.4826 × median(dᵢ)  # the typical deviation
DEV     = (1/n) × Σdᵢ          # the average deviation
```

`MED`, `JIT` and `DEV` are computed from the window, and are replaced when the window moves on.
One more reference outlives the window:

```
SLOT     = 30 minutes a slot, 48 slots, covering 24 hours     # a constant of the implementation
slot[k]  = the fastest check seen within slot k
BASELINE = min(of the slots within the last 24 hours)
STANDING = MED − BASELINE           # standing queue: how much longer the typical delay is than its best
```

The slots are aligned to the wall clock: `k` is which 30 minutes of the day the current time
falls in, so a slot always covers the same part of the day. Entering a slot resets it to the
fastest check of the round, and the smallest value within the slot is kept from then on; 24 hours
later the slot is overwritten by the next measurement of the same slot, so it expires by itself.
See [Why the Baseline Is a Ring of Slots](#why-the-baseline-is-a-ring-of-slots).

So `BASELINE` records **how fast the node can be at each time of day**, not the lowest value
since some event. It needs no expiry timer of its own: the ring is the expiry.

The slot is written before the score is taken, so the observation of the round counts at once.
That cannot make a score of zero: a slot holds a minimum, so after writing, `BASELINE ≤ MIN ≤ MED`
and `STANDING` stays non-negative. The first round needs no case of its own either — the ring then
holds this round's fastest check, so `STANDING` is exactly `MED − MIN`, the same formula as later.

`STANDING` therefore cannot be negative. When the node gets faster the baseline follows it down and
the score falls on its own; when it gets slower the baseline stays and the score rises. The
direction needs no extra handling.

The formulas above describe at least two successful checks. With only one there is no spread to
estimate, so `JIT`, `DEV` and `STANDING` are all set to half of that delay: not having enough
samples is not the same as having no variation. With no successful check there is no delay
statistic at all.

`MIN` is the fastest check of the window. It is what the baseline takes its value from, and it
approximates the delay with the pipe drained: the propagation delay plus the least processing.

`MED` is the level of the delay. `JIT`, `DEV` and `STANDING` all describe variation, so none of
them knows how far the node is, but for different reasons: `JIT` and `DEV` measure the distance
to the median of this window, and move with it; `STANDING` measures the distance to **the best
this node has ever been**. A node steady at 4s and a node steady at 100ms can have all three the
same.

- **`MED`** is the median rather than the average, so a single slow check does not move the
  typical delay.
- **`BASELINE`** is **the fastest check the node has ever been seen at**, with no smoothing. A
  lucky probe lowers it, and the node carries a few milliseconds of false queueing from then on;
  what that buys is a quantity that is not dull. Recorded from the median, every node in a quiet
  period equals its own median, every `STANDING` is zero, and `idle` has nothing left to rank —
  and a node that alternates between two bandwidth pools (two checks near 570ms and two near
  1265ms, a median exactly between them at 919ms) reads as the steadiest of all.
- **`STANDING`** is the delay that queueing adds for good. When a line is full, the datagrams
  waiting in its buffers raise the delay, and this difference is that waiting. Subtracting the
  node's own best cancels the propagation delay, so it does not drift with the distance.
- **`JIT`** is the **typical** deviation, the median of them. A few spikes do not move it;
  `1.4826` converts the median of the deviations to the scale of a standard deviation, see
  [Derivations](#derivations).
- **`DEV`** is the **total** deviation, the average of them. Every check counts by its own share,
  so a rare spike adds one share and a repeated one adds more. It is still computed and reported,
  for diagnosis; the ranking no longer uses it.

The difference between the measures of variation is how they summarise the same checks:

| | Question | Says |
| --- | --- | --- |
| `JIT` | how much does it usually vary? | a node that stalled once is still a close node |
| `DEV` | how much did it cost in total? | a node that stalls is not a steady node |
| `STANDING` | how much longer than its own best? | congestion stays on the clock until it really goes |

### The Scores

The two objectives have different formulas, because a failure sits in a different place in each.

The score of `quick` is what the node typically costs, with the successes and the failures
weighted by their shares. With `ALL` the number of checks, `FAIL` the failures, and `TIMEOUT` the
timeout of a health check:

```
base    = MED + JIT
cost    = min(TIMEOUT, base)
failure = min(TIMEOUT, 3 × cost)
score   = (ALL - FAIL) / ALL × cost + FAIL / ALL × failure
```

A node with a lower typical delay and a smaller typical variation scores lower: the variation is
added to the cost of the delay in milliseconds.

`cost` is the base of the score under the successful checks, `failure` is what a failed check is
worth. A failure counts as at most three times a success and is capped at `TIMEOUT`; that is a
scoring policy, not a measurement of what a retry really costs. Scoring every failure as the whole
timeout it waited for would let one failure outweigh every node that never failed.

The score of `idle` is the standing queue alone; the failures do not enter it:

```
score = min(TIMEOUT, STANDING)
```

Only the standing queue is compared, not the level of the delay: a node at 400ms and one at 100ms
can both queue nothing over their own best, while a node of 100ms that queues up to 600ms cannot. A
node at its best scores zero; tied scores are ordered by the smaller spread first.

Why can a failure not be priced in? `STANDING` can be close to zero, and three times it is close
to zero too. In a measured window, a node that failed five of ten checks and otherwise sat at
410ms had a standing queue of a few tens of milliseconds, which is still a few tens of
milliseconds at three times; a node that never failed and queued up to 200ms ranked behind it.
**The milliseconds of queueing and the cost of a failure are not on one scale, and no coefficient
converts one into the other**: across a measured window the standing queue spanned 170 times, while
what a failure costs is roughly constant in absolute time, so the ratio between them goes from tens
to a fraction. `idle` therefore treats a failure as a question of qualification: `max_fail` is how
many checks of the window may fail before the node stops being qualified. An `idle` group has to
set `max_fail`; without it the limit does not apply and a failing node can still rank first.

If every check failed, the successful delay is unknown and both objectives score `TIMEOUT`; if no
check has run yet, there is no score.

A score is an observation from a finite number of checks, not the constant "true value" of the node
or of its line; comparing nodes means taking the noise of the estimates into account.

### Noise

A score is computed from a finite number of checks, so it is an estimate, not a property the node
holds. Another batch of checks could move it, and with it the difference between two nodes. Here
`n` is the number of successful checks, which is not the `ALL` the failure share is computed from.

#### The Noise of One Score

The standard error is how much a score would move if the checks ran again. The standard error of a
sample mean is estimated by the sample spread over `√n`; for medians and for statistics measured
from an estimated centre, the shape of the distribution matters too. The implementation uses these
proxies:

```
SE_proxy(STANDING) ≈ 1.2533 · sd(x) / √n                            # idle
SE_proxy(score)    ≈ √( (1.2533·sd(x)/√n)² + (1.858·sd(d)/√n)² )     # quick
```

`SE_proxy` is the estimate the implementation uses, not an exact finite-sample standard error. It is
used for [retention](#retention) only, i.e. to judge whether a node in use has fallen clearly behind;
the [band](#tolerance) uses the same shape with `JIT` in place of `sd(x)` to estimate `σ`, because it
answers a different question, see
[Why the Margin of `idle` Is the Error of a Median](#why-the-margin-of-idle-is-the-error-of-a-median).

The first term is the same in both: a score contains `MED`, and a median is what that term is the error
of. The noise of `STANDING` comes from `MED` alone, because the baseline is kept across windows and
does not move with this one — which is why it is steadier than a spread of the window: the spread
looks at two extremes, the baseline at a number that is already settled.

On the first round after a start the baseline is the fastest check of that round, so
`STANDING = MED − MIN`; `MIN` is an extreme and noisier than `MED`, so the margin understates the
real variation of that round and the switch is a little more eager. From the second round the
baseline is built from several rounds and that difference is small enough to ignore.

These estimates rely on the checks being approximately independent and on a distribution that is
roughly stable within the window; small samples or a clear trend can make them wrong, see
[Derivations](#derivations). With fewer than two successful checks the implementation uses the base
score as its noise estimate instead of these formulas.

#### The Noise of the Difference of Two Scores

If the two nodes are measured independently, their variances add:

```
SD(score_node − score_best) ≈ √(SE_proxy_node² + SE_proxy_best²)
```

If they share a probe target or the conditions of one line, the errors can be correlated, and the
variance of the difference also has a `−2·Cov(score_node, score_best)` term. The formula treats the
covariance as zero, which is a simplification. With independent errors the two are squared and
added before the square root; this is neither adding the two errors nor a claim that the noise
cancels. See [Derivations](#derivations).

#### Why the Noise Cannot Be Subtracted Away

Noise is variation around the estimate, not a fixed bias, so no single number can be subtracted from
a score. How it shrinks depends on the statistic as well: the part of a score that contains `MED`
shrinks with `1/√n`, while a baseline that does not move with the window is a constant. The
selection tolerates the uncertainty in a **band** rather than trying to subtract it from each score.

### Tolerance

A group does not switch for a difference it cannot trust. The band is the range around the best node
within which the nodes count as equally good. `tolerance` is the configured part of it, and the
defaults differ because the two scores are not the same quantity:

| Objective | Score       | Default |
| --------- | ----------- | ------- |
| `quick`   | `MED + JIT` | `50ms`  |
| `idle`  | `STANDING`  | `10ms`  |

The band also carries a **resolution margin**, so that measurement noise is not mistaken for a
difference between nodes. Both scores are absolute milliseconds, so the shape of the margin is the
same:

```
band = tolerance + 2√2 × noise_best / √n
cut  = score_best + band
```

The noise of `quick` is `JIT_best`; the noise of `idle` is `1.2533 × JIT_best`, the extra factor
because its score is a median. Both take the **typical deviation** (`JIT`) rather than the spread
`sd(x)`: the job of the margin is to tell the nodes apart, and a margin inflated by one slow probe
takes them all in, see the next section. The coefficient 2 is an engineering heuristic, not a 95%
confidence guarantee.

The margin is added only when at least two checks succeeded; with fewer, `band = tolerance`.

A candidate whose score is at most `cut` is still in the running. More samples shrink the `1/√n`
term of the margin, so the band approaches the configured value, but it is **never narrower than
`tolerance`**.

#### Why the Margin of `quick` Is `JIT_best`

Before comparing, the candidate is unknown, so the implementation assumes it is measured as well as
the best node; both sides use the jitter of the best node as the noise scale.

```
margin = 2 × √( (JIT_best/√n)² + (JIT_best/√n)² )
       = 2√2 × JIT_best/√n
```

The scale is `JIT_best`:

- **Not `DEV_best`**: `DEV` is the average deviation, so one spike enters it by its own share only,
  and using it would read "stalls once in a while" as steadier than it is. `JIT` is the median of
  the deviations and is not moved by a few outliers, which is what a noise scale needs.
- **Not the full noise estimate of the score**: it contains squared deviations, so one slow probe
  can inflate it, and a wider margin lets a clearly worse candidate in. This is a robust engineering
  trade-off, not a mathematical lower bound on the real noise or a 95% resolution guarantee. In
  measured windows the ratio of the score noise estimate to `JIT_best/√n` was about 2.8; the ratio
  depends on the distribution and the window, and is not a constant.

#### Why the Margin of `idle` Is the Error of a Median

`STANDING` is `MED` minus the baseline. The baseline is a settled number: this round moves it only
when it sets a new record, and it can only move it down, to this round's fastest check. The noise of
the score is therefore the noise of `MED`, the standard error of a median:

```
SE(MED) ≈ 1.2533 · σ / √n
```

What has to be estimated is `σ`, the variation of one check of the node. The margin takes `JIT`, the
robust scale of the median deviation, rather than `sd(x)`:

```
margin = 2√2 × 1.2533 × JIT_best / √n
```

The reason is that `sd(x)` is inflated by one slow probe, while **the job of the margin is to tell
apart**. The score of `idle` is itself insensitive to a spike (see
[Why `STANDING` Ignores a Single Spike](#why-standing-ignores-a-single-spike)), so if the margin is
sensitive to one the two part ways: the score says the node is fine while the margin tells the group
that nothing can be told apart, and `idle` has no ranking left. This is the same trade-off as the
margin of `quick`, which takes `JIT_best` rather than the full noise estimate of the score.

What is given up is that `sd(x)` describes better how much the median of the next window will jump —
for a node with many spikes it does jump. A ranking that moves with the median is better than a margin
wide enough that no ranking exists.

The margin is **absolute**: it does not grow with the score. The resolution of `idle` is therefore
the same for a node with 500ms of standing queue and for one with none. That is also what gives the
default `tolerance` of `10ms` its meaning: the score is in milliseconds, the margin is on the order
of tens of milliseconds, and the configured value is on the same scale as the band that is actually
used.

More samples shrink the margin, as they do for `quick`. The uncertainty of the baseline itself is
smaller than that of `MED`, because it is built from several windows.

### Retention

With `interrupt_exist_connections` enabled, when the node in use is replaced, its existing
connections are not necessarily interrupted. A node that just left the selection is kept while its
excess over the band is still within the noise of the two estimates:

```
noise     = 2 × √(SE_proxy_node² + SE_proxy_best²)
allowance = band + min(band, noise)
```

This compares one known node with the best one, so each of them uses its own noise estimate. The two
are combined as independent errors; a correlation from a shared target or line is still ignored.
`min(band, noise)` caps the extra allowance at one band, so a node survives at most **twice the
band** away from the best. Hysteresis is there to ride out measurement noise, not to keep a node
that is clearly worse.

`idle` takes its `SE_proxy` from the full spread `sd(x)` while its band is sized by `1.2533 × JIT`,
and the two can differ by an order of magnitude, so `min(band, noise)` takes the band and a node
survives **twice the band** away. That is what the cap is for — the two numbers answer different
questions, one is how narrow the band has to be to tell the nodes apart, the other is how wrong the
score of a known node may be.

#### Why Retention Uses a Noise Estimate Too

Retention and the selection band both deal with "is the observed difference trustworthy", but they
answer different questions: the band has to apply one robust margin to every unknown candidate,
while retention faces one known node and can use the noise of that node and of the best one. The
coefficient 2 is an engineering heuristic here as well, and does not imply a strict confidence.

#### How `quick` and `idle` Differ

- The noise of `quick` is decided by the spread of the delays and by the spread of the distances to
  the median, and it becomes accurate with more samples.
- The noise of `idle` comes from `MED` alone, one term fewer, and it shrinks sooner with more
  samples. Its score does not fall back to zero when the window turns over: once a node starts to
  queue, and while the baseline is still the level it had when it was idle, `STANDING` keeps it out
  of the band until the queueing goes away or the baseline is re-learned.

#### The Single Node of `urltest`

A `urltest` group uses one node at a time and has no second layer of retention. The hysteresis lives
in the selection: the node in use keeps its place while its score stays within `Limit(best)` of the
best one, which is a tolerance of **one** band.

Retention and selection are two separate decisions: one decides whether the existing connections
are worth breaking, the other which nodes new connections may use.

### Why Not the Standard Deviation

The sample standard deviation is the usual measure of spread, and it is the wrong one here, for
three reasons that compound:

1. **It is measured around the average, which the outlier moves.** One slow check of ten lifts the
   average from 449ms to 897ms, and the other nine checks are then scored against a centre they have
   nothing to do with: their deviation of 457ms becomes 751ms without any of them changing.
2. **It is squared, so one sample can own the result.** In a window where a single check took
   3.15s, 83% of the variance comes from that check, and the reported spread describes that one
   probe rather than the node.
3. **The square root does not restore the scale.** A spike of `X` in `n` samples contributes `X/n`
   to the mean deviation, which is the share of connections it delays, but `X/√n` to the standard
   deviation — 3.2× too much at `n = 10`, and worse as the window grows.

`DEV` is the same idea without the two defects: linear, and measured from a centre the spike cannot
move. A node that alternates between 200ms and 800ms still has a large `DEV`; it is not that `DEV`
ignores instability, it is that the estimate stays a statement about the node.

`idle` now ranks by `STANDING`, which is linear too: a standing queue is a delay difference, not a
square. The difference is that it does not measure "how much it swings within the window" but "how
much longer it waits than at its own best". Both can show the same spread in a window and still
mean different things: a node that alternates between 200ms and 800ms has a median of about 500
while its baseline stays at 200, so `STANDING` is about 300; a node steady at 800ms has a baseline
of 800 and a `STANDING` of zero — it was never faster, so the current slowness is not queueing.
**`idle` picks the first one.** A steady slowness is a known cost that `max_rtt` can cap; a
slowness that comes and goes cannot be anticipated at all.

This does not mean the standard deviation is unused: `quick` still estimates its score noise with
the sample standard deviation. The trade-off here is **not to rank nodes by it directly**.

### Why Not Trend

A window has an order, and none of the statistics above use it: shuffle the samples and `MED`,
`STANDING`, `JIT` and `DEV` are unchanged. So a node that has been getting worse for ten minutes
has the same reading as one that has been like that all along, as long as the level matches.

`STANDING` compensates for that in part, but not the way a trend does: it compares **now with the
best**, not the beginning with the end. A node that goes from 200ms to 600ms has a `MED` of 600 at
once and a baseline of 200, so `STANDING` is 400 at once, without waiting for a trend to accumulate.
A node that goes from 600ms to 200ms pulls its baseline down to 200, so `STANDING` returns to near
zero at once and is not held back by its bad past. **The direction therefore needs no extra
handling: the baseline only moves down, so an improvement is credited at once and a deterioration
is charged at once.**

A half-window comparison (`median(second half) − median(first half)`) does add a layer, and cheaply:
it can tell "just started to get worse" from "has been bad for a while". It is not part of the score
because a score that contains a trend would rank differently when `interval` changes, and a ranking
has to be a property of the node, not of the cadence it was sampled at. A trend belongs to the
decision, next to the tolerance, not to the measurement.

It also needs care: with a `sampling` of 10, two consecutive bad checks out of five in the second
half do not move its median, so the trend only appears from the third one onward. That makes it a
slower signal than the last check, which is already in the statistics.

The window length has a trade-off of the same kind: with a roughly stable distribution a longer
window gives a smoother estimate with less noise, but it also holds older samples, so the score
follows a node that changes more slowly. A shorter window reflects a change sooner, and is more
easily moved by chance. Lowering the noise and following a change are two ends of one knob.

### Why the Baseline Is a Ring of Slots

The baseline records "the best state of this node", and that puts it under two opposite demands: **it
has to remember long enough**, because a line is only idle in the trough of the day, and a memory
that is too short takes the congested level for the idle one; **it has to forget**, because a node
with the same tag may have changed line or machine, and the old baseline is then simply wrong.

A "when does it expire" timer satisfies neither well, because expiry is a single event: nothing
changes until the moment, and then everything is dropped at once. A **ring of slots** turns both
demands into something continuous:

```
48 slots × 30 minutes = 24 hours
every 30 minutes one slot is reset, the other 47 stay as they are
```

- **It remembers long enough**: the ring covers a whole day, every time of day has a record of its
  own, so there is always a full day of evidence behind the baseline.
- **It forgets smoothly**: there is no expiry event, only a slot overwritten by the next
  measurement of the same slot. The baseline never becomes void in one instant, and never makes the
  scores of a whole group fall to zero together.

The slots are cut by the wall clock rather than by uptime, so slot 40 is always 20:00–20:30. That
makes the memory more accurate: a slot records **how fast the node can be at this time of day**,
not the lowest value of some arbitrary 24-hour window.

The number of slots is the knob between a coarse memory and a reliable slot: fewer slots means a
longer slot, and a change within a slot is flattened into its single minimum; more slots means fewer
probes in each, and a record decided by one or two of them. 48 divides a day exactly and leaves
several rounds per slot. Like `sampling` and `interval` it is a **constant of the implementation,
not an option**: changing it has to be done together with the check interval, so it is not something
to hand to a configuration.

**The price.** The baseline is the smallest of the smallest of each slot, so it is still a minimum,
and it leans towards the most optimistic measurement. That matches its purpose (the idle level), but
it means a lucky probe lowers the baseline slightly, see the comparison of estimators in
[Derivations](#derivations).

#### Updating, Expiring and Starting

**Updating.** Each round writes its slot first, then takes its score, so the observation of the
round counts at once:

```
slot[k]  = min(slot[k], MIN)                           # entering a slot resets it to MIN first
BASELINE = min(of the slots within the last 24 hours)
STANDING = MED − BASELINE
```

Writing first cannot make a score of zero: a slot holds a minimum, so afterwards
`BASELINE ≤ MIN ≤ MED`. Taking the score first instead would mean a node that gets faster is only
credited on the next round, which buys nothing.

A round with fewer than two successful checks writes nothing: that single check may be luck, or a
probe that has not warmed up, and it should not set the floor of a whole day. `STANDING` of such a
round is half of that delay, see [The Statistics](#the-statistics).

**Expiring.** There is no expiry step of its own: a slot is overwritten by the next measurement of
the same slot 24 hours later, and drops out. The baseline therefore always comes from **a time of
day that was really observed**, and never mistakes the current congestion for the best the node can
do — which is what "reset to the current value when it expires" would do. If one time of day stays
congested from then on, its record rises over time while the other slots still hold, unless it stays
that way all day, and then the whole baseline rises: an admission that this is simply what the node
is like.

**Steps.** While the node is stable or drifts slowly, the baseline moves by at most one slot of
change per rotation, because only one slot is replaced every 30 minutes. Simulating a node that
degrades by 144ms per day: the one-shot expiry produces a single 144ms step, the ring about 11ms
each time, spread over 48 of them a day. **A step change — another line, another machine — still
leaves one step**: a minimum has to jump to the next smallest value at the moment the last old slot
is overwritten, and the size of that jump is the change itself. That is what forgetting is, not
jitter that could be smoothed away.

**Starting.** The first round writes its own fastest check, so `STANDING` starts out as `MED − MIN`
with no separate branch. From there the baseline grows a slot at a time and covers a full day after
a day. A restart follows the same path: the baseline is a record in memory and is grown again from
scratch, and while it is young each slot covers fewer times of day, so the baseline is more easily
pushed low — the price of forgetting, and not one that leaves the score meaningless.

### Why `STANDING` Ignores a Single Spike

`STANDING` subtracts the median, and a median is decided by rank alone: one spike of three seconds
among ten checks leaves the median at the typical value of the rest, and `STANDING` barely moves.
Sustained congestion pushes the median; a single stall does not.

That is at odds with "a stall of three seconds delays traffic", so here is the trade-off:

- **A spike is short**. It affects the few connections that land on it, while the rise from
  queueing affects **every** connection of the window. Ranking by spikes costs a whole ranking
  decided by luck.
- **A spike needs more samples to be trusted**. The single extreme is the least stable quantity of a
  window; ranking by it would make the score swing with the window, the margin would have to grow
  with it, and the resolution would be worse for it.
- **A higher quantile is available when more sensitivity is wanted**. Taking the current level as
  p90 instead of `MED`: in a measured window, a node with a median of 175ms containing one check of
  655ms has a `MED − MIN` of 20ms and a `p90 − MIN` of 117ms. A higher quantile is more sensitive
  and closer to the extreme, hence less stable. The implementation takes the median, because the
  question it answers is "how long does this connection usually wait".

### Derivations

This section explains the formulas, constants and assumptions behind the noise estimates. First the
notation:

- `n` is the number of successful checks; `xᵢ` is the delay of the `i`-th successful check; `MED`
  is the sample median of those delays.
- `sd(…)` is the sample standard deviation, an estimate of how far a set of values usually sits from
  its centre.
- `Var(…)` is the variance, the square of the size of a fluctuation; `SE` (standard error) is how
  much an estimate usually moves when it is sampled again, and is the square root of its variance.
- `SE_proxy` is the noise estimate the implementation uses. It is an approximation, not an exact
  standard error or a confidence interval.

The derivations assume the checks are independent and come from a distribution that is stable within
the sampling window. The analysis of `JIT` also assumes a symmetric distribution; real networks do
not always satisfy these conditions.

#### `idle`: The Noise of `STANDING`

`idle` ranks by `STANDING = MED − BASELINE`. What we want to estimate is how much this difference
usually moves when another batch of checks is taken.

**Step one: the baseline is a constant within this sample.** `BASELINE` is built from past windows,
and no choice of samples in this window changes it. Subtracting a constant does not change the
variance:

```
Var(STANDING) = Var(MED − BASELINE) = Var(MED)
```

This does not mean the baseline has no error, only that its error does not move from one sample to
the next. The error of the baseline is **systematic**: a lucky probe can set it too low, and every
later score of that node is too high. Such a bias is not averaged away by more samples, so it does
not belong in a standard error; it can only be kept small by how the baseline is estimated, see step
four.

**Step two: the variance of `MED`.** A median depends only on the rank of the samples, so its
variance is decided by the density around the median, `f(m)`:

```
Var(MED) ≈ 1 / (4n·f(m)²)
```

For a normal distribution `f(m) = 1/(σ√(2π))`, which gives `(π/2)·σ²/n`, and the square root is:

```
SE(MED) ≈ 1.2533 · σ / √n
```

`σ` is the variation of one check of the node. The implementation estimates it in two ways: `sd(x)`,
the sample spread, which is what the definition of a standard error is written in, and `JIT`, the
median deviation times `1.4826`, a robust estimate on the same scale that a single spike cannot move.
They are used in two places: retention takes the first, the margin the second, see
[Why the Margin of `idle` Is the Error of a Median](#why-the-margin-of-idle-is-the-error-of-a-median).

The origin of `1.2533 = √(π/2)` is in [`quick`: The Error of `MED`](#quick-the-error-of-med); both
objectives use the same quantity.

**Step three: putting the two together**, which gives the estimate retention uses:

```
SE_proxy(STANDING) ≈ SE(MED) ≈ 1.2533 · sd(x) / √n
```

The margin puts `JIT` in place of `sd(x)` and keeps the shape. Both are **absolute** errors, and do
not grow with the score. A node with 500ms of standing queue and one with none have the same error
scale, because the difference between them is only the constant that was subtracted.

**Step four: the baseline is a minimum, so it leans optimistic.** The baseline has to answer "how
fast can this node be", and the implementation takes the fastest check it has seen, with no
smoothing. Simulating a day of 1440 checks at a true idle level of 200ms with `sd = 8`:

| Estimator | Baseline | Queueing read when idle |
| --- | --- | --- |
| **the smallest check (the implementation)** | **175** | **25** |
| smallest of the medians of the windows | 191 | 9 |
| 10th percentile of the day | 190 | 10 |
| median of the day | 200 | 0 |

A minimum is systematically low by about three standard deviations: a day offers 1440 chances, one
of them is lucky. The price is a few to a few tens of milliseconds of false queueing on every node;
what it buys is a quantity that is not dull.

Recording the median of each window instead is much quieter (9ms of bias), but it answers a
different question: "how fast is this node **usually**". In a quiet period every node equals its own
median, every `STANDING` is zero, and `idle` has nothing to rank. Worse, it is fooled by a real
kind of node: one whose delay alternates between two bandwidth pools. With two lows and two highs
the median sits exactly between the peaks, a value it never reaches, and that midpoint is stable
from round to round, so it reads as the steadiest of all (`MED = 919`, `MIN = 556`: zero from the
median, 363 from the minimum). A minimum is not fooled by that shape.

The bias is **common**, so it does not change the order: two nodes are both a few milliseconds
optimistic, and their difference in `STANDING` is still the difference of their real queueing. It
moves the absolute zero point, which is what the configured `tolerance` is measured against.

#### `quick`: The Error of `MED`

The score of `quick` is `MED + JIT`. Take `MED` first: another batch of checks moves the median as
well. Its error depends on how crowded the neighbourhood of the median is: when the samples are
concentrated around it the median does not move much, and when they are sparse a small change in the
samples can move it further.

Let `f(m)` be the density of the distribution around the population median `m`, which can be read as
"how dense the delays are around the median". With enough samples, the variance of the median is
approximately:

```
Var(MED) ≈ 1 / (4n·f(m)²)
```

For a normal distribution, `f(m) = 1/(σ√(2π))`, and substituting gives:

```
Var(MED) ≈ (π/2)·σ²/n
SE(MED)  ≈ √(π/2)·σ/√n
          ≈ 1.2533·sd(x)/√n
```

The sample `sd(x)` estimates the population `σ`. `1.2533 = √(π/2)` is therefore a coefficient of the
**normal approximation**: under it the standard error of a median is about 25% larger than that of a
mean. With a clearly skewed distribution of delays, or with very few samples, the coefficient is not
necessarily appropriate.

#### `quick`: Why `JIT` is Multiplied by `1.4826`

`JIT` takes the median of the absolute deviations as the "typical variation" and multiplies it by
`1.4826` to put it on the scale of a standard deviation. The constant comes from the normal
distribution:

```
q = median(|X − m|)
P(|X − m| ≤ q) = 1/2
```

In a symmetric normal distribution, "within `q` of `m`" holds half of the probability, so the
probability from the mean up to `m+q` is a quarter, that is `Φ(q/σ) = 0.75`. Therefore:

```
q = Φ⁻¹(0.75)·σ ≈ 0.6745·σ
σ ≈ q / 0.6745 ≈ 1.4826·q
1.4826 = 1 / Φ⁻¹(0.75)
```

In other words, for normal data the median of the deviations is about `0.6745σ`, and after
multiplying by `1.4826` the result estimates the scale of the standard deviation. It is a conversion
constant for the normal distribution; it does not mean real delays follow one.

#### `quick`: The Error of `JIT`

Even with the true centre `m` known, `JIT` varies with which batch of samples was drawn. Let `q` be
the population median of `|X−m|` and `g(q)` the density of the absolute deviations around `q`. With
the centre known, a symmetric distribution and enough samples:

```
SE(median(|X − m|)) ≈ 1 / (2g(q)√n)
SE(JIT) ≈ 1.4826 / (2g(q)√n)
```

Intuitively, the denser the absolute deviations around `q`, the harder it is to move their median and
the smaller the error. If the centre has to be estimated from the same samples, or the distribution
of the delays is asymmetric, the calculation needs those effects as well.

The implementation uses `1.858·sd(d)/√n` for this part, where `1.858 = 1.4826 × 1.2533`. The
approximation applies the normal coefficient of a median to `d`; but even for normal `x` the absolute
deviations `d` are half-normal, so `1.858` is not a rigorously derived standard error coefficient. It
should be read as the estimate the implementation uses, not as a probability guarantee.

#### `quick`: Combining the Errors of `MED` and `JIT`

If the errors of two quantities are independent, the variance of their sum is the sum of their
variances; in general a covariance term has to be added:

```
Var(MED + JIT) =
    Var(MED) + Var(JIT) + 2·Cov(MED, JIT)
```

`MED` and `JIT` are computed from the same samples, so in principle they can be correlated. Under a
symmetric IID distribution they are asymptotically independent, and the implementation neglects the
covariance; with finite samples or another distribution that need not hold. The combined noise
estimate is:

```
SE_proxy(score) ≈ √( SE_proxy(MED)² + SE_proxy(JIT)² )
```

So the coefficients and the variance combination of `quick` rest on the distributional and sampling
assumptions above; this `SE_proxy` is an engineering estimate, not a 95% interval.

#### Comparing Two Nodes: How Uncertain the Difference Is

The question here is how much the difference between the score of a node and the score of the best
node moves when the checks run again. Treat the two scores as random quantities:

```
S_node   = the score of the node, as a random quantity
S_best   = the score of the best node, as a random quantity
D        = S_node − S_best
Var(D)   = Var(S_node) + Var(S_best)
           − 2·Cov(S_node, S_best)
```

When the measurement errors of the two nodes are independent, the covariance is zero. When they share
a probe target or are affected by the same line conditions, it may not be, and the implementation
ignores it. Under independence, substituting the two noise estimates and taking the square root
gives:

```
Var(D) ≈ SE_proxy_node² + SE_proxy_best²
SD(D)  ≈ √(SE_proxy_node² + SE_proxy_best²)
```

`SD(D)` is how much the difference moves on a re-measurement; the `SE_proxy` in the formula is the
estimate the implementation uses, not an exact value.

#### How `SD` and `SE` Relate

`SD` (standard deviation) describes the fluctuation of a random quantity; `SE` (standard error)
describes how much an estimate moves when it is sampled again. A score is an estimate computed from
samples, so its standard deviation is its standard error. Here `SE_proxy` is the implementation's
approximation of that error.

`score_node` and `score_best` are already observed values within the current window; calling them
random quantities is a way of asking how much they could move if the window were measured again.
