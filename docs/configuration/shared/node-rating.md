# Node Rating

## Formulas

Scoring:

```sh
ALL         = the number of checks in the window
MED         = median(x₁ … xₙ)
dᵢ          = |xᵢ − MED|                     # deviation of check i from the median
JIT         = 1.4826 × median(dᵢ)
DEV         = (1/n) × Σdᵢ
SLOT        = the day cut into 48 slots of 30 minutes
SLOT[k]     = the fastest check seen in slot k
FLOOR       = min(the slots of the last 24 hours)  # how fast it can be at its best
fᵢ          = 1 failed / 0 succeeded         # failure indicator of check i
wᵢ(τ)       = exp(−(t − tᵢ) / τ)             # weight by age
S(τ)        = Σ wᵢ(τ)·fᵢ                     # weighted failures
W(τ)        = Σ wᵢ(τ)                        # weighted checks
F(τ)        = S(τ) / W(τ)                    # weighted failure rate, 0…1
FAIL_NEAR   = S(τ_N)                         # near-term failure weight (absolute), what the gate compares
FAILR_NEAR  = F(τ_N)                         # recent failure rate, in the window, τ_N = 5 minutes
FAILR_LONG  = F(τ_L)                         # long-run failure rate, across windows, τ_L = 45 minutes
FAILR       = max(FAILR_NEAR, FAILR_LONG)    # failure rate, the larger of the two horizons
PRES        = max(0, MED − FLOOR) / FLOOR
BAD         = min(1, #{xᵢ > 2 × FLOOR} / ALL + FAILR)
COST        = MED + JIT
FAIL_COST   = min(TIMEOUT, 3 × COST)

idle_score  = 1000 × (PRES + 0.5 × BAD)
quick_score = (1 - FAILR) × COST + FAILR × FAIL_COST
```

Tolerance band (the noise of a score is its reading error, taken at the best-scoring node):

```sh
n         = the number of successful checks
SCORE_NOISE ≈ JIT / √n                                         # quick
SCORE_NOISE ≈ 1000 × 1.2533 × JIT / (FLOOR × √n)               # idle
BAND        = tolerance + 2 × √(SCORE_NOISE² + SCORE_NOISE²)   # the candidate is unknown, assumed as accurate as the best
            = tolerance + 2√2 × SCORE_NOISE                    # no noise term below two successes
```

Hysteresis:

```sh
sd(x)       = √(Σ(xᵢ − mean(x))² / (n − 1))            # sample standard deviation, what a standard error is estimated from
sd(d)       = √(Σ(dᵢ − mean(d))² / (n − 1))            # standard deviation of the deviations
SCORE_SE    = √( (1.2533 × sd(x)/√n)² + (1.858 × sd(d)/√n)² )                          # full score noise of quick
SCORE_SE    = 1000 × √( (1.2533 × sd(x)/(FLOOR × √n))² + 0.5² × BAD × (1 − BAD)/ALL )  # full score noise of idle
NOISE_DIFF  = 2 × √(SCORE_SE_node² + SCORE_SE_best²)   # error of the difference of two scores (2 × SD(D))
ALLOWANCE   = BAND + min(BAND, NOISE_DIFF)             # how far hysteresis lets a node exceed the band, at most two bands
```

## Scoring

There are two objectives a group can score a node by:

- `quick` compares actual delay: a 570ms node is simply slower than a 400ms node.
- `idle` compares degradation: a node that is usually 200ms and 400ms at the evening peak — 400ms may still be lower than a distant node — is already running at twice its own time. So an `idle` score is a ratio in essence, with nothing to do with distance.

`SLOT` follows the wall clock: `k` is which 30 minutes of the day it is. A check fills the slot it was measured in, and only the smallest one in the slot is kept. After 24 hours the same slot is overwritten by the next record, and the old value is gone on its own.

**`MED`** is the delay level. A median rather than an average, so one very slow check cannot pull it.

