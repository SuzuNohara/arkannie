#!/usr/bin/env bash
# arkannie — PATH shim that execs the compiled arkannie binary.
#
# Installed via `make install` as $PREFIX/bin/arkannie (see Makefile).
# The compiled interpreter lives at $ARKANNIE_HOME/bin/arkannie.
#
# ARKANNIE_HOME resolution:
#   1. If the ARKANNIE_HOME environment variable is set, it is used verbatim.
#      During installation the user may export ARKANNIE_HOME pointing at the root
#      of the arkannie repository (the directory that contains bin/arkannie).
#   2. Otherwise ARKANNIE_HOME is derived from the real location of this script,
#      following symlinks — the shim is installed as a symlink/copy in
#      $PREFIX/bin while the binary stays in the repo, so we resolve the
#      canonical script path and take its parent's parent ($ARKANNIE_HOME/bin/<shim>).
set -euo pipefail

if [[ -z "${ARKANNIE_HOME:-}" ]]; then
	# Resolve the real path of this script, following symlinks.
	source="${BASH_SOURCE[0]}"
	while [[ -h "$source" ]]; do
		dir="$(cd -P "$(dirname "$source")" >/dev/null 2>&1 && pwd)"
		source="$(readlink "$source")"
		# If the symlink target is relative, resolve it against $dir.
		[[ "$source" != /* ]] && source="$dir/$source"
	done
	script_dir="$(cd -P "$(dirname "$source")" >/dev/null 2>&1 && pwd)"
	# The shim lives in $ARKANNIE_HOME/bin, so ARKANNIE_HOME is its parent.
	ARKANNIE_HOME="$(cd -P "$script_dir/.." >/dev/null 2>&1 && pwd)"
fi

# Export so the interpreter (which reads ARKANNIE_HOME to locate .agents, .mem and
# .output) receives it even when derived here rather than inherited from the env.
export ARKANNIE_HOME

arkannie_bin="$ARKANNIE_HOME/bin/arkannie"

if [[ ! -x "$arkannie_bin" ]]; then
	echo "arkannie binary not found at $arkannie_bin — run 'make build' or set ARKANNIE_HOME" >&2
	exit 1
fi

exec "$arkannie_bin" "$@"
