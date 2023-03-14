# Go compiler
GO := go

# Flags for the Go compiler
GOFLAGS := -v

# Build target (build all programs)
all: $(patsubst cmd/%,bin/%,$(wildcard cmd/*))

# Build target for each program
bin/%: cmd/%/main.go
	$(GO) build $(GOFLAGS) -o $@ $<

# Clean up all binaries
clean:
	rm -f bin/*

# Test target (run all tests)
test:
	$(GO) test -v ./...

bench:
	go test -v -bench=. -benchmem
