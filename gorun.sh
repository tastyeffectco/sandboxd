#!/usr/bin/env bash
# Run go inside the build image against ./control-plane with persistent caches.
exec docker run --rm \
  -v /home/ubuntu/sandboxd/control-plane:/src \
  -v /home/ubuntu/.gocache/mod:/go/pkg/mod \
  -v /home/ubuntu/.gocache/build:/root/.cache/go-build \
  -w /src golang:1.22-bookworm "$@"
