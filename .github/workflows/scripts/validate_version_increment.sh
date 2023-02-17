#!/usr/bin/env bash

previous_version=$(git show HEAD~1:VERSION)
current_version=$(git show HEAD:VERSION)

if [[ $((current_version)) == $((previous_version+1)) ]]
then
    exit 0
else
    echo "Version '${current_version}' is not a linear increment from the previous '${previous_version}' version; expected new version: '$((previous_version+1))'"
    exit 1
fi