.PHONY: test build clean

test:
	go vet ./...
	go test -race ./...
	node --check internal/pasadatos/web/assets/desktop.js
	node --check internal/pasadatos/web/assets/mobile.js

build:
	./scripts/build-release.sh

clean:
	rm -f release/*.exe release/SHA256SUMS.txt
	rm -f deploy/bin/pasadatos-server-linux-amd64 deploy/bin/SHA256SUMS.txt
