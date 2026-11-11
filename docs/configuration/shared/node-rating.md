# Node Rating

### Goals: Absolute Speed and Relative Quality

There are two objectives a group can score a node by:

- `quick`: **How fast is it usually?** A 170ms node is simply slower than a 40ms node. This is absolute time, in milliseconds.
- `idle`: **How far is it from its own best?** A node that usually takes 200ms and stays at 600ms through the evening may not look slow, but it is running at four times its best and holds up every bit of traffic at that time.

The units differ, and that matters. `quick` compares actual delay, `idle` compares degradation. Measure degradation with actual delay and a distant node is always at a disadvantage: you end up comparing distance instead of condition. So `idle` uses ratios throughout, and stays independent of distance.

### Statistics: From Successful Checks Only

A failed check carries no delay — it says nothing about how fast the node is when it works. Failures and the total count are recorded separately, to decide whether a node qualifies and to price failures for `quick`.

Let `x₁ … xₙ` be the delays of the successful checks in the window, `n` their count, and `dᵢ = |xᵢ − MED|`:

```
MED     = median(x₁ … xₙ)         # the delay of the window, as a median
JIT     = 1.4826 × median(dᵢ)     # how much it usually moves
DEV     = (1/n) × Σdᵢ             # how far it strays on average
```

Those three describe the current window only, and are recomputed whenever it moves. A few more values live across rounds:

```
SLOT     = the day cut into 48 slots of 30 minutes
SLOT[k]  = the fastest check seen in slot k
FLOOR    = min(the slots of the last 24 hours)  # how fast it can be at its best
FAILR    = the recent failure rate, older checks weigh less  # outside the window, see below
PRES     = max(0, MED − FLOOR) / FLOOR      # how much longer than its own best it usually waits, as a ratio
BAD      = #{xᵢ > 2 × FLOOR} / ALL + max(FAIL / ALL, FAILR)  # the share that missed
```

These formulas assume at least two successful checks. With only one, there is nothing to say about variation, so `JIT` and `DEV` both take half of that delay — better to assume variation than to read "not enough samples" as "no variation at all". At that point `FLOOR` is that single check, and both ratios are out of reach, so `idle` ranks by the delay itself (halved). With no successful check at all, there are no delay statistics.

- `SLOT` follows the wall clock: `k` is which 30 minutes of the day it is. A check fills the slot it was measured in, and only the smallest one in the slot is kept. After 24 hours the same slot is overwritten by the next record, and the old value is gone on its own.
- **`MED`** is the delay level. A median rather than an average, so one very slow check cannot pull it.
- **`JIT`** is **how much the delay usually moves**. A few spikes do not move it; `1.4826` is the factor that converts a median deviation into a standard deviation under a normal distribution.
- **`DEV`** is **how much it moves overall**. Every check counts, and spikes add up only when there are many. It shows up in reports to help diagnose, and takes no part in ranking.
- **`FLOOR`** is **the fastest check ever seen**, with no smoothing at all. One lucky probe lowers it and makes the node look perpetually queued; in return it reacts immediately to change.
- **`PRES`** is **how deep** the queueing is: how much longer than its own best it usually waits, as a ratio. Dividing by its own floor cancels the propagation distance, so near and far nodes are comparable.
- **`BAD`** is **how often** it queues: the share of checks above twice its floor, plus the share of failures. It is compared with its own floor too, so distance does not matter; one extreme check counts once. The failure term is the larger of the measured share and `FAILR`.
- **`FAILR`** is the **long-run failure rate**. Old records weigh less and less, so it describes how things have been lately; it ignores the window, and a window that happens to be clean does not reset it.

### Scores: Delay and Queueing

The two objectives have different formulas, because a failure sits in a different place in each.

`quick` scores how much longer this node usually makes you wait: successful checks priced as successes, failures as failures, weighted by their shares. Let `ALL` be the number of checks, `FAIL` the number of failures, and `TIMEOUT` the health check timeout:

```
base    = MED + JIT
cost    = min(TIMEOUT, base)
failure = min(TIMEOUT, 3 × cost)
p       = max(FAIL / ALL, FAILR)
score   = (1 − p) × cost + p × failure
```

A node with lower delay and less variation scores lower. A failure costs at most three successes and is capped at `TIMEOUT`. This is a scoring rule, not a measurement of how long a retry really took — price every failure at the full timeout and a single one buries every node that did not fail.

`idle` scores the depth plus the frequency of queueing, and a failure counts only in the frequency:

