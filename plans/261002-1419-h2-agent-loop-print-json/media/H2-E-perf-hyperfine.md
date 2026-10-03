| Command | Mean [ms] | Min [ms] | Max [ms] | Relative |
|:---|---:|---:|---:|---:|
| `./ask --mode json < ../big1m.txt > /dev/null` | 46.0 ± 2.0 | 42.5 | 56.4 | 1.92 ± 0.11 |
| `./ask-trunk -p < ../big1m.txt > /dev/null` | 24.0 ± 0.9 | 22.6 | 26.8 | 1.00 |

./ask --mode json < ../big1m.txt > /dev/null: median 45.6 ms
./ask-trunk -p < ../big1m.txt > /dev/null: median 23.8 ms
ratio of medians 1.91 (rule: 3x, 1 s absolute)
