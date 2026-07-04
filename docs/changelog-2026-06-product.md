
        <div class="feature-card rounded-xl p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-lg bg-blue-500/10 flex items-center justify-center flex-shrink-0">
              <span class="text-2xl">🧬</span>
            </div>
            <div>
              <h3 class="text-xl font-semibold text-white mb-2">A new machine-first language</h3>
              <p class="text-slate-400 leading-relaxed">MFL was born: a backend language whose source is plain canonical text — one normalized declaration per line — designed to be written and read by machines, not humans. The whole toolchain (lexer, parser, type inference, code generator) ships in Go.</p>
            </div>
          </div>
        </div>

        <div class="feature-card rounded-xl p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-lg bg-emerald-500/10 flex items-center justify-center flex-shrink-0">
              <span class="text-2xl">⚡</span>
            </div>
            <div>
              <h3 class="text-xl font-semibold text-white mb-2">Compiles to native code</h3>
              <p class="text-slate-400 leading-relaxed">Programs are translated to C and compiled with cc -O2 — values are unboxed and there is no runtime VM. fib(40) runs in ~0.20s, neck-and-neck with hand-written C. Static typing is fully inferred: no annotations, type clashes caught at build time.</p>
            </div>
          </div>
        </div>

        <div class="feature-card rounded-xl p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-lg bg-purple-500/10 flex items-center justify-center flex-shrink-0">
              <span class="text-2xl">🧱</span>
            </div>
            <div>
              <h3 class="text-xl font-semibold text-white mb-2">The full Go-flavored core</h3>
              <p class="text-slate-400 leading-relaxed">Slices, structs, and maps; closures and higher-order functions; implicit generics by monomorphization; multiple and named returns; variadic parameters; and range loops — the language reads and writes much like Go, all compiled with no boxing.</p>
            </div>
          </div>
        </div>

        <div class="feature-card rounded-xl p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-lg bg-amber-500/10 flex items-center justify-center flex-shrink-0">
              <span class="text-2xl">🧵</span>
            </div>
            <div>
              <h3 class="text-xl font-semibold text-white mb-2">Concurrency that doesn't leak</h3>
              <p class="text-slate-400 leading-relaxed">Goroutines (pthread-backed), channels for communication, and a per-goroutine arena that reclaims memory when a request finishes — so a long-running server stays memory-bounded (RSS flat across 12,000 requests).</p>
            </div>
          </div>
        </div>

        <div class="feature-card rounded-xl p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-lg bg-blue-500/10 flex items-center justify-center flex-shrink-0">
              <span class="text-2xl">🌐</span>
            </div>
            <div>
              <h3 class="text-xl font-semibold text-white mb-2">Bidirectional JSON over HTTP</h3>
              <p class="text-slate-400 leading-relaxed">BSD sockets, concurrent HTTP serving, JSON serialization and parsing (json(x) / parse(s, T{})), plus a string-operations library — enough to build a routed JSON API on a native binary with no runtime dependencies.</p>
            </div>
          </div>
        </div>

        <div class="feature-card rounded-xl p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-lg bg-emerald-500/10 flex items-center justify-center flex-shrink-0">
              <span class="text-2xl">🚀</span>
            </div>
            <div>
              <h3 class="text-xl font-semibold text-white mb-2">machweb — a backend framework</h3>
              <p class="text-slate-400 leading-relaxed">A tiny web framework written in MFL itself: Request/Response types, response builders, request parsing, and a map-based router that dispatches each request — in its own goroutine — to a handler. Compose it with your app and compile to a single executable.</p>
            </div>
          </div>
        </div>

        <div class="feature-card rounded-xl p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-lg bg-red-500/10 flex items-center justify-center flex-shrink-0">
              <span class="text-2xl">🛡️</span>
            </div>
            <div>
              <h3 class="text-xl font-semibold text-white mb-2">--safe mode, real closures, and scoped arenas</h3>
              <p class="text-slate-400 leading-relaxed">`machin run|build --safe` catches out-of-range indexing, divide-by-zero, and integer overflow at runtime. Closures now capture by reference (Go semantics), so the counter/accumulator idiom just works. And `arena { }` blocks reclaim everything allocated inside them when the block ends, keeping long-lived loops memory-flat.</p>
            </div>
          </div>
        </div>

        <div class="feature-card rounded-xl p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-lg bg-purple-500/10 flex items-center justify-center flex-shrink-0">
              <span class="text-2xl">📦</span>
            </div>
            <div>
              <h3 class="text-xl font-semibold text-white mb-2">Released v0.1.0 → v0.4.1</h3>
              <p class="text-slate-400 leading-relaxed">Five tagged releases this month, a formal language specification (SPEC.md), 35+ runnable examples, a self-hosted server that serves the project's own catalog, and release automation — pushing a `v*` tag now cross-compiles machin for linux/macOS × amd64/arm64 and attaches the binaries to the GitHub release.</p>
            </div>
          </div>
        </div>