**`JIT`** is **how much the delay usually moves**. A few spikes do not move it. It takes the median of the absolute deviations as "typical variation", then multiplies by `1.4826` to put it on the same scale as a standard deviation:

```sh
Φ = the cumulative distribution function of the standard normal distribution
q = median(|X − m|)
P(|X − m| ≤ q) = 1/2
```

In a symmetric normal distribution, "within `q` of `m`" covers half the probability, so from the mean to `m+q` is a quarter, that is `Φ(q/σ) = 0.75`. Hence:

```sh
q      = Φ⁻¹(0.75) · σ ≈ 0.6745 · σ
σ      ≈ q / 0.6745 ≈ 1.4826 · q
1.4826 = 1 / Φ⁻¹(0.75)
```

That is, for normal data the median deviation is about `0.6745σ`, and multiplying by `1.4826` brings it back to the scale of a standard deviation. It is a conversion constant under normality; it does not mean real delays are normally distributed.

**`DEV`** is **how much it moves overall**. Every check counts, and spikes add up only when there are many. It shows up in reports to help diagnose, and takes no part in ranking.

**`FLOOR`** is **the fastest check ever seen**.

**`PRES`** is **how deep** the queueing is: how much longer than its own best it waits, as a percentage. Dividing by its own floor cancels the propagation distance.

**`F(τ)` is the weighted failure rate**: each failure contributes `exp(−(t − tᵢ) / τ)` by age, 1 at the moment it happens and decaying to 0; the sum is divided by the total weight.

**`FAILR_NEAR`** is **the recent failure rate**, that is `F(τ_N)`: the failure rate in the window, with ages decaying at `τ_N`.

**`FAILR_LONG`** is **the long-run failure rate**, that is `F(τ_L)`, with ages decaying at `τ_L`. It and `FAILR_NEAR` are two horizons of the same `F(τ)`: `FAILR_NEAR` covers the last few minutes and decides whether the node can be used right now; `FAILR_LONG` covers tens of minutes and decides whether it is still worth using later.

**`BAD`** is **how often** it queues: the share of checks above twice its floor, plus the share of failures. It is compared with its own floor too, so distance does not matter; one extreme check counts once.

---

Why exponential weighting for the weighted failure rate?

The weight depends only on elapsed time, and two spans can be joined: `w(a + b) = w(a) · w(b)` — `a` minutes followed by `b` minutes must equal `a+b` minutes at once.

> This property is also what lets the weighted failure rate cache only its result, without keeping the whole history.

Among continuous functions that are not identically zero, only the exponential satisfies this, so `w(t) = exp(−t/τ)`. Switch to linear decay, step decay, or a sliding window of fixed count, and the same `τ` no longer means the same span at `1m` and at `5m`.

The near-term failure weight is still tied to eligibility, and the gate is fixed: `FAIL_NEAR < 1` is a necessary condition for a node to qualify.
A fresh failure weighs 1, so the node is immediately disqualified; on the next successful check, if its weight has already fallen below 1, the node returns to the candidates.

It is not simply "the last check": only a single failure comes back on the next round, while consecutive failures add up. At a 2 minute interval, two failures in a row still weigh `0.670 + 0.449 = 1.119` on the next success and need one more round; three in a row take about 4 minutes after the last failure, and nine take about 6 minutes, to fall back below the gate.
It answers "is it still failing now", an order of magnitude shorter than `FAILR_LONG`. The first failure still demotes the node immediately, so the peak-time reaction is unchanged; a sporadic failure in a quiet period decays on its own if it does not recur. There is no need to recognise peak hours: whether the failure continues is the best signal.

