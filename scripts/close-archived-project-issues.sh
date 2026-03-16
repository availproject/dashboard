#!/bin/bash
# Close open GitHub issues that were archived from the project board.
#
# Strategy:
#   1. Fetch all active project items (current board state) + their labels
#   2. For availproject/roadmap: close ALL open issues not on the board
#      For other repos: close open issues whose team label appears on the board
#   3. Close any matched issue that is NOT on the active board
#
# Usage: ./scripts/close-archived-project-issues.sh
# Requires: gh CLI authenticated with sufficient permissions
#
# Project board: https://github.com/orgs/availproject/projects/2

set -euo pipefail

ORG="availproject"
PROJECT_NUM=2
TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT

# Tab-separated files: "owner/repo <TAB> value"
ACTIVE_FILE="$TMP/active.txt"  # owner/repo TAB issue-number
LABELS_FILE="$TMP/labels.txt"  # owner/repo TAB label-name
CLOSE_FILE="$TMP/close.txt"    # owner/repo TAB issue-number
touch "$ACTIVE_FILE" "$LABELS_FILE" "$CLOSE_FILE"

# ── Step 1: Collect all active project items and labels ──────────────────────

echo "==> Fetching active project items and their labels..."
CURSOR=""
while true; do
  ARGS=(-f org="$ORG" -F number=$PROJECT_NUM)
  [ -n "$CURSOR" ] && ARGS+=(-f cursor="$CURSOR")

  PAGE=$(gh api graphql "${ARGS[@]}" -f query='
    query($org: String!, $number: Int!, $cursor: String) {
      organization(login: $org) {
        projectV2(number: $number) {
          items(first: 100, after: $cursor) {
            pageInfo { hasNextPage endCursor }
            nodes {
              content {
                ... on Issue {
                  number
                  labels(first: 20) { nodes { name } }
                  repository { name owner { login } }
                }
              }
            }
          }
        }
      }
    }')

  echo "$PAGE" | jq -r '
    .data.organization.projectV2.items.nodes[]
    | select(.content != null and .content.number != null)
    | "\(.content.repository.owner.login)/\(.content.repository.name)\t\(.content.number)"
  ' >> "$ACTIVE_FILE"

  echo "$PAGE" | jq -r '
    .data.organization.projectV2.items.nodes[]
    | select(.content != null and (.content.labels.nodes | length) > 0)
    | .content as $c
    | "\($c.repository.owner.login)/\($c.repository.name)" as $repo
    | $c.labels.nodes[]
    | "\($repo)\t\(.name)"
  ' >> "$LABELS_FILE"

  HAS_NEXT=$(echo "$PAGE" | jq -r '.data.organization.projectV2.items.pageInfo.hasNextPage')
  [ "$HAS_NEXT" = "true" ] || break
  CURSOR=$(echo "$PAGE" | jq -r '.data.organization.projectV2.items.pageInfo.endCursor')
done

sort -u "$LABELS_FILE" -o "$LABELS_FILE"
TOTAL_ACTIVE=$(wc -l < "$ACTIVE_FILE" | tr -d ' ')
TOTAL_REPOS=$(cut -f1 "$ACTIVE_FILE" | sort -u | wc -l | tr -d ' ')
echo "  $TOTAL_ACTIVE active items across $TOTAL_REPOS repos"

# ── Step 2: Find open issues not on the board ───────────────────────────────

echo ""
echo "==> Searching for open issues not on the board..."

# Repo where it's safe to close ALL open issues not on the board (no label filter)
FULL_CLOSE_REPO="availproject/roadmap"

queue_if_not_active() {
  local REPO=$1 NUM=$2
  grep -qxF "$(printf "%s\t%s" "$REPO" "$NUM")" "$ACTIVE_FILE" && return
  grep -qxF "$(printf "%s\t%s" "$REPO" "$NUM")" "$CLOSE_FILE" && return
  printf "%s\t%s\n" "$REPO" "$NUM" >> "$CLOSE_FILE"
}

# Full close for the roadmap repo: all open issues not on active board
echo "  [$FULL_CLOSE_REPO] fetching all open issues..."
PAGE=1
while true; do
  ISSUES=$(gh api "repos/${FULL_CLOSE_REPO}/issues?state=open&per_page=100&page=${PAGE}" \
    --jq '.[].number' 2>/dev/null || true)
  [ -z "$ISSUES" ] && break
  while IFS= read -r NUM; do
    [ -z "$NUM" ] && continue
    queue_if_not_active "$FULL_CLOSE_REPO" "$NUM"
  done <<< "$ISSUES"
  COUNT=$(echo "$ISSUES" | wc -l | tr -d ' ')
  [ "$COUNT" -lt 100 ] && break
  PAGE=$((PAGE + 1))
done

# Label-filtered close for all other repos
cut -f1 "$ACTIVE_FILE" | sort -u | while IFS= read -r REPO; do
  [ "$REPO" = "$FULL_CLOSE_REPO" ] && continue
  grep -F "$(printf "%s\t" "$REPO")" "$LABELS_FILE" | cut -f2- | sort -u | \
  while IFS= read -r LABEL; do
    ISSUES=$(gh search issues \
      --repo "$REPO" \
      --label "$LABEL" \
      --state open \
      --limit 1000 \
      --json number \
      --jq '.[].number' 2>/dev/null || true)
    [ -z "$ISSUES" ] && continue
    while IFS= read -r NUM; do
      [ -z "$NUM" ] && continue
      queue_if_not_active "$REPO" "$NUM"
    done <<< "$ISSUES"
    sleep 0.5  # respect search rate limit (30 req/min)
  done
done

TOTAL_TO_CLOSE=$(wc -l < "$CLOSE_FILE" | tr -d ' ')
if [ "$TOTAL_TO_CLOSE" -eq 0 ]; then
  echo "  No issues to close."
  exit 0
fi
echo "  $TOTAL_TO_CLOSE issues to close"

# ── Step 3: Close them ───────────────────────────────────────────────────────

echo ""
echo "==> Closing $TOTAL_TO_CLOSE issues..."
CLOSED=0
while IFS=$'\t' read -r REPO NUM; do
  echo "  Closing $REPO#$NUM"
  gh issue close "$NUM" --repo "$REPO" 2>&1
  CLOSED=$((CLOSED + 1))
done < "$CLOSE_FILE"

echo ""
echo "Done. Closed $CLOSED issues."
