# Node Rating

## Scoring Formulas

There are two objectives a group can score a node by:

- `quick`: **How fast is it usually?** A 170ms node is simply slower than a 40ms node. This is absolute time, in milliseconds.
- `idle`: **How far is it from its own best?** A node that usually takes 200ms and stays at 600ms through the evening may not look slow, but it is running at four times its best and holds up every bit of traffic at that time.

The formulas are:

```
MED         = median(x₁ … xₙ)
JIT         = 1.4826 × median(|xᵢ − MED|)
DEV         = (1/n) × Σ|xᵢ − MED|
SLOT        = the day cut into 48 slots of 30 minutes
SLOT[k]     = the fastest check seen in slot k
FLOOR       = min(the slots of the last 24 hours)  # how fast it can be at its best
FAILW       = Σ exp(−(t − tᵢ) / τ_F)         # failed checks only, τ_F = 5 minutes
FAILW_TOTAL = Σ exp(−(t − tᵢ) / τ_F)         # all checks in the window, i.e. FAILW if all failed
FAILR       = a longer-run failure rate, older checks weigh less
ALL         = the number of checks in the window
PRES        = max(0, MED − FLOOR) / FLOOR
BAD         = min(1, #{xᵢ > 2 × FLOOR} / ALL + max(FAILW / FAILW_TOTAL, FAILR))
cost        = MED + JIT        # quick
failure     = min(TIMEOUT, 3 × cost)
p           = max(FAILW / FAILW_TOTAL, FAILR)

score       = (1 - p) × cost + p × failure    # quick
score       = 1000 × (PRES + 0.5 × BAD)       # idle
```

`quick` compares actual delay, `idle` compares degradation. Measure degradation with actual delay and a distant node is always at a disadvantage: you end up comparing distance instead of condition. So `idle` uses ratios throughout, and stays independent of distance.

A failed check carries no delay — it says nothing about how fast the node is when it works. Failures are recorded separately and weighted by age into a near-term failure weight `FAILW`: a fresh failure weighs 1, and older ones weigh less. `FAILW` alone decides whether the node qualifies, and prices failures for both objectives; `FAILR` records a longer horizon with the same mechanism.

These formulas assume at least two successful checks. With only one, there is nothing to say about variation or pressure, so the objective falls back to delay.

`SLOT` follows the wall clock: `k` is which 30 minutes of the day it is. A check fills the slot it was measured in, and only the smallest one in the slot is kept. After 24 hours the same slot is overwritten by the next record, and the old value is gone on its own.

- **`MED`** is the delay level. A median rather than an average, so one very slow check cannot pull it.
- **`JIT`** is **how much the delay usually moves**. A few spikes do not move it; `1.4826` is the factor that converts a median deviation into a standard deviation under a normal distribution.
- **`DEV`** is **how much it moves overall**. Every check counts, and spikes add up only when there are many. It shows up in reports to help diagnose, and takes no part in ranking.
- **`FLOOR`** is **the fastest check ever seen**, with no smoothing at all. One lucky probe lowers it and makes the node look perpetually queued; in return it reacts immediately to change.
- **`PRES`** is **how deep** the queueing is: how much longer than its own best it usually waits, as a ratio. Dividing by its own floor cancels the propagation distance, so near and far nodes are comparable.
- **`FAILW`** is **the near-term failure weight**: each failed check in the window contributes `exp(−(t − tᵢ) / τ_F)`, about 1 just after it happens, 0.67 after two minutes and 0.37 after five. It only sees failures inside the window, but the weights decay, so the window boundary is no longer a hard cut; consecutive failures add up.
- **`BAD`** is **how often** it queues: the share of checks above twice its floor, plus the share of failures. It is compared with its own floor too, so distance does not matter; one extreme check counts once. The failure term is the larger of the recent share `FAILW / FAILW_TOTAL` and `FAILR`.
- **`FAILR`** is the **long-run failure rate**. Old records weigh less and less, so it describes how things have been lately; it ignores the window, and a window that happens to be clean does not reset it. It and `FAILW` are two horizons of the same exponential weighting: `FAILW` covers the last few minutes as an absolute weight and decides whether the node can be used right now; `FAILR` covers tens of minutes as a ratio and decides whether it is still worth using later.

## Tolerance Band

A score close enough to the best counts as equally good, so the group does not switch back and forth over measurement noise. "Close enough" has two parts:

- `tolerance`: the difference the user is willing to switch for. Both objectives default to `50ms`, because both score in milliseconds — 50ms is 50ms for `quick`, and 50ms for `idle` is 5% on the 1000ms scale.
- Measurement noise: the score comes from a finite number of checks and moves when the batch is redone. It is not a fixed bias that can be subtracted from the score; it can only be put into the band.

So the band is "intent + noise":

```
band = tolerance + 2√2 × noise_best / √n
cut  = score_best + band
```

`n` is the number of successful checks; `noise_best` is the typical single-check variation of the best node:

```
noise_best = JIT_best                                # quick
noise_best = 1000 × 1.2533 × JIT_best / FLOOR_best   # idle
```

The band estimates `σ` from `JIT_best`, not `sd(x)`: `JIT` is the median deviation, which one slow probe cannot inflate, so the band is not widened by a spike. `idle` uses only the `PRES` term — `BAD` enters the score but not the band; the band asks "could this node be as fast as the best", while `BAD` asks "will it break".

The more samples, the smaller `1/√n`, and the closer `band` is to `tolerance`, but never below it; with fewer than two successes no noise term is added. Every node scoring at or below `cut` stays a candidate.

## Keeping Existing Connections: Hysteresis

When load balancing is allowed to interrupt existing connections, a node that is replaced can keep its open connections for a while. Load balancing allows connections over different lines at the same time, so there is no need to cut one until it is bad enough; otherwise a node entering and leaving the selection would repeatedly break connections. `urltest` allows only one node at a time and has no such logic.

