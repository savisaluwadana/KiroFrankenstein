#!/bin/sh
# Fail if a built image carries secrets or model identifiers.
# Usage: scripts/check-image-secrets.sh [image]   (default: parallel-api)
# Checks the image's configured env, history and the binary's strings for
# values of LLM_API_KEY / DATABASE_URL / MIGRATE_DATABASE_URL (when set in the
# calling shell), credential-looking URLs, API-key patterns and model-id patterns.
set -eu
IMG="${1:-parallel-api}"
FAIL=0
fail() { echo "FAIL: $*" >&2; FAIL=1; }

ENVS=$(docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$IMG")
HIST=$(docker history --no-trunc --format '{{.CreatedBy}}' "$IMG")
META="$ENVS
$HIST"

# 1. Configured environment must not define secret or LLM variables.
if echo "$ENVS" | grep -Eq '^(LLM_API_KEY|LLM_MODEL|LLM_BASE_URL|DATABASE_URL|MIGRATE_DATABASE_URL)='; then
  fail "image env defines a secret/LLM variable"
fi

# 2. Values from the caller's environment must not appear in env or history.
for v in LLM_API_KEY DATABASE_URL MIGRATE_DATABASE_URL LLM_MODEL; do
  val=$(printenv "$v" 2>/dev/null || true)
  if [ -n "$val" ] && printf '%s\n' "$META" | grep -qF -- "$val"; then
    fail "value of $v found in image env or history"
  fi
done

# 3. Credential-looking URLs, API-key shapes and model identifiers in env or history.
if printf '%s\n' "$META" | grep -Eq 'postgres(ql)?://[^ :/]+:[^ @]+@'; then fail "database URL with password in env or history"; fi
if printf '%s\n' "$META" | grep -Eq 'sk-[A-Za-z0-9_-]{16,}|sk-or-[A-Za-z0-9_-]+'; then fail "API-key pattern in env or history"; fi
if printf '%s\n' "$META" | grep -Eiq '(openai|anthropic|google|meta-llama|mistralai|deepseek|qwen)/[a-z0-9._:-]+'; then fail "model identifier pattern in env or history"; fi

# 4. The binary itself must not embed a provider key or a built-in model id.
BIN=$(mktemp)
CID=$(docker create "$IMG")
trap 'docker rm -f "$CID" >/dev/null 2>&1 || true; rm -f "$BIN"' EXIT
docker cp "$CID:/parallel-api" "$BIN" >/dev/null
if strings -a "$BIN" | grep -Eq 'sk-or-[A-Za-z0-9_-]{8,}|postgres(ql)?://[^ :/]+:[^ @]+@'; then fail "secret-looking string in binary"; fi
if strings -a "$BIN" | grep -Eiq '(openai|anthropic|meta-llama|mistralai|deepseek|qwen)/[a-z0-9][a-z0-9._:-]+'; then fail "model identifier in binary"; fi

# 5. Non-root.
USER_CFG=$(docker inspect --format '{{.Config.User}}' "$IMG")
case "$USER_CFG" in ""|0|root|0:*|root:*) fail "image runs as root (User='$USER_CFG')";; esac

if [ "$FAIL" -ne 0 ]; then exit 1; fi
echo "ok: $IMG has no baked-in secrets or model identifiers and runs as '$USER_CFG'"
