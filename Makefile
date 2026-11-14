BIN := dist/forever-pulse-companion

.PHONY: build test run clean

build:
	mkdir -p dist
	go build -trimpath -buildvcs=false -ldflags "-s -w" -o $(BIN) ./cmd/forever-pulse-companion

test:
	go vet ./...
	go test ./... -count=1

run: build
	./$(BIN)

clean:
	rm -rf dist