```
score = 1000 × (PRES + 0.5 × BAD)
```

`0.5` is the weight of the frequency, and `1000` turns a dimensionless ratio into a millisecond scale, so both objectives score in the same unit and can share one `tolerance`:

| Objective | Score | `tolerance` default | Meaning |
| --- | --- | --- | --- |
| `quick` | `MED + JIT` (ms) | `50ms` | 50ms apart is not worth switching |
| `idle` | `1000 × (PRES + 0.5 × BAD)` | `50ms` | 5% apart is not worth switching |

The same `50ms` means "not worth switching for that little" in both. Logs and reports are in milliseconds too, so there is one unit to remember, not two.

**Why a failure counts only in the frequency.** A failure has no delay to compare, so it cannot enter the depth. Give it `TIMEOUT` as its delay and one failure in thirty checks lifts `PRES` by 0.8 (about 800ms), wiping out every real difference in queueing. **The delay of queueing and the cost of failing are simply not convertible into each other.**

There is a conversion, but a light one: every 10% of `BAD` adds 50ms, so **a 10% failure rate is worth roughly 5% of constant queueing**. That is far lighter than the three-times penalty of `quick` — for `quick` the score *is* a delay, and a failure not priced at the timeout is drowned out; for `idle` the score is a ratio, and "failed once" and "was slow once" are the same kind of thing, so counting it once is enough.

`idle` needs at least two successful checks for both ratios. With only one, `FLOOR` is that check, both ratios are zero, every node ties at the lowest score and the group just picks at random; for that round `idle` falls back to ranking by delay. If every check failed, the successful delay is unknown and both objectives score `TIMEOUT`; if no check has run yet, there is no score.

### Noise: How Uncertain a Score Is

A score comes from a finite number of checks, not from "what this node really is"; when comparing, take the measurement error into account.

Check again and both the score and the ranking can move. Here `n` is the number of successes, while the failure share is over `ALL`.

#### Noise of One Node

A standard error says how much an estimate usually moves when you measure again. For a mean, the sample standard deviation over `√n` estimates it; for a median, and for anything measured from an estimated centre, the distribution matters too. These are the approximations used here:

```
SE(score) ≈ √( (1.2533·sd(x)/√n)² + (1.858·sd(d)/√n)² )                     # quick
SE(score) ≈ 1000 · √( (1.2533·JIT/(FLOOR·√n))² + 0.5²·BAD(1−BAD)/ALL )     # idle
```

