#!/bin/sh

target="$1"

if [ -z "$target" ] ; then
	if [ "$(uname)" = "Linux" ] ; then
		target=linux
		compiler=gcc
	else
		# install mingw-w64 for unarr
		target=windows
		compiler=x86_64-w64-mingw32-gcc
	fi
fi

CGO_ENABLED=1 GOARCH=amd64 GOOS=${target} CC=${compiler} go build -ldflags '-w'
