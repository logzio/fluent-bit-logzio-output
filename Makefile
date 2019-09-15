all:
	go build -buildmode=c-shared -o build/out_logzio.so ./output

clean:
	rm -rf *.so *.h build/