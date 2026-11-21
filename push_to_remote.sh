#!/usr/bin/env bash
# push_to_remote.sh - push the submodule first, then the root repository, with a GitHub
# fine-grained personal access token.
#
#   export GITHUB_TOKEN=github_pat_...        # never put the token in this file
#   ./push_to_remote.sh [--commit "message"] [--dry-run] [--yes]
#
# Order: 1. the submodule (systems/hmf-audit)  2. the root repository. The root records which
# submodule commit it uses, so the submodule commit must already be on its remote when the
# root is pushed; the script checks that and stops otherwise.
#
# Options
#   --commit "msg"  first commit the modified TRACKED files: in the submodule, then in the root
#                   (the root commit also records the new submodule commit). Untracked files, such
#                   as results/, are not added; run `git add` on them yourself first if you want
#                   them pushed.
#   --dry-run       run `git push --dry-run`; nothing is changed on the remote.
#   --yes           do not ask for confirmation before each push.
#
# Environment (all optional except GITHUB_TOKEN)
#   GITHUB_TOKEN     fine-grained token with "Contents: Read and write" on BOTH repositories.
#   GITHUB_USER      user name sent with the token (default: x-access-token).
#   ROOT_REMOTE_URL  where to push the root      (default: the root's `origin`).
#   SUB_REMOTE_URL   where to push the submodule (default: the submodule's `origin`).
#   ROOT_BRANCH      remote branch for the root  (default: the branch checked out now).
#   SUB_BRANCH       remote branch for the submodule (default: same as ROOT_BRANCH).
#   SUB_PATH         submodule path              (default: systems/hmf-audit).
#
# The token is handed to git through a temporary GIT_ASKPASS helper: it is not written into a
# remote URL, git config or the command line, and no credential is stored. Nothing is ever
# force-pushed: a remote that has moved ahead makes the push fail, and you decide what to do.
set -euo pipefail

ROOT_DIR="${REPO_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
SUB_PATH="${SUB_PATH:-systems/hmf-audit}"
COMMIT_MSG=""
DRY_RUN=0
ASSUME_YES=0

die() { echo "error: $*" >&2; exit 1; }
say() { echo "== $*"; }