The long-run failure rate `FAILR_LONG` answers a different question: among the qualified candidates, which one is more likely to fail.
These candidates have had no full failure in the last few minutes, but `FAILR_NEAR` in the score can still be non-zero: a node that has just recovered still carries the remaining weight, and the score keeps pricing it down.
The `τ` of `FAILR_LONG` can only balance two ends — fading an outage that has ended reasonably fast, without being thrown by a single sporadic failure. 45 minutes is the choice: a node that failed for 54 minutes and then recovered should not still be paying for it an hour later.
`W` starts at 10, the weight of "ten clean checks": a cold start is "never seen failing", not "seen failing". Starting from zero, one failure would put `F(τ_L)` at `1.0` — right after startup, before the network is ready, every node's first check fails, and every node would carry a near-perfect estimate for an hour. The prior decays with time as well, so with enough history it can be ignored. With the cache file on, the restored history takes its place.
It discriminates far less than delay does, and mostly breaks ties when delays are close, separating "fast but drops" from "slightly slower but steady"; it does not overturn "the fast one is still fastest".

## Tolerance Band

A score close enough to the best counts as equally good, so the group does not switch back and forth over a bit of measurement noise. "Close enough" has two parts:

1. `tolerance`: the difference the user is willing to switch for.
2. The measurement error of the score: the score comes from a finite number of checks, and a different batch of checks would move it. This is the estimation error of the score, not the jitter of a single check.

The only way to measure the noise of a single check is to redo that same check many times in parallel universes.
A measurement is the true delay plus one random noise, so after enough repeats the scale of the noise, `σ`, can be estimated from the population variance.

```sh
X         = μ + ε                        # the delay of one check
μ         = the true delay at this moment
ε         = the random noise of this one check
Xᵢ        = the value in universe i, random across universes
σ²        = Var(X) = Var(ε)              # the imagined population variance of one check
```

Parallel universes are not available, so one check cannot be redone many times. All that can be done is many checks within the same time window,
estimating `σ` from their spread `sd(x)²` — and that holds only if the scale of the fluctuation stays the same inside the window, so that the checks are samples of one `σ`.

```sh
xⱼ        = the observed value of check j in the real window
mean      = (1/n) · Σxⱼ                     # sample mean
sd(x)²    = (1/(n−1)) · Σ(xⱼ − mean)² ≈ σ²  # σ² estimated from the sample variance
```

To derive the error of the mean, imagine redoing all `n` checks of the window together in parallel universes, `i` being the universe number.
`Xᵢⱼ` is the delay of check `j` in universe `i`; summing over `j` gives `sumᵢ` of universe `i`.
Every universe has a different `sumᵢ`, and the differences between universes are the combined effect of these `n` noises.
For three checks, universe A might get 500 + 520 + 510 = 1530 and universe B 480 + 540 + 500 = 1520;
`Var(sumᵢ)` measures exactly this spread across universes.
The checks are independent, so the variances simply add, which gives `nσ²`.
The `x₁ + … + xₙ` actually measured in our world is the value of one universe, a fixed number with no variance.

The capital `X` is "a value that could be measured", the small `x` is "a value that has been measured": `Xⱼ` is the random delay of check `j`,
`Xᵢⱼ` is its value in universe `i`, and `xⱼ` is the number it actually landed on in this round (our universe).
`Var(sumᵢ)` and `Var(meanᵢ)` take the fluctuation across universes, so they use the random-variable form;
`xⱼ` in `sd(x)²` is a number already measured, and the spread it computes is that of the observations in hand.
`mean = (1/n) · Σxⱼ` is the sample mean already computed; as seen as a random quantity that each universe computes for itself, it is what `Var(meanᵢ)` talks about.

```sh
sumᵢ       = Xᵢ₁ + … + Xᵢₙ                 # the sum in universe i
Var(Xⱼ)    = σ²                            # every check comes from the same distribution
Var(sumᵢ)  = Var(X₁ + … + Xₙ)
           = Σ Var(Xⱼ) + 2·Σ Cov(Xⱼ, Xₖ)   # j < k
           = nσ² + 0                       # independent ⇒ every pairwise covariance is 0
           = nσ²
meanᵢ      = sumᵢ / n
Var(meanᵢ) = Var(sumᵢ / n)
           = (1/n²) · Var(sumᵢ)            # Var(aX) = a²·Var(X)
           = (1/n²) · nσ²
           = σ²/n
SE(mean)   = √Var(meanᵢ) = √(σ²/n) = σ/√n
```

