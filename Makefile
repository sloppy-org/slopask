.PHONY: build test clean

build:
	go build -o slopask ./cmd/slopask

test:
	go test -v -count=1 ./...

clean:
	rm -f slopask
