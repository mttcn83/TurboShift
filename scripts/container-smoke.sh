#!/bin/sh
# Runs INSIDE a disposable container (see scripts/distro-test.sh). It exercises a
# clean install of the turbo binary on that distribution. It never needs, and
# does not use, access to the host: the container has no extra capabilities and
# /sys is mounted read-only by the container runtime.
#
# Environment: BIN (turbo binary), TESTS (dir of compiled go test binaries),
#              PTY (pty smoke driver).
fail=0
pass() { echo "  PASS  $1"; }
bad()  { echo "  FAIL  $1"; fail=1; }

# run DESCRIPTION WANT_EXIT CMD... : checks the exit code, and that nothing panicked
run() {
  desc=$1; want=$2; shift 2
  "$@" >/tmp/out 2>/tmp/err; got=$?
  if grep -qE 'panic:|goroutine [0-9]+ \[|SIGSEGV' /tmp/out /tmp/err; then bad "$desc (PANIC)"; head -5 /tmp/err; return; fi
  if [ "$got" = "$want" ]; then pass "$desc"; else bad "$desc (exit $got, want $want)"; head -5 /tmp/err | sed 's/^/        /'; fi
}
has() { # has DESCRIPTION PATTERN : last command's stdout+stderr contains PATTERN
  if grep -qE "$2" /tmp/out /tmp/err; then pass "$1"; else bad "$1 (missing /$2/)"; head -5 /tmp/out | sed 's/^/        /'; fi
}

. /etc/os-release 2>/dev/null
echo "== ${PRETTY_NAME:-unknown distro} | uid $(id -u) | kernel $(uname -r) | $(uname -m)"

# ---- clean install ----
if [ "$(id -u)" = 0 ]; then dest=/usr/local/bin; else dest=$HOME/bin; fi
install -Dm755 "$BIN" "$dest/turbo" && pass "install to $dest" || { bad "install"; exit 1; }
export PATH="$dest:$PATH"
export XDG_CONFIG_HOME=$(mktemp -d) XDG_STATE_HOME=$(mktemp -d) XDG_DATA_HOME=$(mktemp -d)

run "turbo --version"                0 turbo --version;           has "version string" '^turbo [0-9]'
run "turbo --help"                   0 turbo --help;              has "usage text" 'Usage: turbo'
run "no arguments, no terminal"      0 turbo </dev/null;          has "falls back to help" 'Usage: turbo'
run "unknown command is a usage error" 2 turbo frobnicate

# ---- read-only diagnosis works even with nothing to find ----
run "doctor"                         0 turbo doctor
run "doctor --json"                  0 turbo doctor --json;       has "valid-looking JSON" '"findings"'
run "doctor has no ANSI when piped"  0 turbo doctor;              ! grep -q "$(printf '\033')" /tmp/out && pass "no escape codes in piped output" || bad "escape codes in piped output"
run "status"                         0 turbo status
run "tweaks list"                    0 turbo tweaks list;         has "lists power.profile" 'power.profile'
run "tweaks list --json"             0 turbo tweaks list --json;  has "json rows" '"ID"'
run "tweaks show"                    0 turbo tweaks show power.profile
run "snapshots list (empty)"         0 turbo snapshots list;      has "friendly empty state" 'No restore points'
run "snapshots recover (nothing)"    0 turbo snapshots recover
run "benchmark list (empty)"         0 turbo benchmark list;      has "friendly empty state" 'No runs yet'
run "benchmark find"                 0 turbo benchmark find
run "game options"                   0 turbo game options --gamemode --mangohud; has "launch string" 'gamemoderun mangohud %command%'
run "network (local only)"           0 turbo network;             has "consent text" 'This run will contact'

# ---- unsupported hardware must be explained, never crash or half-apply ----
run "apply on unsupported system fails cleanly" 1 turbo apply power.profile=performance --yes
has "explains why" 'not available|unavailable|not installed|no power'
run "dry-run never writes"           1 turbo apply power.profile=performance --dry-run
run "bad apply syntax"               2 turbo apply nonsense
run "unknown tweak"                  2 turbo apply nope.tweak=1
run "path-like snapshot id refused"  1 turbo snapshots restore ../../etc/passwd
[ -z "$(ls -A "$XDG_STATE_HOME"/turboshift/snapshots 2>/dev/null | grep -v '^lock$')" ] && pass "failed apply left no restore points" || bad "unexpected restore points"

# ---- privileged helper boundary ----
if [ "$(id -u)" != 0 ]; then
  run "helper refuses non-root"      1 turbo __priv write-sysfs /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor performance
  has "says why" 'must run as root'
else
  run "helper refuses non-allowlisted path (as root)" 1 turbo __priv write-sysfs /etc/passwd x
  has "denied by validation" 'refused by validation'
  run "helper refuses unknown operation" 1 turbo __priv exec /bin/sh x
fi

# ---- interactive TUI in a real pseudo-terminal ----
if [ -x "$PTY" ]; then
  "$PTY" "$(command -v turbo)" big "key:s" "wait:3500" "dump:scan" "key:3" "dump:optimizer" "key:8" "dump:settings" "key:q" "wait:500" >/tmp/out 2>/tmp/err
  has "TUI draws the app"            'TURBOSHIFT'
  has "TUI shows a health panel"     'Health'
  has "TUI optimizer lists tweaks"   'Power profile'
  if grep -q 'STILL RUNNING' /tmp/out; then bad "TUI did not quit on q"; else pass "TUI quits on q"; fi
  "$PTY" "$(command -v turbo)" small "key:o" "wait:3500" "dump:opt" "key:<esc>" "key:q" "wait:500" >/tmp/out 2>/tmp/err
  has "80x24 layout keeps the buttons" 'Optimize My PC'
  if grep -q 'STILL RUNNING' /tmp/out; then bad "TUI (80x24) did not quit"; else pass "TUI (80x24) quits"; fi
else
  echo "  SKIP  TUI (no pty driver)"
fi

# ---- the project's own test-suite on this distribution ----
cd /tmp || exit 1
tfail=0; tn=0
for t in "$TESTS"/*.test; do
  tn=$((tn+1)); name=$(basename "$t" .test)
  if "$t" -test.count=1 >/tmp/tout 2>&1; then :; else tfail=$((tfail+1)); echo "  FAIL  go tests: $name"; tail -15 /tmp/tout | sed 's/^/        /'; fi
done
[ "$tfail" = 0 ] && pass "unit tests: $tn packages" || { bad "unit tests: $tfail of $tn packages failed"; }

[ "$fail" = 0 ] && echo "== RESULT: OK" || echo "== RESULT: FAILED"
exit $fail