A median looks only at ranks, not at values, so it is a little "dumber" than the mean. Intuitively: when a batch is redrawn, where the median lands
depends on how many samples fall below the true median `m` — that count follows `Binomial(n, 1/2)`, with a spread of about `√n/2` samples;
the spacing of neighbouring samples around the median is about `1/(n·f(m))`, where `f(m)` is the probability density at `m`.
Multiplying the two gives the fluctuation of the median:

```sh
# the number of samples below m ~ Binomial(n, 1/2), standard deviation √n/2
# the spacing of neighbouring samples around the median ≈ 1/(n·f(m))
SE(MED)  ≈ (√n/2) × 1/(n·f(m)) = 1/(2·f(m)·√n)
Var(MED) ≈ 1/(4n·f(m)²)

# under normality f(m) = 1/(σ√(2π))
SE(MED)  ≈ σ√(2π)/(2√n) = √(π/2)·σ/√n = 1.2533·σ/√n
Var(MED) ≈ (π/2)·σ²/n
```

`π/2` is exactly that loss of efficiency: the variance of a median is `π/2 ≈ 1.57` times that of the mean, the same precision needs `π/2` times the samples, and the `SE` is `1.2533` times as large.

The band uses only the delay part of the score: `MED` for `quick`, `PRES = MED/FLOOR` for `idle`.
Swapping `σ` for its sample estimate `sd(x)` gives the strict form:

```sh
quick: 1.2533 × sd(x)/√n
idle:  1000 × 1.2533 × sd(x)/(FLOOR × √n)
```

The last step is the simplification in the band: `sd(x)` becomes `JIT`, and `quick` drops `1.2533` as well: what the band needs is a narrow scale that tells nodes apart, not an exact standard error.

```sh
quick: SCORE_NOISE ≈ JIT/√n
idle:  SCORE_NOISE ≈ 1000 × 1.2533 × JIT/(FLOOR × √n)
```

The more samples, the smaller `1/√n`, and the closer `band` is to `tolerance`, but never below it; with fewer than two successes no noise term is added. Every node scoring at or below `cut` stays a candidate.

----

`BAD` enters the score, but not the band. Two layers of reason:

The first layer is what a band is for. It asks "could this node be as good as the best one", and its purpose is not to lose a good node over measurement noise. The deviation of `PRES` is sampling noise, symmetric, and one slow check does not make the node slow — being lenient with it is right. `BAD` does not ask about speed, it asks **whether the node breaks**, and the two mistakes cost differently: letting through a node that fails costs real bad requests; wrongly dropping a good node costs one candidate, and there are thirty of them.

The second layer is how it behaves on small samples. `BAD` is a share, and its binomial standard error `√(BAD(1−BAD)/ALL)` equals zero exactly when `BAD` is zero (the failure part, being age-weighted, does the same). Put it in the band and the width would depend on whether the best node happened to hit one bad check this round, rather than on how well it was measured — which is plainly wrong.

The error of `BAD` is not lost; it is used in hysteresis: there the subject is a known node, the goal is not to be interrupted by one fluctuation, and the question is "how much worse could it be" — the opposite. The two places estimate separately and share nothing.

The band compares two scores, the candidate and the best, so the error must be computed from two estimates too. Each side has its own `SCORE_NOISE`, they are independent, and the difference is `√2 × SCORE_NOISE` uncertain; allowing two standard errors makes the margin `2√2 × SCORE_NOISE`. There is one simplification here: the band does not know which node will actually be compared, so it can only assume the candidate is measured as well as the best one — the two terms are then equal, a special case of the two-node difference formula.

## Keeping Existing Connections: Hysteresis

When load balancing is allowed to interrupt existing connections, a node that is replaced is not cut off as long as the excess is still within the noise margin. Load balancing allows connections over different lines at the same time, so there is no need to cut one until it is bad enough; otherwise a node entering and leaving the selection would repeatedly break connections. `urltest` allows only one node at a time and has no such logic.

