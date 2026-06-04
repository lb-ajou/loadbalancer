#!/usr/bin/env bash

set -euo pipefail

session_dir="${1:-}"
output_name="${BENCHMARK_SUMMARY_DIR:-summary}"

require_cmd() {
  if command -v "$1" >/dev/null 2>&1; then
    return
  fi
  echo "FAIL: required command not found: $1" >&2
  exit 1
}

append_row() {
  printf '%s\n' "$1" >>"$raw_csv"
}

metric_value() {
  local file="$1"
  local metric="$2"
  local key="$3"

  if [[ -z "$file" ]] || [[ ! -f "$file" ]]; then
    return
  fi

  awk -v metric="$metric" -v key="$key" '
    index($0, "\"" metric "\"") > 0 { in_metric=1; next }
    in_metric && $0 ~ /^[[:space:]]*}/ { in_metric=0 }
    in_metric && index($0, "\"" key "\"") > 0 {
      line=$0
      sub(/^.*:[[:space:]]*/, "", line)
      sub(/,?[[:space:]]*$/, "", line)
      gsub(/"/, "", line)
      print line
      exit
    }
  ' "$file"
}

stats_summary() {
  local file="$1"

  if [[ -z "$file" ]] || [[ ! -f "$file" ]]; then
    printf ',,,\n'
    return
  fi

  awk -F, '
    function to_mib(value, raw, num, unit) {
      raw = value
      sub(/^[[:space:]]+/, "", raw)
      sub(/[[:space:]]+$/, "", raw)
      if (match(raw, /^[0-9.]+/)) {
        num = substr(raw, RSTART, RLENGTH) + 0
      } else {
        return 0
      }
      unit = substr(raw, RLENGTH + 1)
      if (unit == "KiB") return num / 1024
      if (unit == "MiB") return num
      if (unit == "GiB") return num * 1024
      if (unit == "TiB") return num * 1024 * 1024
      if (unit == "kB") return num / 1000 * 1024 / 1024
      if (unit == "MB") return num * 1000 / 1024
      if (unit == "GB") return num * 1000 * 1000 / 1024
      return num
    }
    NR == 1 { next }
    {
      cpu = $2
      gsub(/%/, "", cpu)
      split($3, mem_parts, " / ")
      mem = to_mib(mem_parts[1])
      cpu_sum += cpu
      mem_sum += mem
      count++
      if (count == 1 || cpu > cpu_peak) cpu_peak = cpu
      if (count == 1 || mem > mem_peak) mem_peak = mem
    }
    END {
      if (count == 0) {
        print ",,,"
        exit
      }
      printf "%.2f,%.2f,%.2f,%.2f\n", cpu_sum / count, cpu_peak, mem_sum / count, mem_peak
    }
  ' "$file"
}

number_summary() {
  local raw_values="$1"
  local sorted
  local count
  local avg
  local min
  local max
  local median
  local mid

  sorted="$(printf '%s\n' "$raw_values" | awk 'NF > 0' | sort -n)"
  if [[ -z "$sorted" ]]; then
    printf ',,,,\n'
    return
  fi

  count="$(printf '%s\n' "$sorted" | awk 'END { print NR }')"
  avg="$(printf '%s\n' "$sorted" | awk '{ sum += $1 } END { printf "%.4f", sum / NR }')"
  min="$(printf '%s\n' "$sorted" | head -n 1)"
  max="$(printf '%s\n' "$sorted" | tail -n 1)"
  mid=$((count / 2 + 1))

  if ((count % 2 == 1)); then
    median="$(printf '%s\n' "$sorted" | sed -n "${mid}p")"
  else
    median="$(printf '%s\n' "$sorted" | awk -v a="$((count / 2))" -v b="$mid" '
      NR == a { left = $1 }
      NR == b { right = $1 }
      END { printf "%.4f", (left + right) / 2 }
    ')"
  fi

  printf '%s,%s,%s,%s,%s\n' "$count" "$avg" "$median" "$min" "$max"
}

