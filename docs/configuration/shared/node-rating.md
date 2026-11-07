# Node Rating

This page explains how groups rank nodes using [health check](/configuration/shared/health-check/) results, and why the rules are designed this way.

### Goals: Absolute Speed and Relative Quality

First decide what the group should optimize. It chooses one goal instead of combining two different goals into a single score:

- `quick`: **How fast is the node usually?** A 170ms node is usually slower than a 40ms node. This is an absolute measurement in milliseconds.
- `idle`: **How much worse is the node than it can be at its best?** A node that usually takes 200ms but stays at 600ms every evening may not seem slow, yet it is running at four times its best and slows down all traffic at that time.

These goals answer different questions and use **different units**. `quick` compares actual delay; `idle` compares how much the node has degraded from its own best. Using actual delay to measure degradation would naturally penalize more distant nodes, comparing distance rather than condition. So `idle` uses ratios throughout, which do not depend on distance. See [Design Choice: Why Both Parts Are Ratios](#design-choice-why-both-parts-are-ratios).

### Statistics: Successful Checks Only

Delay statistics use only **successful** checks. A failed check usually waits until the timeout, so it does not show how fast the node is when it works. Failures and the total number of checks are recorded separately. They are used to decide whether a node qualifies and to calculate the failure penalty for `quick`.

Let `x₁ … xₙ` be the delays of successful checks in the current window, `n` their count, and `dᵢ = |xᵢ − MED|` the absolute difference between each check and the median:

```
MED     = median(x₁ … xₙ)      # median delay in this window
JIT     = 1.4826 × median(dᵢ)  # typical deviation
DEV     = (1/n) × Σdᵢ          # average deviation
```

`MED`, `JIT`, and `DEV` are calculated from the current window and updated when the window moves on. Another reference carries over between rounds:

```
SLOT     = 30 minutes per slot, 48 slots covering 24 hours
SLOT[k]  = fastest delay seen in slot k
FLOOR    = min(values from slots in the last 24 hours) # the node's best observed delay
FAILR    = long-run failure rate                      # continuous in time, outside the window
PRES     = max(0, MED − FLOOR) / FLOOR                # how much longer the typical delay is than its own best, as a ratio
BAD      = #{xᵢ > 2 × FLOOR} / ALL + max(FAIL / ALL, FAILR) # share of attempts that missed the node's limit
```

These formulas apply when there are at least two successful checks. With only one, there is not enough data to estimate variation, so `JIT` and `DEV` are both taken as half of that delay: too few samples should not be mistaken for no variation. Both ratios are undefined there as well — the floor is that one check itself — so `idle` ranks by the delay, see [Scores: Delay and Queueing](#scores-delay-and-queueing). With no successful checks, there are no delay statistics.

- `SLOT` follows wall-clock time: `k` is the current 30-minute period of the day, so each slot always represents the same time of day. When a new slot starts, it is updated with the fastest delay in that round; later, only the minimum for that time period is kept. After 24 hours, the next reading for the same slot replaces the old one, so it expires naturally.
- **`MED`** is the delay level. A median rather than an average reduces the effect of a single slow check.
- **`JIT`** describes the **usual amount of variation**. A few spikes usually do not affect it much. Under a normal-distribution assumption, `1.4826` converts the median deviation to the scale of a standard deviation.
- **`DEV`** describes the **overall cost of variation**. Every check counts: an occasional spike contributes a little, and repeated spikes add more. It is still shown in reports to help with diagnosis, but is no longer used for ranking.
- **`FLOOR`** is the **fastest observed delay**, without smoothing. One lucky probe can lower it, making the node appear to have a little queueing from then on; in return, it reflects changes promptly.
- **`PRES`** is queueing pressure: the **ratio** by which the current delay exceeds **the node's own best**. Dividing by the node's floor cancels out propagation delay, so this ratio does not depend on distance.
- **`BAD`** is the **frequency** of queueing: the share of checks that took more than twice the node's best delay, plus the share of failed checks. It is compared with the floor, so distance does not affect it; one extreme delay also counts only once.

### Scores: Delay and Queueing

The two goals use different formulas because failures count differently for each.

The `quick` score represents the delay a node usually adds. Successful and failed checks are weighted by their share of all checks. Let `ALL` be the total number of checks, `FAIL` the number that failed, and `TIMEOUT` the health check timeout:

```
base    = MED + JIT
cost    = min(TIMEOUT, base)
failure = min(TIMEOUT, 3 × cost)
p       = max(FAIL / ALL, FAILR)
score   = (1 − p) × cost + p × failure
```

Nodes with a lower typical delay and less usual variation get a lower score. Variation is added to the delay cost in milliseconds. `p` is the share of failures: the window's own share, or the long-run rate `FAILR`, whichever is larger, see [Long-Run Failure Rate](#long-run-failure-rate-the-risk-a-clean-window-hides).

`cost` is the base score for a successful check; `failure` is the penalty for a failed one. A failure counts as at most three times the successful-check cost and is capped at `TIMEOUT`. This is a scoring rule, not an exact measurement of the time a real retry takes. If every failure counted as a full timeout, one failure could outweigh every node with no failures.

The `idle` score adds queueing depth to queueing frequency. Failures count only toward frequency:

```
score = 1000 × (PRES + 0.5 × BAD)
```

`0.5` is the weight for `BAD`; `1000` converts the dimensionless result to a millisecond scale. Neither value was fitted to data. See [Design Choice: Why the Threshold Is Two and the Weight Is 0.5](#design-choice-why-the-threshold-is-two-and-the-weight-is-05).

This score does not compare the delay itself. Nodes that usually take 400ms or 100ms may both have nine out of ten checks close to their own floor. A node that usually takes 100ms but exceeds twice its floor every third check does not. When scores tie, the node with the smaller `spread` comes first.

**Why multiply by 1000?** `PRES` and `BAD` are ratios. Multiplying them by 1000 lets us show the score in milliseconds. Both goals then use the same unit and can share the `tolerance` setting and default:

| Goal | Score | Default `tolerance` | Meaning |
| --- | --- | --- | --- |
| `quick` | `MED + JIT` (absolute milliseconds) | `50ms` | A 50ms difference is not worth switching for. |
| `idle` | `1000 × (PRES + 0.5 × BAD)` | `50ms` | A 5% relative degradation on a 1000ms scale is not worth switching for. |

In both goals, `50ms` means a difference this small is not worth switching for. Logs and reports also show both scores in milliseconds, so users do not have to remember two units.

How do failures affect the score? They count only toward **frequency**: each failed check counts once in `BAD`. They cannot count toward **depth**: `PRES` is a ratio, and a failure has no delay to compare. If we used `TIMEOUT` as the delay for a failure, one failure in 30 checks would add 0.8 to `PRES` (about 800ms), outweighing normal queueing differences. This is the same problem as the former approach of pricing failures as `3 × DEV`: **queueing delay and the cost of a failure cannot be converted directly into one another**.

There is a trade-off in this conversion: every 10% increase in `BAD` adds 50ms to the score. So **a 10% failure rate is roughly equal to having 5% more queueing all the time**. This is lighter than `quick`'s three-times penalty for a failure: `quick` scores a delay, so a failure has to be priced at the timeout to matter at all, while `idle` scores a share, where a failure and a slow check are the same kind of thing and counting it once is enough.

`max_fail` is therefore no longer the only thing standing between a failing node and the selection, but it is still **needed as a backstop**. It says "not one failure in this window" and acts as an entrance; `BAD` and `FAILR` act on the score. The two answer different questions: `max_fail` decides whether a node may be used at all right now, and the score decides which of the nodes that may be used is used. Its default stays `0`: without configuring it, nodes with failures belong only to the `alive` category.

Both ratios of `idle` need at least two successful checks. With only one, `FLOOR` is that check itself, so `PRES` and `BAD` are both zero and every node ties at the smallest score there is, leaving the group to pick among them at random. In that round `idle` ranks by the delay instead, on half of it, which is the order `quick` reads the same window in: a node whose one check came out low must not look like the steadiest node there is. The score returns to the ratios above as soon as a second check succeeds.

If every check fails, the successful delay is unknown, so both goals score `TIMEOUT`. If no check has run yet, there is no score.

A score comes from a finite number of checks; it is not the permanent "true value" of a node or line. Comparisons should account for measurement error.

### Long-Run Failure Rate: the Risk a Clean Window Hides

The default of `max_fail` is `0`, so every node that can be picked has no failure in its window. `FAIL / ALL` in both score formulas is therefore always zero, and the failure term only has a value after a node **has already been rejected** — the risk signal stops at the entrance.

But a clean window is not the same as safety. Measured on a real history, taking only the samples whose window was clean and looking at whether the next 8 checks failed:

| Failure pattern | Historical failure rate | Next 8 checks still fail, given a clean window |
| --- | --- | --- |
| Flapping: failures spread through the history | 13.3% | 89.5% |
| Outage: one continuous drop, clean otherwise | 10.0% | 9.9% |

The two nodes have almost the same failure rate and are indistinguishable in the current formula while their windows are clean, yet their residual risk differs by nine times. The difference is the **structure** of the failures: a flapping node can drop again at any moment, an outage node is reliable outside its outage. A window of ten checks cannot see that; the history already says it. Across all nodes, the correlation between "historical failure rate" and "probability of failing soon while the window is clean" is `+0.59`.

So the group keeps one more statistic across rounds:

```
Δt     = wall-clock time since the last update
α      = 1 − exp(−Δt / τ)                  # τ ≈ 45 minutes
FAILR  ← FAILR + α × ((failed this check ? 1 : 0) − FAILR)
```

It is a **continuous-time exponentially weighted estimate**: it describes how likely the node has been to fail in the last τ, and older events fade out exponentially. It starts from a prior worth ten clean checks, so a cold start is "nobody has seen it fail" rather than "it was seen failing": without it, the first check would be all the evidence there is and a single failure at start-up would read as a perfect failure rate for an hour. The prior decays like the rest, so a long enough history is unaffected by it. It enters the score as the larger of itself and the window's own share, see `p` and `BAD` above. The checks the connections asked for count as well: such a check is the second half of what a failed connection said.

**Why decay by wall-clock time rather than by a count of checks:**

- It needs no retention rule of its own. `sampling` and `extra` are counts, and the same count covers five times as much time at `5m` as at `1m`; counting checks would put that difference into the score.
- It does not depend on `interval`: two one-minute gaps move it as much as one two-minute gap, so changing the check interval needs no relearning.
- It does not depend on the runtime resize of `sampling` (see [Debug](#debug)): re-sampling the window does not clear this estimate.

**Why not a whole day.** A whole-day failure rate averages the evening peak away with the quiet hours, and the difference between queueing and dropping is exactly a matter of when. An estimate that decays with τ follows the current period: at the evening peak it describes the evening peak, without a daily average overwriting the difference between periods.

**How long τ should be.** This cannot be settled from the data: stretching the measured span from 20 checks to 100 leaves the separation of "fails soon or not" almost unchanged (AUC between 0.54 and 0.58). The value has to be set by two opposing requirements: short enough to forget an outage that has ended, long enough not to be swayed by a single stray failure. 45 minutes is one point between them — a node that failed continuously for 54 minutes and then recovered should not still be paying for it an hour later.

**How much it matters.** The separation above is only about `0.57`, far less reliable as a ranking basis than the delay itself. Its job is to break a near tie, to tell "fast but flapping" from "a little slower but steady"; it does not change the fact that the fastest node is still the fastest. At a cold start `FAILR` is zero and the score is exactly what it was without it, and it only takes effect once there is evidence.

### Noise: Uncertainty in Scores

A score is an estimate based on a limited number of checks. Another set of checks may change both the score and the gap between two nodes. Here, `n` is the number of successful checks; failure rates use `ALL`.

#### Estimating Noise for One Node

The standard error describes how much a score would usually change if we ran another set of checks. For a sample mean, it can be estimated by dividing the sample standard deviation by `√n`. Medians and measures based on an estimated centre also depend on the data distribution. The implementation uses these approximate noise estimates:

```
SE_proxy(score) ≈ √( (1.2533·sd(x)/√n)² + (1.858·sd(d)/√n)² )                  # quick
SE_proxy(score) ≈ 1000 · √( (1.2533·JIT/(FLOOR·√n))² + 0.5²·BAD(1−BAD)/ALL )  # idle
```

`SE_proxy` is the approximate noise value used by the implementation, not an exact standard error for a finite sample. It is used only for [hysteresis](#keeping-existing-connections-hysteresis), to decide whether the current node has clearly fallen behind. The [tolerance band](#selecting-nodes-the-tolerance-band) answers a different question: can two nodes be told apart? So the two use different methods: the `quick` band uses `JIT`, while the `idle` band uses only the error in `PRES` and leaves out the error in `BAD`. See [Why the `idle` Margin Is Calculated This Way](#why-the-idle-margin-is-calculated-this-way).

The two `quick` terms are the errors in `MED` and `JIT`. The two `idle` terms come from different sources:

- **The first term is the error in `PRES`.** `FLOOR` carries over between windows and does not change with the current one, so the error in `PRES` is the error in `MED` divided by `FLOOR`. `FLOOR` is the fastest delay seen in the past; its one-way bias is not averaged out by more samples, so it is not part of the standard error. See [Derivations](#derivations).
- **The second term is the error in `BAD`.** `BAD` is the share of all attempts that did not meet the limit. Its standard error is estimated with the binomial formula, `√(BAD(1−BAD)/ALL)`. The denominator is **all attempts**, not just successful checks, because failures are included in this measure. This term and the first come from the same checks and may be related; treating them as independent is a simplification.

In the first round after startup, `FLOOR` is the fastest delay in that round, or `MIN`. Since `MIN` is an extreme value, it is noisier than `MED`; the margin will underestimate variation in that first round, making a switch slightly more likely. From the second round on, `FLOOR` combines several rounds, and this effect is small enough to ignore. It scales `PRES` but barely changes the ranking. See [Design Choice: Does an Immature Floor Change the Ranking?](#design-choice-does-an-immature-floor-change-the-ranking).

These estimates assume checks are roughly independent and the delay distribution stays fairly stable during the window. They may be especially inaccurate with few samples or a clear trend; see [Derivations](#derivations). With fewer than two successful checks, the implementation uses the base score as the noise estimate instead of these formulas.

#### Noise in the Difference Between Two Node Scores

If the measurement errors of two nodes are independent, the variance of their difference is the sum of their variances:

```
SD(score_node − score_best) ≈ √(SE_proxy_node² + SE_proxy_best²)
```

If the nodes share a probe target or line conditions, their errors may be related. The variance of the difference would then include `−2·Cov(score_node, score_best)`. The current formula treats the covariance as zero, which is a simplification. With independent errors, square them, add them, then take the square root. This is not the same as adding the errors directly, and it does not mean that noise disappears. See [Derivations](#derivations).

#### Why Noise Cannot Be "Subtracted" from a Score

Noise is variation in an estimate, not a fixed bias, so there is no general number to subtract from a score. Different measures also change in different ways: errors from `MED` and `BAD` shrink with `1/√n`, while `FLOOR` stays fixed within the current window. Selection uses a **tolerance band** to allow for uncertainty instead of subtracting noise from each score.

### Selecting Nodes: The Tolerance Band

When selecting nodes, candidates with scores close to the best are treated as "about as good." This avoids switching over small or uncertain differences. `tolerance` sets the size of this range. The two goals measure different things but use the same default:

| Goal | Score | Default |
| --- | --- | --- |
| `quick` | `MED + JIT` | `50ms` |
| `idle` | `1000 × (PRES + 0.5 × BAD)` | `50ms` |

Both scores are in milliseconds. For `quick`, a 50ms difference is an absolute delay difference; for `idle`, it is a 5% relative degradation on a 1000ms scale.

The selection range also includes a **resolution margin** so measurement noise is not mistaken for a difference between nodes. Both scores use milliseconds, so the margin has the same form:

```
band = tolerance + 2√2 × noise_best / √n
cut  = score_best + band
```

For `quick`, the noise is `JIT_best`. For `idle`, it is `1000 × 1.2533·JIT/FLOOR`, using only the `PRES` term; `BAD` affects the score but not the band. Both divide by the square root of the number of successful checks and use **typical deviation** (`JIT`) to estimate `σ`, not the overall spread `sd(x)`. The margin is meant to tell nodes apart; one slow probe should not make it so wide that every node falls inside. The factor 2 is an engineering rule of thumb, not a 95% confidence guarantee. See the next section.

The implementation adds a noise margin only when `n ≥ 2`. Otherwise, `band = tolerance`.

A candidate stays in range if its score is no higher than `cut`. More checks shrink the `1/√n` part of the margin, so the band gets closer to the configured value but **never becomes narrower than `tolerance`**.

#### Why the `quick` Margin Uses `JIT_best`

Before comparing nodes, the candidate and the accuracy of its measurements are unknown. The implementation assumes the candidate is measured as well as the best node, so both use the best node's `JIT` as the noise scale:

```
margin = 2 × √( (JIT_best/√n)² + (JIT_best/√n)² )
       = 2√2 × JIT_best/√n
```

The noise scale is `JIT_best`:

- **Not `DEV_best`**: `DEV` is the average deviation, so a single spike contributes only its share. Using it to estimate the margin would make a node that stalls once in a while look steadier than it is. `JIT` is the median of the deviations and is not much affected by a few outliers, which makes it a better noise scale.
- **Not the full score-noise estimate**: it includes squared deviations, so one slow probe can make it much larger. The wider margin could then include clearly worse candidates. This is a robust engineering choice, not a mathematical lower bound on noise or a 95% resolution guarantee.

#### Why the `idle` Margin Is Calculated This Way

The `idle` score has two parts, but only the depth part determines the width of the band.

**The error in `PRES`.** `PRES` is `MED` divided by `FLOOR`, minus one. `FLOOR` carries over between windows and is fixed for the current one. Dividing by a constant does not change the relative error, so:

```
SE(PRES) ≈ SE(MED) / FLOOR ≈ 1.2533 · σ / (FLOOR · √n)
```

We need to estimate `σ`, the variation in one check of the node. The margin uses `JIT`, the robust scale based on the median deviation, rather than `sd(x)`: **the margin's job is to tell nodes apart**, and one slow probe can inflate `sd(x)` enough to make all nodes look "about as good."

**`BAD` does not widen the band.** It still counts in the score, but the band is based only on the error in `PRES` plus `tolerance`. There are two reasons.

First, consider what the band is for. It asks whether a node might be as good as the best, so that a good node is not dropped because of measurement noise. The error in `PRES` comes from random sampling: one slow check does not necessarily mean the node is slow, so allowing for that makes sense. `BAD` answers a different question: **will the node fail?** The cost of the two mistakes is not equal. Allowing a node that may stall can mean real failed requests; excluding a good node only removes one option, while there are thirty others. For uncertain failure rates, we should ask for evidence rather than lower the bar. **Uncertainty should not make a node easier to select.**

Second, including the error in `BAD` makes the band behave oddly with small samples. `BAD` is a proportion with binomial standard error `√(BAD(1−BAD)/ALL)`, which is exactly zero when `BAD` is zero. If included, the width of the band would depend on **whether the best node happened to have a bad check in this window**, not on how accurately it was measured.

The error in `BAD` is not ignored completely. It is still used for [hysteresis](#keeping-existing-connections-hysteresis), where the question is whether to keep one known node through a temporary change. That is the opposite of choosing among candidates, so the two decisions use different estimates.

The margin is **absolute**: it does not change with the score. A node with 10% standing queueing and one with almost none use the same margin. This also makes the shared default `tolerance` of `50ms` useful: both scores are usually in the range of tens to hundreds of milliseconds, so the configured value is on the same scale as the actual band.

More checks make the margin smaller, as they do for `quick`. The `FLOOR` itself is less uncertain than `MED` because it combines several windows.

### Keeping Existing Connections: Hysteresis

When the setting to interrupt existing connections is enabled and a node is replaced, its existing connections can stay open for a while. A node that has just left the selection range is not dropped right away if the part of its score beyond the band is still within the noise allowance:

```
noise     = 2 × √(SE_proxy_node² + SE_proxy_best²)
allowance = band + min(band, noise)
```

This compares the node in use with the best node, so each uses its own noise estimate. It assumes their errors are independent; it does not account for a shared probe target or line. `min(band, noise)` caps the extra allowance at one band, so a node can stay up to **two bands** from the best. Hysteresis handles measurement noise; it is not meant to keep a clearly worse node for a long time.

For `idle`, `SE_proxy` uses the full spread `sd(x)` to estimate the error in `PRES`, while the band uses `JIT`. They can differ by an order of magnitude, so `min(band, noise)` reaches the band cap and the node can stay within **two bands**. That is why the extra allowance is capped: the band says how close nodes must be to count as equal, while the noise estimate says how far off the score of a known node might be.

#### Why Connection Retention Also Uses a Noise Estimate

Connection retention and the selection band both ask whether an observed difference is trustworthy, but answer different questions. The band needs one robust margin for every unknown candidate. Retention deals with one known node and can use the noise estimates for that node and the best one. The factor 2 is an engineering rule of thumb here too, not a strict confidence level.

#### How `quick` and `idle` Behave

- The noise estimate for `quick` depends on the spread of delays and the spread of their distances from the median. More checks make it more reliable.
- The noise estimate for `idle` combines the error in `PRES` (from `MED`) and the error in `BAD` (a binomial proportion). The band uses only the first, but retention needs both to estimate how far a known node's score might move. The second term goes to zero when `BAD` is zero, so a node with no failures gets its noise estimate from `PRES` alone. Its score does not return to zero just because the window moves on: once queueing starts, `PRES` and `BAD` keep it outside the selection range while `FLOOR` still reflects its idle state. It returns when queueing eases or the floor is relearned after expiry.

#### Single-Node Selection in `urltest`

A `urltest` group uses one node at a time and does not have the load-balancing group's second layer of connection retention. Hysteresis is part of selection itself: the node in use stays selected while its score is no higher than `Limit(best)`, allowing a difference of at most **one band**.

Keeping existing connections and selecting a node are separate decisions: one decides whether to interrupt existing connections; the other decides which nodes new connections can use.

### Design Choice: Why Not Rank by Standard Deviation

The sample standard deviation is a common measure of variation, but it does not work well as the node's variation score for three reasons:

1. **It is measured from the average, which an outlier can move.** Nine checks at 450ms and one at 3.15s pull the average from 450ms to 720ms. The other nine checks are then compared with a centre that does not represent them: their deviation goes from 0 to 270ms even though none of them changed.
2. **It squares deviations, so one check can dominate.** Squaring makes the weight of a spike grow with its size squared: in the same example, that one check accounts for nine tenths of the variance. The result describes that probe, not the node's usual state.
3. **Taking the square root does not fix the scale.** A spike of size `X` among `n` checks adds `X/n` to the average deviation, matching the share of connections it slows down. It adds `X/√n` to the standard deviation: 3.2 times as much when `n = 10`, and more as the window grows.

`DEV` still measures deviation from the median, but uses a linear average so a spike cannot move the centre. A node that alternates between 200ms and 800ms still has a large `DEV`; it does not ignore instability. It simply avoids letting one check decide the score. In the current design, `DEV` is shown in reports but is not used for ranking.

The two `idle` ratios are also linear or count-based, not squared:

- `PRES` is a ratio, not a square.
- `BAD` is the share of checks that crossed the limit; an extreme check counts once.

They measure how much worse the node is than at its best, and how often that happens—not how much its delay swings during the window. Those can describe different problems. A node that alternates between 200ms and 800ms has a median near 500ms, a floor of 200ms, `PRES` near 1.5, and `BAD` near 50%. A node steady at 800ms has a floor of 800ms, so both ratios are zero: it has never been faster, so its current slowness is not queueing. **`idle` picks the latter.** A steady delay is a known cost that `max_rtt` can handle; a delay that comes and goes cannot be planned for.

This does not mean standard deviation is never used: `quick` still uses the sample standard deviation to estimate score noise. The choice here is **not to use it directly to rank nodes**.

### Design Choice: Why Trends Are Not Part of the Score

Checks in a window have an order, but the measures above do not use it: shuffle the checks and `MED`, `PRES`, `BAD`, `JIT`, and `DEV` stay the same. So a node that has been getting worse for ten minutes can have the same readings as one that has been in that state all along, as long as their delay distributions match.

`PRES` and `BAD` partly address this, but not by looking for a trend. They compare **now with the best seen before**. If a node goes from 200ms to 600ms, `MED` immediately becomes 600, `FLOOR` stays at 200, and `PRES` becomes 2.0; there is no need to wait for a trend to build up. If it goes from 600ms to 200ms, `FLOOR` falls to 200 and `PRES` quickly returns close to zero, without holding the past against it. **There is no need to handle direction separately: `FLOOR` only moves down, so improvements count at once and deteriorations show up at once.**

Comparing the medians of the first and second halves of a window (`median(second half) − median(first half)`) would add useful information at little cost. It could tell "just started getting worse" from "has been bad for a while." But putting a trend into the score would change the ranking when `interval` changes. The ranking should reflect the node itself, not how often it is checked. Trends belong in the decision, like tolerance, not in the measurement.

It also needs care: with `sampling = 10`, two abnormal checks in a row among the five in the second half do not move its median. The trend appears only with a third. This signal is slower than the latest check, which is already included in the statistics.

Window length has a similar trade-off. When delays are fairly stable, a longer window usually gives a smoother estimate with less noise, but it also keeps older checks for longer and reacts more slowly to changes. A shorter window reacts sooner but is more affected by chance. Reducing noise and following changes are two ends of the same choice of window.

### Design Choice: Why the Baseline Is a Ring of Slots

The baseline records "the best state of this node," so it has two competing needs: **remember long enough**, because a line may be idle only during a quiet time of day; and **forget old readings**, because a node with the same name may have moved to another line or machine, making its old baseline wrong.

A timer that expires everything at once does not handle both needs well: nothing changes until expiry, then all the old readings disappear together. A **ring of slots** lets the baseline remember and forget a little at a time:

```
48 slots × 30 minutes = 24 hours
every 30 minutes one slot is reset; the other 47 stay as they are
```

- **It remembers long enough**: the ring covers 24 hours, with a record for each time of day, so the baseline always has a full day's observations to refer to.
- **It forgets gradually**: there is no single expiry event; only one slot is replaced by the next reading for that time. The baseline does not suddenly become invalid, and one reset does not make the scores for the whole group drop to zero.

Slots follow wall-clock time, not uptime, so slot 40 always represents 20:00–20:30. This makes the record more useful: each slot remembers **how fast the node can be at that time of day**, not its lowest delay in any arbitrary 24-hour period.

The number of slots sets the time span each record covers and how many checks it gets. With fewer slots, each covers a longer period, and changes within it are reduced to one minimum. With more slots, each gets fewer checks, so one or two probes may determine its value. 48 divides the day evenly and leaves several rounds of checks in each slot. Like `sampling` and `interval`, it is an **implementation constant, not a setting**. Changing it also requires considering the check frequency, so it should not be configurable on its own.

**The trade-off.** The baseline is the smallest value in each slot, then the smallest of those values. It therefore favors the most optimistic reading. That suits its purpose—recording the idle level—but a lucky probe can lower it slightly and make every node's `PRES` a little larger. See the comparison of estimates in [Derivations](#derivations). This bias is **shared** and does not change the ranking; it only changes how strict `tolerance` feels.

#### Updating, Expiring, and Starting

**Updating.** Each round updates its slot before calculating the score, so the new reading takes effect right away:

```
slot[k]  = min(slot[k], MIN)                       # entering a new slot resets it to MIN first
FLOOR    = min(values from slots in the last 24 hours)
PRES     = max(0, MED − FLOOR) / FLOOR
BAD      = (#{xᵢ > 2 × FLOOR} + FAIL) / ALL
```

Updating first does not make the score zero: each slot holds a minimum, so afterwards `FLOOR ≤ MIN ≤ MED`. `PRES` remains a non-negative ratio and `BAD` remains a share. Calculating the score before the update would only delay the effect of a node getting faster until the next round.

If fewer than two checks succeed, the slot is not updated. That single result may be luck or an unsettled probe, and should not set the floor for the whole day. In that round `idle` ranks by that one delay instead; see [Statistics: Successful Checks Only](#statistics-successful-checks-only).

**Expiring.** There is no separate expiry step: after 24 hours, the next reading for the same slot replaces the old one. The baseline therefore always comes from **a time of day that was actually observed**. This avoids treating current congestion as the fastest level, as a rule like "reset to the current value at expiry" would. If one time of day stays congested, its record gradually rises while the other slots stay as they are. The whole baseline rises only if the node remains congested for a long time—in effect, accepting that this is now its usual state.

**Sudden changes.** If the node stays stable or changes slowly, the baseline moves by at most the change in one slot at a time, since only one slot is replaced every 30 minutes. For a node that gets 144ms slower each day, expiring everything at once would cause a 144ms jump. With slots, the change is spread across 48 updates during the day, about 11ms each. **A sudden change, such as a new line or machine, can still cause a jump**: when the last old reading is overwritten, the minimum has to move to the next-lowest value. The jump is the size of the change itself. That is the cost of forgetting old data, not noise that smoothing can remove.

**Starting up.** The first round writes its fastest check, so `FLOOR` starts at `MIN` without a separate fallback. The ring then fills one slot at a time and takes 24 hours to cover the full day. When the cache file is on, a restart restores the day of slots and the long-run failure rate, see [Design Choice: Why Only the Floor and the Failure Rate Are Cached](#design-choice-why-only-the-floor-and-the-failure-rate-are-cached); when it is off, or its record is unusable (not enabled, a different target or path, expired), a restart builds up again as it always did. While it is rebuilding, each slot covers fewer time periods and the floor may be too high. This is the cost of forgetting old data; the ranking still works. See [Design Choice: Does an Immature Floor Change the Ranking?](#design-choice-does-an-immature-floor-change-the-ranking).

### Design Choice: Why Only the Floor and the Failure Rate Are Cached

Two pieces of state outlive the window: the baseline and the long-run failure rate. The window holds `sampling` checks (`sampling × interval`), while those two look back 24 hours and `τ = 45` minutes. The cost of a restart is therefore very uneven — the window rebuilds quickly, `FLOOR` has to wait for the next daily trough, and `FAILR` has no evidence again until the node **fails once more**.


Each record carries the moment it was written, and its validity is how long the floor lives — 24 hours. The cache file drops the records that have passed when it opens, so a node that is no longer checked does not stay in the file for good.

### Design Choice: Why the Threshold Is Two and the Weight Is 0.5

`BAD` counts checks where the delay exceeded twice the node's best. The weight `0.5` means a node that is always twice as slow adds 500 to the scale. Neither value was fitted to data; each has a simple reason.

**Why two?** Twice the delay means it has doubled. This is a common point for treating a line as starting to queue. Either side of it fails in its own way:

- **Too low (say 1.5 times)** and normal variation and probe noise cross it as well, so every node carries some `BAD` and the nodes can no longer be told apart — the measure is there to separate "queues now and then" from "queues all the time", and a low threshold stops separating them.
- **Too high (say 3 times)** and only a sustained long queue reaches it, nearly every node scores zero, and the measure stops existing: the score falls back to depth alone.

Twice sits between the two: normal movement does not reach it, and real queueing always does.

**Why 0.5?** It puts frequency on the same scale as depth: `PRES` is on the order of 0 to 1, and a node that is twice as slow on every check should be rated about as badly as a node that queues as badly as it can, i.e. add close to 1. A weight of 0.5 makes "twice as slow every time" slightly worse than "always 50% more queueing". That is a judgement about scale, not a fitted value.

It is not a value to pick at random, but it is not one that needs fine tuning either: it sets the **exchange rate**, how many bad checks are worth one percent of queueing. Move the rate and the nodes at the edge of the band change — a wider rate takes in one or two merely worse nodes, a narrower one keeps only the cleanest. That makes it a policy question: how much should "stalls now and then" count as "always a little slower". The order of nodes that clearly differ does not depend on it; only the ones near the band do.

The two parts **must stay separate**, because they catch different defects:

| Node | `PRES` | `BAD` | Rank by depth only | Rank by frequency only |
| --- | --- | --- | --- | --- |
| Fast but often fails | very low | very high | First | Middle |
| Slow but steady | medium | very low | Middle | First |
| Slow and often fails | very high | very high | Last in both | Last in both |

Using only one would miss the other kind of problem.

### Design Choice: Why Both Parts Are Ratios

`idle` asks how much worse a node is than it can be at its best. That can be written as a difference (`MED − FLOOR`) or a ratio (`(MED − FLOOR) / FLOOR`). A ratio is not just a preference; it is the way to remove distance from the comparison.

**A difference brings distance back in.** A 400ms node with a floor of 380 takes 20ms longer than its best. A 900ms node with a floor of 860 takes 40ms longer. By the difference, the second is worse, but part of those 40ms comes from being farther away: at the same amount of queueing, a longer path adds more milliseconds. A difference cannot separate "I am queueing" from "I am simply far", because it carries the floor's unit.

**A ratio cancels the floor.** `PRES` is the extra delay as a share of the node's own floor, so the same amount of queueing is the same number for a near node and a far one; `BAD` only compares against the floor and carries no distance either. Both are dimensionless, which is what "independent of distance" means here.

**There is a trade-off.** A ratio makes the relative change look larger for a node with a lower floor. A node at 176ms with a 149ms floor that occasionally doubles has `PRES` of 0.18, so it can look worse than a 525ms node with `PRES` of 0.03. It is faster in absolute terms, but more overloaded compared with its own best. That is intentional in `idle`, not a side effect. `max_rtt` and `quick` handle absolute delay. One could try to account for both by selecting nodes from different delay ranges, but that would no longer be `idle` alone.

### Design Choice: Does an Immature Floor Change the Ranking?

`FLOOR` needs a full 24 hours to collect readings for every time of day. Just after startup, it is only the fastest check in the current window. Does that make the ranking unreliable?

**No, because `FLOOR` is in the denominator.** If a node's floor is off, its `PRES` changes by the same proportion, and `BAD` is recalculated against that floor too. A difference behaves differently: an error of 10ms in a floor shifts the result by 10ms and can change the order. A ratio scales with the floor, so the ranking mostly stays the same.

The ranking only moves when the floors of **different nodes** are off by different proportions, and neither source of floor error works that way. Taking the minimum biases every node in the same direction, and where a node's floor lands among its own samples depends on how narrow its distribution is — a narrow one settles on its true lower bound quickly. So an immature floor makes every score a little too optimistic and shifts how strict `tolerance` is by that much, but it does not reorder the nodes.

**There is a limit.** The larger the floor error, the worse the ratio works: once the denominator is noisy enough, it performs about as poorly as a difference. So this design assumes the floor is not **far off for an individual node**. A new machine or line can make the old floor completely wrong; the 48-slot ring's 24-hour expiry is meant to handle that.

There is one more connection: a node with more variation tends to have a lower floor, which raises `PRES`. Since variation is itself a problem, this is in the intended direction, but it means `PRES` partly overlaps with `BAD`. The overlap points the same way, so it cannot let a quiet node profit from looking variable.

### Derivations

This section explains the formulas, constants, and assumptions behind the noise estimates. First, the notation:

- `n` is the number of successful checks; `xᵢ` is the delay of the `i`-th successful check; `MED` is the median of those delays.
- `sd(…)` is the sample standard deviation, used to estimate how far a set of values usually falls from its centre.
- `Var(…)` is variance, or the square of the amount of variation. `SE` (standard error) is how much an estimate usually varies when sampled again; it is the square root of the variance.
- `SE_proxy` is the noise estimate used by the implementation. It is an approximation, not an exact standard error or confidence interval.

The derivations assume checks are roughly independent and come from a distribution that stays stable within a window. The analysis of `JIT` also assumes the distribution is symmetric; real networks do not always meet these conditions.

#### `idle`: Estimating the Errors in the Two Ratios

`idle` ranks by `score = 1000 × (PRES + 0.5 × BAD)`. We want to estimate how much this sum would usually change if we ran another set of checks.

**Step one: `FLOOR` is constant within a round.** `FLOOR` is built from the minimum in each slot. Once the current round has updated its slot, `FLOOR` is a fixed value. Subtracting or dividing by a constant gives:

```
Var(PRES) = Var(MED / FLOOR) = Var(MED) / FLOOR²
```

The current round can move `FLOOR` **down** if it finds a faster delay. `PRES` then uses this round's `MIN` as its reference, so it also picks up some of `MIN`'s noise. This happens only when a new record is set, and only in one direction; it is not variation that occurs in every round. The noise estimate therefore focuses on `MED`.

The error in `FLOOR` itself is different: it is **systematic**. A minimum that is too low makes every `PRES` for that node too high, and more samples do not average that bias away. It is not part of the standard error; its direction must be understood instead. See step five.

**Step two: the variance of `MED`.** A median depends on the rank of the samples, so its variance depends on the density near the median, `f(m)`:

```
Var(MED) ≈ 1 / (4n·f(m)²)
```

Under a normal approximation, `f(m) = 1/(σ√(2π))`. Substituting gives `(π/2)·σ²/n`; taking the square root gives:

```
SE(MED) ≈ 1.2533 · σ / √n
```

`σ` is the variation in one check. The implementation estimates it in two ways: `sd(x)` is the sample standard deviation used in the basic standard-error formula; `JIT` is the median deviation multiplied by `1.4826`, a robust estimate on the same scale that a single spike cannot move. They are used for different purposes: retention uses the first, while the margin uses the second. See [Why the `idle` Margin Is Calculated This Way](#why-the-idle-margin-is-calculated-this-way). The origin of `1.2533 = √(π/2)` is explained in [`quick`: The Error in `MED`](#quick-the-error-in-med).

**Step three: combining the two gives the error in `PRES`:**

```
SE(PRES) ≈ SE(MED) / FLOOR ≈ 1.2533 · sd(x) / (FLOOR · √n)
```

For the margin, `sd(x)` is replaced with `JIT`, with the same formula shape. This is an **absolute** error: it does not change with the score. A node with 10% standing queueing and one with almost none have the same error scale because their difference is in `MED`; the error depends on `σ`, `FLOOR`, and `n`.

**Step four: the error in `BAD`.** `BAD` is the share of all attempts that missed the limit, or the number of misses divided by `ALL`. It is a binomial proportion, with standard error:

```
SE(BAD) ≈ √( BAD(1 − BAD) / ALL )
```

This term differs from the first: it does not depend on `σ`, only on `BAD` and the number of attempts. When `BAD` is zero, its error is also zero. This is why it can be used for hysteresis but not for the band; see [Why the `idle` Margin Is Calculated This Way](#why-the-idle-margin-is-calculated-this-way).

The denominator is all attempts, not just successful checks. Failures are already counted in the measure; using a smaller denominator would make the error look too small, especially for a node with many failures.

Both terms come from the same window and may be related. The implementation treats them as independent and adds their variances:

```
SE_proxy(score) ≈ 1000 · √( SE(PRES)² + 0.5² · SE(BAD)² )
```

This is the noise estimate for **hysteresis**. The band uses only the third step, with `JIT` in place of `sd(x)`:

```
band margin = 2√2 · 1000 · 1.2533 · JIT / (FLOOR · √n)
```

**Step five: `FLOOR` uses a minimum, so it is optimistic.** `FLOOR` answers "how fast can this node be?" by using its fastest observed delay, without smoothing. A simulation with 1,440 checks in a day, an idle delay of 200ms, and `sd = 8` gave:

| Estimate | `FLOOR` | `PRES` while idle |
| --- | --- | --- |
| **Minimum (used by the implementation)** | **175** | **14%** |
| Minimum of each window's median | 191 | 5% |
| 10th percentile for the full day | 190 | 5% |
| Median for the full day | 200 | 0% |

The minimum is systematically about three standard deviations too low: with 1,440 chances in a day, one check is likely to be lucky. The cost is that every node's `PRES` is a few percentage points higher; the benefit is that the measure **reacts quickly**.

Recording each window's median is steadier, with only a few percent of error, but answers a different question: "How fast is this node **usually**?" During a steady period each node's median equals its own, so `PRES` is zero for all nodes and `idle` has nothing to rank. It can also fail on a real pattern: a node whose delay alternates between two bandwidth pools may have two low and two high checks. Its median lands between the two levels, at a value it never reached, and stays there. It then appears to be the steadiest node (`MED = 919`, `MIN = 556`; a median floor gives 0, while a minimum floor gives 65%). The minimum does not get fooled by this pattern.

This bias is **shared**, so it does not change the ranking: if two nodes are both optimistic by the same proportion, their `PRES` difference still reflects their actual difference. It only moves the zero point, and therefore changes how strict `tolerance` feels.

#### `quick`: The Error in `MED`

The `quick` score is `MED + JIT`. First consider `MED`: a new set of checks can move the median too. Its error depends on how crowded the values are around it. If many checks are close to the median, it is harder to move; if few are nearby, a small change in the sample can move it farther.

Let `f(m)` be the density around the population median `m`, or how closely delays cluster around the median. With enough samples, the variance of the median is approximately:

```
Var(MED) ≈ 1 / (4n·f(m)²)
```

For a normal distribution, `f(m) = 1/(σ√(2π))`. Substituting gives:

```
Var(MED) ≈ (π/2)·σ²/n
SE(MED)  ≈ √(π/2)·σ/√n
          ≈ 1.2533·sd(x)/√n
```

Here, sample `sd(x)` estimates the population standard deviation `σ`. So `1.2533 = √(π/2)` is a coefficient from the **normal approximation**: under that assumption, the standard error of a median is about 25% larger than that of a mean. It may not fit a strongly skewed delay distribution or a very small sample.

#### `quick`: Why `JIT` Is Multiplied by `1.4826`

`JIT` represents typical variation with the median absolute deviation, then multiplies it by `1.4826` to put it on the standard-deviation scale. This constant comes from the normal distribution:

```
q = median(|X − m|)
P(|X − m| ≤ q) = 1/2
```

In a symmetric normal distribution, half the probability falls within `q` of `m`. So a quarter lies between the mean and `m+q`, which gives `Φ(q/σ) = 0.75`. Therefore:

```
q = Φ⁻¹(0.75)·σ ≈ 0.6745·σ
σ ≈ q / 0.6745 ≈ 1.4826·q
1.4826 = 1 / Φ⁻¹(0.75)
```

For normal data, the median deviation is about `0.6745σ`; multiplying by `1.4826` lets `JIT` estimate the standard-deviation scale. This is a conversion for normally distributed data; it does not mean real delays follow a normal distribution.

#### `quick`: Estimating the Error in `JIT`

Even if the true centre `m` is known, `JIT` changes depending on which checks were sampled. Let `q` be the population median of `|X−m|`, and `g(q)` the density of absolute deviations near `q`. If the centre is known, the distribution is symmetric, and there are enough samples:

```
SE(median(|X − m|)) ≈ 1 / (2g(q)√n)
SE(JIT) ≈ 1.4826 / (2g(q)√n)
```

The more tightly the absolute deviations cluster near `q`, the harder it is to move their median and the smaller the error. If the centre is estimated from the same samples, or delays are not symmetric, those effects also need to be considered.

The implementation uses `1.858·sd(d)/√n` as an approximate noise estimate for this part, where `1.858 = 1.4826 × 1.2533`. This applies the normal median coefficient to `d`. But even when `x` is normally distributed, absolute deviations `d` follow a half-normal distribution, so `1.858` is not an exact standard-error coefficient. It is the estimate used by the implementation, not a precise probability guarantee.

#### `quick`: Combining the Errors in `MED` and `JIT`

If two errors are independent, the variance of their sum is the sum of their variances. In general, a covariance term is needed:

```
Var(MED + JIT) =
    Var(MED) + Var(JIT) + 2·Cov(MED, JIT)
```

`MED` and `JIT` use the same checks and may be related. For a symmetric IID distribution they become independent as the sample grows, so the implementation ignores covariance as an approximation. With few samples or another distribution, that may not hold. The combined noise estimate is:

```
SE_proxy(score) ≈ √( SE_proxy(MED)² + SE_proxy(JIT)² )
```

So the coefficients and variance calculation for `quick` depend on the distribution and sampling assumptions above. This `SE_proxy` is an engineering estimate, not a 95% confidence interval.

#### Comparing Two Nodes: How Uncertain Is the Difference?

This asks how much the gap between a node's score and the best score could change if the checks were run again. Treat the two scores as random quantities:

```
S_node   = the node's score as a random quantity
S_best   = the best node's score as a random quantity
D        = S_node − S_best
Var(D)   = Var(S_node) + Var(S_best)
           − 2·Cov(S_node, S_best)
```

If the nodes' measurement errors are independent, the covariance is zero. If they share a probe target or are affected by the same line conditions, it may not be; the implementation ignores it. Under the independence assumption, substituting the two noise estimates and taking the square root gives:

```
Var(D) ≈ SE_proxy_node² + SE_proxy_best²
SD(D)  ≈ √(SE_proxy_node² + SE_proxy_best²)
```

`SD(D)` describes how much the score difference may change when measured again. `SE_proxy` is the implementation's estimate, not an exact value.

#### How `SD` and `SE` Relate

`SD` (standard deviation) describes how much a random value varies. `SE` (standard error) describes how much an estimate varies when sampled again. A score is estimated from samples, so its standard deviation is its standard error. Here, `SE_proxy` is the implementation's approximate estimate of that error.

`score_node` and `score_best` are fixed observed values in the current window. Calling them random quantities means asking how much they might change if measured again.

#### Long-Run Failure Rate: Why the Weight Is Exponential

`FAIL / ALL` is an **equal-weight** average: every check in the window weighs the same, and once a check leaves the window its weight drops from `1/n` to `0`. That has two consequences: a step at the window boundary, and the same failure rate covering five times as much time at `5m` as at `1m`.

The long-run rate is a different kind of average: **recent checks weigh more, older ones weigh less, and "how much less" depends only on time**. Let check `i` happen at `tᵢ`, the current moment be `t`, and take the weights

```
wᵢ = exp(−(t − tᵢ) / τ)
R  = Σ wᵢ·xᵢ / Σ wᵢ              # xᵢ = 1 when that check failed
```

**Why it has to be exponential.** The weight should depend only on "how long ago", and two stretches of time have to compose:

```
w(a + b) = w(a) · w(b)
```

That is the requirement that "wait `a` minutes, then count `b` minutes" equals "count `a+b` minutes at once". Among continuous functions that are not identically zero, only the exponential satisfies it, so

```
w(t) = exp(−t / τ)
```

With any other shape — linear decay, steps, a fixed-length sliding window — the weights would depend on when the checks happened to land, and the same `τ` would stand for a different stretch of time at `1m` and at `5m`.

**The recursion.** Write `c = exp(−Δt/τ)`. When a new check arrives, every older weight is multiplied by the same `c`, so two running totals are all it takes:

```
S ← c·S + x          # weighted number of failed checks
W ← c·W + 1          # weighted number of checks
R  = S / W
```

Keeping `S` and `W` apart rather than only `R` is what lets a cold start carry a prior, see below. The denser the checks, the larger the steady state of `W`, which is `τ/Δt`, and the less each one matters — precisely because it stands for a shorter stretch of time.

**The prior at a cold start.** `W` begins at 10, the weight of ten clean checks: a cold start is a node nobody has seen fail, not a node seen failing. Starting from nothing would make the first check all the evidence there is, and one failure would put `R` straight at `1.0` — right after a restart, before the network is up, every node's first check fails, and every node would carry an almost perfect failure rate for an hour. The prior decays with the same `c` as the rest, so once the history is long enough its weight is negligible and `R` behaves exactly as it did without it. With the cache file on, the restored history takes the place of that prior.

**How it relates to `FAIL / ALL`.** While the failure rate is constant, the steady state of `R` is that rate, the same number `FAIL / ALL` gives. The two part company only when the rate changes: one steps at the window boundary, the other fades as `exp(−t/τ)`. With `τ = 45 minutes` and a check every minute, a node that had been clean (`R ≈ 0`) and then fails ten times in a row:

| Then healthy for | Window `FAIL / ALL` (10 samples) | `R` |
| --- | --- | --- |
| 0 minutes | `10/10 = 1.000` | `0.199` |
| 10 minutes | `0/10 = 0.000` (a step) | `0.160` |
| 45 minutes | `0/10 = 0.000` | `0.073` |
| 90 minutes | `0/10 = 0.000` | `0.027` |

The table assumes a history long enough for the prior to have faded. The first row is `1 − exp(−10/45)`, and each later one is multiplied by `exp(−Δt/45)`. The window forgets the outage by minute ten, while the node's record takes over an hour to recover — which is the whole reason for using `FAILR` instead of `FAIL / ALL`.

**Effective sample count.** The sum of the weights is `1/(1 − c) ≈ τ / Δt`, so the estimate is "worth" that many recent checks: about 45 at `1m`, 22 at `2m`, 9 at `5m`. The longer the interval, the fewer checks the same `τ` covers and the coarser the estimate, which is what keeps `τ` from being set too small.

**Where it is not exact.** `S` and `W` decay by the same `c`, so a change of spacing needs no correction and the weighted average stays right. What is left is the prior: until it has faded, `W` is below its steady state, so the same failures read slightly higher early on — which is what is wanted, since there is little evidence then.

### Debug

To check this table against your own lines and load, ask the API (a non-ASCII group name has to
be URL encoded):

```bash
curl -s 'http://127.0.0.1:9090/private/health/Name/windows?ahead=2&configs=1:5,1:10,2:5'
```

Right after a restart, when there are too few checks to hold a window, `configs` is empty and
`note` says what is missing.

`rho` compares the rank of every node, `agreement` compares the members of the pool: a load
balance group serves from the whole pool, so what matters is that the two pools are the same set,
not who ranks first inside one. The two answer different questions: a high `rho` does not mean a
stable pool, and a high `agreement` does not mean the order inside the pool is meaningful. A
longer window does not necessarily score a higher `rho`: the more checks it holds, the more of a
node's earlier state it mixes into a prediction of the few checks that follow it.

`agreement` rewards a steady pool, `reaction` and `recovery` report what that steadiness costs: how
many checks a node that turned bad (slower than twice its own floor) stays in the pool, and how
many checks it takes to come back once it is usable again. A longer window is steadier and slower;
both means cover only a handful of events, so neither is a measurement when `reactions` or
`recoveries` is small.

`rho` compares the rank of every node, which the nodes' lasting differences dominate. In a period
where those differences hold, it saturates and every row crowds into a narrow band: do not compare
windows with it then. In a period where the line itself is uneven, the ranking is not stable and
`rho` separates the windows again.

A window needs `samples × stride` + `ahead` checks to be measurable at all, and a group keeps
only `sampling` of them: the rest is not kept. Both can be resized in place, without a restart
and without losing what was already collected:

```bash
curl -X PUT 'http://127.0.0.1:9090/private/health/Name/history?sampling=15&extra=60'
```

`sampling` is how many checks the ranking reads, leave it out to
keep the one in use. `extra` is how many checks to keep behind it, for the measurement only;
leave it out, or set it to `0`, to keep the sampling alone. The checks already collected survive
the resize.

Parameters

| Parameter | Default | Meaning |
| --- | --- | --- |
| `ahead` | `8` | how many checks after the window it is asked to predict |
| `configs` | the default grid | `stride:samples` pairs, e.g. `1:15,2:10`. `stride` is how many checks are skipped between samples, `samples` how many the window holds |

Fields

| Field | Meaning |
| --- | --- |
| `interval` | the actual interval between the checks (median), not the configured one |
| `nodes` | how many nodes were measured |
| `checks` | how many checks the longest history holds |
| `ahead` | how many checks each window was asked to predict |
| `stride` | how many checks are skipped between samples, `1` for consecutive |
| `samples` | how many checks the window holds; its span is `interval × stride × samples` |
| `span` | the actual width of the window |
| `windows` | how many positions of the history the window was measured at; the other fields are the average over them |
| `rho` | rank correlation between the order the window produces and the order the checks after it produce: `1` identical, `0` as good as guessing. It saturates, so read it only as a reference when the history is too short for anything else |
| `delta` | the standard error of `rho`; it collapses to `0` when `windows` is small, which means no precision rather than precision |
| `pool` | how many nodes the band lets in, on average, for that window |
| `agreement` | Jaccard index of the pool the window selects and the pool the checks after it select: `1` the same set, `0` nothing in common |
| `exact` | the share of positions where the two pools were the same set; `agreement` also counts a partial overlap, `exact` counts only a complete match |
| `reaction` | how many checks a node that turned bad stays in the pool; `ahead + 1` is a node still in it at the horizon, absent when there are no reactions |
| `reactions` | how many times a node turned bad while it was in the pool; failures are not reactions, because the pool drops them on the check that fails |
| `recovery` | the other direction: how many checks a node that is usable again takes to come back into the pool. The horizon is the window (`samples × stride`), because a failure has to leave it first; a node that has not returned within it counts as the horizon + 1, absent when there are no recoveries |
| `recoveries` | how many times a node that had been served was dropped and turned good again |
| `note` | what was missing, when not one window could be measured |