The band section used only the reading noise `SCORE_NOISE` of the delay term, and assumed both sides of the comparison are equally accurate. Hysteresis faces a known node, so it needs the complete version of the same thing: the other terms of the score brought in, and each side counted on its own. That is written `SCORE_SE`.
What follows adds only these two new derivations; `√n`, `1.2533`, and the trade of `sd(x)` against `JIT` were covered in the band section.
Below, `n` is the number of successful checks and `ALL` the number of attempts; `SCORE_SE` is the standard error of the score, that is, how much the score would move if the checks were redone.

The score of `quick` is `MED + JIT`. The error of `MED` is already there; what is new is `JIT`. `JIT` is itself a median, so its error follows the same chain as `MED`, only the subject changes to the deviations `d`:

```sh
dᵢ             = |xᵢ − MED|                    # the deviation of each check from MED
sd(d)          = √( Σ(dᵢ − mean(d))² / (n−1) ) # sample standard deviation of the deviations
JIT            = 1.4826 × median(dᵢ)
SE(median(dᵢ)) = 1.2533 × sd(d)/√n             # the chain of MED, with d as the subject
SE(JIT)        = 1.4826 × SE(median(dᵢ))
               = 1.4826 × 1.2533 × sd(d)/√n
               = 1.858 × sd(d)/√n
```

Intuitively, the tighter `d` is, the steadier `JIT`. Strictly, `d` is computed around the estimated `MED`, so the centre carries an error as well;
treating the centre as known, as here, is an approximation.
`1.858` is an estimate too, not an exact probability guarantee: even if `x` is normal, the absolute deviation `d` is half-normal,
so this coefficient is not a standard-error coefficient derived exactly.

The score of `idle` is `1000 × (PRES + 0.5 × BAD)`. The error of `PRES` needs no new derivation; `BAD` is estimated as a proportion:

```
PRES     = MED / FLOOR                     # FLOOR is a constant within a round
SE(PRES) = SE(MED) / FLOOR
SE(BAD)  = √( BAD × (1 − BAD) / ALL )      # binomial standard error of a proportion
```

`SE(BAD)` does not depend on `σ`, only on `BAD` and the number of attempts; it is zero when `BAD` is zero. Its denominator is all attempts rather than successes: failures are already in the numerator, and shrinking the denominator would narrow the error. The failure part is age-weighted, so its effective sample is `(Σw)² / Σw²`, smaller than `ALL`; using `ALL` here is an approximation.

The bias of `FLOOR` is systematic (a minimum reads low, in the same direction for every node), so it stays out of the standard error. The error of `PRES` is absolute: a node constantly queueing 10% and a node that hardly queues share the same error scale, which depends only on `σ`, `FLOOR` and `n`. A round that breaks a record can move `FLOOR` down, and `PRES` then carries a little of the new minimum's noise; that only happens when a record is broken, in one direction, so the error model stays centred on `MED`.

The score error adds the variances of the terms:

```sh
SCORE_SE_quick = √( SE(MED)² + SE(JIT)² )                  # covariance taken as 0
SCORE_SE_idle  = 1000 × √( SE(PRES)² + 0.5² × SE(BAD)² )   # 0.5 squared, 1000 pulled out of the root
```

The two terms of `quick` come from the same batch of samples, and those of `idle` from the same window, so strictly the covariance could be non-zero; it is taken as 0 here.

Hysteresis compares two nodes, and the difference is `D = S_node − S_best`. Both `S` here are random quantities, "what a re-measurement could give", not the two fixed values in hand. The band section assumed both sides equally accurate and so collapsed into `√2 × SCORE_NOISE`; here each side uses its own `SCORE_SE`. Sharing a probe target or a stretch of line can make the covariance non-zero, and it is taken as 0 here as well:

```sh
SD(D)      = √(SCORE_SE_node² + SCORE_SE_best²)
NOISE_DIFF = 2 × SD(D)     # 2 is the two standard errors of the band section
```

