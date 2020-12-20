# install mingw-w64 for unarr
GOARCH=amd64 GOOS=windows CC=x86_64-w64-mingw32-gcc go build -ldflags '-w'
