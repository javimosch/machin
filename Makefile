# machin — MFL toolchain build

BIN     := bin/machin
PREFIX  ?= /usr/local
GOFLAGS ?= -trimpath

.PHONY: all build test cover examples bench install uninstall clean fmt vet

all: build

# Fail if any tracked .go file isn't gofmt-clean (excludes vendor/selfhost, which
# aren't Go source).
fmt:
	@unformatted="$$(gofmt -l . | grep -v '^vendor/' | grep -v '^selfhost/')"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; echo "$$unformatted"; exit 1; \
	fi

# Run go vet across the toolchain.
vet:
	go vet ./...

# Compile the machin toolchain into a single native binary.
build:
	@mkdir -p bin
	go build $(GOFLAGS) -o $(BIN) .
	@echo "built $(BIN)"

# Run the Go test suite (compiles + executes MFL programs natively).
test:
	go test ./...

# Statement coverage of the compiler. `make cover` prints the % and writes an
# HTML report to coverage.html (open it to see which lines are unhit). The corpus
# is the MFL programs the tests compile+run, so this tracks how much of the
# toolchain the test suite actually exercises.
cover:
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1
	@go tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

# Compile and run every non-server example program (see examples/run.sh).
examples: build
	./examples/run.sh

# fib(40) benchmark — native MFL vs the C the compiler emits.
bench: build
	$(BIN) build examples/bench/fib.mfl -o bin/fib
	@echo "running fib(40):"
	@time bin/fib

# Install the toolchain (requires a C compiler on PATH at runtime).
install: build
	install -Dm755 $(BIN) $(DESTDIR)$(PREFIX)/bin/machin
	@echo "installed to $(DESTDIR)$(PREFIX)/bin/machin"

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/machin

clean:
	rm -rf bin