So `NOISE_DIFF` is the error **margin** of the difference, not a standard error of it: that one is `SD(D)`, and `NOISE_DIFF` is twice it.

So the range in which a node is kept is:

```sh
ALLOWANCE = BAND + min(BAND, NOISE_DIFF)
```

The extra `NOISE_DIFF` is capped at one more `BAND`: the `SCORE_SE` of `idle` is often larger than the noise term of the band, and without the cap the kept range would be very wide;
with it, a node is kept at most **two bands** away from the best. Hysteresis is there to ride out measurement noise, not to keep a clearly worse node around for long.

Only nodes of the same status are compared by score; one that has fallen to a worse class is not kept.
A node with fewer than two successes has no estimable error, so the code takes its `SCORE_SE` as the whole score, and `NOISE_DIFF` then usually hits the `BAND` cap.

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

The samples in a window have an order, yet none of `MED`, `PRES`, `JIT` or `DEV` looks at it: shuffle them however you like and they come out the same. So a node that has been getting worse for ten minutes reads exactly like a node that has always been that way, as long as the distribution matches. The slow-check part of `BAD` does not look at order either, but its failure part is age-weighted, so `BAD` distinguishes "failed just now" from "failed long ago" — that is exactly why the recent failure rate exists.

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

**The cost.** The floor is a minimum of per-slot minima — still a minimum, so it leans towards the most optimistic reading. That matches its purpose (an idle level is exactly what is wanted), but it means that once in a while a lucky probe lowers the floor slightly and every node's `PRES` reads a little higher. The bias is **the same for everyone** and does not change the ranking; it only moves how strict `tolerance` is.

**Updating.** Each round writes the slots first and computes scores after, so the round takes effect immediately; computing first and writing after would only delay a node that got faster by one round, for nothing.

**Expiring.** There is no separate expiry step: after 24 hours the same slot is overwritten by the next round and the old record is gone with it. So the baseline always comes from times **actually measured**, and never treats present congestion as a fastest level the way "reset to the current value on expiry" would. A slot that stays congested rises slowly while the others stay as they were; only congestion that lasts everywhere raises the baseline as a whole, which amounts to admitting "this node is simply this slow".

**Sudden changes.** While a node is steady or changes slowly, the baseline moves by one slot's worth per round, because only one slot of 30 minutes is replaced. For a node that gets 144ms slower each day: expiring everything at once is a 144ms jump, while with slots the change spreads over 48 updates in a day, about 11ms each. **A sudden change — a new line, a new machine — still jumps**: when the last old reading is overwritten, the minimum has to fall to the next one, and the jump is the size of the change itself. That is the price of forgetting old data, not noise that smoothing could remove.

**Starting up.** The first round writes its fastest check, so `FLOOR` is `MIN` from the start and needs no fallback. The floor then fills one slot at a time and takes 24 hours to cover a day. With the cache file on, a restart carries on with the whole day of slots and the long-run failure rate; without it (not enabled, the target or path changed, the record expired) a restart starts from zero as it always did. While it fills, each slot covers fewer times of day and the floor tends to read high — the cost of forgetting old data; the ranking still works.

### Why Only the Floor and the Failure Rate Are Cached

Only two pieces of state live outside the window: the baseline and the long-run failure rate. `FAILR_NEAR` is computed from the timestamps of every check in the window and needs no extra storage; so the cache still holds only `FAILR_LONG` as failure state, while `FAILR_NEAR`, `MED` and `JIT` are window statistics that can be rebuilt. After a restart the window is empty and `FAILR_NEAR` starts at zero; `FAILR_LONG` is restored from the cache, so the score still holds down a node that just failed. Making the gate exact across restarts would require storing `FAIL_NEAR` together with a timestamp — two numbers, not a merge of the two horizons. The window holds only `sampling` checks (`sampling × interval`), while one piece looks back 24 hours and the other `τ = 45` minutes. A restart is therefore very lopsided: the window rebuilds in minutes, `FLOOR` waits for the next daily trough, and `FAILR_LONG` has no evidence until the node **fails again**.

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
