#!/usr/bin/env bash

current_version=$(git show HEAD:VERSION)
next_version=$((current_version+1))

echo -n "${next_version}" > VERSION