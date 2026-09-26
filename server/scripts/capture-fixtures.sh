#!/usr/bin/env bash
# Regenerate server/test/fixtures/*.jsonl from the installed pi.
#
# Every fixture is a real capture of `pi --mode rpc` stdout: one LF-terminated JSON
# record per line, preceded by a single `#` header naming the exact command, the pi
# version, the capture date and whether the file is a real capture. pi runs offline
# (--offline --no-extensions, no model server, no network), and the script reads each
# capture until the expected record appears instead of sleeping, so the procedure is
# deterministic: only the header date and pi's per-run session id/timestamps differ
# between two captures of the same fixture. stderr is never part of a fixture.
#
# Usage: server/scripts/capture-fixtures.sh          (uses `pi` from PATH)
#        PI_BIN=/path/to/pi server/scripts/capture-fixtures.sh
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
fixtures_dir="$repo_root/server/test/fixtures"
bridge="$repo_root/bridge/pi-ui-bridge.ts"
pi_bin="${PI_BIN:-pi}"

if ! command -v "$pi_bin" >/dev/null 2>&1; then
	echo "capture: pi not found: $pi_bin (set PI_BIN)" >&2
	exit 1
fi

pi_version=$("$pi_bin" --version)
capture_date=$(date -u +%Y-%m-%d)
work=$(mktemp -d /tmp/piui-capture-XXXXXX)
trap 'rm -rf "$work"' EXIT

mkdir -p "$fixtures_dir"

# pi_cmd renders an argv the way a user would type it, for the fixture header.
pi_cmd() {
	local out="pi" arg
	for arg in "$@"; do
		out+=" $arg"
	done
	printf '%s' "$out"
}

# write_fixture prepends the single `#` header line to a captured record log.
write_fixture() {
	local out=$1 command=$2 records=$3
	{
		printf '# real capture | pi %s | %s | %s\n' "$pi_version" "$capture_date" "$command"
		cat "$records"
	} >"$out"
	echo "capture: wrote $out"
}

# capture runs pi, optionally writes one command line into its stdin, copies stdout
# records until the stop pattern appears and then closes stdin (pi's orderly shutdown).
# capture <records file> <stop pattern> <command or empty> [pi args...]
capture() {
	local records=$1 pattern=$2 command=$3
	shift 3
	local errlog="$work/$(basename "$records").stderr"
	local matched=0 line

	: >"$records"
	coproc PI { timeout 60 "$pi_bin" "$@" 2>"$errlog"; }
	local stdin_fd=${PI[1]} stdout_fd=${PI[0]} pid=$PI_PID

	if [ -n "$command" ]; then
		printf '%s\n' "$command" >&"$stdin_fd"
	fi
	while IFS= read -r line <&"$stdout_fd"; do
		printf '%s\n' "$line" >>"$records"
		case "$line" in
		$pattern)
			matched=1
			break
			;;
		esac
	done
	eval "exec ${stdin_fd}>&-"
	wait "$pid" || true

	if [ "$matched" -eq 0 ]; then
		echo "capture: $records: pi never emitted a record matching $pattern" >&2
		cat "$errlog" >&2
		return 1
	fi
	echo "capture: $(wc -l <"$records") record(s) from $(pi_cmd "$@")"
}

pi_args=(--mode rpc --no-session --offline --no-extensions)
bridge_args=("${pi_args[@]}" -e "$bridge")

# get_state: canned session state of an idle session; the session id is minted per run.
state_command='{"id":"state-1","type":"get_state"}'
capture "$work/get_state.records" '*"type":"response"*' "$state_command" "${pi_args[@]}"
write_fixture "$fixtures_dir/get_state.jsonl" \
	"printf '%s\\n' '$state_command' | $(pi_cmd "${pi_args[@]}")" \
	"$work/get_state.records"

# get_commands: with --no-extensions no extension, prompt template or skill registers
# a command, so the list is empty; that is the deterministic capture of the shape.
commands_command='{"id":"commands-1","type":"get_commands"}'
capture "$work/get_commands.records" '*"type":"response"*' "$commands_command" "${pi_args[@]}"
write_fixture "$fixtures_dir/get_commands.jsonl" \
	"printf '%s\\n' '$commands_command' | $(pi_cmd "${pi_args[@]}")" \
	"$work/get_commands.records"

# bash: runs a shell command and streams bash_execution_update before the response.
bash_command='{"id":"bash-1","type":"bash","command":"echo piui-fixture"}'
capture "$work/bash.records" '*"type":"response"*' "$bash_command" "${pi_args[@]}"
write_fixture "$fixtures_dir/bash.jsonl" \
	"printf '%s\\n' '$bash_command' | $(pi_cmd "${pi_args[@]}")" \
	"$work/bash.records"

# extension_ui_request: the pi-ui-bridge emits its startup notify through the extension
# UI subprotocol; no command is sent, stdin stays open until the notify arrives.
capture "$work/extension_ui_request_notify.records" '*"method":"notify"*' "" "${bridge_args[@]}"
write_fixture "$fixtures_dir/extension_ui_request_notify.jsonl" \
	"stdin held open until the notify record | $(pi_cmd "${bridge_args[@]}")" \
	"$work/extension_ui_request_notify.records"