write_summary_row() {
  local target="$1"
  local tool="$2"
  local scenario="$3"
  local actual
  local p95
  local errors
  local cpu
  local mem

  actual="$(number_summary "$(awk -F, -v target="$target" -v tool="$tool" -v scenario="$scenario" '
    NR > 1 && $2 == target && $3 == tool && $4 == scenario && $5 != "" { print $5 }
  ' "$raw_csv")")"
  p95="$(number_summary "$(awk -F, -v target="$target" -v tool="$tool" -v scenario="$scenario" '
    NR > 1 && $2 == target && $3 == tool && $4 == scenario && $7 != "" { print $7 }
  ' "$raw_csv")")"
  errors="$(number_summary "$(awk -F, -v target="$target" -v tool="$tool" -v scenario="$scenario" '
    NR > 1 && $2 == target && $3 == tool && $4 == scenario && $10 != "" { print $10 }
  ' "$raw_csv")")"
  cpu="$(number_summary "$(awk -F, -v target="$target" -v tool="$tool" -v scenario="$scenario" '
    NR > 1 && $2 == target && $3 == tool && $4 == scenario && $13 != "" { print $13 }
  ' "$raw_csv")")"
  mem="$(number_summary "$(awk -F, -v target="$target" -v tool="$tool" -v scenario="$scenario" '
    NR > 1 && $2 == target && $3 == tool && $4 == scenario && $15 != "" { print $15 }
  ' "$raw_csv")")"

  printf '%s\n' \
    "${target},${tool},${scenario},${actual},${p95},${errors},${cpu},${mem}" \
    >>"$summary_csv"
}

summarize_wrk() {
  local repeat_name="$1"
  local target="$2"
  local tool_dir="$3"
  local stats_dir="$4"
  local stats_file
  local stats
  local avg_cpu
  local peak_cpu
  local avg_mem
  local peak_mem
  local file
  local scenario
  local actual_rps
  local avg_latency
  local error_rate

  stats_file="$(find "$stats_dir" -maxdepth 1 -type f -name "${target}-stats.csv" | head -n 1)"
  stats="$(stats_summary "$stats_file")"
  IFS=, read -r avg_cpu peak_cpu avg_mem peak_mem <<<"$stats"

  while IFS= read -r file; do
    scenario="$(basename "$file" .txt)"
    scenario="${scenario#wrk-}"
    actual_rps="$(awk '/^Requests\/sec:/ { print $2; exit }' "$file")"
    avg_latency="$(awk '/^[[:space:]]*Latency/ { print $2; exit }' "$file")"
    avg_latency="${avg_latency%ms}"
    error_rate="$(awk '
      /Non-2xx or 3xx responses:/ { bad += $5 }
      /Socket errors:/ {
        for (i = 1; i <= NF; i++) {
          if ($i ~ /connect|read|write|timeout/) {
            split($i, pair, "=")
            gsub(/,/, "", pair[2])
            bad += pair[2]
          }
        }
      }
      END {
        if (bad == "") bad = 0
        printf "%.4f", bad
      }
    ' "$file")"

    append_row "${repeat_name},${target},wrk,${scenario},${actual_rps},${avg_latency},,,,${error_rate},,,${avg_cpu},${peak_cpu},${avg_mem},${peak_mem},${tool_dir}"
  done < <(find "$tool_dir" -maxdepth 1 -type f -name 'wrk-c*.txt' | sort)
}

summarize_vegeta() {
  local repeat_name="$1"
  local target="$2"
  local tool_dir="$3"
  local stats_dir="$4"
  local stats_file
  local stats
  local avg_cpu
  local peak_cpu
  local avg_mem
  local peak_mem
  local file
  local scenario
  local actual_rps
  local avg_latency
  local p95
  local p99
  local max_latency
  local success_rate
  local error_rate

  stats_file="$(find "$stats_dir" -maxdepth 1 -type f -name "${target}-stats.csv" | head -n 1)"
  stats="$(stats_summary "$stats_file")"
  IFS=, read -r avg_cpu peak_cpu avg_mem peak_mem <<<"$stats"

  while IFS= read -r file; do
    scenario="$(basename "$file" .txt)"
    scenario="${scenario#vegeta-}"
    actual_rps="$(awk '
      /Requests/ {
        gsub(/[\[\],]/, "", $0)
        print $NF
        exit
      }
    ' "$file")"
    avg_latency="$(awk '/Latencies/ { gsub(/[\[\],]/, "", $0); print $(NF-4); exit }' "$file")"
    p95="$(awk '/Latencies/ { gsub(/[\[\],]/, "", $0); print $(NF-2); exit }' "$file")"
    p99="$(awk '/Latencies/ { gsub(/[\[\],]/, "", $0); print $(NF-1); exit }' "$file")"
    max_latency="$(awk '/Latencies/ { gsub(/[\[\],]/, "", $0); print $NF; exit }' "$file")"
    success_rate="$(awk '/Success/ { gsub(/[\[\],]/, "", $0); print $NF; exit }' "$file")"
    success_rate="${success_rate%\%}"
    if [[ -n "$success_rate" ]]; then
      error_rate="$(awk -v success="$success_rate" 'BEGIN { printf "%.4f", 100 - success }')"
    else
      error_rate=""
    fi

    append_row "${repeat_name},${target},vegeta,${scenario},${actual_rps},${avg_latency%ms},${p95%ms},${p99%ms},${max_latency%ms},${error_rate},${success_rate},,${avg_cpu},${peak_cpu},${avg_mem},${peak_mem},${tool_dir}"
  done < <(find "$tool_dir" -maxdepth 1 -type f -name 'vegeta-r*.txt' | sort)
}

summarize_k6() {
  local repeat_name="$1"
  local target="$2"
  local tool_dir="$3"
  local stats_dir="$4"
  local stats_file
  local stats
  local avg_cpu
  local peak_cpu
  local avg_mem
  local peak_mem
  local file
  local actual_rps
  local avg_latency
  local p95
  local max_latency
  local error_rate
  local success_rate
  local dropped

  stats_file="$(find "$stats_dir" -maxdepth 1 -type f -name "${target}-stats.csv" | head -n 1)"
  stats="$(stats_summary "$stats_file")"
  IFS=, read -r avg_cpu peak_cpu avg_mem peak_mem <<<"$stats"

  file="$(find "$tool_dir" -maxdepth 1 -type f -name 'k6-summary.json' | head -n 1)"
  actual_rps="$(metric_value "$file" "http_reqs" "rate")"
  avg_latency="$(metric_value "$file" "http_req_duration" "avg")"
  p95="$(metric_value "$file" "http_req_duration" "p(95)")"
  max_latency="$(metric_value "$file" "http_req_duration" "max")"
  error_rate="$(metric_value "$file" "http_req_failed" "value")"
  dropped="$(metric_value "$file" "dropped_iterations" "count")"

  if [[ -z "$dropped" ]]; then
    dropped="0"
  fi

  if [[ -n "$error_rate" ]]; then
    success_rate="$(awk -v err="$error_rate" 'BEGIN { printf "%.4f", 1 - err }')"
  else
    success_rate=""
  fi

  append_row \
    "${repeat_name},${target},k6,default,${actual_rps},${avg_latency},${p95},,${max_latency},${error_rate},${success_rate},${dropped},${avg_cpu},${peak_cpu},${avg_mem},${peak_mem},${tool_dir}"
}

require_cmd awk
require_cmd find
require_cmd head
require_cmd mkdir
require_cmd sed
require_cmd sort

if [[ -z "$session_dir" ]] || [[ ! -d "$session_dir" ]]; then
  echo "FAIL: benchmark session directory is required" >&2
  exit 1
fi

output_dir="${session_dir}/${output_name}"
raw_csv="${output_dir}/raw-results.csv"
summary_csv="${output_dir}/summary.csv"
summary_md="${output_dir}/summary.md"

mkdir -p "$output_dir"
printf '%s\n' \
  "repeat,target,tool,scenario,actual_rps,avg_latency_ms,p95_ms,p99_ms,max_latency_ms,error_rate,success_rate,dropped_iterations,avg_cpu_percent,peak_cpu_percent,avg_memory_mib,peak_memory_mib,source_dir" \
  >"$raw_csv"

while IFS= read -r repeat_dir; do
  repeat_name="$(basename "$repeat_dir")"

  while IFS= read -r tool_dir; do
    tool_name="$(basename "$tool_dir")"
    tool="${tool_name%%-*}"
    target="$(printf '%s' "$tool_name" | awk -F- '{ print $2 }')"
    stats_dir="$(find "$repeat_dir" -maxdepth 1 -type d -name "stats-${target}-*" | head -n 1)"

    case "$tool" in
      wrk)
        summarize_wrk "$repeat_name" "$target" "$tool_dir" "$stats_dir"
        ;;
      vegeta)
        summarize_vegeta "$repeat_name" "$target" "$tool_dir" "$stats_dir"
        ;;
      k6)
        summarize_k6 "$repeat_name" "$target" "$tool_dir" "$stats_dir"
        ;;
    esac
  done < <(find "$repeat_dir" -maxdepth 1 -type d \
    \( -name 'wrk-*' -o -name 'vegeta-*' -o -name 'k6-*' \) | sort)
done < <(find "$session_dir" -maxdepth 1 -type d -name 'repeat-*' | sort)

printf '%s\n' \
  "target,tool,scenario,actual_rps_count,actual_rps_avg,actual_rps_median,actual_rps_min,actual_rps_max,p95_count,p95_avg,p95_median,p95_min,p95_max,error_count,error_avg,error_median,error_min,error_max,avg_cpu_count,avg_cpu_avg,avg_cpu_median,avg_cpu_min,avg_cpu_max,avg_memory_count,avg_memory_avg,avg_memory_median,avg_memory_min,avg_memory_max" \
  >"$summary_csv"

while IFS=, read -r target tool scenario; do
  write_summary_row "$target" "$tool" "$scenario"
done < <(awk -F, 'NR > 1 { print $2 "," $3 "," $4 }' "$raw_csv" | sort -u)

{
  printf '# 반복 측정 요약\n\n'
  printf '%s\n' "- session: \`$(basename "$session_dir")\`"
  printf '%s\n' "- raw csv: \`$raw_csv\`"
  printf '%s\n\n' "- summary csv: \`$summary_csv\`"
  printf '## 해석 순서\n\n'
  printf '1. 같은 `target/tool/scenario`에서 `actual_rps_median`과 `p95_median`을 먼저 본다.\n'
  printf '2. `actual_rps_avg`와 `actual_rps_median` 차이가 크면 일부 반복에서 편차가 컸다는 뜻으로 본다.\n'
  printf '3. `error_max`가 `0`보다 크면 해당 조합은 안정성 이슈 후보로 따로 분석한다.\n'
  printf '4. `avg_cpu_avg`, `avg_memory_avg`는 성능 우열 판단보다 비용 대비 효율 비교에 사용한다.\n\n'
  printf '## 집계 표\n\n'
  printf '| Target | Tool | Scenario | RPS avg | RPS median | p95 avg | p95 median | Error avg | CPU avg | Mem avg MiB |\n'
  printf '| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n'
  awk -F, '
    NR == 1 { next }
    {
      printf "| `%s` | `%s` | `%s` | %s | %s | %s | %s | %s | %s | %s |\n",
        $1, $2, $3, v($5), v($6), v($10), v($11), v($15), v($20), v($25)
    }
    function v(value) {
      if (value == "") return "-"
      return value
    }
  ' "$summary_csv"
} >"$summary_md"

echo "saved benchmark summary to $output_dir"
