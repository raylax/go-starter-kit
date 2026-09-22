#!/bin/sh
set -eu

task_tmp=$(mktemp -d)
trap 'rm -rf "$task_tmp"' EXIT HUP INT TERM
cp -R db/sqlc "$task_tmp/sqlc"
cp api/openapi.json "$task_tmp/openapi.json"
make generate
diff -ru "$task_tmp/sqlc" db/sqlc
diff -u "$task_tmp/openapi.json" api/openapi.json
