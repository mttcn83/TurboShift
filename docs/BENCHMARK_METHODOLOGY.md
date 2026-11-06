# Benchmark methodology

The Benchmark Lab answers one question honestly: *did this configuration measurably change performance for this workload?* It does not produce a "score".

## What is measured

TurboShift does not run a synthetic benchmark. You record your real game (or workload) with **MangoHud** and import the CSV log. From the frame times it computes, per run:

- **Average FPS**, time-weighted (total frames / total time), not the mean of per-frame FPS.
- **1% low and 0.1% low FPS**, defined as 1000 / the 99th (99.9th) percentile frame time.
- **Frame-time p50 / p95 / p99** in ms.
- Averages of CPU/GPU load and temperatures when present, and a deliberately hedged *bottleneck hint* ("GPU load is high; the GPU may be the limit").

Imported summaries are stored in `~/.local/share/turboshift/benchmarks/`. Your original log is never modified.

## How a comparison works

You label each run with the configuration it was recorded under (e.g. `balanced`, `performance`) and compare two labels.

1. **The unit of comparison is the run**, not the frame. Frames inside a run are strongly autocorrelated; treating them as independent samples would make everything look significant.
2. For each metric TurboShift reports the **median across runs** of each configuration (robust to one odd run) and the **run-to-run coefficient of variation** (CV).
3. It computes a **two-sided permutation test** on the run values, with the absolute difference of the group means as the test statistic. When there are at most 200 000 ways to split the runs between the two labels, every split is enumerated (an *exact* p-value); beyond that it uses a fixed-seed Monte Carlo of 20 000 random splits, so results are reproducible.
4. A verdict is assigned:

| Verdict | Meaning |
| --- | --- |
| **Measurable improvement / Regression** | p < 0.05 **and** the median difference is at least 1% in the favourable / unfavourable direction. |
| **No statistically clear difference** | Enough runs, but the difference is within normal run-to-run variation. |
| **Inconclusive** | Run-to-run spread above 10% CV: the measurement is too noisy to tell. |
| **Insufficient measurements** | Too few runs for a result of this size to ever reach p < 0.05 with these group sizes. The reason states how many are needed. |

"Statistically significant but tiny" is reported as *not practically meaningful*, not as an improvement.

## How many runs?

The smallest p-value an exact test can reach with *n* runs per configuration is 2 / C(2n, n). It first drops below 0.05 at **4 runs per configuration**; with fewer, no data can prove a difference, and TurboShift says so instead of guessing. 5 or more per side is better.

## Methodology warnings

The comparison warns when runs differ in kernel, GPU, CPU, OS or program, when all of configuration A was recorded before all of B (thermal and cache drift can masquerade as a configuration effect: interleave them, e.g. A B B A A B B A), and when no warm-up was trimmed (`--trim-start`).

## Recording good data

- Same scene, same route, same duration (60-120 s of repeatable gameplay), same settings and resolution.
- Alternate configurations: A B A B A B …
- Let the machine cool to a similar temperature between runs; plug in AC for both.
- Record in the Gaming screen: *Record a MangoHud log* writes `output_folder`, `autostart_log=10` (skips 10 s of loading), `log_duration`.
- Close other heavy programs.

## Limits

Statistics cannot rescue a bad experiment. A "No clear difference" result means *this workload did not show a benefit*, not that the tweak is useless everywhere.