A node that just left the selection is not cut off while the part beyond the band is still within the noise:

```
noise     = 2 × √(SE_node² + SE_best²)
allowance = band + min(band, noise)
```

`SE` is the standard error of a node's score. `2` is the two-standard-error scale: a difference beyond two `SD`s counts as really worse. Only nodes of the same status are compared by score; one that has fallen to a worse class is not kept. `min(band, noise)` caps the extra at one `band`, so a node is kept at most **two bands** away from the best. Hysteresis is there to ride out measurement noise, not to keep a clearly worse node around.

## Design Choices

### Why Not Rank by Standard Deviation

The sample standard deviation is the usual measure of spread, but it does not work as a node's variation score, for three reasons:

1. **It is centred on the mean, and the mean is dragged by outliers.** Nine of ten checks at 450ms and one at 3.15s moves the mean from 450ms to 720ms, and the other nine suddenly carry a 270ms deviation — though none of them changed.
2. **It squares the deviations, so one sample can take over.** In the same example that single check accounts for nine tenths of the variance; what comes out describes that probe, not the node's usual state.
3. **Rooting it does not bring it back to the right scale.** A spike of size `X` among `n` samples contributes `X/n` to the average deviation, which matches the share of connections it slows; it contributes `X/√n` to the standard deviation, already more than three times as much at `n = 10`, and the gap grows with the window.

`DEV` still measures deviation from the median, but as a linear average, so no spike moves the centre. A node alternating between 200ms and 800ms still has a large `DEV`; this is not ignoring instability, it is refusing to let one sample decide. Today it is used in reports and not in ranking.

Both ratios of `idle` are linear or counted, with no squares: `PRES` is a ratio, the slow-check part of `BAD` counts occurrences, and the failure part is age-weighted with weights no greater than 1, so one extreme value counts once. They do not measure "how much it swings within the window" but "how far below its own best it is, how often, and how recently". The two things can show the same spread in a window and still mean different things: a node alternating between 200ms and 800ms has a median near 500 with a floor still at 200, so `PRES` is about 1.5 and `BAD` about half; a node steady at 800ms has a floor of 800 too, so both ratios are zero — it has never been faster, so the slowness is not queueing. **`idle` picks the latter.** A steady slowness is a known cost that `max_rtt` can handle; a delay that comes and goes is not, because there is no way to plan around traffic that is fast and slow by turns.

The standard deviation is not banished: `quick` still uses it to estimate score noise, it just is not the ranking score itself.

### Why Trends Are Not Part of the Score

The samples in a window have an order, yet none of `MED`, `PRES`, `JIT` or `DEV` looks at it: shuffle them however you like and they come out the same. So a node that has been getting worse for ten minutes reads exactly like a node that has always been that way, as long as the distribution matches. The slow-check part of `BAD` does not look at order either, but its failure part is age-weighted, so `BAD` distinguishes "failed just now" from "failed long ago" — that is exactly why `FAILW` exists.

`PRES` and `BAD` cover part of it, but differently: they do not compare earlier with later inside the window, they compare **now with the historical best**. A node going from 200ms to 600ms has `MED` at 600 at once and `FLOOR` still at 200, so `PRES` jumps to 2.0 without waiting for a trend to build; a node going back from 600ms to 200ms has `FLOOR` drop to 200 with it and `PRES` returns near zero at once, with no grudge from the past. **So direction needs no separate machinery: `FLOOR` only moves down, an improvement is recognised immediately and a deterioration is paid immediately.**

Comparing the two halves of a window (`median(second half) − median(first half)`) does add information and is cheap to compute, and it can tell "just started getting worse" from "has been worse for a while". It is not in the score because the moment it is, changing `interval` changes the ranking. A ranking should describe the node, not how often it happens to be checked. A trend is a decision, like tolerance, not a measurement.

It also has to be used with care: at `sampling` 10, two bad samples in a row cannot move the median of the five in the second half — the third is the first that shows. That makes it slower than "the latest check", which is already in the statistics.

Window length is the same trade: while the delay distribution holds roughly steady, a longer window is smoother and less noisy; but old samples weigh more, and the score follows a node that changes more slowly. A shorter window follows faster and is more easily pushed around by chance. Less noise and keeping up with change are the two ends of one choice of window.

### Why the Baseline Is a Ring of Slots

The baseline records "the best this node ever looks like", and it is pulled by two opposing demands: **it must remember long enough**, because idle time only appears at the daily trough and too short a memory takes a congested level for an idle one; **and it must forget**, because a node with the same name may have a new line or a new machine, and the old baseline is then simply wrong.

A single "when does this expire" timer cannot serve both, because expiry is a one-off: nothing changes until the moment, and then everything does. Cutting the day into slots has no such problem:

```
48 slots × 30 minutes = 24 hours
every 30 minutes exactly one slot is reset, the other 47 stay
```

- **It remembers long enough**: the ring covers 24 hours, every time of day has its own record, and the baseline always has a full day to refer to.
- **It forgets smoothly**: there is no "expiry" step, only one slot overwritten by the next record of that slot. The baseline never goes stale all at once, and never resets so that every node's score collapses together.

The slots follow the wall clock rather than uptime, so slot 40 is always 20:00 to 20:30. Each slot then records **how fast this node can be at this time of day**, not the lowest value of any 24 hours.

The number of slots sets the time resolution, and how many probes each slot holds: fewer slots mean longer stretches whose every fluctuation collapses into one minimum; more slots mean fewer probes each, and a record that may be decided by one or two. 48 divides the day evenly and leaves room for several rounds of checks per slot, which is why it was chosen. It belongs with `sampling` and `interval`: changing it means reconsidering the probe rate, so it is not something to hand to a user on its own.

