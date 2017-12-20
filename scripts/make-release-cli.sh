#!/usr/bin/env bash
set -e

# upx is required
upx -V > /dev/null

BUILDS=(
    "GOARCH=amd64 GOOS=linux"
    # "GOARCH=amd64 GOOS=windows"
    "GOARCH=amd64 GOOS=darwin"
)

REPO_OWNER=bytearena
REPO=ba
TAG=v$1

GH_API="https://api.github.com"
GH_REPO="$GH_API/repos/$REPO_OWNER/$REPO"
GH_TAGS="$GH_REPO/releases/tags/$TAG"
AUTH="Authorization: token $GITHUB_API_TOKEN"
FILENAME=ba-$TAG
DIRECTORY=../../build/releases

mkdir -p $DIRECTORY

if [[ "$TAG" == 'latest' ]]; then
    GH_TAGS="$GH_REPO/releases/latest"
fi

# Validate token.
curl -o /dev/null -sH "$AUTH" $GH_REPO || { echo "Error: Invalid repo, token or network issue!"; exit 1; }

# Create release
curl -s -k -X POST \
    -H "Content-Type: application/json" \
    -H "$AUTH" \
    "$GH_API/repos/$REPO_OWNER/$REPO/releases" -d "{\"tag_name\": \"$TAG\", \"target_commitish\": \"master\", \"name\": \"$TAG\", \"body\": \"Release of version $TAG\", \"draft\": false, \"prerelease\": false}"

# Read asset tags.
response=$(curl -sH "$AUTH" $GH_TAGS)

# Get ID of the asset based on given filename.
eval $(echo "$response" | grep -m 1 "id.:" | grep -w id | tr : = | tr -cd '[[:alnum:]]=')
[ "$id" ] || { echo "Error: Failed to get release id for tag: $tag"; echo "$response" | awk 'length($0)<100' >&2; exit 1; }

cd cmd/ba

for i in "${BUILDS[@]}"
do
    echo $i
    eval $i

    FILE=$DIRECTORY/$FILENAME-$GOARCH-$GOOS
    ASSET="https://uploads.github.com/repos/$REPO_OWNER/$REPO/releases/$id/assets?name=$(basename $FILE)"

    echo "Building $FILE release..."

    env $i go build -o "$FILE" -ldflags="-s -w -X github.com/bytearena/ba/common/utils.version=$TAG"
    upx -9 $FILE
    du -sh $FILE

    echo "Uploading $FILE release..."

    curl $ASSET \
        --progress-bar \
        --data-binary "@$FILE" \
        -H "$AUTH" \
        -H "Content-Type: application/octet-stream"
done
