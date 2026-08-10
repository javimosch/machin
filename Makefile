# machin — MFL toolchain build

BIN     := bin/machin
PREFIX  ?= /usr/local
GOFLAGS ?= -trimpath

.PHONY: all build test mfl-test mfl-cover mfl-cov-floor version-check cover examples bench cov-floor install uninstall clean

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
	$(BIN) test framework/bson.src framework/tests/bson_test.src
	$(BIN) test framework/xml.src framework/tests/xml_test.src
	$(BIN) test framework/reactive.src framework/tests/reactive_test.src

# Same suites with function + statement coverage (#589), as a human-readable report.
mfl-cover: build
	$(BIN) test --cover framework/flags.src framework/tests/flags_test.src
	$(BIN) test --cover framework/bson.src framework/tests/bson_test.src
	$(BIN) test --cover framework/xml.src framework/tests/xml_test.src
	$(BIN) test --cover framework/reactive.src framework/tests/reactive_test.src

# MFL coverage floors — a RATCHET, like cov-floor is for the Go side. Set just
# under what the suites measure today (flags.src: 90.9% function, 82.1% statement
# for the whole composed program), so a regression fails CI without the repo
# claiming a number it has not earned.
#
# Raise these as #593 adds suites for the other pure modules. Lowering one is
# allowed but must be deliberate and explained in the commit — that is the whole
# point of a floor being a number in a file rather than a habit.
# Fail if main advertises a version that was never released — see the script for
# the v0.123.0 incident this exists to prevent recurring.
version-check:
	@./tools/version-check.sh

MFL_FUNC_FLOOR ?= 90
MFL_STMT_FLOOR ?= 75
# bson.src is fully covered (24/24); hold it there rather than at the shared floor.
BSON_FUNC_FLOOR ?= 100
# xml.src is fully covered too (31/31).
XML_FUNC_FLOOR ?= 100
# reactive.src tops out at 13/17 (76.5%): bind/each/hydrate/mount call the
# extern "env" DOM imports, which have no native definition, so calling one makes
# the test program fail to LINK rather than fail an assertion. The ceiling is a
# property of the module, not a gap in the suite — raising this floor requires a
# native stub or a wasm test path (#593), not more tests.
REACTIVE_FUNC_FLOOR ?= 76

mfl-cov-floor: build
	@$(BIN) test --cover --json framework/flags.src framework/tests/flags_test.src 2>/dev/null \
		| python3 tools/cov-floor.py --module framework/flags.src \
			--func $(MFL_FUNC_FLOOR) --stmt $(MFL_STMT_FLOOR)
	@$(BIN) test --cover --json framework/bson.src framework/tests/bson_test.src 2>/dev/null \
		| python3 tools/cov-floor.py --module framework/bson.src \
			--func $(BSON_FUNC_FLOOR) --stmt $(MFL_STMT_FLOOR)
	@$(BIN) test --cover --json framework/xml.src framework/tests/xml_test.src 2>/dev/null \
		| python3 tools/cov-floor.py --module framework/xml.src \
			--func $(XML_FUNC_FLOOR) --stmt $(MFL_STMT_FLOOR)
	@$(BIN) test --cover --json framework/reactive.src framework/tests/reactive_test.src 2>/dev/null \
		| python3 tools/cov-floor.py --module framework/reactive.src \
			--func $(REACTIVE_FUNC_FLOOR) --stmt $(MFL_STMT_FLOOR)

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
