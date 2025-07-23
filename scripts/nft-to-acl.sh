#!/usr/bin/env bash
# nft to shadowsocks acl

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <input.nft> <output.acl>"
  exit 1
fi

INPUT_FILE="$1"
OUTPUT_FILE="$2"

if [[ ! -f "$INPUT_FILE" ]]; then
  echo "❌ Input file not found: $INPUT_FILE"
  exit 1
fi

echo "📄 Reading from: $INPUT_FILE"
echo "📝 Writing to:   $OUTPUT_FILE"

# Start writing to the output file, change the output or the script if you need different type of ACL
{
  echo "[outbound_block_list]"
  echo "# Generated from $INPUT_FILE"
  echo
} > "$OUTPUT_FILE"

# Parse IPs and append them line by line
awk '
/elements *= *\{/ {
    in_elements=1
    sub(/^.*\{/, "", $0)
}
in_elements {
    gsub(/[,{}]/, "", $0)
    split($0, parts, " ")
    for (i in parts) {
        entry = parts[i]
        gsub(/^ +| +$/, "", entry)
        if (length(entry) > 0) print entry
    }
}
/\}/ && in_elements { in_elements=0 }
' "$INPUT_FILE" | sort -u >> "$OUTPUT_FILE"

echo "✅ Done. Output saved to $OUTPUT_FILE"
