#!/usr/bin/env bash
#
# Runs the Phase 1 acceptance measurements and keeps every raw sample under a
# gitignored output directory (default <repo>/.piui/spike). This is the script
# docs/spike-report.md points at: it builds the static server binary and the
# fake-pi harness, runs one measurement per acceptance criterion and prints the
# summaries. It never invents numbers and never touches the git index.
#
# Usage:
#   server/scripts/measure.sh
#
# Environment knobs:
#   SPIKE_OUT_DIR          output directory (default <repo>/.piui/spike)
#   PI_BIN                 real pi executable (default: pi on PATH)
#   SPIKE_SESSIONS         children of the real-pi spawn run (C1/C4, default 8)
#   SPIKE_RSS_2            sessions of the 2-session idle RSS run (C2, default 2)
#   SPIKE_RSS_8            sessions of the 8-session idle RSS run (C3, default 8)
#   SPIKE_RAPID_*          sessions/clients/events/rate of the C5/C6 run
#                          (default 1/1/210000/7000: 30 s paced 40% above the
#                          5 000 events/s threshold, so the pipeline — not the
#                          source — is what the run measures; see the comment on
#                          the run itself and docs/spike-report.md)
#   SPIKE_SOAK_*           sessions/clients/events/rate of the C9 soak
#                          (default 1/1/360000/2000)
#   SPIKE_SKIP_SOAK=1      skip the three minute soak
#
# C7 (framing) and C8 (SIGTERM reaps every child) are covered by the test suites
# the script runs first: server/test/adversarial and server/test/e2e.
set -euo pipefail

server_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_dir="$(cd "$server_dir/.." && pwd)"

out_dir="${SPIKE_OUT_DIR:-$repo_dir/.piui/spike}"
pi_bin="${PI_BIN:-$(command -v pi || true)}"

sessions="${SPIKE_SESSIONS:-8}"
rss_2="${SPIKE_RSS_2:-2}"
rss_8="${SPIKE_RSS_8:-8}"
rapid_sessions="${SPIKE_RAPID_SESSIONS:-1}"
rapid_clients="${SPIKE_RAPID_CLIENTS:-1}"
rapid_events="${SPIKE_RAPID_EVENTS:-210000}"
rapid_rate="${SPIKE_RAPID_RATE:-7000}"
soak_sessions="${SPIKE_SOAK_SESSIONS:-1}"
soak_clients="${SPIKE_SOAK_CLIENTS:-1}"
soak_events="${SPIKE_SOAK_EVENTS:-360000}"
soak_rate="${SPIKE_SOAK_RATE:-2000}"

log_file="$out_dir/measure.log"

log() { printf '[measure] %s\n' "$*" | tee -a "$log_file"; }

# Raw samples are megabytes of JSONL. The output directory must be gitignored
# before anything is written, or a stray `git add -A` could stage them.
require_ignored() {
	if ! git -C "$repo_dir" check-ignore -q "$out_dir"; then
		printf 'measure: %s is not gitignored; refusing to write raw samples there\n' "$out_dir" >&2
		exit 1
	fi
}

# run NAME [flags...] executes one `pi-ui measure` with its own output directory:
# the summary lands in <out>/<NAME>/summary.txt and the raw sample files next to it.
run() {
	local name="$1"
	shift
	local dir="$out_dir/$name"
	mkdir -p "$dir"
	log "run $name: pi-ui measure $* --out $dir/summary.txt"
	"$out_dir/pi-ui" measure "$@" --out "$dir/summary.txt" 2>&1 | tee -a "$log_file"
}

mkdir -p "$out_dir"
require_ignored
: >"$log_file"

# Load context and the exact tree belong with the numbers: a p95 measured on a
# busy host is read differently, and the report can point at the commit.
log "tree: $(git -C "$repo_dir" rev-parse --short HEAD)"
log "load before: $(uptime)"

log "building pi-ui (static) and fake-pi into $out_dir"
(
	cd "$server_dir"
	CGO_ENABLED=0 go build -trimpath -o "$out_dir/pi-ui" ./cmd/pi-ui
	go build -trimpath -o "$out_dir/fake-pi" ./test/fake-pi
)

# C7 and C8 live in the test suites; running them here keeps one command able to
# reproduce the whole matrix.
log "running server test suites (includes the C7 framing and C8 shutdown checks)"
(cd "$server_dir" && go test -race ./test/adversarial/... ./test/e2e/... )

# C1/C4: real pi child spawn and idle RSS. Skipped cleanly when pi is absent.
if [[ -n "$pi_bin" && -x "$pi_bin" ]]; then
	run real-pi-spawn --pi "$pi_bin" --sessions "$sessions"
else
	log "real pi not found (PI_BIN=${pi_bin:-unset}); skipping the C1/C4 spawn run"
fi

# C2/C3: idle in-process server with 2 and 8 sessions (the same command also runs
# a single-event throughput pass, which the report ignores for these criteria).
run server-rss-2 --fake-pi "$out_dir/fake-pi" --sessions "$rss_2" --clients 1 --events 1 --rate 0
run server-rss-8 --fake-pi "$out_dir/fake-pi" --sessions "$rss_8" --clients 1 --events 1 --rate 0

# C5/C6: sustained rapid stream, one client. The source is paced *above* the 5 000
# events/s threshold on purpose: a source paced at exactly 5 000/s can only measure
# at or below 5 000/s, so it could never show the pipeline meeting the criterion.
# 7 000/s for 30 s leaves the source out of the measurement and still leaves the
# subscriber queue ~70 ms of slack, which keeps the run stable on a busy host.
run throughput-rapid --fake-pi "$out_dir/fake-pi" \
	--sessions "$rapid_sessions" --clients "$rapid_clients" \
	--events "$rapid_events" --rate "$rapid_rate"

# C9: three minute soak with the RSS growth sampled.
if [[ "${SPIKE_SKIP_SOAK:-0}" != "1" ]]; then
	run throughput-soak --fake-pi "$out_dir/fake-pi" \
		--sessions "$soak_sessions" --clients "$soak_clients" \
		--events "$soak_events" --rate "$soak_rate"
else
	log "SPIKE_SKIP_SOAK=1: the C9 soak was skipped"
fi

# Summary: every run's own summary, concatenated for the report.
summary="$out_dir/summary.txt"
: >"$summary"
for dir in "$out_dir"/*/; do
	[[ -f "$dir/summary.txt" ]] || continue
	printf '== %s\n' "$(basename "$dir")" >>"$summary"
	cat "$dir/summary.txt" >>"$summary"
done
log "summaries:"
sed 's/^/[measure] /' "$summary"

log "raw samples: $out_dir (gitignored); log: $log_file"
log "load after: $(uptime)"
