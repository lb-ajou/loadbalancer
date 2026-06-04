#!/usr/bin/env bash

set -euo pipefail

results_root="${1:-plan/benchmarks}"
targets_csv="${BENCHMARK_MATRIX_TARGETS:-proxy,caddy,nginx,haproxy}"
tools_csv="${BENCHMARK_MATRIX_TOOLS:-wrk,vegeta,k6}"
repeats="${BENCHMARK_REPEATS:-5}"
cooldown_seconds="${BENCHMARK_COOLDOWN_SECONDS:-10}"
session_name="${BENCHMARK_SESSION_NAME:-matrix-$(date +%Y%m%d-%H%M%S)}"
session_dir="${results_root}/${session_name}"
manifest_file="${session_dir}/manifest.csv"
wrk_stats_seconds="${BENCHMARK_WRK_STATS_SECONDS:-45}"
vegeta_stats_seconds="${BENCHMARK_VEGETA_STATS_SECONDS:-75}"
k6_stats_seconds="${BENCHMARK_K6_STATS_SECONDS:-150}"
auto_summary="${BENCHMARK_AUTO_SUMMARY:-1}"

require_cmd() {
  if command -v "$1" >/dev/null 2>&1; then
    return
  fi
  echo "FAIL: required command not found: $1" >&2
  exit 1
}

split_csv() {
  printf '%s\n' "$1" | tr ',' '\n'
}

parse_args() {
  local input="$1"
  read -r -a parsed <<<"$input"
  printf '%s\n' "${parsed[@]}"
}

stats_seconds_for() {
  case "$1" in
    wrk) printf '%s\n' "$wrk_stats_seconds" ;;
    vegeta) printf '%s\n' "$vegeta_stats_seconds" ;;
    k6) printf '%s\n' "$k6_stats_seconds" ;;
    *)
      echo "FAIL: unsupported tool: $1" >&2
      exit 1
      ;;
  esac
}

run_tool() {
  local tool="$1"
  local repeat_dir="$2"
  local args_text
  local -a args=()

  case "$tool" in
    wrk)
      args_text="${BENCHMARK_WRK_ARGS:-30s 4 50,100,200,400}"
      ;;
    vegeta)
      args_text="${BENCHMARK_VEGETA_ARGS:-300,600,900,1200 60s}"
      ;;
    k6)
      args_text="${BENCHMARK_K6_ARGS:-30s 60s 30s 1200}"
      ;;
    *)
      echo "FAIL: unsupported tool: $tool" >&2
      exit 1
      ;;
  esac

  while IFS= read -r arg; do
    if [[ -n "$arg" ]]; then
      args+=("$arg")
    fi
  done < <(parse_args "$args_text")

  case "$tool" in
    wrk)
      tools/benchmark-wrk.sh "${args[@]}" "$repeat_dir"
      ;;
    vegeta)
      tools/benchmark-vegeta.sh "${args[@]}" "$repeat_dir"
      ;;
    k6)
      tools/benchmark-k6.sh "${args[@]}" "$repeat_dir"
      ;;
  esac
}

require_cmd docker
require_cmd mkdir
require_cmd sleep

if ! [[ "$repeats" =~ ^[0-9]+$ ]] || ((repeats <= 0)); then
  echo "FAIL: BENCHMARK_REPEATS must be a positive integer" >&2
  exit 1
fi

if ! [[ "$cooldown_seconds" =~ ^[0-9]+$ ]] || ((cooldown_seconds < 0)); then
  echo "FAIL: BENCHMARK_COOLDOWN_SECONDS must be a non-negative integer" >&2
  exit 1
fi

mkdir -p "$session_dir"
printf '%s\n' \
  "repeat,target,tool,results_dir,stats_seconds,cooldown_seconds" >"$manifest_file"

echo "benchmark matrix"
echo "- session dir: $session_dir"
echo "- targets: $targets_csv"
echo "- tools: $tools_csv"
echo "- repeats: $repeats"

for repeat in $(seq 1 "$repeats"); do
  repeat_name="$(printf 'repeat-%02d' "$repeat")"
  repeat_dir="${session_dir}/${repeat_name}"
  mkdir -p "$repeat_dir"

  for target in $(split_csv "$targets_csv"); do
    for tool in $(split_csv "$tools_csv"); do
      stats_seconds="$(stats_seconds_for "$tool")"
      printf '%s\n' \
        "${repeat_name},${target},${tool},${repeat_dir},${stats_seconds},${cooldown_seconds}" \
        >>"$manifest_file"

      echo "running ${repeat_name} ${target} ${tool}"
      BENCHMARK_TARGET="$target" tools/benchmark-stats.sh \
        "$stats_seconds" 2 "$repeat_dir" &
      stats_pid=$!

      BENCHMARK_TARGET="$target" run_tool "$tool" "$repeat_dir"
      wait "$stats_pid"

      if ((cooldown_seconds > 0)); then
        sleep "$cooldown_seconds"
      fi
    done
  done
done

if [[ "$auto_summary" != "0" ]]; then
  tools/benchmark-summary.sh "$session_dir"
fi

echo "saved benchmark matrix results to $session_dir"
