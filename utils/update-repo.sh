#!/usr/bin/env bash
#
# Rename this project from the template defaults to a new module path.
#
# Usage:
#   ./utils/update-repo.sh                          # auto-detect from git origin
#   ./utils/update-repo.sh github.com/user/myapp    # explicit module path
#
set -euo pipefail

OLD_MODULE="github.com/deweysasser/golang-program"

get_module_from_origin() {
    local url
    url=$(git remote get-url origin 2>/dev/null) || {
        echo "Error: no argument provided and no git origin remote found." >&2
        exit 1
    }

    # Normalize the URL to a Go module path:
    #   git@github.com:user/repo.git  -> github.com/user/repo
    #   https://github.com/user/repo.git -> github.com/user/repo
    #   ssh://git@github.com/user/repo.git -> github.com/user/repo
    url="${url%.git}"                          # strip .git suffix
    url="${url#https://}"                      # strip https://
    url="${url#http://}"                       # strip http://
    url="${url#ssh://}"                        # strip ssh://
    url="${url#git@}"                          # strip git@ (SSH shorthand)
    url="${url/://}"                           # convert first : to / (git@host:user/repo)

    echo "$url"
}

if [ $# -ge 1 ]; then
    NEW_MODULE="$1"
else
    NEW_MODULE=$(get_module_from_origin)
fi

if [ "$NEW_MODULE" = "$OLD_MODULE" ]; then
    echo "Module is already set to $OLD_MODULE — nothing to do."
    exit 0
fi

echo "Renaming project: $OLD_MODULE -> $NEW_MODULE"

# Files that contain the old module path and need updating.
FILES=(
    go.mod
    main.go
    .chglog/config.yml
)

for f in "${FILES[@]}"; do
    if [ -f "$f" ]; then
        if grep -q "$OLD_MODULE" "$f"; then
            sed -i "s|${OLD_MODULE}|${NEW_MODULE}|g" "$f"
            echo "  updated $f"
        fi
    fi
done

# Also catch any other .go files that import the old module.
while IFS= read -r -d '' gofile; do
    # Skip files already handled above.
    skip=false
    for f in "${FILES[@]}"; do
        if [ "$gofile" = "./$f" ]; then
            skip=true
            break
        fi
    done
    $skip && continue

    if grep -q "$OLD_MODULE" "$gofile"; then
        sed -i "s|${OLD_MODULE}|${NEW_MODULE}|g" "$gofile"
        echo "  updated $gofile"
    fi
done < <(find . -name '*.go' -print0)

# Extract the project name (last path component of the module path).
NEW_NAME="${NEW_MODULE##*/}"
OLD_NAME="${OLD_MODULE##*/}"

# Update .gitignore to ignore the new binary name instead of the old one.
if [ -f .gitignore ]; then
    if grep -q "^${OLD_NAME}$" .gitignore; then
        sed -i "s|^${OLD_NAME}$|${NEW_NAME}|" .gitignore
        echo "  updated .gitignore: ${OLD_NAME} -> ${NEW_NAME}"
    fi
fi

# Update README.md heading if it still has the default name.
if [ -f README.md ]; then
    if head -1 README.md | grep -q "^# golang-program$"; then
        sed -i "1s|^# golang-program$|# ${NEW_NAME}|" README.md
        echo "  updated README.md heading to '# ${NEW_NAME}'"
    fi
fi

echo "Done. You may want to run 'go mod tidy' and 'make' to verify."
