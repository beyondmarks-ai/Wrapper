#!/usr/bin/env bash
set -euo pipefail

mkdir -p ./bin

if [ "$(go env GOOS)" = "darwin" ]; then
    CGO_ENABLED=1 go build -trimpath -o ./bin/wrap .
else
    CGO_ENABLED=0 go build -trimpath -o ./bin/wrap .
fi

echo "Build ready: ./bin/wrap"