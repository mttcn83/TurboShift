#!/usr/bin/env bash
# Real-kernel round trip: apply -> verify -> drift-aware restore -> forced
# failure and automatic rollback, against the live /proc/sys of a throw-away
# network namespace. The namespace belongs to an unprivileged user namespace,
# so the host's settings are never touched (the script checks that too).
#
#   scripts/kernel-test.sh [path/to/turbo]
#
# Needs `unshare` and unprivileged user namespaces; otherwise it prints SKIP
# and exits 0 so CI on locked-down runners stays green.
set -u
TURBO=$(realpath "${1:-./turbo}")
[ -x "$TURBO" ] || { echo "no executable at $TURBO (run: make build)"; exit 2; }
if ! unshare -Urn true 2>/dev/null; then
	echo "SKIP: unprivileged user+network namespaces are not available here"
	exit 0
fi

hostwatch() { cat /proc/sys/net/ipv4/tcp_ecn /proc/sys/net/ipv4/tcp_mtu_probing /proc/sys/net/ipv4/tcp_notsent_lowat /proc/sys/net/ipv4/tcp_rmem /proc/sys/net/ipv4/tcp_wmem /proc/sys/net/core/rmem_max /proc/sys/fs/inotify/max_user_watches /proc/sys/fs/inotify/max_user_instances; }
host_before=$(hostwatch)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cat >"$work/inner.sh" <<INNER
set -u
export HOME="$work/home" XDG_CONFIG_HOME="$work/c" XDG_STATE_HOME="$work/s" XDG_DATA_HOME="$work/d"
mkdir -p "\$HOME"
t="$TURBO"
fails=0
ok()  { echo "  ok    \$1"; }
bad() { echo "  FAIL  \$1"; fails=\$((fails+1)); }
val() { cat "/proc/sys/net/ipv4/\$1"; }
want() { [ "\$(val "\$1")" = "\$2" ] && ok "\$3" || bad "\$3 (\$1 is \$(val "\$1"), wanted \$2)"; }

ecn0=\$(val tcp_ecn); mtu0=\$(val tcp_mtu_probing); lowat0=\$(val tcp_notsent_lowat)

"\$t" apply net.notsent-lowat=16384 net.ecn=1 net.mtu-probing=1 --yes >/dev/null 2>&1
[ \$? -eq 0 ] && ok "apply exits 0" || bad "apply exits 0"
want tcp_notsent_lowat 16384 "notsent_lowat applied to the live kernel"
want tcp_ecn 1 "ecn applied to the live kernel"
want tcp_mtu_probing 1 "mtu_probing applied to the live kernel"

# Someone else changes one of the settings behind our back.
echo 2 >/proc/sys/net/ipv4/tcp_mtu_probing
id=\$("\$t" snapshots list --json | sed -n 's/.*"id": "\(.*\)",/\1/p' | head -1)
"\$t" snapshots restore "\$id" >"$work/restore.out" 2>&1
[ \$? -eq 3 ] && ok "restore with drift exits 3 (partial)" || bad "restore with drift exits 3"
want tcp_ecn "\$ecn0" "ecn restored exactly"
want tcp_notsent_lowat "\$lowat0" "notsent_lowat restored exactly"
want tcp_mtu_probing 2 "the drifted setting is left alone"
grep -q "changed by something else" "$work/restore.out" && ok "drift is reported" || bad "drift is reported"
"\$t" snapshots restore "\$id" --force >/dev/null 2>&1
want tcp_mtu_probing "\$mtu0" "--force restores the drifted setting"

# A step that the kernel refuses (vm.* is not writable from this namespace)
# must roll back the steps before it.
"\$t" apply net.ecn=1 mem.swappiness=10 --yes >"$work/fail.out" 2>&1
[ \$? -eq 3 ] && ok "failed transaction exits 3" || bad "failed transaction exits 3"
want tcp_ecn "\$ecn0" "earlier step rolled back after the failure"
grep -q "rolled back" "$work/fail.out" && ok "rollback is reported" || bad "rollback is reported"

"\$t" apply net.ecn=1 --yes >/dev/null 2>&1
"\$t" undo --yes >/dev/null 2>&1
want tcp_ecn "\$ecn0" "undo puts the last change back"

# Multi-number settings: the kernel prints them with tabs and the restore
# must put the exact original back.
spaced() { tr '\t' ' ' <"\$1"; }
rm0=\$(spaced /proc/sys/net/ipv4/tcp_rmem); wm0=\$(spaced /proc/sys/net/ipv4/tcp_wmem)
"\$t" apply net.buffers=fast --yes >"$work/buf.out" 2>&1
if [ \$? -eq 0 ]; then
	[ "\$(spaced /proc/sys/net/ipv4/tcp_rmem)" = "4096 131072 16777216" ] && ok "tcp_rmem ceiling raised on the live kernel" || bad "tcp_rmem ceiling raised (is \$(spaced /proc/sys/net/ipv4/tcp_rmem))"
	[ "\$(spaced /proc/sys/net/ipv4/tcp_wmem)" = "4096 16384 16777216" ] && ok "tcp_wmem ceiling raised on the live kernel" || bad "tcp_wmem ceiling raised"
	"\$t" undo --yes >/dev/null 2>&1
	[ "\$(spaced /proc/sys/net/ipv4/tcp_rmem)" = "\$rm0" ] && [ "\$(spaced /proc/sys/net/ipv4/tcp_wmem)" = "\$wm0" ] && ok "buffer limits restored exactly" || bad "buffer limits restored exactly"
else
	# This namespace refuses net.core.*; the point is that the half-applied
	# change is rolled back completely and the next command still works.
	[ "\$(spaced /proc/sys/net/ipv4/tcp_rmem)" = "\$rm0" ] && [ "\$(spaced /proc/sys/net/ipv4/tcp_wmem)" = "\$wm0" ] && ok "a write the kernel refuses is rolled back completely (net.buffers)" || bad "refused write rolled back completely (net.buffers)"
	grep -q "rolled back" "$work/buf.out" && ok "the rollback is reported" || bad "the rollback is reported: \$(tail -3 "$work/buf.out" | tr '\n' ' ')"
fi

w0=\$(cat /proc/sys/fs/inotify/max_user_watches); i0=\$(cat /proc/sys/fs/inotify/max_user_instances)
"\$t" apply sys.watchers=more --yes >"$work/w.out" 2>&1
if [ \$? -eq 0 ]; then
	[ "\$(cat /proc/sys/fs/inotify/max_user_watches)" = 524288 ] && ok "inotify watch limit raised on the live kernel" || bad "inotify watch limit raised"
	"\$t" undo --yes >/dev/null 2>&1
	[ "\$(cat /proc/sys/fs/inotify/max_user_watches)" = "\$w0" ] && [ "\$(cat /proc/sys/fs/inotify/max_user_instances)" = "\$i0" ] && ok "inotify limits restored exactly" || bad "inotify limits restored exactly"
else
	echo "  skip  sys.watchers (not writable from this namespace): \$(tail -2 "$work/w.out" | tr '\n' ' ')"
fi
exit \$fails
INNER

echo "== real-kernel round trip (network namespace)"
unshare -Urn bash "$work/inner.sh"
rc=$?
host_after=$(hostwatch)
if [ "$host_before" = "$host_after" ]; then echo "  ok    host settings untouched"; else echo "  FAIL  host settings changed!"; rc=1; fi
[ $rc -eq 0 ] && echo "== RESULT: OK" || echo "== RESULT: FAILED"
exit $rc
