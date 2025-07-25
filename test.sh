#!/usr/bin/env bash

printf "Updating sqlc definitions..."
if ! sqlc generate -f ./db/sqlc.yaml; then
	printf "failed.\n"
	echo "Error: sqlc generation"
	exit 1
else
	printf "done.\n"
fi

echo "Running tests..."
if ! go test -v ./...; then
	echo "Error: tests failed"
	exit 1
fi

