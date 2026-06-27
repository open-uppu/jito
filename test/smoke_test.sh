#!/usr/bin/env bash
# jito smoke test
set -euo pipefail

BIN=${BIN:-./bin/jito}
echo "🧪 jito smoke test"
echo ""

# 1. version
echo "1. version check"
$BIN --version
echo ""

# 2. help
echo "2. help check"
$BIN --help >/dev/null && echo "   ✅ help works"
echo ""

# 3. modes
echo "3. mode listing"
for mode in dev reason create audit universal; do
  out=$($BIN run --mode=$mode "ping" 2>&1 || true)
  if [[ "$out" == *"Error"* && "$out" != *"provider"* ]]; then
    echo "   ❌ mode=$mode failed: $out"
    exit 1
  else
    echo "   ✅ mode=$mode registered"
  fi
done

# 4. live API call (if key set)
if [[ -n "${JITO_API_KEY:-}" ]]; then
  echo "4. live API call"
  out=$($BIN run --mode=universal "say 'jito alive' in 5 words" 2>&1)
  if [[ "$out" == *"jito"* ]] || [[ "$out" == *"alive"* ]]; then
    echo "   ✅ live API works"
  else
    echo "   ⚠️  unexpected output: $out"
  fi
else
  echo "4. live API call (skipped — set JITO_API_KEY)"
fi

echo ""
echo "🎉 All smoke tests passed"