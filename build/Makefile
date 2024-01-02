build-linux:
	env GOOS=linux GOARCH=amd64 go build -ldflags "-w -s" -o mars-amd64 ../main.go
linux-upx:
	docker run --rm -w $$PWD -v $$PWD:$$PWD gruebel/upx:latest --best --lzma -o mars-amd64-upx mars-amd64