**The cost.** The floor is a minimum of per-slot minima — still a minimum, so it leans towards the most optimistic reading. That matches its purpose (an idle level is exactly what is wanted), but it means that once in a while a lucky probe lowers the floor slightly and every node's `PRES` reads a little higher, see [Derivations](#derivations). The bias is **the same for everyone** and does not change the ranking; it only moves how strict `tolerance` is.

#### Updating, Expiring, Starting Up

**Updating.** Each round writes the slots first and computes scores after, so the round takes effect immediately:

```
slot[k]     = min(slot[k], MIN)            # a new slot starts from MIN, then keeps its minimum
FLOOR       = min(the slots of the last 24 hours)
FAILW       = Σ exp(−(t − tᵢ) / τ_F)       # failed checks in the window, age-weighted
FAILW_TOTAL = Σ exp(−(t − tᵢ) / τ_F)       # all checks in the window, age-weighted
PRES        = max(0, MED − FLOOR) / FLOOR
BAD         = min(1, #{xᵢ > 2 × FLOOR} / ALL + max(FAILW / FAILW_TOTAL, FAILR))
```

Writing first does not zero the score: a slot holds a minimum, so after the write `FLOOR ≤ MIN ≤ MED`, `PRES` is still a non-negative ratio and `BAD` still a share. Computing first and writing after would only delay a node that got faster by one round, for nothing.

**Expiring.** There is no separate expiry step: after 24 hours the same slot is overwritten by the next round and the old record is gone with it. So the baseline always comes from times **actually measured**, and never treats present congestion as a fastest level the way "reset to the current value on expiry" would. A slot that stays congested rises slowly while the others stay as they were; only congestion that lasts everywhere raises the baseline as a whole, which amounts to admitting "this node is simply this slow".

**Sudden changes.** While a node is steady or changes slowly, the baseline moves by one slot's worth per round, because only one slot of 30 minutes is replaced. For a node that gets 144ms slower each day: expiring everything at once is a 144ms jump, while with slots the change spreads over 48 updates in a day, about 11ms each. **A sudden change — a new line, a new machine — still jumps**: when the last old reading is overwritten, the minimum has to fall to the next one, and the jump is the size of the change itself. That is the price of forgetting old data, not noise that smoothing could remove.

