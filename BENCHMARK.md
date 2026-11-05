# Benchmark: update checks on a 1.1M-instance database

Three builds on the same database with the same load: **main** (`4f2a710a`), **this PR**, and **this PR with the app
and group ID caches removed** (the change proposed for #1630).

**Setup.** Azure Database for PostgreSQL, 2 vCores (Standard_D2ds_v4), 500-IOPS disk, PostgreSQL 18. One application
with 1.1M instances in 8 groups of 11k to 616k instances. Nebraska with 2 replicas, new pods for every run. A load
generator in the same Kubernetes cluster sends the requests below. Builds run in rotated order, 2 to 5 runs per build
and scenario.

**Scenarios**

* **Regular check-ins.** Instances already on their group's version check in with one request carrying a ping, an
  update check and an event 3/2 with no previous version. Each instance was last seen more than 5 minutes earlier, so
  every check-in is written. 3 minutes at a steady rate.
* **Rollout.** The same check-ins, while 2 or 10 instances per second start an update: the update check that gets it,
  events 13, 14 and 800 in separate requests, a ping, and the first check-in on the new version (event 3/2 with the
  old version as previous version, ping, update check), 2 s apart.
* **Update waves.** Instances that contact Nebraska only to update. Each sends an update check without a ping, then
  events 13, 14 and 800, then a completion request on the new version (event 3/2, ping, update check), 3 s apart.
  10 waves of 50 instances of the 616k group, 30 s apart, run three ways: the instances silent for days; seen 2 hours
  before; and silent while 10k other instances of the group were updated during the last day.
* **API reads.** The app, the app list, the group and channel lists, one group, one channel, one instance.
* **Capacity.** Regular check-ins at increasing rates, 2 runs per rate.

## Results

| | main | this PR | this PR, caches removed |
|---|---|---|---|
| **Regular check-ins, 200/s** | | | |
| latency p50 / p99 | 57 / 1,184 ms | 11 / 22 ms | 15 / 26 ms |
| DB statements per check-in | 27 | 5 | 7 |
| DB CPU per check-in | 5.9 ms | 2.0 ms | 2.3 ms |
| WAL per check-in | 5.1 kB | 4.1 kB | 4.1 kB |
| Nebraska CPU per check-in | 3.5 ms | 1.0 ms | 1.2 ms |
| **Rollout, 200 req/s** | | | |
| 2 updates starting per second | 1% answered within 10 s | all answered, p50 12 ms | all answered, p50 15 ms |
| 10 updates starting per second | 0.4% answered | all answered, p50 12 ms | all answered, p50 15 ms |
| **DB time of one update, update check to completion** | | | |
| in a group of 11k / 132k / 616k active instances | 44 / 312 / 3,235 ms | 5 / 5 / 6 ms | 5 / 6 / 6 ms |
| **Update waves** | | | |
| instances silent for days: update checks that got the update | 0 of 1,500 | 1,500 of 1,500 | 1,500 of 1,500 |
| instances seen 2 hours before: latency p50 | 40 ms | 18 ms | 21 ms |
| instances seen 2 hours before: DB statements per request | 21 | 6 | 8 |
| 10k others updated in the last day: last completion of a wave | 50 ms | 290 ms | 300 ms |
| **API reads, 4 per second, latency p50** | | | |
| group list / channel list | 91 / 42 ms | 18 / 6 ms | 16 / 8 ms |
| one group / channel / instance | 11 / 9 / 9 ms | 4 / 4 / 4 ms | 4 / 4 / 6 ms |
| **Highest steady check-in rate that passes** | 200/s (250 fails) | 600/s (900 fails) | 600/s (900 fails) |

A rate passes when it holds, every answer is the expected one, no pod restarts and p95 stays under 500 ms. At 600/s
the disk is the limit for this PR, at 900/s the CPU.

* main's rollouts collapse because every update check from an out-of-date instance counts the whole group: in the
  616k group each count took about 10 s under this load.
* On main, an update check sent without a ping by an instance not seen for over a day gets no update. This PR fixes
  that.
* main's p99 at 200/s comes from stalls of a few seconds during a brief periodic slowdown of the test cluster; the PR
  builds absorbed the same slowdown with p99 at most about 20 ms higher.
* Upgrading: migration 0026 builds the three new indexes in about 4 s with 1.1M instances, and check-ins wait for it;
  0027 drops the six redundant indexes of #1447 in milliseconds.
