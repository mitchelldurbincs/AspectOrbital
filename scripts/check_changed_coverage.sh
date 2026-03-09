#!/usr/bin/env bash
set -euo pipefail

profile="${1:-coverage.out}"

# Minimum per-file coverage targets for changed logic files.
thresholds=(
  "cmd/beeminder-spoke/goal_math.go=70"
  "cmd/beeminder-spoke/reminder_engine.go=80"
  "cmd/finance-spoke/scheduler.go=15"
  "cmd/finance-spoke/formatter.go=85"
  "cmd/finance-spoke/state_store.go=80"
  "cmd/accountability-spoke/http.go=35"
)

awk -v targets="${thresholds[*]}" '
BEGIN {
  split(targets, pairs, " ");
  for (i in pairs) {
    split(pairs[i], kv, "=");
    wanted[kv[1]] = 1;
    min_cov[kv[1]] = kv[2] + 0;
  }
}
NR == 1 { next }
{
  split($1, loc, ":");
  file = loc[1];
  sub(/^personal-infrastructure\//, "", file);
  if (!(file in wanted)) next;

  stmts = $2 + 0;
  count = $3 + 0;
  total[file] += stmts;
  if (count > 0) covered[file] += stmts;
}
END {
  failed = 0;
  for (file in wanted) {
    t = total[file] + 0;
    c = covered[file] + 0;
    pct = (t == 0 ? 0 : (100 * c / t));
    printf "%s %.2f%% (%d/%d), target=%d%%\n", file, pct, c, t, min_cov[file];
    if (pct < min_cov[file]) {
      failed = 1;
      printf "Coverage check failed for %s: %.2f%% < %d%%\n", file, pct, min_cov[file] > "/dev/stderr";
    }
  }
  exit failed;
}
' "$profile"