**Starting up.** The first round writes its fastest check, so `FLOOR` is `MIN` from the start and needs no fallback. The floor then fills one slot at a time and takes 24 hours to cover a day. With the cache file on, a restart carries on with the whole day of slots and the long-run failure rate; without it (not enabled, the target or path changed, the record expired) a restart starts from zero as it always did. While it fills, each slot covers fewer times of day and the floor tends to read high — the cost of forgetting old data; the ranking still works, see [Why Both Parts Are Ratios](#why-both-parts-are-ratios).

### Why Only the Floor and the Failure Rate Are Cached

Only two pieces of state live outside the window: the baseline and the long-run failure rate. `FAILW` and `FAILW_TOTAL` are computed from the timestamps of every check in the window and need no extra storage; so the cache still holds only `FAILR` as failure state, while `FAILW`, `FAILW_TOTAL`, `MED` and `JIT` are window statistics that can be rebuilt. After a restart the window is empty and `FAILW` starts at zero; `FAILR` is restored from the cache, so the score still holds down a node that just failed. Making the gate exact across restarts would require storing `FAILW` together with a timestamp — two numbers, not a merge of the two horizons. The window holds only `sampling` checks (`sampling × interval`), while one piece looks back 24 hours and the other `τ = 45` minutes. A restart is therefore very lopsided: the window rebuilds in minutes, `FLOOR` waits for the next daily trough, and `FAILR` has no evidence until the node **fails again**.

The samples of the window are not kept — twenty minutes rebuilds them, while holding on to those hundreds of checks would take two orders of magnitude more space.

**A record belongs to a path and a node, not to a check.** What is kept is the node as that path reaches it: a different target or chain of relays keeps its own records, while two checks with the same target and relays share the records of the nodes they share. That sharing is accurate enough — on one path, sampling twice a window or a hundred times a day gives every node the same floor in the end, and the two differ by noise rather than by origin. A writer only touches the nodes it measured, and what comes back describes the node itself.

A record only matters for 24 hours, which is how long the floor lives; records older than that are dropped, so a node that is no longer checked does not hold a place forever.

### Why the Threshold Is Two and the Weight Is 0.5

`BAD` counts a check as bad when it is twice as slow as the node's own best, and the weight `0.5` puts a node that doubles every time at 500 on the scale. Neither number was fitted; each has its own reason.

**Why two.** Twice is "it took twice as long", and the most common line for deciding that a link has started to queue. Both ends fail in their own way: too low (1.5, say) and ordinary jitter and probe noise cross it, so every node carries some `BAD` and they stop being distinguishable — this term exists to tell "queues now and then" from "queues all the time". Too high (3, say) and only a long standing queue reaches it, nearly every node reads zero and the term disappears, leaving the score to rank by depth alone. Two sits between them: normal variation cannot reach it, and real queueing always does.

**Why 0.5.** It converts "frequency" onto the same ruler as "depth". `PRES` lives between 0 and 1, and a node that doubles every time should be judged about as badly as the worst constant queueing, i.e. gain an amount near 1. Taking 0.5 means "doubles every time" is slightly worse than "queues 50% constantly". That is a judgement about magnitude, not a fit.

It is not an arbitrary value, but it does not need fine tuning either — it sets the **conversion rate**: how many bad checks equal one percentage point of queueing. Move the rate and the nodes at the edge change places: wider takes one or two merely mediocre nodes, narrower keeps only the cleanest. So it is a policy question — how much you want "stalls now and then" to be worth the same as "is always a bit slow". Among clearly different nodes the order does not depend on it; only the few at the edge of the band do.

The two parts **have to stay separate**, because they catch different faults:

| Node | `PRES` | `BAD` | Depth only | Frequency only |
| --- | --- | --- | --- | --- |
| fast but drops | very low | very high | first | middle |
| slow but steady | medium | very low | middle | first |
| slow and drops | very high | very high | last | last |

Keep only one of them and the other kind of fault slips through.

### Why Both Parts Are Ratios

`idle` answers "how far is this node from its own best", and "how far" can be written two ways: as a difference (`MED − FLOOR`) or as a ratio (`(MED − FLOOR) / FLOOR`). The ratio is not a preference; it is the only form that removes distance.

**A difference brings distance back.** A 400ms node with a 380ms floor waits 20ms more; a 900ms node with an 860ms floor waits 40ms more. By the difference the second is worse, but part of those 40ms is simply being further away: at the same amount of queueing, a longer line adds more milliseconds. A difference cannot separate "I am queueing" from "I am simply far", because it carries the units of the floor. The ratio cancels the floor: `PRES` is "the extra wait as a share of its own floor", the same number for a near and a far node at the same queueing, and `BAD` only compares against its own floor, so distance drops out too. Both are dimensionless, which is what "independent of distance" means.

**The cost is real, but it does not scramble the ranking.** A ratio amplifies the noise of a low floor: a small denominator turns the same absolute error into a larger ratio. So the floor must not be wildly wrong per node — which is what the ring of slots is there to ensure. When the floor is wrong **the same way for everyone**, the ranking still holds: a floor off by some factor scales `PRES` by the same factor and recomputes `BAD` against the same floor. That is the essential difference from the difference form, where a floor 10ms off is a 10ms shift and moves a node up or down.

The ranking only moves when the floors of **different nodes** are off by disproportionate amounts, and neither source of error is choosy: one is the systematic optimism of taking a minimum, which pushes every node the same way; the other is which sampled value each node's floor lands on, which depends on that node's own spread, and a narrow spread converges quickly to its true lower end. So while the floor is still filling, the scores read a little optimistic and `tolerance` reads a little stricter; the ranking is unchanged.

**Where the limit is.** The more the floor errs, the worse the ratio form fares: once the denominator is inflated by noise enough, it is no better than the difference. A new machine or a new line invalidates an old floor outright, and that is exactly what the 24 hour ring handles. One coupling is worth noting: a node that varies more has its floor pushed lower and its `PRES` lifted — variation is itself a fault, so the direction agrees with the intent, but it means `PRES` carries some variation information and overlaps with `BAD`. The overlap points the same way, so a quiet node never gains by looking variable.

## Derivations

This chapter follows the order of the formulas above: first the statistics and failure terms of the scoring formulas, then the noise of the tolerance band and hysteresis, and finally the limits of these approximations.

The notation:

- `n` is the number of successful checks; `xᵢ` is the delay of the `i`th successful check; `MED` is their sample median.
- `sd(…)` is the sample standard deviation, used to estimate how far a set of values usually sits from its centre.
- `Var(…)` is the variance, the square of the size of the fluctuations; `SE` (standard error) is how much an estimate usually moves under repeated sampling, the square root of the variance.
- `SE` here is an approximation, not an exact standard error or confidence interval.

The derivations assume the checks are independent and come from a stable distribution within a sampling window; the analysis of `JIT` also assumes symmetry. Real networks do not always oblige.

### Scoring Formulas: Constants and Failure Terms

The statistics of the scoring formulas are defined above; here are the non-obvious choices among them: the `JIT` constant, the `FLOOR` minimum, the `FAILW` gate, and the `FAILR` decay.

#### `quick`: Why `JIT` Is Multiplied by `1.4826`

`JIT` uses the median of the absolute deviations for "typical variation", then multiplies by `1.4826` to put it on the same scale as a standard deviation. The constant comes from the normal distribution:

```
q = median(|X − m|)
P(|X − m| ≤ q) = 1/2
```

In a symmetric normal distribution, "within `q` of `m`" covers half the probability, so from the mean to `m+q` is a quarter, that is `Φ(q/σ)=0.75`. Hence:

```
q = Φ⁻¹(0.75)·σ ≈ 0.6745·σ
σ ≈ q / 0.6745 ≈ 1.4826·q
1.4826 = 1 / Φ⁻¹(0.75)
```

So for normal data the median deviation is about `0.6745σ`, and multiplying by `1.4826` brings it back to the scale of a standard deviation. It is a conversion constant under normality; it does not mean real delays are normal.

#### Why `FLOOR` Takes the Minimum

`FLOOR` takes the minimum, so it is optimistic.

There are many probes in a day, one of them is bound to be lucky, and the minimum therefore reads systematically low (the more chances, the lower it goes, which is the nature of an extremum). The cost is that every node's `PRES` is a few percent higher; what it buys is a quantity that **does not lag**.

Recording per-window medians instead would be much quieter, but it answers a different question: "how fast is this node **usually**". In steady periods every node's median equals itself, so every `PRES` is zero and `idle` has nothing to rank. Worse is a real kind of node whose delay rotates between two pools: with two low and two high the median lands exactly between the peaks — a value it has never reached — and that midpoint is stable round after round, so the node is read as the steadiest one there is. A minimum is not fooled by that structure.

The bias is **shared**, so it does not change the ranking: both nodes read optimistic by the same factor, and their difference in `PRES` is still the difference in reality. It only shifts the zero point, that is, how strict `tolerance` is.

#### Recent Failures: `FAILW` and Eligibility

Eligibility needs no configuration; the gate is fixed:

```
qualified  ⟺  FAILW < 1
```

A failure weighs 1, so the rule means "the most recent failure has not yet decayed below one full failure". A fresh failure weighs 1 and the node is immediately disqualified; on the next successful check its weight has fallen to `exp(−interval / 5min)`, below 1, and the node returns to the candidates.

It is not simply "the last check": only a single failure comes back on the next round, while consecutive failures add up. At a 2 minute interval, two failures in a row still weigh `0.670 + 0.449 = 1.119` on the next success and need one more round; three in a row take about 4 minutes after the last failure, and nine take about 6 minutes, to fall back below the gate (`FAILW` converges to `1 / (1 − exp(−Δt/τ_F))`, about 3.0). So a long outage does not drag the node for all `sampling` intervals after it recovers. The weight is not computed for the gate alone: `BAD` and `p` in the score also contain `FAILW / FAILW_TOTAL`; the gate reuses it.

**`τ_F` is 5 minutes.** It answers "is it still failing now", an order of magnitude shorter than `FAILR`. The first failure still demotes the node immediately, so the peak-time reaction is unchanged; a sporadic failure in a quiet period decays on its own if it does not recur. There is no need to recognise peak hours: whether the failure continues is the best signal.

The failure term in the score uses the same `FAILW / FAILW_TOTAL`, so the gate and the score read the same evidence: when the gate lets a node through, the score has already been reduced by the same weight, and there are no two failure standards.

`urltest` uses the same gate: a node that just failed does not enter the candidates, and returns after the next successful check. Its score is also priced with `FAILW / FAILW_TOTAL`, so after the gate lets it through, the remaining weight still affects how it compares with the best node.

The old `fail / total` was an **equal-weight average**: every check in the window weighed the same, and leaving the window dropped its weight from `1/n` straight to `0`. So it jumped at the window boundary, and the same count meant a different span at different intervals. `FAILW / FAILW_TOTAL` lets each failure decay with age, so the boundary jump largely disappears; but it still remembers only failures inside the window, and failures outside the window are left to `FAILR`.

#### Why Not One Number: Two Time Constants

A ratio and an absolute value do not conflict: under the same `τ`, the accumulated weighted failures `S` and weighted checks `W` give both the absolute `S` and the ratio `S / W`. What separates them is the time constant: the gate needs the short 5 minute memory, while the long-run failure rate needs the 45 minute memory. One `τ` cannot do both — 45 minutes would hold a three-failure burst for half an hour, while 5 minutes would wash out a flickering node too quickly. The implementation therefore shares the same exponential weighting and recursion but keeps two horizons; they are called `FAILW` (short, absolute) and `FAILR` (long, ratio). `FAILR` cannot be derived from `FAILW`, nor the other way around: their `τ` differs; only within the same horizon can the absolute and the ratio be converted into each other.

#### Long-Run Failure Rate: `FAILR`

The long-run failure rate answers a different question: among the qualified candidates, which one is more likely to fail? The gate is fixed at `FAILW < 1`, so these candidates have had no full failure in the last few minutes; `FAILW / FAILW_TOTAL` can still be non-zero for a node that has just recovered, and the score keeps pricing it down.

Nodes fail in very different ways: one kind **flickers**, with failures scattered across its history and more always possible; another has **long outages**, one continuous drop followed by a long clean stretch. Their historical failure rates can look alike, yet once `FAILW` falls below the gate the flickering node fails again much sooner. The near-term weight, with only a few minutes of memory, cannot tell them apart.

So `FAILR` is kept across rounds, and the larger of it and the recent share enters the score, see `p` and `BAD` above.

`FAILR` averages differently: **the nearer a check, the more it weighs, and the weight depends only on elapsed time**. With check `i` at `tᵢ` and the present at `t`:

```
wᵢ = exp(−(t − tᵢ) / τ)
R  = Σ wᵢ·xᵢ / Σ wᵢ              # xᵢ = 1 means this check failed
FAILR = R
```

**Why it has to be exponential.** The weight depends only on elapsed time, and two spans can be joined: `w(a + b) = w(a) · w(b)` — `a` minutes followed by `b` minutes must equal `a+b` minutes at once. Among continuous functions that are not identically zero, only the exponential satisfies this, so `w(t) = exp(−t/τ)`. Switch to linear decay, step decay, or a sliding window of fixed count, and the same `τ` no longer means the same span at `1m` and at `5m`.

**Recursion.** With `c = exp(−Δt/τ)`, every new check multiplies all old weights by `c`, so two running sums are all that is needed:

```
S ← c·S + x          # weighted failures
W ← c·W + 1          # weighted checks
R  = S / W
```

Keeping `R` alone would also work at a fixed interval; keeping them apart is what lets a cold start carry a prior. The denser the checks, the larger the steady state of `W`, which is `τ/Δt`, and the less each one matters — exactly "this check stands for a short stretch of time".

**The prior at a cold start.** `W` starts at 10, the weight of ten clean checks: a cold start is "never seen failing", not "seen failing". Starting from zero would make the first check all the evidence, and one failure would put `R` at `1.0` — right after startup, before the network is ready, every node's first check fails, and every node would carry a near-perfect failure rate for an hour. The prior decays by the same `c`, so with enough history it can be ignored. With the cache file on, the restored history takes its place.

**Why decay by elapsed time instead of by count.** A count-based rate forces `sampling` and `extra` to specify how many checks to keep, and at an interval of `1m` versus `5m` the same count covers five times the span — a difference that then leaks into the score. With time there is no such problem: two 1 minute intervals and one 2 minute interval decay the same over the same span, and changing `interval` does not change what the estimate means. It is also unaffected by changing the sampling at runtime, since resampling the window does not reset it.

**Why not a whole day.** A daily failure rate mixes the evening peak with the quiet hours, while queueing and dropping vary by time of day. A `τ` decay follows the recent past: the evening peak shows up as the evening peak, undiluted by a day's average.

**How long `τ` should be.** Data gives no clear answer: widening the range many times over barely helps in predicting whether the node will fail next. So it is a balance between two ends — fading an outage that has ended reasonably fast, without being thrown by a single sporadic failure. 45 minutes is the choice: a node that failed for 54 minutes and then recovered should not still be paying for it an hour later.

**How many checks it is worth.** The weights sum to `1/(1 − c) ≈ τ/Δt`, so it is roughly worth the last `τ/Δt` checks: about 45 at a `1m` interval, 22 at `2m`, 9 at `5m`. The longer the interval, the fewer checks the same `τ` covers and the coarser the estimate, so `τ` cannot be too short.

**How it differs from the recent share.** While the failure rate is constant, `R` ends up close to it and agrees with the recent share; they part company only when the rate changes — the recent share follows `τ_F = 5 minutes` down quickly, while `R` decays as `exp(−t/45)`. With `τ = 45 minutes` and a check every minute, a node that had been clean (`R ≈ 0`) that then fails 10 times in a row, and succeeds from then on:

| Minutes of success after | Recent share `FAILW / FAILW_TOTAL` (10 samples) | `R` |
| --- | --- | --- |
| 0 | `1.000` | `0.199` |
| 5 | `0.269` | `0.178` |
| 10 | `0.000` | `0.160` |
| 45 | `0.000` | `0.073` |
| 90 | `0.000` | `0.027` |

The table assumes enough history before it, so the prior has long faded: the first row is `1 − exp(−10/45)`, and each row after multiplies by `exp(−Δt/45)`. The first row is a full window of 10 failures, `FAILW / FAILW_TOTAL = 1`; the denominator is the sum of weights when every check in the window failed. At minute 5 the earliest 5 failures have left the window; the remaining 5 weigh about 1.28, the window's total weight is about 4.77, and the share is about 0.27. At minute 10 the successful checks have pushed them all out and the recent share is zero. `R` needs over an hour to settle back — which is why `FAILR` is used instead of the recent share alone.

**How much it matters.** It discriminates far less than delay does, and mostly breaks ties when delays are close, separating "fast but drops" from "slightly slow but steady". It does not overturn "the fast one is still fastest".

### Tolerance Band: Noise Margin

The band is user intent plus measurement noise. The intent is `tolerance`; the noise term takes only the best node's typical variation `JIT_best`. Why that choice, and why `idle` does not put the error of `BAD` into the band, is below.

#### Why the Margin Uses `JIT_best`

At comparison time the candidate is unknown, and so is how well it has been measured, so assume it is measured as well as the best one. For `quick`, each node brings a reading error of `JIT_best/√n`; the difference is `√2` times as uncertain, and two standard errors are allowed:

```
margin_quick = 2 × √( (JIT_best/√n)² + (JIT_best/√n)² )
             = 2√2 × JIT_best/√n
```

For `idle`, substituting the error of `PRES` gives the term used in `band` above:

```
margin_idle = 2√2 × 1000 × 1.2533 × JIT_best / (FLOOR · √n)
```

`quick` takes `JIT` directly as `noise_best`, so the first expression has no `1.2533`; the `PRES` error of `idle` carries `1.2533/FLOOR`.

Why `JIT_best`:

- **Not `DEV_best`.** `DEV` is the average deviation, in which one spike only takes its own share; estimating the margin from it would make a node that "stalls once in a while" look steadier than it is. `JIT` is the median deviation, which a few outliers cannot move.
- **Not the full score noise.** It contains squared deviations, so one slow probe inflates it; widen the band and clearly worse nodes come in too. This is a safe trade, not a mathematical lower bound.

The margin is **absolute**: it does not scale with the score, so a node constantly queueing 10% and a node that never queues get the same width. That is also what makes the shared `50ms` meaningful — both scores live in the tens to hundreds of milliseconds, so the configured value and the actual band are on the same ruler. More samples narrow the margin, as with `quick`. `FLOOR` is itself less uncertain than `MED`, because it is gathered over several windows.

#### Why the `idle` Band Does Not Use `BAD`

`BAD` enters the score but not the band. Two reasons:

The first is what a band is for. It asks "could this node be as good as the best one", and its purpose is not to lose a good node over measurement noise. The deviation of `PRES` is sampling noise, symmetric, and one slow check does not make the node slow — being lenient with it is right. `BAD` does not ask about speed, it asks **whether the node breaks**, and the two mistakes cost differently: letting through a node that stalls costs real bad requests; wrongly dropping a good node costs one candidate, and there are thirty. So for an uncertain frequency, ask for evidence rather than relax the bar. **Uncertainty must not make a node more likely to be chosen.**

The second is how it behaves on small samples. `BAD` is a share, and its binomial standard error `√(BAD(1−BAD)/ALL)` equals zero exactly when `BAD` is zero (the failure part, being age-weighted, does the same). Put it in the band and the width depends on whether the best node happened to hit one bad check this round, rather than on how well it was measured — which is plainly wrong.

The error of `BAD` is not lost; it is used in hysteresis, where the subject is a known node, the goal is not to be interrupted by one fluctuation, and the question is "how much worse could it be" — the opposite. The two places estimate separately and share nothing.

### Hysteresis: Noise Estimates

The previous section said that the error of `BAD` does not enter the band; hysteresis has to decide whether to keep a known node, so it needs the full score error. First estimate the noise of one node, then the difference of two.

#### Noise of One Node

A standard error says how much an estimate usually moves when you measure again. For a mean, the sample standard deviation over `√n` estimates it; for a median, and for anything measured from an estimated centre, the distribution matters too. These are the approximations used here:

```
SE(score) ≈ √( (1.2533·sd(x)/√n)² + (1.858·sd(d)/√n)² )                     # quick
SE(score) ≈ 1000 · √( (1.2533·sd(x)/(FLOOR·√n))² + 0.5²·BAD(1−BAD)/ALL )   # idle
```

`d` is each delay's deviation from `MED`. The two terms of `quick` are the errors of `MED` and of `JIT`; `idle`'s two terms are the errors of `PRES` and of `BAD`. `FLOOR` is constant within a round and its bias is systematic, so it stays out of the standard error. The band above uses `JIT_best`, while hysteresis uses `sd(x)`; the two scales differ and should not be mixed. These are approximations, not exact standard errors; the sections below explain where they come from. The single-node `SE` is only the input: the real decision compares two nodes, see the two-node comparison later.

#### `quick`: The Error in `MED`

`quick` scores `MED + JIT`. Start with `MED`: another batch of checks moves the median too. How far depends on how crowded it is around the median — with the samples bunched there, the median does not move easily; with them sparse, a few samples changing can move it far.

Let `f(m)` be the probability density around the population median `m`, which can be read as "how densely delays land near the median". With enough samples the variance of the median is approximately:

```
Var(MED) ≈ 1 / (4n·f(m)²)
```

For a normal distribution `f(m) = 1/(σ√(2π))`, which gives:

```
Var(MED) ≈ (π/2)·σ²/n
SE(MED)  ≈ √(π/2)·σ/√n
          ≈ 1.2533·sd(x)/√n
```

The sample `sd(x)` estimates the population standard deviation `σ`. So `1.2533 = √(π/2)` is the factor under a **normal approximation**; another distribution needs its own derivation.

#### `quick`: Estimating the Error in `JIT`

Even knowing the true centre `m`, `JIT` moves with whichever samples were drawn. Let `q` be the population median of `|X−m|` and `g(q)` the density of the absolute deviation around `q`. With the centre known, the distribution symmetric and enough samples:

```
SE(median(|X − m|)) ≈ 1 / (2g(q)√n)
SE(JIT) ≈ 1.4826 / (2g(q)√n)
```

Intuitively, the denser the absolute deviations around `q`, the less the median moves and the smaller the error. If the centre also has to be estimated from the same samples, or the distribution is skewed, those effects have to be accounted for as well.

Here `1.858·sd(d)/√n` is used for this term, where `1.858 = 1.4826 × 1.2533`. That amounts to applying the normal-median factor to `d`; but even when `x` is normal, the absolute deviation `d` is half-normal, so `1.858` is not a derived standard-error coefficient. It is an estimate, not a guarantee.

#### `quick`: Combining the Errors in `MED` and `JIT`

When the errors of two quantities are independent, the variance of their sum is the sum of their variances; in general a covariance term is added:

```
Var(MED + JIT) =
    Var(MED) + Var(JIT) + 2·Cov(MED, JIT)
```

`MED` and `JIT` are computed from the same samples and can in principle be correlated. Under a symmetric IID distribution they are asymptotically independent, so the covariance is approximately ignored here; with finite samples or another distribution that need not hold. The combined noise estimate is:

```
SE(score) ≈ √( SE(MED)² + SE(JIT)² )
```

So the coefficients and the variance combination in `quick` depend on the distribution and sample assumptions above; this `SE` is an estimate, not a 95% interval.

#### `idle`: The Errors in `PRES` and `BAD`

`idle` ranks by `score = 1000 × (PRES + 0.5 × BAD)`, and what has to be estimated is how much that sum usually moves when the batch of checks is redone.

**`FLOOR` is a constant within one round.** It is gathered from the per-slot minima and is a fixed value once the round is written; subtracting it subtracts a constant, dividing by it divides by a constant. So each ratio only varies with what is inside the window:

```
Var(PRES) = Var(MED / FLOOR) = Var(MED) / FLOOR²
```

A round can move `FLOOR` **down** (when a faster check shows up), and then `PRES` is measured against `MIN` and carries a little of `MIN`'s noise as well. But that only happens when a record is broken, in one direction, and is not the per-round jitter, so the noise model stays centred on `MED`.

The error of `FLOOR` itself is another matter, and it is **systematic**: a minimum reads low, so every node's `PRES` reads high in the same direction, and no amount of sampling averages it away — it cannot enter a standard error, only be understood and allowed for, see the earlier `FLOOR` section.

**The variance of `MED`.** As with `quick`, `SE(MED) ≈ 1.2533·σ/√n`, where `σ` is the variation of a single check. It has two estimators: `sd(x)` is the sample standard deviation, which is what the definition of a standard error uses, and `JIT` is the median deviation times `1.4826`, the robust estimator on the same scale that a spike cannot move. Hysteresis uses the first, the margin uses the second, see the tolerance band section.

**Together, that is the error of `PRES`** (the margin then swaps `sd(x)` for `JIT`, the shape unchanged):

```
SE(PRES) ≈ SE(MED) / FLOOR ≈ 1.2533 · sd(x) / (FLOOR · √n)
```

This is an **absolute** error and does not scale with the score: a node constantly queueing 10% and a node that never queues share the same error scale — they differ only in `MED`, and the error depends only on `σ`, `FLOOR` and `n`.

**The error of `BAD`.** Its slow-check part is a share of `ALL` attempts, and its failure part is `FAILW / FAILW_TOTAL`, where each check carries an age weight no greater than 1. Together it is still estimated as a binomial proportion:

```
SE(BAD) ≈ √( BAD(1 − BAD) / ALL )
```

The failure part's denominator is the sum of weights of all checks in the same window; age weighting makes the effective sample `(Σw)² / Σw²` smaller than `ALL`, so using `ALL` understates the error. The band is dominated by the jitter term, so this simplification stays. The error does not depend on `σ`, only on `BAD` and the number of attempts; when `BAD` is zero it is zero too, which is exactly why it can go into hysteresis but not into the band. The denominator is all attempts rather than successes: failures are already in the numerator, and a smaller denominator would narrow the error, while a node with many failures is the one whose error should be large.

Both terms come from the same window and are strictly correlated; here the variances are added and the correlation is taken as zero:

```
SE(score) ≈ 1000 · √( SE(PRES)² + 0.5² · SE(BAD)² )
```

That is the noise estimate used by **hysteresis**. The band uses only the `PRES` term; its margin is in the tolerance band section.

#### How `SD` and `SE` Relate

`SE` and `SD` are easy to confuse, so distinguish them first. `SD` (standard deviation) describes how a random quantity itself fluctuates; `SE` (standard error) describes how an estimate fluctuates under repeated sampling. A score is an estimate computed from samples, so its standard deviation is its standard error. `SE` is used here for the approximation to that error.

The node's score and the best node's score are already fixed values in the current window; calling them random quantities is asking "if this were measured again, how far apart could they be".

#### Comparing Two Nodes: How Uncertain Is the Difference?

The band margin has already used the difference of two nodes; hysteresis faces a known node, so its `SE` is the full score error, not just `PRES`. The question is how much the difference between a node's score and the best score moves when everything is measured again. Treat the two scores as random quantities:

```
S_node   = the node's score as a random quantity
S_best   = the best node's score as a random quantity
D        = S_node − S_best
Var(D)   = Var(S_node) + Var(S_best)
           − 2·Cov(S_node, S_best)
```

When the errors of the two nodes are independent the covariance is zero. If they share a probe target or a stretch of line it may not be, and it is ignored here. Under independence, substituting the estimates above and taking the square root:

```
Var(D) ≈ SE_node² + SE_best²
SD(D)  ≈ √(SE_node² + SE_best²)
```

`SD(D)` is how much the difference moves when measured again; the `SE`s in it are estimates, not exact values.

Hysteresis uses this `SD(D)`:

```
noise     = 2 × SD(D)
allowance = band + min(band, noise)
```

`2` is `hysteresisSigma` in the code: two standard errors, the empirical scale for "this difference is still unclear"; it is not a strict 95% guarantee. `min(band, noise)` caps the extra at one `band`: the `SE` of `idle` is often larger than the band's noise term, and without the cap the allowance would be very wide; with it, a node is kept at most two bands away from the best.

Only nodes of the same status are compared by score; a node that has fallen to a worse class (say, from qualified to alive) is not kept. A node with fewer than two successes has no estimable error, so the code takes its `SE` as the whole score, and `noise` then usually hits the `band` cap.

### Distribution Assumptions and Resampling

The coefficients above come from a normal approximation, and the measured delays are not normal: skewness is usually 3–4, excess kurtosis in the teens, and the tail is thick and long. The formulas are therefore only approximations — on this data they overstate the `quick` noise by about 15%, the `idle` noise by about 28%, and the `JIT` term by about 70%.

To avoid assuming a distribution, one can **resample** (bootstrap): draw the window's checks with replacement 128 times, price the whole score on each draw, and take the standard deviation of those 128 scores. Whether that works can be checked by generating data from a known distribution and noise: with the true noise known, the ratio of the resampled estimate to it has a median of 0.8–1.1 across normal, lognormal, exponential, uniform, triangular `f(x)=x`, Cauchy and heavy-tailed mixtures. It costs about 20µs per node per ranking rebuild (about 0.7ms for 35 nodes); the current implementation does not use it and keeps the closed form.

## Debugging

To check these estimates against your own lines and load, ask the API directly (URL-encode a non-ASCII group name):

```bash
curl -s 'http://127.0.0.1:9090/private/health/Name/windows?ahead=2&configs=1:5,1:10,2:5'
```

`rho` compares the ranking of all nodes; `agreement` compares membership of the pool — load balancing takes connections from the whole pool, so the same pool is the same pool, and who ranks first inside it does not matter. A longer window does not necessarily give a higher `rho`: it mixes the node's earlier state into the prediction of the next few checks. `agreement` rewards stability, and `reaction` and `recovery` report the price of that stability. A window needs `samples × stride + ahead` checks to be measured, while a group keeps only `sampling` by default. To change the sample count and retention in place, without a restart and without losing collected checks:

```bash
curl -X PUT 'http://127.0.0.1:9090/private/health/Name/history?sampling=15&extra=60'
```

`sampling` is the number the algorithm uses; omit it to keep the current value. `extra` is the number kept beyond the sample count, for diagnostics only; omit it or set it to `0` to keep only the sample count.

Parameters

| Parameter | Default | Description |
| --- | --- | --- |
| `ahead` | `8` | how many checks after the window it must predict |
| `configs` | the default grid | a list of `stride:samples`, e.g. `1:15,2:10`. `stride` is how many checks to skip between samples; `samples` is the number of samples in the window |

Fields

| Field | Description |
| --- | --- |
| `interval` | the actual spacing of the check records (median), not the configured value |
| `nodes` | the number of nodes measured |
| `checks` | how many checks the longest history holds |
| `ahead` | how many checks after each window it was asked to predict |
| `stride` | how many checks to skip between samples, `1` for consecutive |
| `samples` | the number of samples in the window; the span is `interval × stride × samples` |
| `span` | the actual width of the window |
| `windows` | how many positions the window was placed at in the history; the other fields are averages over them |
| `rho` | the rank correlation between the order the window produces and the order of the checks after it: `1` is identical, `0` is guessing. It saturates and loses resolution, so use it only as a hint when the history is short |
| `delta` | the standard error of `rho`; with few `windows` it collapses to `0`, which means no precision |
| `pool` | how many nodes the band let into the pool on average for that window |
| `agreement` | the Jaccard similarity between the pool the window selects and the pool selected by the checks after it: `1` is the same set, `0` is no overlap |
| `exact` | the share of positions where the two pools are identical; `agreement` counts partial overlap as similar, `exact` only accepts an exact match |
| `reaction` | after a node gets slower, how many checks the window takes to remove it from the pool; `ahead + 1` means it is still in the pool at the end of the horizon, and the field is absent when there are no `reactions` |
| `reactions` | how many times in the history a node "was in use and then got slower"; failures do not count, because a failure is removed at that check |
| `recovery` | the same in the other direction: after a node recovers, how many checks it takes to re-enter the pool. The horizon is the window width (`samples × stride`), leaving enough room for the near-term weight to decay; it is usually faster than the old window because it does not wait for `sampling` failures to leave. Not back within the horizon is recorded as "horizon + 1"; the field is absent when there are no `recoveries` |
| `recoveries` | how many times in the history a node "was in use, was removed, and then recovered" |
| `note` | when no window can be measured, how much is still missing |
