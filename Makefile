.PHONY: build web test demo

# Build the web dashboard, then the Go binary that embeds it.
build: web
	go build -o caycle .

web:
	cd web && npm install --no-audit --no-fund && npm run build

test:
	go test ./...

# Try everything without a bike: http://localhost:8080
demo: build
	./caycle serve -demo
