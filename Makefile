.PHONY: build web test browser-test install desktop-linux desktop-windows server-linux

build: web
	CGO_ENABLED=0 go build -trimpath -o bin/ssh-gateway ./cmd/ssh-gateway

web:
	cd web && npm ci && npm run build

test:
	go test -race ./... -timeout=120s
	go vet ./...

browser-test: web
	cd web && npm test

install: build
	./scripts/install.sh

desktop-linux: web
	go -C desktop build -tags 'desktop,production,webkit2_41' -o ../dist/ssh-gateway-desktop .
	bash packaging/linux/build.sh

desktop-windows: web
	go -C desktop run ./tools/winres
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C desktop build -tags 'desktop,production' -ldflags '-H windowsgui' -o ../dist/ssh-gateway-desktop-windows.exe .

server-linux: web
	bash packaging/server/build.sh
