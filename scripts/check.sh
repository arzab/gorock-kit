#!/usr/bin/env bash
# Runs a check across every module in the repo.
# There is no root go.mod, so `go test ./...` from the root matches nothing.
#
# Usage: scripts/check.sh [build|vet|test|tidy-check|lint|all] [module...]
#   scripts/check.sh test               # test all modules
#   scripts/check.sh test rockengine    # test one module
set -euo pipefail

cd "$(dirname "$0")/.."

cmd="${1:-all}"
shift || true

if [ "$#" -gt 0 ]; then
  modules=("$@")
else
  modules=()
  for d in rock*/ examples/*/; do
    [ -f "$d/go.mod" ] && modules+=("${d%/}")
  done
fi

# race_supported reports whether `go test -race` can build here. It needs cgo and
# a working C compiler; a misconfigured toolchain (common on Windows) cannot build it.
# Override with RACE=1 or RACE=0.
race_supported() {
  if [ -n "${RACE:-}" ]; then
    [ "$RACE" = "1" ]
    return
  fi
  [ "$(go env CGO_ENABLED)" = "1" ] || return 1
  local tmp
  tmp="$(mktemp -d)"
  printf 'module probe\n\ngo 1.21\n' > "$tmp/go.mod"
  printf 'package main\n\nfunc main() {}\n' > "$tmp/main.go"
  if (cd "$tmp" && GOWORK=off go build -race -o probe.bin . >/dev/null 2>&1); then
    rm -rf "$tmp"; return 0
  fi
  rm -rf "$tmp"; return 1
}

race_flag=""
if [ "$cmd" = "test" ] || [ "$cmd" = "all" ]; then
  if race_supported; then
    race_flag="-race"
  else
    echo "note: race detector unavailable here (needs cgo + C compiler); running tests without -race" >&2
  fi
fi

run() {
  local step="$1" m="$2"
  case "$step" in
    build)
      # For modules with a main package (examples/*), -o <dir> keeps go build
      # from dropping a binary in the module. It cannot be used for library-only
      # modules: with no main package, go build -o <dir> fails.
      if (cd "$m" && go list -f '{{.Name}}' ./... | grep -qx main); then
        local out rc; out="$(mktemp -d)"
        (cd "$m" && go build -o "$out/" ./...); rc=$?
        rm -rf "$out"; return $rc
      fi
      (cd "$m" && go build ./...)
      ;;
    vet)   (cd "$m" && go vet ./...) ;;
    test) (cd "$m" && go test $race_flag -count=1 ./...) ;;
    tidy-check)
      (cd "$m" && cp go.mod go.mod.bak && { [ -f go.sum ] && cp go.sum go.sum.bak || true; } \
        && go mod tidy \
        && diff -q go.mod go.mod.bak >/dev/null \
        && { [ ! -f go.sum.bak ] || diff -q go.sum go.sum.bak >/dev/null; }) \
        || { echo "$m: go.mod/go.sum are not tidy (run: cd $m && go mod tidy)"; restore "$m"; return 1; }
      restore "$m"
      ;;
    lint) (cd "$m" && golangci-lint run ./...) ;;
    *) echo "unknown command: $step" >&2; exit 2 ;;
  esac
}

restore() {
  local m="$1"
  [ -f "$m/go.mod.bak" ] && mv "$m/go.mod.bak" "$m/go.mod"
  [ -f "$m/go.sum.bak" ] && mv "$m/go.sum.bak" "$m/go.sum"
  return 0
}

if [ "$cmd" = "all" ]; then
  steps=(build vet test)
else
  steps=("$cmd")
fi

failed=0
for step in "${steps[@]}"; do
  for m in "${modules[@]}"; do
    echo "==> $step $m"
    run "$step" "$m" || failed=1
  done
done
exit "$failed"
