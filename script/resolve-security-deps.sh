#!/usr/bin/env bash
set -euo pipefail
# Only used for dependency resolution during development. Commit the generated
# go.mod and go.sum before enabling the release workflow.
go mod edit -go=1.27.0
go mod tidy
go get golang.org/x/net@latest google.golang.org/grpc@latest \
  golang.org/x/text@latest golang.org/x/crypto@latest \
  github.com/go-viper/mapstructure/v2@latest \
  google.golang.org/protobuf@latest
go get github.com/gin-gonic/gin@latest github.com/gorilla/websocket@latest \
  gorm.io/gorm@latest gorm.io/driver/sqlite@latest github.com/mattn/go-sqlite3@latest
go mod tidy
