#!/usr/bin/env bash
# Regenerate internal/protocol/gen from the repository-level schemas/*.json.
#
# Idempotent and deterministic: rerunning it on an unchanged schemas/ tree
# rewrites the generated files byte for byte identically, which is what
# `make drift` (and CI) rely on. With no schemas yet the script reports
# "nothing to do" and exits 0, so the module builds before schemas/ lands.
#
# Per schema <name>.json it runs two pinned steps:
#   1. go-jsonschema  -> internal/protocol/gen/<name>_gen.go
#   2. ./scripts/genschemas -> internal/protocol/gen/<name>_schema_gen.go
# Keep the generator version in sync with tools.go and go.mod.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

schemas_dir=../schemas
gen_dir=internal/protocol/gen
generator=github.com/atombender/go-jsonschema@v0.24.1

shopt -s nullglob
schemas=("$schemas_dir"/*.json)
shopt -u nullglob

if [ ! -d "$schemas_dir" ] || [ "${#schemas[@]}" -eq 0 ]; then
	echo "gen: no schemas in $schemas_dir, nothing to do"
	exit 0
fi

for schema in "${schemas[@]}"; do
	name="$(basename "$schema" .json)"
	echo "gen: $name"
	go run "$generator" --package gen --output "$gen_dir/${name}_gen.go" "$schema"
	go run ./scripts/genschemas -schema "$schema" -name "$name" -out "$gen_dir/${name}_schema_gen.go"
done
