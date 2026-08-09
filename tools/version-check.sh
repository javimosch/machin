#!/usr/bin/env bash
# Fail if main advertises a version that was never released.
#
# WHY: v0.123.0 shipped as a merged release commit — CHANGELOG entry, README
# badge, machinVersion bumped — and the tag was never pushed. There is no
# v0.123.0 on GitHub to this day. `machin guide` reported a version nobody could
# download, and nothing anywhere went red. One silent miss in 134 releases.
#
# The release guard in release.yml only inspects tags that ARE pushed; it cannot
# see one that never was. This closes that gap from the other side.
#
# THE ONE-COMMIT GRACE is what makes this usable. The release checklist bumps the
# version in a PR and tags the squashed commit AFTER it merges, so there is always
# a window where main carries an untagged version. That exact commit is allowed.
# Merge anything ON TOP of an untagged version bump and this goes red, with the
# fix stated in the failure: push the tag.
set -euo pipefail

VERSION=$(grep -oE 'machinVersion = "[0-9]+\.[0-9]+\.[0-9]+"' guide.go | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
if [ -z "${VERSION}" ]; then
  echo "version-check: could not read machinVersion from guide.go" >&2
  exit 2
fi
TAG="v${VERSION}"

if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null; then
  echo "  ${TAG} is tagged — ok"
  exit 0
fi

# Untagged. Allowed only if HEAD is the release commit that introduced it.
SUBJECT=$(git log -1 --pretty=%s)
case "${SUBJECT}" in
  "release: ${TAG}"*)
    echo "  ${TAG} not tagged yet, but HEAD is its release commit — ok (tag it now)"
    echo "    git tag -a ${TAG} -m \"MFL ${TAG}\" && git push origin ${TAG}"
    exit 0
    ;;
esac

cat >&2 <<EOF
version-check: FAIL — guide.go says ${VERSION}, but ${TAG} was never tagged, and
HEAD is not that version's release commit:

  HEAD: ${SUBJECT}

main is advertising a version nobody can download. This is exactly how v0.123.0
was lost. Fix it by tagging the release commit:

  git log --oneline --grep '^release: ${TAG}'
  git tag -a ${TAG} -m "MFL ${TAG}" <that-commit> && git push origin ${TAG}

(Or, if the bump was a mistake, correct machinVersion.)
EOF
exit 1