while [[ $# -gt 0 ]]; do
    case "$1" in
    --commit) [[ $# -ge 2 && -n "$2" ]] || die "--commit needs a message"; COMMIT_MSG="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --yes | -y) ASSUME_YES=1; shift ;;
    -h | --help) sed -n '2,31p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) die "unknown option $1 (see --help)" ;;
    esac
done

command -v git >/dev/null || die "git is not installed"
SUB_DIR="$ROOT_DIR/$SUB_PATH"
git -C "$ROOT_DIR" rev-parse --git-dir >/dev/null 2>&1 || die "$ROOT_DIR is not a git repository"
# An empty (uninitialized) submodule directory makes git fall back to the PARENT repository, so
# insist that the submodule is a repository of its own, not merely somewhere inside one.
[[ -d "$SUB_DIR" ]] && [[ "$(git -C "$SUB_DIR" rev-parse --show-toplevel 2>/dev/null)" == "$(cd "$SUB_DIR" && pwd -P)" ]] \
    || die "the submodule $SUB_PATH is not checked out as its own repository (git submodule update --init $SUB_PATH)"

# An SSH remote cannot use a token: push over HTTPS instead (for this push only; no config is changed).
to_https() {
    local url="$1"
    case "$url" in
    git@github.com:*) url="https://github.com/${url#git@github.com:}" ;;
    ssh://git@github.com/*) url="https://github.com/${url#ssh://git@github.com/}" ;;
    esac
    printf '%s' "$url"
}

ROOT_BRANCH="${ROOT_BRANCH:-$(git -C "$ROOT_DIR" branch --show-current)}"
[[ -n "$ROOT_BRANCH" ]] || die "the root repository is on a detached HEAD; set ROOT_BRANCH"
SUB_BRANCH="${SUB_BRANCH:-$ROOT_BRANCH}"
ROOT_URL="$(to_https "${ROOT_REMOTE_URL:-$(git -C "$ROOT_DIR" remote get-url origin)}")"
SUB_URL="$(to_https "${SUB_REMOTE_URL:-$(git -C "$SUB_DIR" remote get-url origin)}")"

uses_token() { [[ "$1" == https://* ]]; }
if uses_token "$ROOT_URL" || uses_token "$SUB_URL"; then
    [[ -n "${GITHUB_TOKEN:-}" ]] || die "GITHUB_TOKEN is not set (export GITHUB_TOKEN=github_pat_... first)"
fi

# --- credentials: a temporary askpass helper, removed on exit -------------------------------
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cat >"$WORK/askpass.sh" <<'EOF'
#!/bin/sh
case "$1" in
Username*) printf '%s\n' "${PUSH_USER:-x-access-token}" ;;
*) printf '%s\n' "$PUSH_TOKEN" ;;
esac
EOF
chmod 700 "$WORK/askpass.sh"
export GIT_ASKPASS="$WORK/askpass.sh" GIT_TERMINAL_PROMPT=0
export PUSH_USER="${GITHUB_USER:-x-access-token}" PUSH_TOKEN="${GITHUB_TOKEN:-}"
unset GITHUB_TOKEN

dirty() { [[ -n "$(git -C "$1" status --porcelain --untracked-files=no)" ]]; }

confirm() {
    [[ $ASSUME_YES -eq 1 ]] && return 0
    local answer
    read -r -p "$1 [y/N] " answer
    [[ "$answer" == y || "$answer" == Y ]]
}

# push_repo <dir> <label> <url> <branch>: push HEAD to <branch> on <url>, never forced.
push_repo() {
    local dir="$1" label="$2" url="$3" branch="$4"
    say "$label: pushing HEAD to $url ($branch)"
    git -C "$dir" log --oneline -n 3 | sed 's/^/     /'
    confirm "push the $label now?" || die "stopped before pushing the $label"
    local args=()
    [[ $DRY_RUN -eq 1 ]] && args+=(--dry-run)
    # credential.helper= stops git from storing the token anywhere.
    git -C "$dir" -c credential.helper= push ${args[@]+"${args[@]}"} "$url" "HEAD:refs/heads/$branch"
}

# --- 1. optional commits (submodule first, so the root can record its new commit) -----------
if [[ -n "$COMMIT_MSG" ]]; then
    for pair in "submodule:$SUB_DIR" "root repository:$ROOT_DIR"; do
        label="${pair%%:*}"
        dir="${pair#*:}"
        if dirty "$dir"; then
            say "$label: committing modified tracked files"
            git -C "$dir" add -u
            git -C "$dir" commit -q -m "$COMMIT_MSG"
        else
            say "$label: nothing to commit"
        fi
    done
fi
for pair in "submodule:$SUB_DIR" "root repository:$ROOT_DIR"; do
    label="${pair%%:*}"
    dir="${pair#*:}"
    if dirty "$dir"; then
        echo "warning: the $label has uncommitted changes; they are NOT part of what will be pushed (use --commit)." >&2
    fi
done

# --- 2. the submodule first -------------------------------------------------------------------
push_repo "$SUB_DIR" "submodule" "$SUB_URL" "$SUB_BRANCH"

# --- 3. the root records a submodule commit; it must be in what was just pushed --------------
recorded="$(git -C "$ROOT_DIR" rev-parse "HEAD:$SUB_PATH")"
if ! git -C "$SUB_DIR" merge-base --is-ancestor "$recorded" HEAD 2>/dev/null; then
    die "the root repository records submodule commit ${recorded:0:12}, which is not in the submodule history just pushed; not pushing the root"
fi
if [[ "$recorded" != "$(git -C "$SUB_DIR" rev-parse HEAD)" ]]; then
    echo "note: the root records submodule commit ${recorded:0:12}, older than the submodule HEAD; commit the root with --commit to point it at the new one." >&2
fi

# --- 4. the root repository -------------------------------------------------------------------
push_repo "$ROOT_DIR" "root repository" "$ROOT_URL" "$ROOT_BRANCH"

say "done$([[ $DRY_RUN -eq 1 ]] && echo ' (dry run: nothing was changed on the remotes)')"