They are approximations, not exact standard errors. They serve one purpose: telling whether the node in use has really fallen behind, see [Hysteresis](#keeping-existing-connections-hysteresis). The [tolerance band](#selecting-nodes-the-tolerance-band) asks something else — whether two nodes can be told apart — so the algorithm differs: the `quick` band uses `JIT`, while `idle` uses only the `PRES` term. Why, see [Why the `idle` Margin Is Calculated This Way](#why-the-idle-margin-is-calculated-this-way).

The two terms of `quick` are the errors of `MED` and of `JIT`. `idle`'s two terms come from different places:

- **The error in `PRES`.** `FLOOR` is an old value kept across windows, and nothing in this window moves it, so dividing by it leaves the error of `PRES` as the error of `MED` divided by `FLOOR`. And since `FLOOR` is the historical fastest, its noise is a shift in one direction, which no amount of sampling averages away — so it stays out of the standard error, see [Derivations](#derivations).
- **The error in `BAD`.** `BAD` is a share — how many of `ALL` attempts missed — so it is estimated as a binomial proportion, `√(BAD(1−BAD)/ALL)`. The denominator is all attempts rather than successes, because failures are already counted in this term. Both terms come from the same checks and are strictly correlated; treating them as independent is a simplification.

These estimates assume the checks are independent of each other and that the distribution stays roughly stable within the window; with few samples or a clear trend they can easily be off.

#### The Noise in a Difference

If the errors of two nodes are unrelated, the variance of the difference is their sum:

```
SD(score_node − score_best) ≈ √(SE_node² + SE_best²)
```

If they share a probe target or a stretch of line, the errors are correlated and a `2·Cov` term belongs in the difference; taking it as zero is a simplification. Note that it is squares added and then rooted — not the two errors added up — and it does not mean the noise is cancelled.

#### Why Noise Cannot Be "Subtracted" from a Score

Noise says how much an estimate moves; it is not a fixed bias, so there is no general number to subtract from a score. The errors also move in their own ways: those of `MED` and `BAD` shrink with `1/√n`, while `FLOOR` does not move at all within the window. So selection uses a **tolerance band** to absorb the uncertainty instead of deducting from the score.

### Selecting Nodes: The Tolerance Band

A score close enough to the best counts as equally good, so the group does not switch back and forth over measurement noise. How close is set by `tolerance`; both objectives default to `50ms`, since both score in milliseconds — 50ms is 50ms for `quick`, and 50ms for `idle` is 5% on the 1000ms scale.

Beyond `tolerance`, the band also makes room for the noise of the measurement itself, or the jitter between two rounds would be read as a difference between nodes. Both scores are in milliseconds, so both objectives use the same shape:

```
band = tolerance + 2√2 × noise_best / √n
cut  = score_best + band
```

`quick` takes `JIT_best` as the noise; `idle` takes `1000 × 1.2533·JIT/FLOOR`, the `PRES` term only — `BAD` enters the score but not the band. Both divide by the square root of the number of successes, and both estimate `σ` from the **typical deviation** `JIT` rather than `sd(x)`: the band's job is to tell nodes apart, and a band widened by one slow probe swallows every node. The factor 2 is a rule of thumb, not a claim of 95% confidence.

Every node scoring at or below `cut` stays a candidate. The more samples, the smaller the `1/√n` term, so `band` comes closer to `tolerance` — but never below it. With fewer than two successes no noise term is added, and `band` is just `tolerance`.

#### Why the `quick` Margin Uses `JIT_best`

At comparison time the candidate is unknown, and so is how well it has been measured, so assume it is measured as well as the best one:

```
margin = 2 × √( (JIT_best/√n)² + (JIT_best/√n)² )
       = 2√2 × JIT_best/√n
```

Why `JIT_best`:

- **Not `DEV_best`.** `DEV` is the average deviation, in which one spike only takes its own share; estimating the margin from it would make a node that "stalls once in a while" look steadier than it is. `JIT` is the median deviation, which a few outliers cannot move.
- **Not the full score noise.** It contains squared deviations, so one slow probe inflates it; widen the band and clearly worse nodes come in too. This is a safe trade, not a mathematical lower bound.

#### Why the `idle` Margin Is Calculated This Way

`idle` scores two terms, but only the depth decides how wide the band is.

**The error in `PRES`.** `PRES` is `MED` divided by `FLOOR`, minus one, and `FLOOR` is a value kept across windows that no draw in this window changes. Dividing by a constant leaves the relative error alone, so:

```
SE(PRES) ≈ SE(MED) / FLOOR ≈ 1.2533 · σ / (FLOOR · √n)
```

What has to be estimated is `σ`, the variation of a single check. That is taken from `JIT`, not `sd(x)`: **the band's job is to discriminate**, and `sd(x)` is inflated by one slow probe until every node counts as "about as good".

**`BAD` stays out of the band.** It still enters the score, but the band is only the error of `PRES` plus `tolerance`. Two reasons:

The first is what a band is for. It asks "could this node be as good as the best one", and its purpose is not to lose a good node over measurement noise. The deviation of `PRES` is sampling noise, symmetric, and one slow check does not make the node slow — being lenient with it is right. `BAD` does not ask about speed, it asks **whether the node breaks**, and the two mistakes cost differently: letting through a node that stalls costs real bad requests; wrongly dropping a good node costs one candidate, and there are thirty. So for an uncertain frequency, ask for evidence rather than relax the bar. **Uncertainty must not make a node more likely to be chosen.**

The second is how it behaves on small samples. `BAD` is a share, and its binomial standard error `√(BAD(1−BAD)/ALL)` equals zero exactly when `BAD` is zero. Put it in the band and the width depends on whether the best node happened to hit one bad check this round, rather than on how well it was measured — which is plainly wrong.

The error of `BAD` is not lost; it is used in [hysteresis](#keeping-existing-connections-hysteresis), where the subject is a known node, the goal is not to be interrupted by one fluctuation, and the question is "how much worse could it be" — the opposite. The two places estimate separately and share nothing.

The margin is **absolute**: it does not scale with the score, so a node constantly queueing 10% and a node that never queues get the same width. That is also what makes the shared `50ms` meaningful — both scores live in the tens to hundreds of milliseconds, so the configured value and the actual band are on the same ruler. More samples narrow the margin, as with `quick`. `FLOOR` is itself less uncertain than `MED`, because it is gathered over several windows.

### Keeping Existing Connections: Hysteresis

With interrupting existing connections enabled, a node can keep its open connections for a while after it is replaced. A node that just left the band is not cut off as long as the part beyond the band is still within the noise:

```
noise     = 2 × √(SE_node² + SE_best²)
allowance = band + min(band, noise)
```

This compares the node in use with the best one, so each brings its own noise estimate; they are assumed independent, which may not hold when they share a probe target or a line. `min(band, noise)` caps the extra at one `band`, so a node is kept at most **two bands** away from the best. Hysteresis is there to ride out measurement noise, not to keep a clearly worse node around.

For `idle`, the `SE` estimates the error of `PRES` from the full `sd(x)`, while the band is computed from `JIT`; the two can differ by an order of magnitude, so `min(band, noise)` lands on `band` and the allowance is **two bands**. That is exactly what the cap is for — the two numbers answer different questions: how narrow the band must be to tell nodes apart, and how far a known node's score might be.

`quick` and `idle` differ a little: the `quick` noise comes from the spread of the delays together with the spread of the absolute deviations, and gets better with more samples; the `idle` noise combines `PRES` (through the error of `MED`) with `BAD` (a binomial share), and the band uses only the former, because hysteresis wants to know how much worse the node might be. The second term goes to zero when `BAD` is zero, so a perfectly clean node gets its error from `PRES` alone.

#### Single-Node Selection in `urltest`

`urltest` picks one node at a time and has no connection retention on top, so hysteresis goes straight into the selection: the node in use is kept while its score is no more than `Limit(best)`, which tolerates at most **one band**.

Connection retention and node selection are two separate decisions: one decides whether to cut existing connections, the other which nodes new connections may use.

### Design Choice: Why Not Rank by Standard Deviation

The sample standard deviation is the usual measure of spread, but it does not work as a node's variation score, for three reasons:

1. **It is centred on the mean, and the mean is dragged by outliers.** Nine of ten checks at 450ms and one at 3.15s moves the mean from 450ms to 720ms, and the other nine suddenly carry a 270ms deviation — though none of them changed.
2. **It squares the deviations, so one sample can take over.** In the same example that single check accounts for nine tenths of the variance; what comes out describes that probe, not the node's usual state.
3. **Rooting it does not bring it back to the right scale.** A spike of size `X` among `n` samples contributes `X/n` to the average deviation, which matches the share of connections it slows; it contributes `X/√n` to the standard deviation, already more than three times as much at `n = 10`, and the gap grows with the window.

`DEV` still measures deviation from the median, but as a linear average, so no spike moves the centre. A node alternating between 200ms and 800ms still has a large `DEV`; this is not ignoring instability, it is refusing to let one sample decide. Today it is used in reports and not in ranking.

Both ratios of `idle` are linear or counted, with no squares: `PRES` is a ratio, `BAD` counts occurrences, and one extreme value counts once. They do not measure "how much it swings within the window" but "how far below its own best it is, and how often". The two things can show the same spread in a window and still mean different things: a node alternating between 200ms and 800ms has a median near 500 with a floor still at 200, so `PRES` is about 1.5 and `BAD` about half; a node steady at 800ms has a floor of 800 too, so both ratios are zero — it has never been faster, so the slowness is not queueing. **`idle` picks the latter.** A steady slowness is a known cost that `max_rtt` can handle; a delay that comes and goes is not, because there is no way to plan around traffic that is fast and slow by turns.

The standard deviation is not banished: `quick` still uses it to estimate score noise, it just is not the ranking score itself.

### Design Choice: Why Trends Are Not Part of the Score

The samples in a window have an order, yet none of the statistics above look at it: shuffle them however you like and `MED`, `PRES`, `BAD`, `JIT` and `DEV` come out the same. So a node that has been getting worse for ten minutes reads exactly like a node that has always been that way, as long as the distribution matches.

`PRES` and `BAD` cover part of it, but differently: they do not compare earlier with later inside the window, they compare **now with the historical best**. A node going from 200ms to 600ms has `MED` at 600 at once and `FLOOR` still at 200, so `PRES` jumps to 2.0 without waiting for a trend to build; a node going back from 600ms to 200ms has `FLOOR` drop to 200 with it and `PRES` returns near zero at once, with no grudge from the past. **So direction needs no separate machinery: `FLOOR` only moves down, an improvement is recognised immediately and a deterioration is paid immediately.**

Comparing the two halves of a window (`median(second half) − median(first half)`) does add information and is cheap to compute, and it can tell "just started getting worse" from "has been worse for a while". It is not in the score because the moment it is, changing `interval` changes the ranking. A ranking should describe the node, not how often it happens to be checked. A trend is a decision, like tolerance, not a measurement.

It also has to be used with care: at `sampling` 10, two bad samples in a row cannot move the median of the five in the second half — the third is the first that shows. That makes it slower than "the latest check", which is already in the statistics.

Window length is the same trade: while the delay distribution holds roughly steady, a longer window is smoother and less noisy; but old samples weigh more, and the score follows a node that changes more slowly. A shorter window follows faster and is more easily pushed around by chance. Less noise and keeping up with change are the two ends of one choice of window.

### Design Choice: Why the Baseline Is a Ring of Slots

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
slot[k]  = min(slot[k], MIN)               # a new slot starts from MIN, then keeps its minimum
FLOOR    = min(the slots of the last 24 hours)
PRES     = max(0, MED − FLOOR) / FLOOR
BAD      = (#{xᵢ > 2 × FLOOR} + FAIL) / ALL
```

Writing first does not zero the score: a slot holds a minimum, so after the write `FLOOR ≤ MIN ≤ MED`, `PRES` is still a non-negative ratio and `BAD` still a share. Computing first and writing after would only delay a node that got faster by one round, for nothing.

**Expiring.** There is no separate expiry step: after 24 hours the same slot is overwritten by the next round and the old record is gone with it. So the baseline always comes from times **actually measured**, and never treats present congestion as a fastest level the way "reset to the current value on expiry" would. A slot that stays congested rises slowly while the others stay as they were; only congestion that lasts everywhere raises the baseline as a whole, which amounts to admitting "this node is simply this slow".

**Sudden changes.** While a node is steady or changes slowly, the baseline moves by one slot's worth per round, because only one slot of 30 minutes is replaced. For a node that gets 144ms slower each day: expiring everything at once is a 144ms jump, while with slots the change spreads over 48 updates in a day, about 11ms each. **A sudden change — a new line, a new machine — still jumps**: when the last old reading is overwritten, the minimum has to fall to the next one, and the jump is the size of the change itself. That is the price of forgetting old data, not noise that smoothing could remove.

**Starting up.** The first round writes its fastest check, so `FLOOR` is `MIN` from the start and needs no fallback. The floor then fills one slot at a time and takes 24 hours to cover a day. With the cache file on, a restart carries on with the whole day of slots and the long-run failure rate; without it (not enabled, the target or path changed, the record expired) a restart starts from zero as it always did. While it fills, each slot covers fewer times of day and the floor tends to read high — the cost of forgetting old data; the ranking still works, see [Why Both Parts Are Ratios](#design-choice-why-both-parts-are-ratios).

### Design Choice: Why Only the Floor and the Failure Rate Are Cached

Only two pieces of state live outside the window: the baseline and the long-run failure rate. The window holds `sampling` checks (`sampling × interval`), while one of them looks back 24 hours and the other `τ = 45` minutes. A restart is therefore very lopsided: the window rebuilds in minutes, `FLOOR` waits for the next daily trough, and `FAILR` has no evidence until the node **fails again**.

The samples of the window are not kept — twenty minutes rebuilds them, while holding on to those hundreds of checks would take two orders of magnitude more space.

**A record belongs to a path and a node, not to a check.** What is kept is the node as that path reaches it: a different target or chain of relays keeps its own records, while two checks with the same target and relays share the records of the nodes they share. That sharing is accurate enough — on one path, sampling twice a window or a hundred times a day gives every node the same floor in the end, and the two differ by noise rather than by origin. A writer only touches the nodes it measured, and what comes back describes the node itself.

A record only matters for 24 hours, which is how long the floor lives; records older than that are dropped, so a node that is no longer checked does not hold a place forever.

### Design Choice: Why the Threshold Is Two and the Weight Is 0.5

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

### Design Choice: Why Both Parts Are Ratios

`idle` answers "how far is this node from its own best", and "how far" can be written two ways: as a difference (`MED − FLOOR`) or as a ratio (`(MED − FLOOR) / FLOOR`). The ratio is not a preference; it is the only form that removes distance.

**A difference brings distance back.** A 400ms node with a 380ms floor waits 20ms more; a 900ms node with an 860ms floor waits 40ms more. By the difference the second is worse, but part of those 40ms is simply being further away: at the same amount of queueing, a longer line adds more milliseconds. A difference cannot separate "I am queueing" from "I am simply far", because it carries the units of the floor. The ratio cancels the floor: `PRES` is "the extra wait as a share of its own floor", the same number for a near and a far node at the same queueing, and `BAD` only compares against its own floor, so distance drops out too. Both are dimensionless, which is what "independent of distance" means.

**The cost is real, but it does not scramble the ranking.** A ratio amplifies the noise of a low floor: a small denominator turns the same absolute error into a larger ratio. So the floor must not be wildly wrong per node — which is what the ring of slots is there to ensure. When the floor is wrong **the same way for everyone**, the ranking still holds: a floor off by some factor scales `PRES` by the same factor and recomputes `BAD` against the same floor. That is the essential difference from the difference form, where a floor 10ms off is a 10ms shift and moves a node up or down.

The ranking only moves when the floors of **different nodes** are off by disproportionate amounts, and neither source of error is choosy: one is the systematic optimism of taking a minimum, which pushes every node the same way; the other is which sampled value each node's floor lands on, which depends on that node's own spread, and a narrow spread converges quickly to its true lower end. So while the floor is still filling, the scores read a little optimistic and `tolerance` reads a little stricter; the ranking is unchanged.

**Where the limit is.** The more the floor errs, the worse the ratio form fares: once the denominator is inflated by noise enough, it is no better than the difference. A new machine or a new line invalidates an old floor outright, and that is exactly what the 24 hour ring handles. One coupling is worth noting: a node that varies more has its floor pushed lower and its `PRES` lifted — variation is itself a fault, so the direction agrees with the intent, but it means `PRES` carries some variation information and overlaps with `BAD`. The overlap points the same way, so a quiet node never gains by looking variable.

### Derivations

This section explains the formulas, constants and assumptions behind the noise estimates. First, the notation:

- `n` is the number of successful checks; `xᵢ` is the delay of the `i`th successful check; `MED` is their sample median.
- `sd(…)` is the sample standard deviation, used to estimate how far a set of values usually sits from its centre.
- `Var(…)` is the variance, the square of the size of the fluctuations; `SE` (standard error) is how much an estimate usually moves under repeated sampling, the square root of the variance.
- `SE` here is an approximation, not an exact standard error or confidence interval.

The derivations assume the checks are independent and come from a stable distribution within a sampling window; the analysis of `JIT` also assumes symmetry. Real networks do not always oblige.

#### `idle`: Estimating the Errors of the Two Ratios

`idle` ranks by `score = 1000 × (PRES + 0.5 × BAD)`, and what has to be estimated is how much that sum usually moves when the batch of checks is redone.

**`FLOOR` is a constant within one round.** It is gathered from the per-slot minima and is a fixed value once the round is written; subtracting it subtracts a constant, dividing by it divides by a constant. So each ratio only varies with what is inside the window:

```
Var(PRES) = Var(MED / FLOOR) = Var(MED) / FLOOR²
```

A round can move `FLOOR` **down** (when a faster check shows up), and then `PRES` is measured against `MIN` and carries a little of `MIN`'s noise as well. But that only happens when a record is broken, in one direction, and is not the per-round jitter, so the noise model stays centred on `MED`.

The error of `FLOOR` itself is another matter, and it is **systematic**: a minimum reads low, so every node's `PRES` reads high in the same direction, and no amount of sampling averages it away — it cannot enter a standard error, only be understood and allowed for, see the last step.

**The variance of `MED`.** A median depends only on the order of the samples, so its variance depends on the probability density `f(m)` around the median: `Var(MED) ≈ 1/(4n·f(m)²)`. Under a normal approximation `f(m) = 1/(σ√(2π))`, which gives `(π/2)·σ²/n`, and after the square root:

```
SE(MED) ≈ 1.2533 · σ / √n
```

`σ` is the variation of a single check. It has two estimators: `sd(x)` is the sample standard deviation, which is what the definition of a standard error uses, and `JIT` is the median deviation times `1.4826`, the robust estimator on the same scale that a spike cannot move. Hysteresis uses the first, the band the second, see [Why the `idle` Margin Is Calculated This Way](#why-the-idle-margin-is-calculated-this-way).

**Together, that is the error of `PRES`** (the band then swaps `sd(x)` for `JIT`, the shape unchanged):

```
SE(PRES) ≈ SE(MED) / FLOOR ≈ 1.2533 · sd(x) / (FLOOR · √n)
```

This is an **absolute** error and does not scale with the score: a node constantly queueing 10% and a node that never queues share the same error scale — they differ only in `MED`, and the error depends only on `σ`, `FLOOR` and `n`.

**The error of `BAD`.** It is the share of `ALL` attempts that missed, a binomial proportion:

```
SE(BAD) ≈ √( BAD(1 − BAD) / ALL )
```

It does not involve `σ`, only `BAD` and the number of attempts; when `BAD` is zero it is zero too, which is exactly why it can go into hysteresis but not into the band. The denominator is all attempts rather than successes: failures are already in the numerator, and a smaller denominator would narrow the error, while a node with many failures is the one whose error should be large.

Both terms come from the same window and are strictly correlated; here the variances are added and the correlation is taken as zero:

```
SE(score) ≈ 1000 · √( SE(PRES)² + 0.5² · SE(BAD)² )
```

That is the noise estimate used by **hysteresis**. The band uses the `PRES` term only:

```
band margin = 2√2 · 1000 · 1.2533 · JIT / (FLOOR · √n)
```

**The last step: `FLOOR` is a minimum, so it is optimistic.** There are many probes in a day, one of them is bound to be lucky, and the minimum therefore reads systematically low (the more chances, the lower it goes, which is the nature of an extremum). The cost is that every node's `PRES` is a few percent higher; what it buys is a quantity that **does not lag**.

Recording per-window medians instead would be much quieter, but it answers a different question: "how fast is this node **usually**". In steady periods every node's median equals itself, so every `PRES` is zero and `idle` has nothing to rank. Worse is a real kind of node whose delay rotates between two pools: with two low and two high the median lands exactly between the peaks — a value it has never reached — and that midpoint is stable round after round, so the node is read as the steadiest one there is. A minimum is not fooled by that structure.

The bias is **shared**, so it does not change the ranking: both nodes read optimistic by the same factor, and their difference in `PRES` is still the difference in reality. It only shifts the zero point, that is, how strict `tolerance` is.

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

#### Comparing Two Nodes: How Uncertain Is the Difference?

The question is how much the difference between a node's score and the best score moves when everything is measured again. Treat the two scores as random quantities:

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

#### How `SD` and `SE` Relate

`SD` (standard deviation) describes how a random quantity itself fluctuates; `SE` (standard error) describes how an estimate fluctuates under repeated sampling. A score is an estimate computed from samples, so its standard deviation is its standard error. `SE` is used here for the approximation to that error.

`score_node` and `score_best` are already fixed values in the current window; calling them random quantities is asking "if this were measured again, how far apart could they be".

#### The Long-Run Failure Rate

`max_fail` defaults to `0`, so only nodes with no failure in the current window become candidates. That makes `FAIL / ALL` zero in both score formulas: the failure term only acts after a node has been excluded, and says nothing about the risk a candidate still carries.

But a clean window does not mean the next check will pass. Nodes fail in very different ways: one kind **flickers**, with failures scattered across its history and more always possible; another has **long outages**, one continuous drop followed by a long clean stretch. Their historical failure rates can look alike, yet once the window is clean the flickering node fails again much sooner. A window of ten checks cannot tell them apart.

So `FAILR` is kept across rounds, and the larger of it and the measured share enters the score, see `p` and `BAD` above.

**Why decay by elapsed time instead of by count.** A count-based rate forces `sampling` and `extra` to specify how many checks to keep, and at an interval of `1m` versus `5m` the same count covers five times the span — a difference that then leaks into the score. With time there is no such problem: two 1 minute intervals and one 2 minute interval decay the same over the same span, and changing `interval` does not change what the estimate means. It is also unaffected by changing the sampling at runtime, since resampling the window does not reset it.

**Why not a whole day.** A daily failure rate mixes the evening peak with the quiet hours, while queueing and dropping vary by time of day. A `τ` decay follows the recent past: the evening peak shows up as the evening peak, undiluted by a day's average.

**How long `τ` should be.** Data gives no clear answer: widening the range many times over barely helps in predicting whether the node will fail next. So it is a balance between two ends — fading an outage that has ended reasonably fast, without being thrown by a single sporadic failure. 45 minutes is the choice: a node that failed for 54 minutes and then recovered should not still be paying for it an hour later.

**How much it matters.** It discriminates far less than delay does, and mostly breaks ties when delays are close, separating "fast but drops" from "slightly slow but steady". It does not overturn "the fast one is still fastest".

`FAIL / total` is an **equal-weight average**: every check in the window weighs the same, and leaving the window drops its weight from `1/n` straight to `0`. So it jumps at the window boundary, and the same count means a different span at different intervals.

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

**How it differs from `FAIL / total`.** While the failure rate is constant, `R` ends up close to it and agrees with `FAIL / total`; they part company only when the rate changes — one jumps at the window boundary, the other decays as `exp(−t/τ)`. With `τ = 45 minutes` and a check every minute, a node that had been clean (`R ≈ 0`) that then fails 10 times in a row, and succeeds from then on:

| Minutes of success after | Window `FAIL / total` (10 samples) | `R` |
| --- | --- | --- |
| 0 | `10/10 = 1.000` | `0.199` |
| 10 | `0/10 = 0.000` (a jump) | `0.160` |
| 45 | `0/10 = 0.000` | `0.073` |
| 90 | `0/10 = 0.000` | `0.027` |

The table assumes enough history before it, so the prior has long faded: the first row is `1 − exp(−10/45)`, and each row after multiplies by `exp(−Δt/45)`. The window stops recording the outage at minute 10, while `R` needs over an hour to settle back — which is why `FAILR` is used instead of `FAIL / ALL` alone.

**How many checks it is worth.** The weights sum to `1/(1 − c) ≈ τ/Δt`, so it is roughly worth the last `τ/Δt` checks: about 45 at a `1m` interval, 22 at `2m`, 9 at `5m`. The longer the interval, the fewer checks the same `τ` covers and the coarser the estimate, so `τ` cannot be too short.

### Debugging

To check these estimates against your own lines and load, ask the API directly (URL-encode the group name if it is not ASCII):

```bash
curl -s 'http://127.0.0.1:9090/private/health/Name/windows?ahead=2&configs=1:5,1:10,2:5'
```

`rho` compares the ranking of all nodes, `agreement` compares the members of the pool — load balancing takes connections from the whole pool, so an identical pool is identical and which member ranks first does not matter. A longer window does not necessarily raise `rho`: it mixes a node's earlier state into the prediction of the checks that follow. `agreement` rewards stability, and `reaction` and `recovery` report what that stability costs. A window needs "`samples × stride` + `ahead`" checks to be measurable, while a group keeps only `sampling` of them by default. To adjust the sampling and the retained count in place, without a restart and without losing the checks already collected:

```bash
curl -X PUT 'http://127.0.0.1:9090/private/health/Name/history?sampling=15&extra=60'
```

`sampling` is what the algorithm reads, left out it keeps the current value; `extra` is what is kept beyond that, for diagnostics only, left out or `0` keeps the sampling alone.

Parameters

| Parameter | Default | Meaning |
| --- | --- | --- |
| `ahead` | `8` | how many checks after the window it is asked to predict |
| `configs` | default grid | a list of `stride:samples`, e.g. `1:15,2:10`. `stride` is how many checks apart the samples are taken, `samples` how many are in the window |

Fields

| Field | Meaning |
| --- | --- |
| `interval` | the actual spacing of the recorded checks (median), not the configured value |
| `nodes` | how many nodes were measured |
| `checks` | how many checks the longest history holds |
| `ahead` | how many checks after the window each window was asked to predict |
| `stride` | how many checks apart the samples are taken, `1` is consecutive |
| `samples` | the samples in the window, whose span is `interval × stride × samples` |
| `span` | the actual width of the window |
| `windows` | how many positions in the history the window was placed at; the other fields are averages over them |
| `rho` | the rank correlation between the order the window produced and the order the following checks produced: `1` is identical, `0` is guessing. Once saturated it has no resolution and is only a reference for short histories |
| `delta` | the standard error of `rho`; it collapses to `0` with few `windows`, which means no precision at all |
| `pool` | how many nodes the band let in, on average, at that window |
| `agreement` | the Jaccard similarity between the pool of the window and the pool of the checks after it: `1` is the same set, `0` is no member in common |
| `exact` | the share of positions where the two pools are identical; `agreement` counts partial overlap as similar, `exact` only identical pools |
| `reaction` | how many checks the window needs to drop a node from the pool after it gets slower; `ahead + 1` means still in the pool at the end of the horizon, absent when there are no `reactions` |
| `reactions` | how often in the history a node was in use and then got slower; failures do not count, since a failure is dropped from the pool in the same check |
| `recovery` | the same in the other direction: how many checks after a node recovers it re-enters the pool. The horizon is the width of the window (`samples × stride`), because the failure has to leave the window first; not back within it is recorded as "horizon + 1", absent when there are no `recoveries` |
| `recoveries` | how often in the history a node was in use, dropped, and then recovered |
| `note` | when no window can be measured, how much is still missing |
