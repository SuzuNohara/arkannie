#!/bin/sh
# claude-stub.sh — test stand-in for the claude CLI (internal/spawn tests).
# Behavior is selected with the STUB_MODE env var:
#   echo (default) — print received argv as a JSON array on stdout, exit 0
#   hang           — ignore SIGTERM and sleep 60s (tests the SIGKILL path);
#                    writes $$ to $STUB_PIDFILE if set, so tests can verify
#                    the process group is dead after the kill.
#   fail           — print a message to stderr and exit 3
case "${STUB_MODE:-echo}" in
hang)
	trap '' TERM
	if [ -n "${STUB_PIDFILE:-}" ]; then
		printf '%s\n' "$$" >"$STUB_PIDFILE"
	fi
	sleep 60
	exit 0
	;;
fail)
	echo "stub: simulated failure" >&2
	exit 3
	;;
*)
	out="["
	sep=""
	for a in "$@"; do
		esc=$(printf '%s' "$a" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
		out="${out}${sep}\"${esc}\""
		sep=","
	done
	printf '%s]\n' "$out"
	exit 0
	;;
esac
