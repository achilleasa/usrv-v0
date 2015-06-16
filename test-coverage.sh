#!/usr/bin/env bash

#
# To use this make sure you export CODECOV_TOKEN env var
#

set -e
echo "" > coverage.txt

# Main package
go test -v -coverprofile=profile.out -covermode=atomic ./
if [ -f profile.out ]; then
    cat profile.out >> coverage.txt
    echo '<<<<<< EOF' >> coverage.txt
    rm profile.out
fi

# Sub-packages
for d in $(find ./* -maxdepth 10 -type d); do
    if ls $d/*.go &> /dev/null; then
        go test -v -coverprofile=profile.out -covermode=atomic $d
        if [ -f profile.out ]; then
            cat profile.out >> coverage.txt
            echo '<<<<<< EOF' >> coverage.txt
            rm profile.out
        fi
    fi
done

bash <(curl -s https://codecov.io/bash)