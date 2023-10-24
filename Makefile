all:
	go build -trimpath -buildmode=c-shared -o build/out_logzio.so ./output

clean:
	rm -rf *.so *.h build/

windows:
	# Building modules for windows from macOS with CGO enabled requires cross dedicated compiler, e.g: mingw-w64 toolchain
	# https://stackoverflow.com/questions/36915134/go-golang-cross-compile-from-mac-to-windows-fatal-error-windows-h-file-not-f
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -trimpath -buildmode=c-shared -o build/out_logzio-windows.so ./output

linux-amd:
	# Building modules for linux from macOS with CGO enabled requires dedicated cross compiler, e.g:
	# brew tap messense/macos-cross-toolchains
	# brew install x86_64-unknown-linux-gnu
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=x86_64-unknown-linux-gnu-gcc go build -trimpath -buildmode=c-shared -o build/out_logzio-linux.so ./output

linux-arm:
	# brew install aarch64-unknown-linux-gnu
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-unknown-linux-gnu-gcc go build -trimpath -buildmode=c-shared -o build/out_logzio-linux-arm64.so ./output