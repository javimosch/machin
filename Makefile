# machin — MFL toolchain build

BIN     := bin/machin
PREFIX  ?= /usr/local
GOFLAGS ?= -trimpath

.PHONY: all build test examples bench bench-report install uninstall clean

all: build

# Compile the machin toolchain into a single native binary.
build:
	@mkdir -p bin
	go build $(GOFLAGS) -o $(BIN) .
	@echo "built $(BIN)"

# Run the Go test suite (compiles + executes MFL programs natively).
test:
	go test ./...

# Compile and run every example program.
examples: build
	./examples/run.sh

# fib(40) benchmark — native MFL vs the C the compiler emits.
bench: build
	$(BIN) build examples/bench/fib.mfl -o bin/fib
	@echo "running fib(40):"
	@time bin/fib

# Reproducible benchmark report — MFL vs hand-written C vs Rust.
# Verifies all implementations agree, then prints a Markdown table.
# See docs/BENCHMARKS.md.
bench-report:
	@./examples/bench/run.sh

# Install the toolchain (requires a C compiler on PATH at runtime).
install: build
	install -Dm755 $(BIN) $(DESTDIR)$(PREFIX)/bin/machin
	@echo "installed to $(DESTDIR)$(PREFIX)/bin/machin"

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/machin

clean:
	rm -rf bin
