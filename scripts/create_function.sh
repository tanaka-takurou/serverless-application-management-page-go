#!/bin/bash
cd `dirname $0`/../
rm function.zip
rm bootstrap
zip -r9 function.zip templates
GOARCH=arm64 GOOS=linux CGO_ENABLED=0 go build -o bootstrap main.go
zip -g function.zip bootstrap
