# machin — MFL toolchain build

BIN     := bin/machin
PREFIX  ?= /usr/local
GOFLAGS ?= -trimpath

.PHONY: all build test mfl-test mfl-cover cover examples bench cov-floor install uninstall clean

all: build

# Compile the machin toolchain into a single native binary.
build:
	@mkdir -p bin
	go build $(GOFLAGS) -o $(BIN) .
	@echo "built $(BIN)"

# Run the Go test suite (compiles + executes MFL programs natively), then the MFL
# suites written in machin itself.
test: mfl-test
	go test ./...

# Run the MFL test suites — the ones written IN machin, via `machin test`, rather
# than through the Go harness. Each suite is one invocation: `machin test`
# composes its inputs into a single program, so separate suites must not share one.
#
# This exists because framework/tests/flags_test.src sat in the repo unexecuted by
# anything: the Go test that appears to cover flags.src actually writes its own
# inline snippet. Nothing would have noticed if the .src suite broke — and its own
# documented invocation had in fact stopped working.
mfl-test: build
	$(BIN) test framework/flags.src framework/tests/flags_test.src

# Same suites with function + statement coverage (#589). Not wired into `test`:
# coverage is a report to read, not a gate, and there is no threshold to enforce
# yet — 15 of 16 framework modules have no MFL tests at all (see the tracking issue).
mfl-cover: build
	$(BIN) test --cover framework/flags.src framework/tests/flags_test.src

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

# Coverage floor — runs TestTypesCoverageFloor which fails if package total
# drops below TYPES_COVERAGE_FLOOR (default 87.0%, post-Phase-1-6 floor with
# ~0.8pp headroom). The guard test itself adds ~1.2pp to the package
# denominator (subprocess-skip path), so the post-test floor sits below the
# pre-test 89.1% reference. Bypasses `-short` via `-count=1 -run` so it
# always runs.
#   make cov-floor TYPES_COVERAGE_FLOOR=88.0   # ad-hoc check (lower bar)
#   make cov-floor TYPES_COVERAGE_FLOOR=95.0   # stretch target
TYPES_COVERAGE_FLOOR ?= 87.0
cov-floor:
	@TYPES_COVERAGE_FLOOR=$(TYPES_COVERAGE_FLOOR) \
		go test -count=1 -run '^TestTypesCoverageFloor$$' -v .

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
