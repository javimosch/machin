package main

import (
	"fmt"
	"strconv"
	"strings"
)

// CompileToC type-checks the program and emits standalone C99. The C is fed to
// `cc -O2`, so MFL's runtime cost is whatever the C compiler's optimizer
// produces — native, on par with C/Rust/Zig for scalar code.
func CompileToC(p *Program, safe bool) (string, error) {
	src, _, err := CompileToCTarget(p, safe, targetNative)
	return src, err
}

// CompileToCTarget compiles to C for a given target ("native" or "wasm"). For
// the wasm target it also returns the C names of the program's `export func`s
// (the symbols the wasm linker exports). The emitted C differs by target: a wasm
// build tags FFI imports with wasm import attributes, omits the POSIX socket/tty
// runtime unless used, and omits the native `int main` entry point.
func CompileToCTarget(p *Program, safe bool, target string) (string, []string, error) {
	if target == "" {
		target = targetNative
	}
	liftClosures(p) // idempotent: a no-op once literals are already lifted
	c, err := Check(p)
	if err != nil {
		return "", nil, err
	}
	g := &cgen{c: c, safe: safe, target: target, probeVars: debugProbeVars, jsonMemo: map[string]string{}, parseMemo: map[string]string{}, chanJSONMemo: map[string][2]string{}}
	src, err := g.program(p)
	if err != nil {
		return "", nil, err
	}
	return src, c.ExportNames(), nil
}

// cTypeName renders a declared type string (int, float, bool, string, []elem,
// or a struct name) as C — used for struct field declarations.
func cTypeName(t string) string {
	if strings.HasPrefix(t, "[]") {
		return "mfl_slice"
	}
	if strings.HasPrefix(t, "map[") {
		return "mfl_map*"
	}
	if strings.HasPrefix(t, "chan ") {
		return "mfl_chan*"
	}
	switch t {
	case "int":
		return "int64_t"
	case "float":
		return "double"
	case "bool":
		return "int"
	case "string":
		return "char*"
	case "func":
		return "mfl_closure"
	}
	return "mfl_" + t
}

type cgen struct {
	c            *Checker
	buf          strings.Builder // function bodies
	tramp        strings.Builder // goroutine trampolines
	goID         int
	jsonFns      strings.Builder      // generated per-type JSON serializers + parsers
	jsonMemo     map[string]string    // type string -> serializer function name
	parseMemo    map[string]string    // type string -> parser function name
	chanJSONMemo map[string][2]string // type string -> {serWrapper, desWrapper}
	jsonID       int
	rangeID      int             // unique temp names for for-range loops
	tmpID        int             // unique temp names for multi-assignment
	arenaID      int             // unique temp names for scoped-arena blocks
	curFn        string          // name of the function currently being emitted
	safe         bool            // emit runtime bounds / div-by-zero / overflow checks
	usesTLS      bool            // program calls https_get/https_post -> emit + link OpenSSL
	usesWSS      bool            // program calls wss_* -> emit WebSocket runtime + link OpenSSL
	usesRegex    bool            // program calls regex_* -> emit POSIX regex runtime
	usesSQLite   bool            // program calls sqlite_* -> emit SQLite runtime + link -lsqlite3
	usesCrypto   bool            // program calls crypto builtins -> emit crypto runtime + link -lcrypto
	usesSelect   bool            // program uses `select` (now record/replay-gated; kept for diagnostics)
	usesXEdDSA   bool            // program calls xeddsa_* -> emit XEdDSA runtime + link -lsodium -lcrypto
	usesMath     bool            // program calls math builtins (sin/cos/sqrt/...) -> emit math runtime + link -lm
	usesNoise    bool            // program calls noise2/noise3 -> emit Perlin noise runtime + link -lm
	usesNet      bool            // program calls dial/listen/accept/read/write/close(fd) -> emit POSIX socket runtime
	usesTTY      bool            // program calls raw_mode/read_key -> emit termios/select runtime
	target       string          // "" or "native" (default) -> cc; "wasm" -> zig cc, lean runtime, FFI as imports, exports
	globals      map[string]bool // package-global names (emitted as C statics, mfl_g_<name>)
	bodyOnly     bool            // oracle mode: emit only the program-specific C (skip the static runtime blocks)
	probeVars    []string        // `machin replay --print <var>`: emit a probe after each assignment to these vars
}

// debugProbeVars is set by `machin replay --print` just before it (re)builds the recorded
// program, so codegen instruments the named variables. It is empty for every normal build,
// so `machin run`/`build` and the cgentest oracle-diff emit no probes and pay nothing.
var debugProbeVars []string

// wantsProbe reports whether `name` is being watched by `machin replay --print`.
func (g *cgen) wantsProbe(name string) bool {
	for _, v := range g.probeVars {
		if v == name {
			return true
		}
	}
	return false
}

// build targets.
const (
	targetNative = "native"
	targetWasm   = "wasm"
)

func (g *cgen) wasm() bool { return g.target == targetWasm }

const cRuntime = `#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdarg.h>
#include <stddef.h>
#include <ctype.h>
#include <unistd.h>
#include <time.h>
#include <sys/time.h>
#include <pthread.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <netdb.h>
#include <dirent.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <errno.h>
#include <termios.h>
#include <sys/select.h>
#ifndef __wasm__
#include <sys/mman.h>   /* mmap_file / madvise_free — POSIX-only; wasi-libc has no mmap (issue #463) */
#include <sys/wait.h>
#include <signal.h>
#endif

/* slices: a Go-style header over an unboxed backing array */
typedef struct { void* data; int64_t len; int64_t cap; } mfl_slice;

/* closures: a function pointer plus a heap environment of captured values */
typedef struct { void* fn; void* env; } mfl_closure;

/* ---- arena memory management ----
   Value buffers (strings, slice backings, closure environments) are allocated
   from a per-goroutine arena and reclaimed in bulk when the goroutine finishes.
   The main goroutine's arena lives for the whole program. This bounds the
   memory of a long-running concurrent server: each request handler runs in its
   own goroutine and frees everything it allocated on return. Subsystems that
   free explicitly (channels, maps, goroutine args) use raw malloc/free. */
typedef struct mfl_blk { struct mfl_blk* next; size_t size; } mfl_blk;
typedef struct { mfl_blk* head; } mfl_arena;
static mfl_arena mfl_main_arena = { NULL };
static _Thread_local mfl_arena* mfl_arena_cur = NULL;
static void* mfl_alloc(size_t sz) {
    if (!mfl_arena_cur) mfl_arena_cur = &mfl_main_arena;
    if (sz == 0) sz = 1;
    mfl_blk* b = malloc(sizeof(mfl_blk) + sz);
    b->size = sz; b->next = mfl_arena_cur->head; mfl_arena_cur->head = b;
    return (void*)(b + 1);
}
/* mfl_calloc is only ever used to box a local captured by a nested closure
   (see function() in codegen.go); that box must outlive the arena of whoever
   declares it, so it's plain calloc, not mfl_alloc (#314). */
static void* mfl_calloc(size_t n, size_t sz) { return calloc(n, sz); }
static void* mfl_realloc(void* old, size_t sz) {
    void* p = mfl_alloc(sz);
    if (old) { size_t o = ((mfl_blk*)old - 1)->size; memcpy(p, old, o < sz ? o : sz); }
    return p; /* old reclaimed with its arena */
}
/* substr strlen-cache: mfl_substr only needs strlen(s) to clamp the end offset.
   In hot loops (the lexer, parsers, scanners) the same source pointer is sliced
   thousands of times, so caching its length by pointer identity turns a per-call
   O(strlen) scan into O(1) while preserving exact clamping semantics. Two distinct
   *live* strings never share an address (the arena frees nothing mid-life), so
   pointer identity ⇒ same length — but a freed block's address can be reused by a
   later malloc, so mfl_arena_free invalidates the cache. */
static _Thread_local const char* mfl_strlen_cache_s = NULL;
static _Thread_local int64_t mfl_strlen_cache_n = 0;
static inline int64_t mfl_strlen_cached(const char* s) {
    if (s == mfl_strlen_cache_s) return mfl_strlen_cache_n;
    int64_t n = (int64_t)strlen(s);
    mfl_strlen_cache_s = s; mfl_strlen_cache_n = n;
    return n;
}
static void mfl_arena_free(mfl_arena* a) {
    mfl_blk* b = a->head;
    while (b) { mfl_blk* n = b->next; free(b); b = n; }
    a->head = NULL;
    mfl_strlen_cache_s = NULL; /* freed addresses may be reused — drop stale length */
}

/* --safe runtime checks (used only when the program is built with --safe) */
static void mfl_panic(const char* msg);   /* defined after the record/replay runtime (it enriches a crash with the schedule that led there) */
static int64_t mfl_bounds(int64_t i, int64_t n) {
    if (i < 0 || i >= n) { char b[80]; snprintf(b, 80, "index out of range [%lld] with length %lld", (long long)i, (long long)n); mfl_panic(b); }
    return i;
}
static int64_t mfl_idiv(int64_t a, int64_t b) { if (b == 0) mfl_panic("integer divide by zero"); return a / b; }
static int64_t mfl_imod(int64_t a, int64_t b) { if (b == 0) mfl_panic("integer modulo by zero"); return a % b; }
static int64_t mfl_iadd(int64_t a, int64_t b) { int64_t r; if (__builtin_add_overflow(a, b, &r)) mfl_panic("integer overflow (+)"); return r; }
static int64_t mfl_isub(int64_t a, int64_t b) { int64_t r; if (__builtin_sub_overflow(a, b, &r)) mfl_panic("integer overflow (-)"); return r; }
static int64_t mfl_imul(int64_t a, int64_t b) { int64_t r; if (__builtin_mul_overflow(a, b, &r)) mfl_panic("integer overflow (*)"); return r; }

static mfl_slice mfl_append(mfl_slice s, const void* elem, int64_t es) {
    if (s.len >= s.cap) {
        int64_t nc = s.cap ? s.cap * 2 : 4;
        s.data = mfl_realloc(s.data, nc * es); s.cap = nc;
    }
    memcpy((char*)s.data + s.len * es, elem, es);
    s.len++;
    return s;
}
static mfl_slice mfl_lit_i64(int64_t n, ...) {
    mfl_slice s = { n ? mfl_alloc(n * 8) : NULL, n, n };
    va_list ap; va_start(ap, n);
    for (int64_t i = 0; i < n; i++) ((int64_t*)s.data)[i] = va_arg(ap, int64_t);
    va_end(ap); return s;
}
static mfl_slice mfl_lit_f64(int64_t n, ...) {
    mfl_slice s = { n ? mfl_alloc(n * 8) : NULL, n, n };
    va_list ap; va_start(ap, n);
    for (int64_t i = 0; i < n; i++) ((double*)s.data)[i] = va_arg(ap, double);
    va_end(ap); return s;
}
static mfl_slice mfl_lit_str(int64_t n, ...) {
    mfl_slice s = { n ? mfl_alloc(n * sizeof(char*)) : NULL, n, n };
    va_list ap; va_start(ap, n);
    for (int64_t i = 0; i < n; i++) ((char**)s.data)[i] = va_arg(ap, char*);
    va_end(ap); return s;
}
static int64_t mfl_exit(int64_t code) { exit((int)code); return 0; }
static int64_t mfl_flush(void) { fflush(stdout); return 0; }
static void mfl_sleep(int64_t ms) {
    struct timespec ts = { ms / 1000, (ms % 1000) * 1000000L };
    nanosleep(&ts, NULL);
}

/* channels: a mutex + condvar FIFO. An element's heap data lives in the sending
   goroutine's arena, which is reclaimed when that goroutine finishes — so the
   channel must copy it somewhere stable on send and into the receiver's arena on
   receive. Two marshaling modes (chosen by codegen from the element type):
     - string mode (stroff[]): byte offsets of every string (char*) reachable by
       value in the element; send deep-copies them, receive adopts them.
     - json mode (ser/des): for elements containing a slice or map, the whole
       value is serialized to JSON on send and parsed back on receive — a general
       deep copy that handles arbitrary nesting (slices, maps, structs).
   Scalar elements need neither and are a plain memcpy. */
static char* mfl_dup_arena(const char* s, size_t n); /* defined later in the runtime */
typedef struct mfl_cnode { struct mfl_cnode* next; void* data; } mfl_cnode;
typedef struct {
    pthread_mutex_t mu; pthread_cond_t cnd;
    mfl_cnode *head, *tail; int64_t es; int closed;
    int nstr; int* stroff;
    char* (*ser)(const void*);          /* json mode: element -> arena JSON */
    void (*des)(const char*, void*);    /* json mode: JSON -> *out (arena) */
} mfl_chan;
static mfl_chan* mfl_make_chan(int64_t es, char* (*ser)(const void*), void (*des)(const char*, void*), int nstr, ...) {
    mfl_chan* c = malloc(sizeof(mfl_chan));
    pthread_mutex_init(&c->mu, NULL); pthread_cond_init(&c->cnd, NULL);
    c->head = c->tail = NULL; c->es = es; c->closed = 0;
    c->ser = ser; c->des = des;
    c->nstr = nstr; c->stroff = NULL;
    if (nstr > 0) {
        c->stroff = (int*)malloc((size_t)nstr * sizeof(int));
        va_list ap; va_start(ap, nstr);
        for (int i = 0; i < nstr; i++) c->stroff[i] = va_arg(ap, int);
        va_end(ap);
    }
    return c;
}
/* freeze: replace each string field (found at the given byte offsets within
   elem) with a stable malloc'd copy -- the value is being handed off away
   from the current arena (to a channel, or across a go-statement's arena
   boundary; #310). Shared by mfl_chan_freeze and mfl_go's argument passing. */
static void mfl_freeze_strs(int nstr, int* stroff, void* elem) {
    for (int i = 0; i < nstr; i++) {
        char** p = (char**)((char*)elem + stroff[i]);
        if (*p) { size_t n = strlen(*p); char* d = (char*)malloc(n + 1); memcpy(d, *p, n + 1); *p = d; }
    }
}
/* thaw: move each frozen (malloc'd) string into the CURRENT arena, freeing the
   malloc'd copy. After this the value's strings live exactly as long as
   whichever goroutine's arena is current when this runs. */
static void mfl_thaw_strs(int nstr, int* stroff, void* elem) {
    for (int i = 0; i < nstr; i++) {
        char** p = (char**)((char*)elem + stroff[i]);
        if (*p) { char* a = mfl_dup_arena(*p, strlen(*p)); free(*p); *p = a; }
    }
}
static void mfl_chan_freeze(mfl_chan* c, void* elem) { mfl_freeze_strs(c->nstr, c->stroff, elem); }
static void mfl_chan_thaw(mfl_chan* c, void* elem) { mfl_thaw_strs(c->nstr, c->stroff, elem); }
/* record/replay hooks (defined below, before mfl_chan_send). */
static void mfl_rr_enter(void); static void mfl_rr_mark(void); static void mfl_rr_exit(void);
/* close a channel: receivers drain the buffer then get "not ok". Wakes every
   blocked receiver so range/recv stop instead of hanging forever. close is an
   observable, ordered channel op -- it takes a turn so replay reproduces its
   position relative to receivers (a recv after close gets "not ok"). */
static void mfl_chan_close(mfl_chan* c) {
    mfl_rr_enter();
    pthread_mutex_lock(&c->mu);
    c->closed = 1;
    pthread_cond_broadcast(&c->cnd);
    mfl_rr_mark();
    pthread_mutex_unlock(&c->mu);
    mfl_rr_exit();
}
/* ---- record/replay (Phase 0 spike) ------------------------------------------
   SOUND because the program is proved data-race-free: the only inter-goroutine
   nondeterminism is the ORDER channel operations complete. We record that order
   as a sequence of goroutine ids (--record) and, on --replay, gate each
   channel op so it fires in exactly the recorded order -- reproducing the run
   without recording a single memory access (which is what makes rr unsound
   under races and x86-only). Scope: channel-only concurrency, no I/O yet. */
/* Stable goroutine id = a parent-relative PATH ("0", "0.1", "0.1.2"). Assigned in
   the spawning goroutine's program order (not in the racing new thread), so a
   goroutine's id is identical across record and replay even when goroutines spawn
   goroutines concurrently — the global-counter scheme of the spike would race. */
static _Thread_local char* mfl_gid_path = NULL;  /* this goroutine's path */
static _Thread_local int mfl_spawn_ctr = 0;      /* this goroutine's spawn count */
static char* mfl_path_child(const char* parent, int idx) {
    char* r = malloc(strlen(parent) + 16);
    sprintf(r, "%s.%d", parent, idx);
    return r;
}
static int mfl_rr_mode = 0;              /* 0 off, 1 record, 2 replay */
static FILE* mfl_rr_out = NULL;
static char** mfl_rr_trace = NULL; static int mfl_rr_n = 0; static int mfl_rr_pos = 0;  /* schedule (S lines) */
static pthread_mutex_t mfl_rr_mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t  mfl_rr_cnd = PTHREAD_COND_INITIALIZER;

/* trace format v1: a header line "MFLRR 1", then tagged lines --
   "S <path>"        one channel-op completion, in order (the schedule); and
   "I <path> <hex>"  one I/O result (time/stdin), hex-encoded. Each goroutine
   replays its OWN I/O queue in order, so the I/O log needs no global ordering --
   the schedule already orders everything observable across goroutines. */
static char* mfl_hexenc(const char* s, size_t n) {
    char* r = malloc(n * 2 + 1);
    for (size_t i = 0; i < n; i++) sprintf(r + i * 2, "%02x", (unsigned char)s[i]);
    r[n * 2] = 0; return r;
}
static char* mfl_hexdec(const char* h, size_t* outlen) {
    size_t n = strlen(h) / 2; char* r = malloc(n + 1);
    for (size_t i = 0; i < n; i++) { unsigned v; sscanf(h + i * 2, "%2x", &v); r[i] = (char)v; }
    r[n] = 0; *outlen = n; return r;
}
/* per-goroutine I/O queue (replay): path -> the values that gid observed, in order */
typedef struct { char* path; char** vals; int n, cap, cur; } mfl_io_q;
static mfl_io_q* mfl_io_qs = NULL; static int mfl_io_nq = 0, mfl_io_capq = 0;
static mfl_io_q* mfl_io_find(const char* path) {
    for (int i = 0; i < mfl_io_nq; i++) if (strcmp(mfl_io_qs[i].path, path) == 0) return &mfl_io_qs[i];
    if (mfl_io_nq == mfl_io_capq) { mfl_io_capq = mfl_io_capq ? mfl_io_capq * 2 : 8; mfl_io_qs = realloc(mfl_io_qs, mfl_io_capq * sizeof(mfl_io_q)); }
    mfl_io_q* q = &mfl_io_qs[mfl_io_nq++]; q->path = strdup(path); q->vals = NULL; q->n = q->cap = q->cur = 0; return q;
}
static void mfl_io_push(const char* path, const char* hex) {
    mfl_io_q* q = mfl_io_find(path);
    if (q->n == q->cap) { q->cap = q->cap ? q->cap * 2 : 4; q->vals = realloc(q->vals, q->cap * sizeof(char*)); }
    q->vals[q->n++] = strdup(hex);
}
static int mfl_rr_io_underrun = 0;   /* replay needed more I/O than was recorded -> divergence */
static const char* mfl_io_pop(void) {
    mfl_io_q* q = mfl_io_find(mfl_gid_path);
    if (q->cur < q->n) return q->vals[q->cur++];
    mfl_rr_io_underrun = 1;
    return "";
}
/* record an I/O result (called by the interposed builtins). */
static void mfl_rr_io_log_hex(const char* h) {
    pthread_mutex_lock(&mfl_rr_mu);
    if (mfl_rr_out) fprintf(mfl_rr_out, "I %s %s\n", mfl_gid_path, h);
    pthread_mutex_unlock(&mfl_rr_mu);
}
static void mfl_rr_io_log_i64(int64_t v) { char b[32]; snprintf(b, sizeof b, "%lld", (long long)v); char* h = mfl_hexenc(b, strlen(b)); mfl_rr_io_log_hex(h); free(h); }
static void mfl_rr_io_log_bytes(const char* s, size_t n) { char* h = mfl_hexenc(s, n); mfl_rr_io_log_hex(h); free(h); }
static int64_t mfl_rr_io_pop_i64(void) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); int64_t v = L ? strtoll(s, NULL, 10) : 0; free(s); return v; }
/* pop the next recorded I/O value into a caller buffer (up to n bytes); the recorded
   length matches the record-time buffer, so a short read means underrun (already flagged). */
static void mfl_rr_io_pop_into(uint8_t* dst, size_t n) {
    size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L);
    size_t m = L < n ? L : n;
    if (m) memcpy(dst, s, m);
    free(s);
}

/* the determinism boundary of the recorded program (program-dependent, emitted by
   codegen after this fixed runtime): 1 if the program uses FFI or select. */
static int mfl_rr_prog_boundary(void);
static int mfl_rr_besteffort = 0;   /* trace was recorded from a boundary-unsafe program */
static int mfl_rr_verify = 0;       /* MFL_RR_VERIFY: report a faithfulness self-check */
static int mfl_rr_json = 0;         /* MFL_RR_JSON: emit a crash as a JSON causal report */
static int mfl_rr_probe_on = 0;     /* MFL_RR_PROBE: machin replay --print value-history mode */

/* value-query debugger: codegen emits mfl_rr_probe("<var>", <value>) after each assignment
   to a --print-watched variable. Because replay is deterministic, the printed sequence is the
   exact history that variable took in the recorded run; the last line before a panic is its
   value at the crash. Tagged with the goroutine path + schedule position for ordering. */
static void mfl_rr_probe(const char* name, const char* val) {
    if (!mfl_rr_probe_on) return;
    pthread_mutex_lock(&mfl_rr_mu);
    fprintf(stderr, "probe %s %s = %s @op%d\n", mfl_gid_path ? mfl_gid_path : "0", name, val, mfl_rr_pos);
    pthread_mutex_unlock(&mfl_rr_mu);
}

static void mfl_rr_init(void) {
    mfl_gid_path = strdup("0");           /* main goroutine */
    if (getenv("MFL_RR_VERIFY")) mfl_rr_verify = 1;
    if (getenv("MFL_RR_JSON")) mfl_rr_json = 1;
    if (getenv("MFL_RR_PROBE")) mfl_rr_probe_on = 1;
    const char* rec = getenv("MFL_RR_RECORD");
    const char* rep = getenv("MFL_RR_REPLAY");
    if (rec) {
        mfl_rr_mode = 1; mfl_rr_out = fopen(rec, "w");
        /* honest header: a boundary-unsafe program can't be faithfully replayed.
           The program path lets machin replay <trace> re-run without re-naming it. */
        if (mfl_rr_out) {
            fprintf(mfl_rr_out, "MFLRR 1\nboundary %s\n", mfl_rr_prog_boundary() ? "best-effort" : "faithful");
            const char* src = getenv("MFL_RR_SRC");
            if (src) fprintf(mfl_rr_out, "program %s\n", src);
            const char* sf = getenv("MFL_RR_SAFE");
            fprintf(mfl_rr_out, "safe %s\n", (sf && sf[0] == '1') ? "1" : "0");
        }
    } else if (rep) {
        mfl_rr_mode = 2;
        FILE* f = fopen(rep, "r");
        if (f) {
            int cap = 64; mfl_rr_trace = malloc(cap * sizeof(char*));
            char line[16384];
            while (fgets(line, sizeof line, f)) {
                if (strncmp(line, "boundary ", 9) == 0) { if (strstr(line, "best-effort")) mfl_rr_besteffort = 1; }
                else if (line[0] == 'S') {
                    char p[256]; if (sscanf(line, "S %255s", p) == 1) {
                        if (mfl_rr_n == cap) { cap *= 2; mfl_rr_trace = realloc(mfl_rr_trace, cap * sizeof(char*)); }
                        mfl_rr_trace[mfl_rr_n++] = strdup(p);
                    }
                } else if (line[0] == 'I') {
                    /* an I-value can be empty hex ("I 0 " for an empty read / http err ""),
                       which sscanf reports as 1 field -- still a real recorded value, so
                       push it (h left ""), or record and replay disagree on the queue depth. */
                    char p[256], h[16000]; h[0] = 0;
                    if (sscanf(line, "I %255s %15999s", p, h) >= 1) mfl_io_push(p, h);
                }
            }
            fclose(f);
        }
        if (mfl_rr_besteffort)
            fprintf(stderr, "warning: best-effort trace (the program uses FFI) -- replay may diverge from the recorded run\n");
    }
}
/* replay: block until it is this goroutine's turn to complete a channel op.
   Only the current-turn goroutine runs its op between enter and exit, so the
   op's effect order equals the recorded order. */
static void mfl_rr_enter(void) {
    if (mfl_rr_mode != 2) return;
    pthread_mutex_lock(&mfl_rr_mu);
    while (mfl_rr_pos < mfl_rr_n && strcmp(mfl_rr_trace[mfl_rr_pos], mfl_gid_path) != 0)
        pthread_cond_wait(&mfl_rr_cnd, &mfl_rr_mu);
    pthread_mutex_unlock(&mfl_rr_mu);
}
/* record: append this goroutine's path in true channel-effect order (called while
   holding the channel mutex). */
static void mfl_rr_mark(void) {
    if (mfl_rr_mode != 1) return;
    pthread_mutex_lock(&mfl_rr_mu);
    if (mfl_rr_out) fprintf(mfl_rr_out, "S %s\n", mfl_gid_path);
    pthread_mutex_unlock(&mfl_rr_mu);
}
/* replay: advance the turn and wake the next goroutine. */
static void mfl_rr_exit(void) {
    if (mfl_rr_mode != 2) return;
    pthread_mutex_lock(&mfl_rr_mu);
    mfl_rr_pos++;
    pthread_cond_broadcast(&mfl_rr_cnd);
    pthread_mutex_unlock(&mfl_rr_mu);
}
/* Concurrent stdout writes are observable and un-synchronized, so their
   interleaving is nondeterminism too -- gate each print statement like a channel
   op. record: a print-lock keeps a line atomic + records its order; replay: the
   turn system serializes prints into the recorded order. No-op when not
   recording (a normal run pays nothing). */
static pthread_mutex_t mfl_print_mu = PTHREAD_MUTEX_INITIALIZER;
static void mfl_rr_print_begin(void) {
    if (mfl_rr_mode == 2) mfl_rr_enter();
    else if (mfl_rr_mode == 1) pthread_mutex_lock(&mfl_print_mu);
}
static void mfl_rr_print_end(void) {
    if (mfl_rr_mode == 2) mfl_rr_exit();
    else if (mfl_rr_mode == 1) { mfl_rr_mark(); pthread_mutex_unlock(&mfl_print_mu); }
}
static void mfl_rr_finish(void) {
    if (mfl_rr_out) { fclose(mfl_rr_out); mfl_rr_out = NULL; }
    /* --verify: an honest self-check that replay stayed on-script. A faithful
       replay consumes exactly the recorded schedule and never underruns the I/O
       log; anything else means the replay diverged from the recorded run. */
    if (mfl_rr_mode == 2 && mfl_rr_verify) {
        int ok = (mfl_rr_pos == mfl_rr_n) && !mfl_rr_io_underrun && !mfl_rr_besteffort;
        fprintf(stderr, "replay-verify: schedule %d/%d ops, io-underrun=%s, boundary=%s -> %s\n",
                mfl_rr_pos, mfl_rr_n, mfl_rr_io_underrun ? "yes" : "no",
                mfl_rr_besteffort ? "best-effort" : "faithful", ok ? "FAITHFUL" : "DIVERGED");
    }
}

/* A crash IS the artifact. When record/replay is active, enrich a panic with the
   goroutine that hit it and how far into the schedule it got; under MFL_RR_JSON,
   emit a structured causal report an agent reads instead of reproduces -- the
   panicking goroutine, the message, the schedule position, and the causal chain
   (the sequence of channel-op goroutines that led to the crash). */
static void mfl_panic(const char* msg) {
    if (mfl_rr_mode != 0 && mfl_rr_json) {
        fputs("{\"panic\":\"", stderr);
        for (const char* p = msg; *p; p++) { if (*p == '"' || *p == '\\') fputc('\\', stderr); fputc(*p, stderr); }
        fprintf(stderr, "\",\"goroutine\":\"%s\",\"scheduleOp\":%d,\"scheduleTotal\":%d,\"causalChain\":[",
                mfl_gid_path ? mfl_gid_path : "0", mfl_rr_pos, mfl_rr_n);
        int lim = mfl_rr_pos < mfl_rr_n ? mfl_rr_pos : mfl_rr_n;
        for (int i = 0; i < lim; i++) fprintf(stderr, "%s\"%s\"", i ? "," : "", mfl_rr_trace[i]);
        fputs("]}\n", stderr);
    } else {
        fputs("panic: ", stderr); fputs(msg, stderr); fputc('\n', stderr);
        if (mfl_rr_mode != 0)
            fprintf(stderr, "  (%s: goroutine %s, schedule op %d/%d)\n",
                    mfl_rr_mode == 1 ? "record" : "replay",
                    mfl_gid_path ? mfl_gid_path : "0", mfl_rr_pos, mfl_rr_n);
    }
    exit(1);
}

static void mfl_chan_send(mfl_chan* c, const void* v) {
    mfl_rr_enter();
    mfl_cnode* n = malloc(sizeof(mfl_cnode));
    if (c->ser) {
        char* j = c->ser(v);                 /* arena JSON of the whole value */
        size_t L = strlen(j);
        n->data = malloc(L + 1); memcpy(n->data, j, L + 1);
    } else {
        n->data = malloc(c->es); memcpy(n->data, v, c->es);
        mfl_chan_freeze(c, n->data);
    }
    n->next = NULL;
    pthread_mutex_lock(&c->mu);
    if (c->tail) c->tail->next = n; else c->head = n;
    c->tail = n;
    pthread_cond_signal(&c->cnd);
    mfl_rr_mark();
    pthread_mutex_unlock(&c->mu);
    mfl_rr_exit();
}
/* deliver node n's payload into out (receiver arena), then free the node. */
static void mfl_chan_deliver(mfl_chan* c, mfl_cnode* n, void* out) {
    if (c->des) {
        c->des((const char*)n->data, out);
    } else {
        memcpy(out, n->data, c->es);
        mfl_chan_thaw(c, out);
    }
    free(n->data); free(n);
}
/* blocking receive with ok: 1 and fills out if a value arrived; 0 if the channel
   is closed and drained (out left untouched). The primitive behind range-over-
   channel and the comma-ok receive. */
static int mfl_chan_recv2(mfl_chan* c, void* out) {
    mfl_rr_enter();
    pthread_mutex_lock(&c->mu);
    while (!c->head && !c->closed) pthread_cond_wait(&c->cnd, &c->mu);
    mfl_cnode* n = c->head;
    /* draining a closed+empty channel (returns "not ok") is still an observable,
       ordered op — it must record/gate a turn like a real receive, or record and
       replay disagree on the op count and replay hangs. */
    if (!n) { mfl_rr_mark(); pthread_mutex_unlock(&c->mu); mfl_rr_exit(); return 0; }
    c->head = n->next;
    if (!c->head) c->tail = NULL;
    mfl_rr_mark();
    pthread_mutex_unlock(&c->mu);
    mfl_chan_deliver(c, n, out);
    mfl_rr_exit();
    return 1;
}
static void mfl_chan_recv(mfl_chan* c, void* out) {
    if (!mfl_chan_recv2(c, out)) memset(out, 0, c->es);
}
/* non-blocking receive for select: returns 1 if the case is ready — either a
   value arrived (*ok = 1, out filled) or the channel is closed and drained
   (*ok = 0, out zeroed). Returns 0 if not ready (open and empty). */
static int mfl_chan_tryrecv2(mfl_chan* c, void* out, int* ok) {
    pthread_mutex_lock(&c->mu);
    mfl_cnode* n = c->head;
    if (n) { c->head = n->next; if (!c->head) c->tail = NULL; }
    int closed = c->closed;
    /* a select recv that fires IS an ordered channel effect — mark it in schedule
       order (under c->mu, like mfl_chan_recv2) so a select-using program records a
       faithful, not best-effort, trace. Only fires in record mode; replay never
       polls (it forces the recorded case with a blocking recv). */
    if ((n || closed) && mfl_rr_mode == 1) mfl_rr_mark();
    pthread_mutex_unlock(&c->mu);
    if (n) { mfl_chan_deliver(c, n, out); *ok = 1; return 1; }
    if (closed) { memset(out, 0, c->es); *ok = 0; return 1; }
    return 0;
}

/* maps: a chained hash table keyed by int64 or string, fixed-size values */
typedef struct mfl_ment { struct mfl_ment* next; int64_t ik; char* sk; void* val; } mfl_ment;
typedef struct { mfl_ment** buckets; int64_t nb, count, vs; int sk; } mfl_map;
static uint64_t mfl_hash_i(int64_t k) { uint64_t x=(uint64_t)k; x^=x>>33; x*=0xff51afd7ed558ccdULL; x^=x>>33; return x; }
static uint64_t mfl_hash_s(const char* s) { uint64_t h=1469598103934665603ULL; while(*s){ h^=(unsigned char)*s++; h*=1099511628211ULL; } return h; }
static mfl_map* mfl_make_map(int keyIsStr, int64_t vs) {
    mfl_map* m = malloc(sizeof(mfl_map));
    m->nb = 16; m->count = 0; m->sk = keyIsStr; m->vs = vs;
    m->buckets = calloc(m->nb, sizeof(mfl_ment*));
    return m;
}
static mfl_ment** mfl_map_at(mfl_map* m, int64_t ik, const char* sk) {
    uint64_t h = m->sk ? mfl_hash_s(sk) : mfl_hash_i(ik);
    mfl_ment** pp = &m->buckets[h & (m->nb - 1)];
    while (*pp) { mfl_ment* e=*pp; if (m->sk ? strcmp(e->sk,sk)==0 : e->ik==ik) return pp; pp=&e->next; }
    return pp;
}
/* Double the bucket array and rehash when the load factor hits 1. Without this
   the table stays at 16 buckets forever, so N inserts are O(N^2) (every insert
   walks a chain of length N/16) — 25s for a 128k-entry map. Amortized O(1). */
static void mfl_map_grow(mfl_map* m) {
    int64_t nn = m->nb * 2;
    mfl_ment** nb2 = calloc(nn, sizeof(mfl_ment*));
    for (int64_t b = 0; b < m->nb; b++) {
        mfl_ment* e = m->buckets[b];
        while (e) {
            mfl_ment* nx = e->next;
            uint64_t h = m->sk ? mfl_hash_s(e->sk) : mfl_hash_i(e->ik);
            int64_t idx = (int64_t)(h & (uint64_t)(nn - 1));
            e->next = nb2[idx]; nb2[idx] = e;
            e = nx;
        }
    }
    free(m->buckets); m->buckets = nb2; m->nb = nn;
}
static void mfl_map_set(mfl_map* m, int64_t ik, const char* sk, const void* val) {
    mfl_ment** pp = mfl_map_at(m, ik, sk);
    if (*pp) { memcpy((*pp)->val, val, m->vs); return; }
    mfl_ment* e = malloc(sizeof(mfl_ment)); e->next=NULL; e->ik=ik; e->sk=NULL;
    if (m->sk) { e->sk = malloc(strlen(sk)+1); strcpy(e->sk, sk); }
    e->val = malloc(m->vs); memcpy(e->val, val, m->vs);
    *pp = e; m->count++;
    if (m->count > m->nb) mfl_map_grow(m);
}
static void mfl_map_get(mfl_map* m, int64_t ik, const char* sk, void* out) {
    mfl_ment** pp = mfl_map_at(m, ik, sk);
    if (*pp) memcpy(out, (*pp)->val, m->vs); else memset(out, 0, m->vs);
}
static int mfl_map_has(mfl_map* m, int64_t ik, const char* sk) { return *mfl_map_at(m, ik, sk) != NULL; }
static void mfl_map_del(mfl_map* m, int64_t ik, const char* sk) {
    mfl_ment** pp = mfl_map_at(m, ik, sk);
    if (*pp) { mfl_ment* e=*pp; *pp=e->next; free(e->sk); free(e->val); free(e); m->count--; }
}
static int64_t mfl_map_len(mfl_map* m) { return m->count; }
static mfl_slice mfl_map_keys(mfl_map* m) {
    int64_t es = m->sk ? (int64_t)sizeof(char*) : (int64_t)sizeof(int64_t);
    mfl_slice s = { m->count ? mfl_alloc(m->count*es) : NULL, m->count, m->count };
    int64_t idx = 0;
    for (int64_t b = 0; b < m->nb; b++)
        for (mfl_ment* e = m->buckets[b]; e; e = e->next) {
            if (m->sk) ((char**)s.data)[idx] = e->sk; else ((int64_t*)s.data)[idx] = e->ik;
            idx++;
        }
    return s;
}

// A string's zero value is "" — but an auto-zeroed string slot (an omitted struct
// literal field, a grown slice element, a default map value) is a NULL char*. So
// the string ops treat NULL as "" rather than dereferencing it.
static const char* mfl_s(const char* s) { return s ? s : ""; }
static int mfl_strcmp(const char* a, const char* b) { return strcmp(mfl_s(a), mfl_s(b)); }
static char* mfl_cat(const char* a, const char* b) {
    a = mfl_s(a); b = mfl_s(b);
    size_t la = strlen(a), lb = strlen(b);
    char* r = mfl_alloc(la + lb + 1);
    memcpy(r, a, la); memcpy(r + la, b, lb); r[la + lb] = 0;
    return r;
}
static char* mfl_str_i(int64_t v) { char* b = mfl_alloc(24); snprintf(b, 24, "%lld", (long long)v); return b; }
static char* mfl_str_d(double v)  { char* b = mfl_alloc(32); snprintf(b, 32, "%g", v); return b; }
/* reinterpret a double's IEEE-754 bits as an int64 and back — the byte-level access
   needed to (de)serialize 64-bit floats (e.g. BSON doubles). */
static int64_t mfl_f64_bits(double d) { int64_t i; memcpy(&i, &d, 8); return i; }
static double mfl_f64_from_bits(int64_t i) { double d; memcpy(&d, &i, 8); return d; }
static char* mfl_str_b(int64_t v) { return v ? "true" : "false"; }
static char* mfl_dup(const char* s) { size_t n = strlen(s); char* r = mfl_alloc(n+1); memcpy(r, s, n+1); return r; }
/* raw heap memory: a pointer is an int (intptr_t round-trip), as with the ptr
   FFI type. alloc() is zeroed (calloc) so building C structs is safe. For
   filling C buffers (vertex arrays) and structs to hand to a C API. */
static int64_t mfl_raw_alloc(int64_t n) { return (int64_t)(intptr_t)calloc(1, (size_t)(n > 0 ? n : 0)); }
static void mfl_raw_free(int64_t p) { free((void*)(intptr_t)p); }
/* mmap_file / madvise_free are POSIX file-mapping helpers — native-only. wasi-libc
   has no mmap/madvise, so guard them out of the wasm build (a frontend that calls
   neither then emits no sys/mman.h reference). Matches the mfl_system guard. #463 */
#ifndef __wasm__
/* madvise_free: hint the kernel to drop the resident pages of an mmap'd region
   (MADV_DONTNEED) — RSS falls, pages re-fault lazily on next access. For idle
   release of large mmap_file mappings without unmapping. */
static void mfl_madvise_free(int64_t ptr, int64_t len) {
    if (ptr && len > 0) madvise((void*)(intptr_t)ptr, (size_t)len, MADV_DONTNEED);
}
/* mmap a file read-only into memory -> (pointer-as-int, size-in-bytes), or (0,0)
   on error. Zero-copy access to a large on-disk buffer (a model checkpoint, a
   memory-mapped asset) via peek_*; pages fault in lazily. The mapping lives
   until the process exits. Multi-assign only: p, n := mmap_file(path). */
typedef struct { int64_t ptr; int64_t len; } mfl_mmap_result;
static mfl_mmap_result mfl_mmap_file(const char* path) {
    mfl_mmap_result R; R.ptr = 0; R.len = 0;
    int fd = open(path, O_RDONLY);
    if (fd < 0) return R;
    struct stat st;
    if (fstat(fd, &st) != 0 || st.st_size <= 0) { close(fd); return R; }
    void* p = mmap(NULL, (size_t)st.st_size, PROT_READ, MAP_PRIVATE, fd, 0);
    close(fd);
    if (p == MAP_FAILED) return R;
    R.ptr = (int64_t)(intptr_t)p; R.len = (int64_t)st.st_size;
    return R;
}
#endif   /* __wasm__ — mmap_file / madvise_free */
/* read a NUL-terminated string from a raw pointer into an MFL (arena) string — the
   host->wasm direction: the JS host writes UTF-8 + a NUL into wasm memory at a
   pointer the program alloc'd, then passes it here. */
static char* mfl_ptr_str(int64_t p) { return p ? mfl_dup((const char*)(intptr_t)p) : mfl_dup(""); }
static void mfl_poke_f32(int64_t p, int64_t o, double v) { *(float*)((char*)(intptr_t)p + o) = (float)v; }
static void mfl_poke_i32(int64_t p, int64_t o, int64_t v) { *(int32_t*)((char*)(intptr_t)p + o) = (int32_t)v; }
static void mfl_poke_u8(int64_t p, int64_t o, int64_t v) { *(uint8_t*)((char*)(intptr_t)p + o) = (uint8_t)v; }
static void mfl_poke_u16(int64_t p, int64_t o, int64_t v) { *(uint16_t*)((char*)(intptr_t)p + o) = (uint16_t)v; }
static void mfl_poke_ptr(int64_t p, int64_t o, int64_t v) { *(void**)((char*)(intptr_t)p + o) = (void*)(intptr_t)v; }
static double mfl_peek_f32(int64_t p, int64_t o) { return (double)*(float*)((char*)(intptr_t)p + o); }
static int64_t mfl_peek_i32(int64_t p, int64_t o) { return (int64_t)*(int32_t*)((char*)(intptr_t)p + o); }
static int64_t mfl_peek_i8(int64_t p, int64_t o) { return (int64_t)*(int8_t*)((char*)(intptr_t)p + o); }
static int64_t mfl_peek_u8(int64_t p, int64_t o) { return (int64_t)*(uint8_t*)((char*)(intptr_t)p + o); }
/* signed-byte dot product with a 32-bit accumulator (the vector-friendly width:
 * cc autovectorizes this where an int64 reduction stays half-speed). Exact while
 * |sum| < 2^31 - always true for i8*i8 up to n ~ 133k - the quantized-matmul
 * group kernel of the AI domain, as sha256 is to the crypto domain. */
static int64_t mfl_dot_i8(int64_t a, int64_t b, int64_t n) {
    const int8_t* x = (const int8_t*)(intptr_t)a;
    const int8_t* w = (const int8_t*)(intptr_t)b;
    int32_t acc = 0;
    for (int64_t k = 0; k < n; k++) acc += (int32_t)x[k] * (int32_t)w[k];
    return (int64_t)acc;
}
/* grouped, dual-scaled int8 dot: sum over length-gs groups of
 * (int32 group dot) * xscale[g] * wscale[g]. The whole quantized-matmul inner
 * product in ONE call -- one int32 reduction per group (autovectorizes to
 * vpmaddwd/vpdpbusd) with the two per-group fp32 scales applied group-at-a-time.
 * Replaces an MFL loop that called dot_i8 + two peek_f32 per group, whose
 * per-group call overhead capped throughput. xq/wq are int8 buffers (n bytes);
 * xs/ws are fp32 group-scale buffers (n/gs floats). n must be a multiple of gs. */
/* float32 dot product of two raw f32 buffers (a[k]*b[k], k<n) with an fp32
 * accumulator -- the vectorizable inner product for attention scores (q.k) and
 * any dense float kernel, where an MFL loop of peek_f32*peek_f32 is scalar and
 * call-bound. */
static double mfl_dot_f32(int64_t a, int64_t b, int64_t n) {
    const float* x = (const float*)(intptr_t)a;
    const float* y = (const float*)(intptr_t)b;
    float acc = 0.0f;
    for (int64_t k = 0; k < n; k++) acc += x[k] * y[k];
    return (double)acc;
}
/* AXPY: y[k] += s * x[k] for k<n, over raw f32 buffers. The attention value
 * accumulation (weighted sum of V rows) and any scaled-add; vectorizes where an
 * MFL peek/poke loop cannot. */
static void mfl_axpy_f32(int64_t y, double s, int64_t x, int64_t n) {
    float* yy = (float*)(intptr_t)y;
    const float* xx = (const float*)(intptr_t)x;
    float sf = (float)s;
    for (int64_t k = 0; k < n; k++) yy[k] += sf * xx[k];
}
static double mfl_dot_q8(int64_t xq, int64_t xs, int64_t wq, int64_t ws, int64_t n, int64_t gs) {
    const int8_t* x = (const int8_t*)(intptr_t)xq;
    const float* xsc = (const float*)(intptr_t)xs;
    const int8_t* w = (const int8_t*)(intptr_t)wq;
    const float* wsc = (const float*)(intptr_t)ws;
    double val = 0.0;
    int64_t ng = gs > 0 ? n / gs : 0;
    for (int64_t g = 0; g < ng; g++) {
        const int8_t* xg = x + g * gs;
        const int8_t* wg = w + g * gs;
        int32_t acc = 0;
        for (int64_t k = 0; k < gs; k++) acc += (int32_t)xg[k] * (int32_t)wg[k];
        val += (double)acc * (double)xsc[g] * (double)wsc[g];
    }
    return val;
}
/* batched q8 matmul: for output rows i in [lo,hi) and positions b in [0,B):
   out[b*ostride + i] = dot_q8(xq + b*n, xs + b*n/gs, w + i*n, ws + i*n/gs). The
   weight row is loaded once and reused across all B positions (the prefill GEMM)
   -- one call replaces B*(hi-lo) MFL-level dot_q8 calls, so the compiler keeps
   the row hot and vectorizes the inner product. */
static void mfl_matmul_q8_batch(int64_t ob, int64_t ostride, int64_t xqb, int64_t xsb, int64_t wq, int64_t ws, int64_t n, int64_t gs, int64_t B, int64_t lo, int64_t hi) {
    float* out = (float*)(intptr_t)ob;
    const int8_t* xbase = (const int8_t*)(intptr_t)xqb;
    const float* xsbase = (const float*)(intptr_t)xsb;
    const int8_t* wbase = (const int8_t*)(intptr_t)wq;
    const float* wsbase = (const float*)(intptr_t)ws;
    int64_t ng = gs > 0 ? n / gs : 0;
    for (int64_t i = lo; i < hi; i++) {
        const int8_t* wrow = wbase + i * n;
        const float* wsc = wsbase + i * ng;
        for (int64_t b = 0; b < B; b++) {
            const int8_t* x = xbase + b * n;
            const float* xsc = xsbase + b * ng;
            double val = 0.0;
            for (int64_t g = 0; g < ng; g++) {
                const int8_t* xg = x + g * gs;
                const int8_t* wg = wrow + g * gs;
                int32_t acc = 0;
                for (int64_t k = 0; k < gs; k++) acc += (int32_t)xg[k] * (int32_t)wg[k];
                val += (double)acc * (double)xsc[g] * (double)wsc[g];
            }
            out[b * ostride + i] = (float)val;
        }
    }
}
/* grouped, dual-scaled int4 dot: like dot_q8 but the weights are split-nibble
 * int4 (a group of gs weights packed into gs/2 bytes; byte k holds w[k] in the
 * low nibble and w[k+gs/2] in the high nibble, each stored as value+8 in 0..15).
 * Activations stay int8. Halves the weight bytes moved -- the memory-bound-decode
 * win. n multiple of gs; wq has n/2 packed bytes. */
static double mfl_dot_q4(int64_t xq, int64_t xs, int64_t wq, int64_t ws, int64_t n, int64_t gs) {
    const int8_t* x = (const int8_t*)(intptr_t)xq;
    const float* xsc = (const float*)(intptr_t)xs;
    const uint8_t* w = (const uint8_t*)(intptr_t)wq;
    const float* wsc = (const float*)(intptr_t)ws;
    double val = 0.0;
    int64_t ng = gs > 0 ? n / gs : 0;
    int64_t half = gs / 2;
    for (int64_t g = 0; g < ng; g++) {
        const int8_t* xg = x + g * gs;
        const uint8_t* wg = w + g * half;
        int32_t acc = 0;
        for (int64_t k = 0; k < half; k++) {
            uint8_t b = wg[k];
            acc += (int32_t)xg[k]        * ((int32_t)(b & 15) - 8);
            acc += (int32_t)xg[k + half] * ((int32_t)(b >> 4) - 8);
        }
        val += (double)acc * (double)xsc[g] * (double)wsc[g];
    }
    return val;
}
/* base64 (standard alphabet, padded) over text. */
static char* mfl_base64_encode(const char* s) {
    static const char t[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    size_t n = strlen(s), j = 0, i = 0;
    char* out = (char*)mfl_alloc(4 * ((n + 2) / 3) + 1);
    for (; i + 3 <= n; i += 3) {
        uint32_t v = ((uint32_t)(unsigned char)s[i] << 16) | ((uint32_t)(unsigned char)s[i+1] << 8) | (unsigned char)s[i+2];
        out[j++] = t[(v >> 18) & 63]; out[j++] = t[(v >> 12) & 63];
        out[j++] = t[(v >> 6) & 63];  out[j++] = t[v & 63];
    }
    if (n - i == 1) {
        uint32_t v = (uint32_t)(unsigned char)s[i] << 16;
        out[j++] = t[(v >> 18) & 63]; out[j++] = t[(v >> 12) & 63];
        out[j++] = '='; out[j++] = '=';
    } else if (n - i == 2) {
        uint32_t v = ((uint32_t)(unsigned char)s[i] << 16) | ((uint32_t)(unsigned char)s[i+1] << 8);
        out[j++] = t[(v >> 18) & 63]; out[j++] = t[(v >> 12) & 63];
        out[j++] = t[(v >> 6) & 63];  out[j++] = '=';
    }
    out[j] = 0;
    return out;
}
/* lenient base64 decode: accepts standard and url-safe ('-' '_'), ignores
   padding/whitespace, so it also decodes JWT segments. "" for empty input. */
static int mfl_b64val(int c) {
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '+' || c == '-') return 62;
    if (c == '/' || c == '_') return 63;
    return -1;
}
static char* mfl_base64_decode(const char* s) {
    size_t n = strlen(s), j = 0;
    char* out = (char*)mfl_alloc(n + 1);
    int buf = 0, bits = 0;
    for (size_t i = 0; i < n; i++) {
        int v = mfl_b64val((unsigned char)s[i]);
        if (v < 0) continue;
        buf = (buf << 6) | v; bits += 6;
        if (bits >= 8) { bits -= 8; out[j++] = (char)((buf >> bits) & 0xFF); }
    }
    out[j] = 0;
    return out;
}
/* URL percent-encoding (RFC 3986). url_encode keeps the unreserved set
   A-Za-z0-9-._~ and %XX-encodes everything else (space -> %20). url_decode
   reverses it and is lenient: it also maps '+' to space (form style) and passes
   a malformed % through unchanged. */
static int mfl_hexval(int c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}
static char* mfl_url_encode(const char* s) {
    static const char hex[] = "0123456789ABCDEF";
    size_t n = strlen(s), j = 0;
    char* out = (char*)mfl_alloc(n * 3 + 1);
    for (size_t i = 0; i < n; i++) {
        unsigned char c = (unsigned char)s[i];
        if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
            c == '-' || c == '_' || c == '.' || c == '~') {
            out[j++] = (char)c;
        } else {
            out[j++] = '%';
            out[j++] = hex[c >> 4];
            out[j++] = hex[c & 15];
        }
    }
    out[j] = 0;
    return out;
}
static char* mfl_url_decode(const char* s) {
    size_t n = strlen(s), j = 0;
    char* out = (char*)mfl_alloc(n + 1);
    for (size_t i = 0; i < n; i++) {
        char c = s[i];
        if (c == '+') {
            out[j++] = ' ';
        } else if (c == '%' && i + 2 < n) {
            int hi = mfl_hexval((unsigned char)s[i+1]);
            int lo = mfl_hexval((unsigned char)s[i+2]);
            if (hi >= 0 && lo >= 0) { out[j++] = (char)((hi << 4) | lo); i += 2; }
            else { out[j++] = c; }
        } else {
            out[j++] = c;
        }
    }
    out[j] = 0;
    return out;
}
/* bytes: a NUL-safe binary buffer (pointer + length), the type strings can't be.
   Values are immutable (builtins return fresh arena buffers), so passing one by
   value just shares the backing — same discipline as strings. */
typedef struct { uint8_t* data; int64_t len; } mfl_bytes;
static mfl_bytes mfl_bytes_from_str(const char* s) {
    int64_t n = (int64_t)strlen(s);
    mfl_bytes b; b.len = n; b.data = (uint8_t*)mfl_alloc(n ? n : 1);
    memcpy(b.data, s, (size_t)n);
    return b;
}
static char* mfl_bytes_str(mfl_bytes b) {   /* NUL-terminated; truncates at an embedded 0 */
    char* out = (char*)mfl_alloc(b.len + 1);
    memcpy(out, b.data, (size_t)b.len);
    out[b.len] = 0;
    return out;
}
static char* mfl_bytes_hex(mfl_bytes b) {
    static const char hx[] = "0123456789abcdef";
    char* out = (char*)mfl_alloc(b.len * 2 + 1);
    for (int64_t i = 0; i < b.len; i++) { out[i*2] = hx[b.data[i] >> 4]; out[i*2+1] = hx[b.data[i] & 15]; }
    out[b.len*2] = 0;
    return out;
}
static mfl_bytes mfl_bytes_unhex(const char* s) {   /* skips non-hex chars (spaces, colons) */
    int64_t n = (int64_t)strlen(s);
    mfl_bytes b; b.len = 0; b.data = (uint8_t*)mfl_alloc(n / 2 + 1);
    int hi = -1;
    for (int64_t i = 0; i < n; i++) {
        int v = mfl_hexval((unsigned char)s[i]);
        if (v < 0) continue;
        if (hi < 0) hi = v;
        else { b.data[b.len++] = (uint8_t)((hi << 4) | v); hi = -1; }
    }
    return b;
}
static int64_t mfl_byte_at(mfl_bytes b, int64_t i) { return (i < 0 || i >= b.len) ? -1 : (int64_t)b.data[i]; }
static mfl_bytes mfl_bytes_sub(mfl_bytes b, int64_t start, int64_t end) {
    if (start < 0) start = 0;
    if (end > b.len) end = b.len;
    if (end < start) end = start;
    int64_t n = end - start;
    mfl_bytes r; r.len = n; r.data = (uint8_t*)mfl_alloc(n ? n : 1);
    memcpy(r.data, b.data + start, (size_t)n);
    return r;
}
/* find needle in haystack at or after start, NUL-safe; -1 if absent. For binary
   protocols (e.g. a multipart/form-data boundary inside an upload body). */
static int64_t mfl_bytes_index(mfl_bytes h, mfl_bytes nd, int64_t from) {
    if (from < 0) from = 0;
    if (nd.len == 0) return from <= h.len ? from : -1;
    for (int64_t i = from; i + nd.len <= h.len; i++) {
        if (memcmp(h.data + i, nd.data, (size_t)nd.len) == 0) return i;
    }
    return -1;
}
static mfl_bytes mfl_bytes_concat(mfl_bytes a, mfl_bytes b) {
    mfl_bytes r; r.len = a.len + b.len; r.data = (uint8_t*)mfl_alloc(r.len ? r.len : 1);
    memcpy(r.data, a.data, (size_t)a.len);
    memcpy(r.data + a.len, b.data, (size_t)b.len);
    return r;
}
/* binary-safe base64: encode raw bytes (incl. NUL), decode to raw bytes. The
   string forms (mfl_base64_encode/decode) stop at a NUL; these carry an explicit
   length, for binary protocols / crypto (e.g. SCRAM salts and proofs). */
static char* mfl_base64_encode_bytes(mfl_bytes b) {
    static const char t[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    size_t n = (size_t)b.len, j = 0, i = 0; const unsigned char* s = b.data;
    char* out = (char*)mfl_alloc(4 * ((n + 2) / 3) + 1);
    for (; i + 3 <= n; i += 3) {
        uint32_t v = ((uint32_t)s[i] << 16) | ((uint32_t)s[i+1] << 8) | s[i+2];
        out[j++] = t[(v >> 18) & 63]; out[j++] = t[(v >> 12) & 63];
        out[j++] = t[(v >> 6) & 63];  out[j++] = t[v & 63];
    }
    if (n - i == 1) {
        uint32_t v = (uint32_t)s[i] << 16;
        out[j++] = t[(v >> 18) & 63]; out[j++] = t[(v >> 12) & 63];
        out[j++] = '='; out[j++] = '=';
    } else if (n - i == 2) {
        uint32_t v = ((uint32_t)s[i] << 16) | ((uint32_t)s[i+1] << 8);
        out[j++] = t[(v >> 18) & 63]; out[j++] = t[(v >> 12) & 63];
        out[j++] = t[(v >> 6) & 63];  out[j++] = '=';
    }
    out[j] = 0;
    return out;
}
static mfl_bytes mfl_base64_decode_bytes(const char* s) {
    size_t n = strlen(s), j = 0;
    mfl_bytes b; b.data = (uint8_t*)mfl_alloc(n ? n : 1);
    int buf = 0, bits = 0;
    for (size_t i = 0; i < n; i++) {
        int v = mfl_b64val((unsigned char)s[i]);
        if (v < 0) continue;
        buf = (buf << 6) | v; bits += 6;
        if (bits >= 8) { bits -= 8; b.data[j++] = (uint8_t)((buf >> bits) & 0xFF); }
    }
    b.len = (int64_t)j;
    return b;
}
/* SHA-256 + HMAC-SHA256 (pure C, no dependency). Operate on NUL-terminated text
   and return a lowercase hex digest. */
static uint32_t mfl_ror32(uint32_t x, int n) { return (x >> n) | (x << (32 - n)); }
static void mfl_sha256_raw(const unsigned char* msg, size_t len, unsigned char out[32]) {
    static const uint32_t k[64] = {
        0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
        0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
        0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
        0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
        0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
        0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
        0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
        0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2 };
    uint32_t h[8] = {0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19};
    size_t newlen = ((len + 8) / 64 + 1) * 64;
    unsigned char* m = (unsigned char*)calloc(newlen, 1);
    memcpy(m, msg, len);
    m[len] = 0x80;
    uint64_t bits = (uint64_t)len * 8;
    for (int i = 0; i < 8; i++) m[newlen - 1 - i] = (unsigned char)(bits >> (8 * i));
    for (size_t off = 0; off < newlen; off += 64) {
        uint32_t w[64];
        for (int i = 0; i < 16; i++)
            w[i] = ((uint32_t)m[off+i*4] << 24) | ((uint32_t)m[off+i*4+1] << 16) | ((uint32_t)m[off+i*4+2] << 8) | m[off+i*4+3];
        for (int i = 16; i < 64; i++) {
            uint32_t s0 = mfl_ror32(w[i-15],7) ^ mfl_ror32(w[i-15],18) ^ (w[i-15] >> 3);
            uint32_t s1 = mfl_ror32(w[i-2],17) ^ mfl_ror32(w[i-2],19) ^ (w[i-2] >> 10);
            w[i] = w[i-16] + s0 + w[i-7] + s1;
        }
        uint32_t a=h[0],b=h[1],c=h[2],d=h[3],e=h[4],f=h[5],g=h[6],hh=h[7];
        for (int i = 0; i < 64; i++) {
            uint32_t S1 = mfl_ror32(e,6) ^ mfl_ror32(e,11) ^ mfl_ror32(e,25);
            uint32_t ch = (e & f) ^ ((~e) & g);
            uint32_t t1 = hh + S1 + ch + k[i] + w[i];
            uint32_t S0 = mfl_ror32(a,2) ^ mfl_ror32(a,13) ^ mfl_ror32(a,22);
            uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
            uint32_t t2 = S0 + maj;
            hh=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
        }
        h[0]+=a; h[1]+=b; h[2]+=c; h[3]+=d; h[4]+=e; h[5]+=f; h[6]+=g; h[7]+=hh;
    }
    free(m);
    for (int i = 0; i < 8; i++) { out[i*4]=(unsigned char)(h[i]>>24); out[i*4+1]=(unsigned char)(h[i]>>16); out[i*4+2]=(unsigned char)(h[i]>>8); out[i*4+3]=(unsigned char)h[i]; }
}
static char* mfl_hex32(const unsigned char d[32]) {
    static const char* h = "0123456789abcdef";
    char* s = (char*)mfl_alloc(65);
    for (int i = 0; i < 32; i++) { s[i*2] = h[d[i] >> 4]; s[i*2+1] = h[d[i] & 15]; }
    s[64] = 0;
    return s;
}
static char* mfl_sha256(const char* s) {
    unsigned char d[32];
    mfl_sha256_raw((const unsigned char*)s, strlen(s), d);
    return mfl_hex32(d);
}
static char* mfl_hmac_sha256(const char* key, const char* msg) {
    unsigned char k[64];
    size_t klen = strlen(key);
    if (klen > 64) {
        unsigned char kh[32];
        mfl_sha256_raw((const unsigned char*)key, klen, kh);
        memcpy(k, kh, 32); memset(k + 32, 0, 32);
    } else {
        memcpy(k, key, klen); memset(k + klen, 0, 64 - klen);
    }
    unsigned char ipad[64], opad[64];
    for (int i = 0; i < 64; i++) { ipad[i] = k[i] ^ 0x36; opad[i] = k[i] ^ 0x5c; }
    size_t mlen = strlen(msg);
    unsigned char* ibuf = (unsigned char*)malloc(64 + mlen);
    memcpy(ibuf, ipad, 64); memcpy(ibuf + 64, msg, mlen);
    unsigned char inner[32];
    mfl_sha256_raw(ibuf, 64 + mlen, inner);
    free(ibuf);
    unsigned char obuf[96];
    memcpy(obuf, opad, 64); memcpy(obuf + 64, inner, 32);
    unsigned char d[32];
    mfl_sha256_raw(obuf, 96, d);
    return mfl_hex32(d);
}
static char* mfl_json_str(const char* s) { /* quote + escape a string */
    if (!s) s = "";
    size_t n = strlen(s), j = 0;
    char* b = mfl_alloc(n*2 + 3);
    b[j++] = '"';
    for (size_t i = 0; i < n; i++) {
        char c = s[i];
        if (c=='"' || c=='\\') { b[j++]='\\'; b[j++]=c; }
        else if (c=='\n') { b[j++]='\\'; b[j++]='n'; }
        else if (c=='\t') { b[j++]='\\'; b[j++]='t'; }
        else if (c=='\r') { b[j++]='\\'; b[j++]='r'; }
        else b[j++]=c;
    }
    b[j++]='"'; b[j]=0;
    return b;
}
static char* mfl_http_body(const char* s) { /* bytes after the blank line of an HTTP message */
    const char* b = strstr(s, "\r\n\r\n");
    return mfl_dup(b ? b + 4 : "");
}

/* JSON parsing: a cursor (const char**) walked by recursive-descent helpers */
static void mfl_js_ws(const char** p) { while (**p==' '||**p=='\t'||**p=='\n'||**p=='\r') (*p)++; }
static int64_t mfl_js_int(const char** p) { mfl_js_ws(p); char* e; long long v = strtoll(*p, &e, 10); *p = e; return v; }
static double mfl_js_float(const char** p) { mfl_js_ws(p); char* e; double v = strtod(*p, &e); *p = e; return v; }
static int mfl_js_bool(const char** p) { mfl_js_ws(p); if (**p=='t') { *p += 4; return 1; } if (**p=='f') { *p += 5; return 0; } return 0; }
/* mfl_hex4: the 4 hex digits at p as an int, or -1 if any of them aren't hex
   (a malformed \u escape -- the caller falls back to treating it literally). */
static int mfl_hex4(const char* p) {
    int v = 0;
    for (int i = 0; i < 4; i++) {
        char c = p[i]; int d;
        if (c >= '0' && c <= '9') d = c - '0';
        else if (c >= 'a' && c <= 'f') d = c - 'a' + 10;
        else if (c >= 'A' && c <= 'F') d = c - 'A' + 10;
        else return -1;
        v = v * 16 + d;
    }
    return v;
}
/* mfl_utf8_encode: UTF-8 encode one Unicode code point into out, returning the
   byte count written (1-4). */
static size_t mfl_utf8_encode(char* out, int cp) {
    if (cp < 0x80) { out[0] = (char)cp; return 1; }
    if (cp < 0x800) { out[0] = (char)(0xC0 | (cp >> 6)); out[1] = (char)(0x80 | (cp & 0x3F)); return 2; }
    if (cp < 0x10000) { out[0] = (char)(0xE0 | (cp >> 12)); out[1] = (char)(0x80 | ((cp >> 6) & 0x3F)); out[2] = (char)(0x80 | (cp & 0x3F)); return 3; }
    out[0] = (char)(0xF0 | (cp >> 18)); out[1] = (char)(0x80 | ((cp >> 12) & 0x3F)); out[2] = (char)(0x80 | ((cp >> 6) & 0x3F)); out[3] = (char)(0x80 | (cp & 0x3F)); return 4;
}
static char* mfl_js_str(const char** p) {
    mfl_js_ws(p);
    if (**p != '"') return mfl_dup("");
    (*p)++;
    char* out = mfl_alloc(strlen(*p) + 1); size_t j = 0;
    while (**p && **p != '"') {
        char c = **p;
        if (c == '\\') {
            (*p)++; char e = **p;
            if (e == 'u') {
                int cp = mfl_hex4(*p + 1);
                if (cp < 0) { out[j++] = 'u'; (*p)++; }
                else {
                    *p += 5; /* past "u" + 4 hex digits */
                    if (cp >= 0xD800 && cp <= 0xDBFF && (*p)[0] == '\\' && (*p)[1] == 'u') {
                        int lo = mfl_hex4(*p + 2);
                        if (lo >= 0xDC00 && lo <= 0xDFFF) {
                            cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                            *p += 6;
                        }
                    }
                    j += mfl_utf8_encode(out + j, cp);
                }
            }
            else if (e=='n') { out[j++]='\n'; (*p)++; }
            else if (e=='t') { out[j++]='\t'; (*p)++; }
            else if (e=='r') { out[j++]='\r'; (*p)++; }
            else { out[j++] = e; (*p)++; }
        } else { out[j++] = c; (*p)++; }
    }
    if (**p == '"') (*p)++;
    out[j] = 0; return out;
}
static void mfl_js_skip(const char** p) { /* skip one JSON value, including extra object fields */
    mfl_js_ws(p);
    char c = **p;
    if (c == '"') { mfl_js_str(p); return; }
    if (c == '{') { (*p)++; mfl_js_ws(p); if (**p=='}') { (*p)++; return; }
        while (1) { mfl_js_str(p); mfl_js_ws(p); if (**p==':') (*p)++; mfl_js_skip(p); mfl_js_ws(p); if (**p==',') { (*p)++; continue; } break; }
        if (**p=='}') (*p)++; return; }
    if (c == '[') { (*p)++; mfl_js_ws(p); if (**p==']') { (*p)++; return; }
        while (1) { mfl_js_skip(p); mfl_js_ws(p); if (**p==',') { (*p)++; continue; } break; }
        if (**p==']') (*p)++; return; }
    while (**p && **p!=',' && **p!='}' && **p!=']') (*p)++;
}
static int mfl_js_more(const char** p) { mfl_js_ws(p); if (**p==',') { (*p)++; return 1; } return 0; }

/* string operations */
static char* mfl_substr(const char* s, int64_t i, int64_t j) {
    int64_t n = mfl_strlen_cached(s);
    if (i < 0) i = 0; if (j > n) j = n; if (i > j) i = j;
    int64_t len = j - i;
    char* r = mfl_alloc(len + 1); memcpy(r, s + i, len); r[len] = 0;
    return r;
}
static int64_t mfl_index(const char* s, const char* sub) { const char* f = strstr(s, sub); return f ? (int64_t)(f - s) : -1; }
static int mfl_contains(const char* s, const char* sub) { return strstr(s, sub) != NULL; }
static int mfl_has_prefix(const char* s, const char* p) { return strncmp(s, p, strlen(p)) == 0; }
static int mfl_has_suffix(const char* s, const char* p) { size_t ls = strlen(s), lp = strlen(p); return lp <= ls && strcmp(s + ls - lp, p) == 0; }
static char* mfl_charat(const char* s, int64_t i) { int64_t n = strlen(s); if (i < 0 || i >= n) return mfl_dup(""); char* r = mfl_alloc(2); r[0] = s[i]; r[1] = 0; return r; }
static char* mfl_to_upper(const char* s) { size_t n = strlen(s); char* r = mfl_alloc(n + 1); for (size_t i = 0; i < n; i++) r[i] = toupper((unsigned char)s[i]); r[n] = 0; return r; }
static char* mfl_to_lower(const char* s) { size_t n = strlen(s); char* r = mfl_alloc(n + 1); for (size_t i = 0; i < n; i++) r[i] = tolower((unsigned char)s[i]); r[n] = 0; return r; }
static char* mfl_trim(const char* s) {
    while (*s && isspace((unsigned char)*s)) s++;
    int64_t n = strlen(s);
    while (n > 0 && isspace((unsigned char)s[n-1])) n--;
    char* r = mfl_alloc(n + 1); memcpy(r, s, n); r[n] = 0; return r;
}
static char* mfl_replace(const char* s, const char* old, const char* neww) {
    size_t lo = strlen(old);
    if (lo == 0) return mfl_dup(s);
    size_t ln = strlen(neww), cnt = 0;
    const char* t = s; while ((t = strstr(t, old))) { cnt++; t += lo; }
    char* r = mfl_alloc(strlen(s) + cnt * (ln > lo ? ln - lo : 0) + 1);
    char* w = r; const char* p = s;
    while (1) { const char* f = strstr(p, old); if (!f) { strcpy(w, p); break; }
        memcpy(w, p, f - p); w += f - p; memcpy(w, neww, ln); w += ln; p = f + lo; }
    return r;
}
static mfl_slice mfl_split(const char* s, const char* sep) {
    mfl_slice out = {0};
    size_t ls = strlen(sep);
    if (ls == 0) { int64_t n = strlen(s);
        for (int64_t i = 0; i < n; i++) { char* c = mfl_alloc(2); c[0] = s[i]; c[1] = 0; out = mfl_append(out, &c, sizeof(char*)); }
        return out; }
    const char* p = s;
    while (1) { const char* f = strstr(p, sep);
        if (!f) { char* piece = mfl_dup(p); out = mfl_append(out, &piece, sizeof(char*)); break; }
        size_t len = f - p; char* piece = mfl_alloc(len + 1); memcpy(piece, p, len); piece[len] = 0;
        out = mfl_append(out, &piece, sizeof(char*)); p = f + ls; }
    return out;
}
static char* mfl_join(mfl_slice xs, const char* sep) {
    if (xs.len == 0) return mfl_dup("");
    char** parts = (char**)xs.data;
    size_t ls = strlen(sep), total = 0;
    for (int64_t i = 0; i < xs.len; i++) total += strlen(mfl_s(parts[i]));
    total += ls * (size_t)(xs.len - 1);
    char* r = mfl_alloc(total + 1);
    char* w = r;
    for (int64_t i = 0; i < xs.len; i++) {
        if (i > 0) { memcpy(w, sep, ls); w += ls; }
        const char* p = mfl_s(parts[i]);
        size_t lp = strlen(p);
        memcpy(w, p, lp); w += lp;
    }
    *w = 0;
    return r;
}

/* read one line from stdin (without the trailing newline); "" at EOF */
static char* mfl_input(void) {
    size_t cap = 128, len = 0;
    char* buf = mfl_alloc(cap);
    int c;
    while ((c = getchar()) != EOF && c != '\n') {
        if (len + 1 >= cap) { cap *= 2; buf = mfl_realloc(buf, cap); }
        buf[len++] = (char)c;
    }
    buf[len] = 0;
    return buf;
}
/* read all of stdin verbatim until EOF (no line splitting). Exact for text;
   an embedded NUL would truncate the string view (machin strings are C strings). */
static char* mfl_read_stdin(void) {
    if (mfl_rr_mode == 2) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); char* a = mfl_dup_arena(s, L); free(s); return a; }
    size_t cap = 65536, len = 0;
    char* buf = (char*)malloc(cap);
    size_t n;
    while ((n = fread(buf + len, 1, cap - len - 1, stdin)) > 0) {
        len += n;
        if (len + 1 >= cap) { cap *= 2; buf = (char*)realloc(buf, cap); }
    }
    char* r = mfl_dup_arena(buf, len);
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes(buf, len);   /* record the exact input */
    free(buf);
    return r;
}

/* command-line arguments, environment, and wall-clock time */
static int mfl_argc = 0;
static char** mfl_argv = NULL;
static mfl_slice mfl_args(void) {
    mfl_slice s = { mfl_argc ? mfl_alloc(mfl_argc * sizeof(char*)) : NULL, mfl_argc, mfl_argc };
    for (int i = 0; i < mfl_argc; i++) ((char**)s.data)[i] = mfl_argv[i];
    return s;
}
static char* mfl_env(const char* k) { char* v = getenv(k); return v ? v : ""; }
static int64_t mfl_now(void) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    int64_t v = (int64_t)time(NULL);
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(v);
    return v;
}
static int64_t mfl_now_ms(void) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    struct timeval tv; gettimeofday(&tv, NULL);
    int64_t v = (int64_t)tv.tv_sec * 1000 + tv.tv_usec / 1000;
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(v);
    return v;
}
/* decompose a Unix timestamp (local time) into
   [year, month(1-12), day(1-31), hour, minute, second, weekday(0=Sun), yearday(1-366)] */
static mfl_slice mfl_time_fields(int64_t unix) {
    time_t t = (time_t)unix;
    struct tm tmv;
    localtime_r(&t, &tmv);
    int64_t v[8] = { tmv.tm_year + 1900, tmv.tm_mon + 1, tmv.tm_mday,
        tmv.tm_hour, tmv.tm_min, tmv.tm_sec, tmv.tm_wday, tmv.tm_yday + 1 };
    mfl_slice s = { mfl_alloc(8 * sizeof(int64_t)), 8, 8 };
    memcpy(s.data, v, sizeof(v));
    return s;
}
/* format a Unix timestamp (local time) with a strftime(3) pattern:
   %Y %m %d %H %M %S %A %a %B %b %p %j %z %Z %F %T ... ("" if it overflows) */
static char* mfl_time_format(int64_t unix, const char* fmt) {
    time_t t = (time_t)unix;
    struct tm tmv;
    localtime_r(&t, &tmv);
    char buf[512];
    size_t n = strftime(buf, sizeof(buf), fmt, &tmv);
    char* out = mfl_alloc(n + 1);
    memcpy(out, buf, n);
    out[n] = 0;
    return out;
}
/* construct a Unix timestamp from calendar fields (local time, the inverse of
   time_fields): mktime normalizes out-of-range fields (e.g. day 32 rolls over)
   and resolves DST via tm_isdst=-1. */
static int64_t mfl_time_make(int64_t y, int64_t mo, int64_t d, int64_t h, int64_t mi, int64_t s) {
    struct tm tmv;
    memset(&tmv, 0, sizeof(tmv));
    tmv.tm_year = (int)(y - 1900);
    tmv.tm_mon = (int)(mo - 1);
    tmv.tm_mday = (int)d;
    tmv.tm_hour = (int)h;
    tmv.tm_min = (int)mi;
    tmv.tm_sec = (int)s;
    tmv.tm_isdst = -1;
    return (int64_t)mktime(&tmv);
}
/* like time_format but in UTC (gmtime): the form .ics / RFC-3339 timestamps want. */
static char* mfl_time_format_utc(int64_t unix, const char* fmt) {
    time_t t = (time_t)unix;
    struct tm tmv;
    gmtime_r(&t, &tmv);
    char buf[512];
    size_t n = strftime(buf, sizeof(buf), fmt, &tmv);
    char* out = mfl_alloc(n + 1);
    memcpy(out, buf, n);
    out[n] = 0;
    return out;
}
static int64_t mfl_parse_int(const char* s) { return (int64_t)strtoll(s, NULL, 10); }
static double mfl_parse_float(const char* s) { return strtod(s, NULL); }

/* file system: read/write whole files, list a directory, make a directory */

/* fopen(dir, "rb") SUCCEEDS on Linux (opening a directory read-only is legal
   at the syscall level), but ftell() on it returns LONG_MAX (not -1) rather
   than failing — so the usual "if (n < 0) n = 0" guard never catches it, and
   the caller ends up trying to alloc ~9.2 exabytes. list_dir()'s entries can
   be subdirectories, so read_file/read_file_bytes on one of those is a real,
   easy-to-hit path, not a contrived one — check with stat() first. */
static int mfl_is_dir(const char* path) {
    struct stat st;
    return stat(path, &st) == 0 && S_ISDIR(st.st_mode);
}
static char* mfl_read_file(const char* path) {
    /* a file read is external input: recording its contents makes replay self-contained —
       it reproduces even after the original files are gone (the "mailable crash"). */
    if (mfl_rr_mode == 2) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); char* a = mfl_dup_arena(s, L); free(s); return a; }
    if (mfl_is_dir(path)) { if (mfl_rr_mode == 1) mfl_rr_io_log_bytes("", 0); return mfl_dup(""); }
    FILE* f = fopen(path, "rb");
    if (!f) { if (mfl_rr_mode == 1) mfl_rr_io_log_bytes("", 0); return mfl_dup(""); }
    fseek(f, 0, SEEK_END); long n = ftell(f); fseek(f, 0, SEEK_SET);
    if (n < 0) n = 0;
    char* buf = mfl_alloc((size_t)n + 1);
    size_t r = fread(buf, 1, (size_t)n, f); buf[r] = 0;
    fclose(f);
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes(buf, r);
    return buf;
}
static int64_t mfl_write_file(const char* path, const char* content) {
    FILE* f = fopen(path, "wb");
    if (!f) return -1;
    size_t len = strlen(content);
    size_t w = fwrite(content, 1, len, f);
    fclose(f); return (int64_t)w;
}
/* write raw bytes to a file, NUL-safe (length-driven) — for binary uploads/assets. */
static int64_t mfl_write_file_bytes(const char* path, mfl_bytes b) {
    FILE* f = fopen(path, "wb");
    if (!f) return -1;
    size_t w = b.len ? fwrite(b.data, 1, (size_t)b.len, f) : 0;
    fclose(f); return (int64_t)w;
}
/* raw region file I/O: dump/restore a memory region (ptr,nbytes) to/from a file
   with a single fwrite/fread. For large buffers that a bytes value cannot hold
   cheaply (e.g. a serialized KV cache). Returns bytes transferred, -1 on open. */
static int64_t mfl_write_file_raw(const char* path, int64_t ptr, int64_t nbytes) {
    FILE* f = fopen(path, "wb");
    if (!f) return -1;
    size_t w = nbytes > 0 ? fwrite((const void*)(intptr_t)ptr, 1, (size_t)nbytes, f) : 0;
    fclose(f);
    return (int64_t)w;
}
static int64_t mfl_read_file_raw(const char* path, int64_t ptr, int64_t nbytes) {
    FILE* f = fopen(path, "rb");
    if (!f) return -1;
    size_t r = nbytes > 0 ? fread((void*)(intptr_t)ptr, 1, (size_t)nbytes, f) : 0;
    fclose(f);
    return (int64_t)r;
}
/* delete a file (0 on success, -1 on error) — e.g. removing a stored upload. */
static int64_t mfl_remove_file(const char* path) { return remove(path) == 0 ? 0 : -1; }
/* read a file's raw bytes (NUL-safe, unlike read_file which returns a C string).
   Empty bytes if the file can't be opened. */
static mfl_bytes mfl_read_file_bytes(const char* path) {
    mfl_bytes b; b.len = 0; b.data = (uint8_t*)mfl_alloc(1);
    /* same rationale as mfl_read_file: record the raw bytes for a self-contained replay. */
    if (mfl_rr_mode == 2) {
        size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L);
        b.data = (uint8_t*)mfl_alloc(L ? L : 1); b.len = (int64_t)L;
        if (L) memcpy(b.data, s, L); free(s); return b;
    }
    if (mfl_is_dir(path)) { if (mfl_rr_mode == 1) mfl_rr_io_log_bytes("", 0); return b; }
    FILE* f = fopen(path, "rb");
    if (!f) { if (mfl_rr_mode == 1) mfl_rr_io_log_bytes("", 0); return b; }
    fseek(f, 0, SEEK_END); long n = ftell(f); fseek(f, 0, SEEK_SET);
    if (n < 0) n = 0;
    b.data = (uint8_t*)mfl_alloc((size_t)n ? (size_t)n : 1);
    b.len = (int64_t)fread(b.data, 1, (size_t)n, f);
    fclose(f);
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes((char*)b.data, (size_t)b.len);
    return b;
}
static mfl_slice mfl_list_dir(const char* path) {
    mfl_slice out = {0};
    DIR* d = opendir(path);
    if (!d) return out;
    struct dirent* e;
    while ((e = readdir(d))) {
        if (strcmp(e->d_name, ".") == 0 || strcmp(e->d_name, "..") == 0) continue;
        char* name = mfl_dup(e->d_name);
        out = mfl_append(out, &name, sizeof(char*));
    }
    closedir(d); return out;
}
static int64_t mfl_mkdir(const char* path) {
    int r = mkdir(path, 0755);
    if (r < 0 && errno == EEXIST) return 0;
    return r;
}
#ifndef __wasm__
/* run a shell command, return its exit code (-1 if it could not be launched). For
   process orchestration — e.g. spawning a detached daemon with a trailing "&". */
static int64_t mfl_system(const char* cmd) {
    int r = system(cmd);
    if (r == -1) return -1;
    return (int64_t)WEXITSTATUS(r);
}
/* run a shell command and capture its output: returns (exit_code, stdout, stderr).
   The command runs via /bin/sh in a subshell with stdout/stderr redirected to temp
   files (so there is no pipe-buffer deadlock), then both are read back. Captured
   text is NUL-terminated — a command producing binary output should redirect it to
   a file itself (e.g. mongodump --archive piped to gzip > out.gz). */
typedef struct { int64_t code; char* out; char* err; } mfl_exec_result;
static mfl_exec_result mfl_exec(const char* cmd) {
    mfl_exec_result R; R.code = -1; R.out = mfl_dup(""); R.err = mfl_dup("");
    char op[] = "/tmp/mfl-exec-XXXXXX", ep[] = "/tmp/mfl-exec-XXXXXX";
    int fo = mkstemp(op); if (fo < 0) return R; close(fo);
    int fe = mkstemp(ep); if (fe < 0) { unlink(op); return R; } close(fe);
    size_t n = strlen(cmd) + strlen(op) + strlen(ep) + 16;
    char* full = (char*)malloc(n);
    if (full) {
        snprintf(full, n, "( %s ) >%s 2>%s", cmd, op, ep);
        int r = system(full);
        R.code = (r == -1) ? -1 : (int64_t)WEXITSTATUS(r);
        R.out = mfl_read_file(op);
        R.err = mfl_read_file(ep);
        free(full);
    }
    unlink(op); unlink(ep);
    return R;
}
#endif

/* copy n bytes into a fresh NUL-terminated arena string */
static char* mfl_dup_arena(const char* s, size_t n) {
    char* r = (char*)mfl_alloc(n + 1);
    if (n) memcpy(r, s, n);
    r[n] = 0;
    return r;
}

/* ---- JSON path query (json_get) ----
   A non-allocating scanner: it walks the document following a jq-style path
   (.key, [index], chained) and returns the located value's raw JSON text. No
   tree is built — values not on the path are skipped, respecting nesting and
   string escapes (unlike naive substring search). */
typedef struct { char* value; char* err; } mfl_json_result;

static const char* mfl_jq_ws(const char* p) {
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    return p;
}
static const char* mfl_jq_str(const char* p) { /* p at opening quote -> past closing */
    if (*p != '"') return NULL;
    p++;
    while (*p) {
        if (*p == '\\') { p++; if (!*p) return NULL; p++; continue; }
        if (*p == '"') return p + 1;
        p++;
    }
    return NULL;
}
static const char* mfl_jq_val(const char* p) { /* skip one value -> just past it */
    p = mfl_jq_ws(p);
    if (*p == '"') return mfl_jq_str(p);
    if (*p == '{' || *p == '[') {
        char open = *p, close = open == '{' ? '}' : ']';
        p = mfl_jq_ws(p + 1);
        if (*p == close) return p + 1;
        for (;;) {
            if (open == '{') {
                p = mfl_jq_ws(p);
                p = mfl_jq_str(p); if (!p) return NULL;
                p = mfl_jq_ws(p);
                if (*p != ':') return NULL;
                p++;
            }
            p = mfl_jq_val(p); if (!p) return NULL;
            p = mfl_jq_ws(p);
            if (*p == ',') { p++; continue; }
            if (*p == close) return p + 1;
            return NULL;
        }
    }
    if (*p == ',' || *p == '}' || *p == ']' || *p == 0) return NULL;
    while (*p && *p != ',' && *p != '}' && *p != ']' && *p != ' ' && *p != '\t' && *p != '\n' && *p != '\r') p++;
    return p;
}
static const char* mfl_jq_member(const char* p, const char* key, size_t keylen) {
    p = mfl_jq_ws(p);
    if (*p != '{') return NULL;
    p = mfl_jq_ws(p + 1);
    if (*p == '}') return NULL;
    for (;;) {
        p = mfl_jq_ws(p);
        if (*p != '"') return NULL;
        const char* ks = p + 1;
        const char* ke = mfl_jq_str(p); if (!ke) return NULL;
        size_t klen = (size_t)((ke - 1) - ks);
        p = mfl_jq_ws(ke);
        if (*p != ':') return NULL;
        const char* vs = mfl_jq_ws(p + 1);
        if (klen == keylen && memcmp(ks, key, keylen) == 0) return vs;
        const char* ve = mfl_jq_val(vs); if (!ve) return NULL;
        p = mfl_jq_ws(ve);
        if (*p == ',') { p++; continue; }
        return NULL;
    }
}
static const char* mfl_jq_elem(const char* p, long n) {
    p = mfl_jq_ws(p);
    if (*p != '[') return NULL;
    p = mfl_jq_ws(p + 1);
    if (*p == ']') return NULL;
    long i = 0;
    for (;;) {
        const char* vs = mfl_jq_ws(p);
        if (i == n) return vs;
        const char* ve = mfl_jq_val(vs); if (!ve) return NULL;
        p = mfl_jq_ws(ve);
        if (*p == ',') { p++; i++; continue; }
        return NULL;
    }
}
static mfl_json_result mfl_json_get(const char* json, const char* path) {
    mfl_json_result R;
    R.value = mfl_dup_arena("", 0);
    R.err = mfl_dup_arena("", 0);
    const char* cur = mfl_jq_ws(json);
    const char* p = path;
    while (*p) {
        if (*p == '.') {
            p++;
            const char* ks = p;
            while (*p && *p != '.' && *p != '[') p++;
            size_t klen = (size_t)(p - ks);
            if (klen == 0) continue;
            cur = mfl_jq_member(cur, ks, klen);
            if (!cur) { R.err = mfl_dup_arena("notfound", 8); return R; }
        } else if (*p == '[') {
            p++;
            char* endp;
            long idx = strtol(p, &endp, 10);
            if (endp == p || *endp != ']') { R.err = mfl_dup_arena("path", 4); return R; }
            p = endp + 1;
            cur = mfl_jq_elem(cur, idx);
            if (!cur) { R.err = mfl_dup_arena("notfound", 8); return R; }
        } else {
            R.err = mfl_dup_arena("path", 4);
            return R;
        }
    }
    const char* end = mfl_jq_val(cur);
    if (!end) { R.err = mfl_dup_arena("parse", 5); return R; }
    R.value = mfl_dup_arena(cur, (size_t)(end - cur));
    return R;
}
`

// netRuntime is the POSIX socket layer (Go's net package, low-level): listen/
// accept/dial/read/write/close over TCP fds. Part of the always-on runtime for
// the native target; under the wasm target it is emitted only when the program
// actually calls one of these builtins (a frontend app that touches no sockets
// then references no POSIX networking symbols at all).
const netRuntime = `/* networking: the low-level shape of Go's net package */
static int64_t mfl_listen(int64_t port) {
    /* replay: return the recorded fd without binding a real port (it may be taken, and
       no bytes flow through it on replay anyway — reads are served from the I/O log). */
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    int opt = 1; setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
    struct sockaddr_in a; memset(&a, 0, sizeof(a));
    a.sin_family = AF_INET; a.sin_addr.s_addr = INADDR_ANY; a.sin_port = htons((uint16_t)port);
    if (bind(fd, (struct sockaddr*)&a, sizeof(a)) < 0) { perror("bind"); exit(1); }
    if (listen(fd, 64) < 0) { perror("listen"); exit(1); }
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(fd);
    return fd;
}
static int64_t mfl_accept(int64_t fd) {
    /* replay: recorded fd, and crucially DON'T block on a real accept (no client exists). */
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    int64_t r = accept((int)fd, NULL, NULL);
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(r);
    return r;
}
/* peer_addr: the remote IP of a connected socket (getpeername), "" on error — the real
   client IP when not behind a proxy (behind one, prefer X-Forwarded-For). */
static const char* mfl_peer_addr(int64_t fd) {
    if (mfl_rr_mode == 2) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); char* a = mfl_dup_arena(s, L); free(s); return a; }
    const char* out = "";
    struct sockaddr_storage ss; socklen_t sl = sizeof(ss);
    char host[64] = {0};
    if (getpeername((int)fd, (struct sockaddr*)&ss, &sl) == 0 &&
        getnameinfo((struct sockaddr*)&ss, sl, host, sizeof(host), NULL, 0, NI_NUMERICHOST) == 0) {
        char* r = (char*)mfl_alloc(strlen(host) + 1); strcpy(r, host); out = r;
    }
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes(out, strlen(out));
    return out;
}
/* socket_timeout: cap blocking recv/send on a socket to ms milliseconds (0 = none), so a
   slow client can't park a connection forever. Returns 0 on success, -1 on error. */
static int64_t mfl_socket_timeout(int64_t fd, int64_t ms) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    struct timeval tv; tv.tv_sec = ms / 1000; tv.tv_usec = (ms % 1000) * 1000;
    int a = setsockopt((int)fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
    int b = setsockopt((int)fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));
    int64_t r = (a == 0 && b == 0) ? 0 : -1;
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(r);
    return r;
}
/* dial: connect a TCP socket to host:port, returning an fd (-1 on failure).
   The fd is used with the same read/write/close as an accepted connection. */
static int64_t mfl_dial(const char* host, int64_t port) {
    /* replay: recorded fd, no real connect (the peer's bytes come from the I/O log). */
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    struct addrinfo hints, *res, *rp;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_UNSPEC; hints.ai_socktype = SOCK_STREAM;
    char ps[16]; snprintf(ps, sizeof(ps), "%lld", (long long)port);
    if (getaddrinfo(host, ps, &hints, &res) != 0) { if (mfl_rr_mode == 1) mfl_rr_io_log_i64(-1); return -1; }
    int fd = -1;
    for (rp = res; rp; rp = rp->ai_next) {
        fd = socket(rp->ai_family, rp->ai_socktype, rp->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, rp->ai_addr, rp->ai_addrlen) == 0) break;
        close(fd); fd = -1;
    }
    freeaddrinfo(res);
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(fd);
    return fd;
}
static char* mfl_read(int64_t fd) {
    /* a socket read is external input, like read_file/read_stdin — record the bytes so
       replay reproduces the exchange with no network (the trace is self-contained). */
    if (mfl_rr_mode == 2) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); char* a = mfl_dup_arena(s, L); free(s); return a; }
    char* buf = mfl_alloc(65536);
    ssize_t n = read((int)fd, buf, 65535);
    if (n < 0) n = 0;
    buf[n] = 0;
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes(buf, (size_t)n);
    return buf;
}
// mfl_read_bytes is the NUL-safe socket read: returns the raw bytes of one chunk
// (empty at EOF / on error). For binary wire protocols where read() (a C string)
// would truncate at the first 0 byte.
static mfl_bytes mfl_read_bytes(int64_t fd) {
    if (mfl_rr_mode == 2) {
        size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L);
        mfl_bytes b; b.data = (uint8_t*)mfl_alloc(L ? L : 1); b.len = (int64_t)L;
        if (L) memcpy(b.data, s, L); free(s); return b;
    }
    mfl_bytes b; b.data = (uint8_t*)mfl_alloc(65536);
    ssize_t n = read((int)fd, b.data, 65536);
    if (n < 0) n = 0;
    b.len = (int64_t)n;
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes((char*)b.data, (size_t)n);
    return b;
}
/* a write is an output side-effect: replay records/returns the byte count but performs
   no real send (the fd is a recorded sentinel, so writing would EPIPE). */
static int64_t mfl_write(int64_t fd, const char* s) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    int64_t r = (int64_t)write((int)fd, s, strlen(s));
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(r);
    return r;
}
/* write the exact bytes of a buffer to an fd (NUL-safe, for binary responses). */
static int64_t mfl_write_bytes(int64_t fd, mfl_bytes b) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    size_t off = 0;
    while (off < (size_t)b.len) {
        ssize_t w = write((int)fd, b.data + off, (size_t)b.len - off);
        if (w <= 0) break;
        off += (size_t)w;
    }
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64((int64_t)off);
    return (int64_t)off;
}
static void mfl_close(int64_t fd) { close((int)fd); }
`

// ttyRuntime is terminal raw mode + non-blocking single-key reads (termios +
// select), for TUIs and terminal games. Always-on for native; under wasm emitted
// only when raw_mode/read_key is used (a browser app references neither).
const ttyRuntime = `/* terminal raw mode + non-blocking single-key read (for TUIs and games).
   raw_mode(1) puts the tty in cbreak + no-echo with VMIN=0/VTIME=0 so reads
   never block; raw_mode(0) restores the saved settings. */
static struct termios mfl_tty_saved;
static int mfl_tty_raw = 0;
static int64_t mfl_raw_mode(int64_t on) {
    if (on) {
        if (mfl_tty_raw) return 0;
        struct termios t;
        if (tcgetattr(STDIN_FILENO, &t) != 0) return -1;
        mfl_tty_saved = t;
        t.c_lflag &= ~(ICANON | ECHO);
        t.c_cc[VMIN] = 0;
        t.c_cc[VTIME] = 0;
        if (tcsetattr(STDIN_FILENO, TCSANOW, &t) != 0) return -1;
        mfl_tty_raw = 1;
    } else {
        if (!mfl_tty_raw) return 0;
        tcsetattr(STDIN_FILENO, TCSANOW, &mfl_tty_saved);
        mfl_tty_raw = 0;
    }
    return 0;
}
/* non-blocking read of one key; a 1-char string, or "" if nothing is waiting.
   In raw mode VMIN=0 already makes read() return immediately; otherwise poll
   with select() so we never block. */
static char* mfl_read_key(void) {
    char* buf = mfl_alloc(2);
    buf[0] = 0; buf[1] = 0;
    unsigned char c = 0;
    if (mfl_tty_raw) {
        if (read(STDIN_FILENO, &c, 1) == 1) buf[0] = (char)c;
    } else {
        fd_set fds; FD_ZERO(&fds); FD_SET(STDIN_FILENO, &fds);
        struct timeval tv; tv.tv_sec = 0; tv.tv_usec = 0;
        if (select(STDIN_FILENO + 1, &fds, NULL, NULL, &tv) > 0) {
            if (read(STDIN_FILENO, &c, 1) == 1) buf[0] = (char)c;
        }
    }
    return buf;
}
`

// tlsCoreRuntime holds the shared OpenSSL plumbing (a verified TLS dial) used by
// both the HTTPS client and the WebSocket client. Emitted whenever a program
// uses native TLS (https_* or wss_*); linked against -lssl -lcrypto. A single
// process-global SSL_CTX is shared across all connections (OpenSSL makes SSL_new
// on a shared CTX thread-safe), so per-connection setup is just SSL_new+connect.
const tlsCoreRuntime = `#include <openssl/ssl.h>
#include <openssl/err.h>
#include <openssl/rand.h>
#include <openssl/pem.h>
#include <openssl/x509.h>

#ifdef MFL_HAS_CABUNDLE
/* Compiled in by build.go (machin build --static) as a separate translation
   unit from the embedded, gzipped vendor/certs/cacert.pem.gz. Not present in a
   normal (non-static) build. */
extern const unsigned char mfl_ca_bundle_pem[];
extern const unsigned long mfl_ca_bundle_pem_len;
#endif

static SSL_CTX* mfl_ssl_ctx(void) {
    static SSL_CTX* ctx = NULL;
    static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
    pthread_mutex_lock(&mu);
    if (!ctx) {
        ctx = SSL_CTX_new(TLS_client_method());
        if (ctx) {
            SSL_CTX_set_default_verify_paths(ctx);
#ifdef MFL_HAS_CABUNDLE
            /* Fallback trust roots for a static binary running with no system CA
               store (e.g. FROM scratch) — added alongside, not instead of, whatever
               the system store above already loaded. */
            BIO* cabio = BIO_new_mem_buf(mfl_ca_bundle_pem, (int)mfl_ca_bundle_pem_len);
            if (cabio) {
                X509_STORE* store = SSL_CTX_get_cert_store(ctx);
                X509* cert;
                while ((cert = PEM_read_bio_X509(cabio, NULL, NULL, NULL)) != NULL) {
                    X509_STORE_add_cert(store, cert); /* dup return (already present) is harmless */
                    X509_free(cert);
                }
                BIO_free(cabio);
            }
#endif
        }
    }
    pthread_mutex_unlock(&mu);
    return ctx;
}

/* the handshake half of a client dial: SNI + verified hostname on an already-
   connected fd. Does NOT close fd on failure — used both by dial (which owns
   the fd it just opened) and by tls_client_fd/STARTTLS (which upgrades a
   caller-owned fd already in use for a plaintext exchange; the caller decides
   whether to close it). On failure, *stage (if non-NULL) is set to "tls". */
static SSL* mfl_tls_handshake_client(int fd, const char* host, const char** stage) {
    SSL_CTX* ctx = mfl_ssl_ctx();
    if (!ctx) { if (stage) *stage = "tls"; return NULL; }
    SSL* ssl = SSL_new(ctx);
    SSL_set_fd(ssl, fd);
    SSL_set_tlsext_host_name(ssl, host);
    SSL_set1_host(ssl, host);
    SSL_set_verify(ssl, SSL_VERIFY_PEER, NULL);
    if (SSL_connect(ssl) != 1) { if (stage) *stage = "tls"; SSL_free(ssl); return NULL; }
    return ssl;
}

/* dial host:port and complete a verified TLS handshake (SNI + hostname).
   Returns a connected SSL* (fd retrievable via SSL_get_fd) or NULL. On failure,
   *stage (if non-NULL) is set to why: "dns", "connect", or "tls". */
static SSL* mfl_tls_dial_e(const char* host, int port, const char** stage) {
    struct addrinfo hints, *res = NULL;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    char ports[16];
    snprintf(ports, sizeof(ports), "%d", port);
    if (getaddrinfo(host, ports, &hints, &res) != 0) { if (stage) *stage = "dns"; return NULL; }
    int fd = -1;
    for (struct addrinfo* a = res; a; a = a->ai_next) {
        fd = socket(a->ai_family, a->ai_socktype, a->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, a->ai_addr, a->ai_addrlen) == 0) break;
        close(fd);
        fd = -1;
    }
    freeaddrinfo(res);
    if (fd < 0) { if (stage) *stage = "connect"; return NULL; }
    SSL* ssl = mfl_tls_handshake_client(fd, host, stage);
    if (!ssl) { close(fd); return NULL; } /* dial owns fd: close it on handshake failure */
    return ssl;
}

static SSL* mfl_tls_dial(const char* host, int port) { return mfl_tls_dial_e(host, port, NULL); }

static void mfl_tls_hangup(SSL* ssl) {
    if (!ssl) return;
    int fd = SSL_get_fd(ssl);
    SSL_shutdown(ssl);
    if (fd >= 0) close(fd);
    SSL_free(ssl);
}

/* tls_client_fd: the STARTTLS primitive — upgrade an already-connected,
   plaintext-negotiated fd (from a plain dial()) to TLS in place. Does not close
   fd on failure (caller owns it, e.g. to log/close/retry plaintext). */
static int64_t mfl_tls_client_fd(int64_t fd, const char* host) {
    SSL* ssl = mfl_tls_handshake_client((int)fd, host, NULL);
    return ssl ? (int64_t)(intptr_t)ssl : 0;
}

/* tls_server_ctx/tls_accept: server-side TLS termination — serve_tls in
   framework/machweb.src is the MFL-side consumer. No client-cert verification
   (not mutual TLS) and one cert per ctx (no SNI multi-cert) — v1 scope. */
static int64_t mfl_tls_server_ctx(const char* certfile, const char* keyfile) {
    SSL_CTX* ctx = SSL_CTX_new(TLS_server_method());
    if (!ctx) return 0;
    if (SSL_CTX_use_certificate_file(ctx, certfile, SSL_FILETYPE_PEM) != 1 ||
        SSL_CTX_use_PrivateKey_file(ctx, keyfile, SSL_FILETYPE_PEM) != 1) {
        SSL_CTX_free(ctx);
        return 0;
    }
    return (int64_t)(intptr_t)ctx;
}
static int64_t mfl_tls_accept(int64_t ctxHandle, int64_t fd) {
    /* replay: recorded handle sentinel, no real TLS handshake (reads come from the log). */
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    SSL_CTX* ctx = (SSL_CTX*)(intptr_t)ctxHandle;
    if (!ctx) { if (mfl_rr_mode == 1) mfl_rr_io_log_i64(0); return 0; }
    SSL* ssl = SSL_new(ctx);
    SSL_set_fd(ssl, (int)fd);
    if (SSL_accept(ssl) != 1) { SSL_free(ssl); if (mfl_rr_mode == 1) mfl_rr_io_log_i64(0); return 0; }
    int64_t r = (int64_t)(intptr_t)ssl;
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(r);
    return r;
}

/* generic read/write/close over a tls handle (an SSL*, from tls_accept or
   tls_client_fd) — mirror mfl_read/mfl_read_bytes/mfl_write/mfl_write_bytes's
   chunk semantics (one SSL_read's worth, up to 64K; a full-write loop) exactly,
   just over SSL_read/SSL_write instead of read(2)/write(2). */
static char* mfl_tls_read_str(int64_t h) {
    /* a TLS read is external input: record the plaintext chunk so replay reproduces it
       with no handshake, like mfl_read for raw sockets. */
    if (mfl_rr_mode == 2) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); char* a = mfl_dup_arena(s, L); free(s); return a; }
    SSL* ssl = (SSL*)(intptr_t)h;
    char* buf = mfl_alloc(65536);
    int n = ssl ? SSL_read(ssl, buf, 65535) : 0;
    if (n < 0) n = 0;
    buf[n] = 0;
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes(buf, (size_t)n);
    return buf;
}
static mfl_bytes mfl_tls_read_bytes_h(int64_t h) {
    if (mfl_rr_mode == 2) {
        size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L);
        mfl_bytes b; b.data = (uint8_t*)mfl_alloc(L ? L : 1); b.len = (int64_t)L;
        if (L) memcpy(b.data, s, L); free(s); return b;
    }
    SSL* ssl = (SSL*)(intptr_t)h;
    mfl_bytes b; b.data = (uint8_t*)mfl_alloc(65536);
    int n = ssl ? SSL_read(ssl, b.data, 65536) : 0;
    if (n < 0) n = 0;
    b.len = (int64_t)n;
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes((char*)b.data, (size_t)n);
    return b;
}
static int64_t mfl_tls_write_str(int64_t h, const char* s) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    SSL* ssl = (SSL*)(intptr_t)h;
    int64_t r = ssl ? (int64_t)SSL_write(ssl, s, (int)strlen(s)) : -1;
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(r);
    return r;
}
static int64_t mfl_tls_write_bytes_h(int64_t h, mfl_bytes b) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    SSL* ssl = (SSL*)(intptr_t)h;
    if (!ssl) { if (mfl_rr_mode == 1) mfl_rr_io_log_i64(-1); return -1; }
    size_t off = 0;
    while (off < (size_t)b.len) {
        int w = SSL_write(ssl, b.data + off, (int)((size_t)b.len - off));
        if (w <= 0) break;
        off += (size_t)w;
    }
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64((int64_t)off);
    return (int64_t)off;
}
/* close is a side-effect with a constant return; on replay just skip the real hangup (the
   handle is a recorded sentinel). No I/O-queue entry either way, so the queue stays balanced. */
static int64_t mfl_tls_close_h(int64_t h) { if (mfl_rr_mode == 2) return 0; mfl_tls_hangup((SSL*)(intptr_t)h); return 0; }
`

// tlsRuntime is a minimal HTTPS/1.1 client built on tlsCoreRuntime. Emitted only
// when a program calls https_get/https_post. Handles cert verification,
// Content-Length, chunked transfer-encoding, and redirects; returns the body.
const tlsRuntime = `
/* read the whole TLS stream into a malloc'd buffer (caller frees); NUL-terminated */
static char* mfl_tls_readall(SSL* ssl, size_t* outlen) {
    size_t cap = 16384, len = 0;
    char* buf = (char*)malloc(cap);
    for (;;) {
        if (len + 8192 > cap) { cap *= 2; buf = (char*)realloc(buf, cap); }
        int n = SSL_read(ssl, buf + len, 8192);
        if (n <= 0) break;
        len += (size_t)n;
    }
    buf[len] = 0;
    *outlen = len;
    return buf;
}

/* decode HTTP/1.1 chunked transfer-encoding (caller frees) */
static char* mfl_chunk_decode(const char* body, size_t blen, size_t* outlen) {
    char* out = (char*)malloc(blen + 1);
    size_t o = 0;
    const char* p = body;
    const char* end = body + blen;
    while (p < end) {
        char* nl;
        long sz = strtol(p, &nl, 16);
        if (nl == p) break;
        while (nl < end && *nl != '\n') nl++;
        if (nl < end) nl++;
        p = nl;
        if (sz <= 0) break;
        if (p + sz > end) sz = end - p;
        memcpy(out + o, p, (size_t)sz);
        o += (size_t)sz;
        p += sz;
        while (p < end && (*p == '\r' || *p == '\n')) p++;
    }
    out[o] = 0;
    *outlen = o;
    return out;
}

/* (status, body, err) — err is "" on an HTTP response (status is the code), or a
   transport-failure reason ("dns"/"connect"/"tls"/"scheme") with status 0. */
typedef struct { int64_t status; char* body; char* err; } mfl_http_result;

/* record/replay for the HTTP result: an HTTP round-trip is external input, so the whole
   result (status + body + err) is logged and popped — a program that crashes processing an
   API response replays with the exact response baked in, no network. Instrumented at the
   public wrappers (not mfl_http_do) so redirects log exactly once per logical call. */
static void mfl_rr_log_http(mfl_http_result R) {
    mfl_rr_io_log_i64(R.status);
    mfl_rr_io_log_bytes(R.body ? R.body : "", R.body ? strlen(R.body) : 0);
    mfl_rr_io_log_bytes(R.err ? R.err : "", R.err ? strlen(R.err) : 0);
}
static mfl_http_result mfl_rr_pop_http(void) {
    mfl_http_result R;
    R.status = mfl_rr_io_pop_i64();
    { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); R.body = mfl_dup_arena(s, L); free(s); }
    { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); R.err = mfl_dup_arena(s, L); free(s); }
    return R;
}

static mfl_http_result mfl_http_do(const char* method, const char* url, const char* reqbody, const char* ctype, const char* extra, int redirects);

/* plain (non-TLS) transport for http:// URLs, mirroring the TLS path's staged
   error vocabulary ("dns"/"connect"). */
static int mfl_tcp_dial_e(const char* host, int port, const char** stage) {
    struct addrinfo hints, *res, *rp;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_UNSPEC; hints.ai_socktype = SOCK_STREAM;
    char ps[16]; snprintf(ps, sizeof(ps), "%d", port);
    if (getaddrinfo(host, ps, &hints, &res) != 0) { if (stage) *stage = "dns"; return -1; }
    int fd = -1;
    for (rp = res; rp; rp = rp->ai_next) {
        fd = socket(rp->ai_family, rp->ai_socktype, rp->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, rp->ai_addr, rp->ai_addrlen) == 0) break;
        close(fd); fd = -1;
    }
    freeaddrinfo(res);
    if (fd < 0 && stage) *stage = "connect";
    return fd;
}
static void mfl_sock_writeall(int fd, const char* buf, size_t n) {
    size_t off = 0;
    while (off < n) { ssize_t w = send(fd, buf + off, n - off, 0); if (w <= 0) break; off += (size_t)w; }
}
static char* mfl_sock_readall(int fd, size_t* outlen) {
    size_t cap = 16384, len = 0;
    char* buf = (char*)malloc(cap);
    for (;;) {
        if (len + 8192 > cap) { cap *= 2; buf = (char*)realloc(buf, cap); }
        ssize_t n = recv(fd, buf + len, 8192, 0);
        if (n <= 0) break;
        len += (size_t)n;
    }
    buf[len] = 0;
    *outlen = len;
    return buf;
}

static mfl_http_result mfl_http_do(const char* method, const char* url, const char* reqbody, const char* ctype, const char* extra, int redirects) {
    mfl_http_result R;
    R.status = 0;
    R.body = mfl_dup_arena("", 0);
    R.err = mfl_dup_arena("", 0);

    char host[256] = {0}, path[2048] = {0};
    int is_https = 1;
    const char* p = url;
    if (strncmp(p, "https://", 8) == 0) p += 8;
    else if (strncmp(p, "http://", 7) == 0) { p += 7; is_https = 0; }
    /* else: no scheme — assume https */
    int port = is_https ? 443 : 80;
    int i = 0;
    while (*p && *p != '/' && *p != ':' && i < 255) host[i++] = *p++;
    host[i] = 0;
    if (*p == ':') { p++; port = atoi(p); while (*p && *p != '/') p++; }
    if (*p == '/') strncpy(path, p, sizeof(path) - 1);
    else { path[0] = '/'; path[1] = 0; }

    size_t blen = reqbody ? strlen(reqbody) : 0;
    const char* ex = extra ? extra : "";   /* caller-supplied header lines (each ending \r\n) */
    size_t reqcap = blen + strlen(path) + strlen(host) + 256 + (ctype ? strlen(ctype) : 0) + strlen(ex);
    char* req = (char*)malloc(reqcap);
    int rl;
    if (blen > 0) {
        if (ctype) {
            rl = snprintf(req, reqcap,
                "%s %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: machin/0.8\r\nAccept: */*\r\n%sContent-Type: %s\r\nContent-Length: %zu\r\nConnection: close\r\n\r\n",
                method, path, host, ex, ctype, blen);
        } else {
            /* http_request: caller owns Content-Type (via extra) */
            rl = snprintf(req, reqcap,
                "%s %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: machin/0.8\r\nAccept: */*\r\n%sContent-Length: %zu\r\nConnection: close\r\n\r\n",
                method, path, host, ex, blen);
        }
        memcpy(req + rl, reqbody, blen);
        rl += (int)blen;
    } else {
        rl = snprintf(req, reqcap,
            "%s %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: machin/0.8\r\nAccept: */*\r\n%sConnection: close\r\n\r\n",
            method, path, host, ex);
    }
    size_t rlen;
    char* raw;
    const char* stage = "connect";
    if (is_https) {
        SSL* ssl = mfl_tls_dial_e(host, port, &stage);
        if (!ssl) { R.err = mfl_dup_arena(stage, strlen(stage)); free(req); return R; }
        SSL_write(ssl, req, rl);
        raw = mfl_tls_readall(ssl, &rlen);
        mfl_tls_hangup(ssl);
    } else {
        int fd = mfl_tcp_dial_e(host, port, &stage);
        if (fd < 0) { R.err = mfl_dup_arena(stage, strlen(stage)); free(req); return R; }
        mfl_sock_writeall(fd, req, rl);
        raw = mfl_sock_readall(fd, &rlen);
        close(fd);
    }
    free(req);

    int status = 0;
    { const char* sp = strchr(raw, ' '); if (sp) status = atoi(sp + 1); }
    R.status = status;
    char* hb = strstr(raw, "\r\n\r\n");
    if (!hb) { R.body = mfl_dup_arena(raw, rlen); free(raw); return R; }
    size_t hdrlen = (size_t)(hb - raw);
    char* body = hb + 4;
    size_t bodylen = rlen - (size_t)(body - raw);

    if (redirects > 0 && (status == 301 || status == 302 || status == 303 || status == 307 || status == 308)) {
        char* hdrs = mfl_dup_arena(raw, hdrlen);
        char* loc = strcasestr(hdrs, "\nlocation:");
        if (loc) {
            loc += strlen("\nlocation:");
            while (*loc == ' ' || *loc == '\t') loc++;
            char* e = loc;
            while (*e && *e != '\r' && *e != '\n') e++;
            char locurl[2048];
            size_t ll = (size_t)(e - loc);
            if (ll > sizeof(locurl) - 1) ll = sizeof(locurl) - 1;
            memcpy(locurl, loc, ll);
            locurl[ll] = 0;
            if (strncmp(locurl, "http", 4) == 0) {
                free(raw);
                const char* m = (status == 307 || status == 308) ? method : "GET";
                const char* rb = (status == 307 || status == 308) ? reqbody : NULL;
                return mfl_http_do(m, locurl, rb, ctype, extra, redirects - 1);
            }
        }
    }

    char* hdrs = mfl_dup_arena(raw, hdrlen);
    if (strcasestr(hdrs, "transfer-encoding: chunked") || strcasestr(hdrs, "transfer-encoding:chunked")) {
        size_t dl;
        char* dec = mfl_chunk_decode(body, bodylen, &dl);
        R.body = mfl_dup_arena(dec, dl);
        free(dec);
    } else {
        char* cl = strcasestr(hdrs, "content-length:");
        if (cl) {
            size_t want = (size_t)strtoul(cl + strlen("content-length:"), NULL, 10);
            if (want < bodylen) bodylen = want;
        }
        R.body = mfl_dup_arena(body, bodylen);
    }
    free(raw);
    return R;
}

static char* mfl_https_get(const char* url) {
    if (mfl_rr_mode == 2) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); char* a = mfl_dup_arena(s, L); free(s); return a; }
    char* b = mfl_http_do("GET", url, NULL, NULL, "", 5).body;
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes(b ? b : "", b ? strlen(b) : 0);
    return b;
}
static char* mfl_https_post(const char* url, const char* body) {
    if (mfl_rr_mode == 2) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); char* a = mfl_dup_arena(s, L); free(s); return a; }
    char* b = mfl_http_do("POST", url, body, "application/json", "", 5).body;
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes(b ? b : "", b ? strlen(b) : 0);
    return b;
}
static mfl_http_result mfl_http_get(const char* url) {
    if (mfl_rr_mode == 2) return mfl_rr_pop_http();
    mfl_http_result R = mfl_http_do("GET", url, NULL, NULL, "", 5);
    if (mfl_rr_mode == 1) mfl_rr_log_http(R);
    return R;
}
/* general authenticated request: caller supplies the method, any extra header
   lines (e.g. "Authorization: Bearer x", "Content-Type: application/json") as a
   []string, and a body. Returns (status, body, err) like http_get. */
static mfl_http_result mfl_http_request(const char* method, const char* url, mfl_slice headers, const char* body) {
    if (mfl_rr_mode == 2) return mfl_rr_pop_http();
    size_t tot = 1;
    for (int64_t i = 0; i < headers.len; i++) { char* h = ((char**)headers.data)[i]; if (h) tot += strlen(h) + 2; }
    char* hb = (char*)malloc(tot);
    size_t o = 0;
    for (int64_t i = 0; i < headers.len; i++) {
        char* h = ((char**)headers.data)[i];
        if (h) { size_t l = strlen(h); memcpy(hb + o, h, l); o += l; hb[o++] = '\r'; hb[o++] = '\n'; }
    }
    hb[o] = 0;
    mfl_http_result R = mfl_http_do(method, url, body, NULL, hb, 5);
    if (mfl_rr_mode == 1) mfl_rr_log_http(R);
    free(hb);
    return R;
}
`

// wssRuntime is a WebSocket (RFC 6455) client over TLS, built on tlsCoreRuntime.
// Emitted only when a program calls wss_*. wss_open performs the HTTP/1.1 Upgrade
// handshake; send/recv implement client frame masking, fragmentation reassembly,
// and automatic ping->pong / close handling. The connection is held as an int64
// handle (a pointer), matching how machin carries opaque FFI handles.
const wssRuntime = `
typedef struct { SSL* ssl; } mfl_ws;

/* read exactly n bytes from the TLS stream; 0 ok, -1 on EOF/error */
static int mfl_ssl_read_n(SSL* ssl, unsigned char* buf, size_t n) {
    size_t got = 0;
    while (got < n) {
        int r = SSL_read(ssl, buf + got, (int)(n - got));
        if (r <= 0) return -1;
        got += (size_t)r;
    }
    return 0;
}

static void mfl_b64(const unsigned char* in, int len, char* out) {
    static const char* t = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    int o = 0;
    for (int i = 0; i < len; i += 3) {
        int n = in[i] << 16;
        if (i + 1 < len) n |= in[i+1] << 8;
        if (i + 2 < len) n |= in[i+2];
        out[o++] = t[(n >> 18) & 63];
        out[o++] = t[(n >> 12) & 63];
        out[o++] = (i + 1 < len) ? t[(n >> 6) & 63] : '=';
        out[o++] = (i + 2 < len) ? t[n & 63] : '=';
    }
    out[o] = 0;
}

/* write one masked client frame (opcode, payload). Client frames must be masked. */
static void mfl_ws_frame(mfl_ws* w, int opcode, const unsigned char* payload, size_t len) {
    unsigned char hdr[14];
    int h = 0;
    hdr[h++] = (unsigned char)(0x80 | (opcode & 0x0f));
    if (len < 126) {
        hdr[h++] = (unsigned char)(0x80 | len);
    } else if (len < 65536) {
        hdr[h++] = 0x80 | 126;
        hdr[h++] = (unsigned char)((len >> 8) & 0xff);
        hdr[h++] = (unsigned char)(len & 0xff);
    } else {
        hdr[h++] = 0x80 | 127;
        for (int s = 56; s >= 0; s -= 8) hdr[h++] = (unsigned char)((len >> s) & 0xff);
    }
    unsigned char mask[4];
    RAND_bytes(mask, 4);
    memcpy(hdr + h, mask, 4);
    h += 4;
    unsigned char* buf = (unsigned char*)malloc(h + len);
    memcpy(buf, hdr, h);
    for (size_t k = 0; k < len; k++) buf[h + k] = payload[k] ^ mask[k & 3];
    SSL_write(w->ssl, buf, (int)(h + len));
    free(buf);
}

static int64_t mfl_wss_open_real(const char* url) {
    char host[256] = {0}, path[2048] = {0};
    int port = 443;
    const char* p = url;
    if (strncmp(p, "wss://", 6) == 0) p += 6;
    else if (strncmp(p, "ws://", 5) == 0) return 0; /* TLS only */
    else return 0;
    int i = 0;
    while (*p && *p != '/' && *p != ':' && i < 255) host[i++] = *p++;
    host[i] = 0;
    if (*p == ':') { p++; port = atoi(p); while (*p && *p != '/') p++; }
    if (*p == '/') strncpy(path, p, sizeof(path) - 1);
    else { path[0] = '/'; path[1] = 0; }

    SSL* ssl = mfl_tls_dial(host, port);
    if (!ssl) return 0;

    unsigned char rnd[16];
    RAND_bytes(rnd, 16);
    char key[32];
    mfl_b64(rnd, 16, key);
    char req[4096];
    int rl = snprintf(req, sizeof(req),
        "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nUser-Agent: machin/0.9\r\n\r\n",
        path, host, key);
    SSL_write(ssl, req, rl);

    /* read the handshake response one byte at a time so we stop exactly at the
       blank line and never swallow the first data frame */
    char resp[4096];
    int ro = 0;
    while (ro < (int)sizeof(resp) - 1) {
        int r = SSL_read(ssl, resp + ro, 1);
        if (r <= 0) break;
        ro++;
        if (ro >= 4 && resp[ro-4] == '\r' && resp[ro-3] == '\n' && resp[ro-2] == '\r' && resp[ro-1] == '\n') break;
    }
    resp[ro] = 0;
    if (!strstr(resp, " 101")) { mfl_tls_hangup(ssl); return 0; }

    mfl_ws* w = (mfl_ws*)malloc(sizeof(mfl_ws));
    w->ssl = ssl;
    return (int64_t)(intptr_t)w;
}
/* record/replay: the handshake is external I/O — replay returns the recorded handle
   sentinel (no real connect); record logs whatever the real open returned. */
static int64_t mfl_wss_open(const char* url) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    int64_t r = mfl_wss_open_real(url);
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(r);
    return r;
}

static int64_t mfl_wss_send(int64_t h, const char* msg) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    mfl_ws* w = (mfl_ws*)(intptr_t)h;
    int64_t r = w ? 0 : -1;
    if (w) mfl_ws_frame(w, 0x1, (const unsigned char*)msg, strlen(msg));
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(r);
    return r;
}

/* core: block until a full message; returns a malloc'd buffer (caller frees) and
   its length via *outlen, or NULL on close/error. Replies to pings, ignores
   pongs, reassembles fragments. An empty message returns a non-NULL 1-byte buf. */
static unsigned char* mfl_ws_recv_raw(mfl_ws* w, size_t* outlen) {
    *outlen = 0;
    if (!w) return NULL;
    unsigned char* msg = NULL;
    size_t mlen = 0;
    for (;;) {
        unsigned char hd[2];
        if (mfl_ssl_read_n(w->ssl, hd, 2) < 0) { free(msg); return NULL; }
        int fin = hd[0] & 0x80;
        int opcode = hd[0] & 0x0f;
        int masked = hd[1] & 0x80;
        uint64_t len = hd[1] & 0x7f;
        if (len == 126) {
            unsigned char e[2];
            if (mfl_ssl_read_n(w->ssl, e, 2) < 0) { free(msg); return NULL; }
            len = ((uint64_t)e[0] << 8) | e[1];
        } else if (len == 127) {
            unsigned char e[8];
            if (mfl_ssl_read_n(w->ssl, e, 8) < 0) { free(msg); return NULL; }
            len = 0;
            for (int s = 0; s < 8; s++) len = (len << 8) | e[s];
        }
        unsigned char mk[4] = {0,0,0,0};
        if (masked && mfl_ssl_read_n(w->ssl, mk, 4) < 0) { free(msg); return NULL; }
        unsigned char* pl = (unsigned char*)malloc(len ? len : 1);
        if (len && mfl_ssl_read_n(w->ssl, pl, len) < 0) { free(pl); free(msg); return NULL; }
        if (masked) for (uint64_t k = 0; k < len; k++) pl[k] ^= mk[k & 3];

        if (opcode == 0x9) { mfl_ws_frame(w, 0xA, pl, len); free(pl); continue; } /* ping -> pong */
        if (opcode == 0xA) { free(pl); continue; }                                /* pong */
        if (opcode == 0x8) { mfl_ws_frame(w, 0x8, pl, len); free(pl); free(msg); return NULL; } /* close */

        unsigned char* nm = (unsigned char*)realloc(msg, mlen + len);
        msg = nm;
        if (len) memcpy(msg + mlen, pl, len);
        mlen += len;
        free(pl);
        if (fin) { *outlen = mlen; return msg ? msg : (unsigned char*)calloc(1, 1); }
    }
}

/* text recv: "" on close/error (truncates at an embedded NUL — for binary use wss_recv_bin) */
static char* mfl_wss_recv(int64_t h) {
    if (mfl_rr_mode == 2) { size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L); char* a = mfl_dup_arena(s, L); free(s); return a; }
    size_t n;
    unsigned char* m = mfl_ws_recv_raw((mfl_ws*)(intptr_t)h, &n);
    char* r = m ? mfl_dup_arena((char*)m, n) : mfl_dup_arena("", 0);
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes(m ? (char*)m : "", m ? n : 0);
    free(m);
    return r;
}

/* binary recv: a NUL-safe bytes message; empty bytes on close/error */
static mfl_bytes mfl_wss_recv_bin(int64_t h) {
    if (mfl_rr_mode == 2) {
        size_t L; char* s = mfl_hexdec(mfl_io_pop(), &L);
        mfl_bytes b; b.data = (uint8_t*)mfl_alloc(L ? L : 1); b.len = (int64_t)L;
        if (L) memcpy(b.data, s, L); free(s); return b;
    }
    size_t n = 0;
    unsigned char* m = mfl_ws_recv_raw((mfl_ws*)(intptr_t)h, &n);
    mfl_bytes b; b.len = m ? (int64_t)n : 0; b.data = (uint8_t*)mfl_alloc(b.len ? b.len : 1);
    if (m) { memcpy(b.data, m, n); free(m); }
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes((char*)b.data, (size_t)b.len);
    return b;
}

/* binary send: one masked binary frame (opcode 0x2) carrying the bytes payload */
static int64_t mfl_wss_send_bin(int64_t h, mfl_bytes b) {
    if (mfl_rr_mode == 2) return mfl_rr_io_pop_i64();
    mfl_ws* w = (mfl_ws*)(intptr_t)h;
    int64_t r = w ? 0 : -1;
    if (w) mfl_ws_frame(w, 0x2, b.data, (size_t)b.len);
    if (mfl_rr_mode == 1) mfl_rr_io_log_i64(r);
    return r;
}

/* close: side-effect with a constant return; on replay skip the real hangup (sentinel
   handle). No I/O-queue entry either way, so the queue stays balanced. */
static int64_t mfl_wss_close(int64_t h) {
    if (mfl_rr_mode == 2) return 0;
    mfl_ws* w = (mfl_ws*)(intptr_t)h;
    if (!w) return -1;
    mfl_ws_frame(w, 0x8, NULL, 0);
    mfl_tls_hangup(w->ssl);
    free(w);
    return 0;
}
`

// noiseRuntime is Ken Perlin's improved gradient noise (3D; noise2 is the z=0
// slice), emitted (and linked -lm for floor) only when a program calls noise*.
// Deterministic — a fixed permutation. Range ~[-1, 1]. Layer it (fbm) in MFL.
const noiseRuntime = `#include <math.h>
static int mfl_nperm[512];
static int mfl_nperm_done = 0;
static void mfl_noise_init(void) {
    static const int p[256] = {151,160,137,91,90,15,131,13,201,95,96,53,194,233,7,225,140,36,103,30,69,142,8,99,37,240,21,10,23,190,6,148,247,120,234,75,0,26,197,62,94,252,219,203,117,35,11,32,57,177,33,88,237,149,56,87,174,20,125,136,171,168,68,175,74,165,71,134,139,48,27,166,77,146,158,231,83,111,229,122,60,211,133,230,220,105,92,41,55,46,245,40,244,102,143,54,65,25,63,161,1,216,80,73,209,76,132,187,208,89,18,169,200,196,135,130,116,188,159,86,164,100,109,198,173,186,3,64,52,217,226,250,124,123,5,202,38,147,118,126,255,82,85,212,207,206,59,227,47,16,58,17,182,189,28,42,223,183,170,213,119,248,152,2,44,154,163,70,221,153,101,155,167,43,172,9,129,22,39,253,19,98,108,110,79,113,224,232,178,185,112,104,218,246,97,228,251,34,242,193,238,210,144,12,191,179,162,241,81,51,145,235,249,14,239,107,49,192,214,31,181,199,106,157,184,84,204,176,115,121,50,45,127,4,150,254,138,236,205,93,222,114,67,29,24,72,243,141,128,195,78,66,215,61,156,180};
    for (int i = 0; i < 256; i++) { mfl_nperm[i] = p[i]; mfl_nperm[256 + i] = p[i]; }
    mfl_nperm_done = 1;
}
static double mfl_nfade(double t) { return t * t * t * (t * (t * 6 - 15) + 10); }
static double mfl_nlerp(double t, double a, double b) { return a + t * (b - a); }
static double mfl_ngrad(int hash, double x, double y, double z) {
    int h = hash & 15;
    double u = h < 8 ? x : y;
    double v = h < 4 ? y : (h == 12 || h == 14 ? x : z);
    return ((h & 1) == 0 ? u : -u) + ((h & 2) == 0 ? v : -v);
}
static double mfl_noise3(double x, double y, double z) {
    if (!mfl_nperm_done) mfl_noise_init();
    int X = (int)floor(x) & 255, Y = (int)floor(y) & 255, Z = (int)floor(z) & 255;
    x -= floor(x); y -= floor(y); z -= floor(z);
    double u = mfl_nfade(x), v = mfl_nfade(y), w = mfl_nfade(z);
    int A = mfl_nperm[X] + Y, AA = mfl_nperm[A] + Z, AB = mfl_nperm[A + 1] + Z;
    int B = mfl_nperm[X + 1] + Y, BA = mfl_nperm[B] + Z, BB = mfl_nperm[B + 1] + Z;
    return mfl_nlerp(w, mfl_nlerp(v, mfl_nlerp(u, mfl_ngrad(mfl_nperm[AA], x, y, z), mfl_ngrad(mfl_nperm[BA], x - 1, y, z)),
                                     mfl_nlerp(u, mfl_ngrad(mfl_nperm[AB], x, y - 1, z), mfl_ngrad(mfl_nperm[BB], x - 1, y - 1, z))),
                        mfl_nlerp(v, mfl_nlerp(u, mfl_ngrad(mfl_nperm[AA + 1], x, y, z - 1), mfl_ngrad(mfl_nperm[BA + 1], x - 1, y, z - 1)),
                                     mfl_nlerp(u, mfl_ngrad(mfl_nperm[AB + 1], x, y - 1, z - 1), mfl_ngrad(mfl_nperm[BB + 1], x - 1, y - 1, z - 1))));
}
static double mfl_noise2(double x, double y) { return mfl_noise3(x, y, 0.0); }
`

// mathRuntime is the native floating-point math suite over libm's <math.h>,
// emitted (and linked -lm) only when a program calls a math builtin. Each is a
// thin wrapper named mfl_math_<libm-name> on a double; machin's float is double,
// so signatures line up exactly. _GNU_SOURCE (set in cRuntime) exposes M_PI.
const mathRuntime = `#include <math.h>
static double mfl_math_sin(double x){return sin(x);}
static double mfl_math_cos(double x){return cos(x);}
static double mfl_math_tan(double x){return tan(x);}
static double mfl_math_asin(double x){return asin(x);}
static double mfl_math_acos(double x){return acos(x);}
static double mfl_math_atan(double x){return atan(x);}
static double mfl_math_exp(double x){return exp(x);}
static double mfl_math_log(double x){return log(x);}
static double mfl_math_log2(double x){return log2(x);}
static double mfl_math_log10(double x){return log10(x);}
static double mfl_math_sqrt(double x){return sqrt(x);}
static double mfl_math_cbrt(double x){return cbrt(x);}
static double mfl_math_floor(double x){return floor(x);}
static double mfl_math_ceil(double x){return ceil(x);}
static double mfl_math_round(double x){return round(x);}
static double mfl_math_trunc(double x){return trunc(x);}
static double mfl_math_fabs(double x){return fabs(x);}
static double mfl_math_pow(double a,double b){return pow(a,b);}
static double mfl_math_atan2(double a,double b){return atan2(a,b);}
static double mfl_math_fmod(double a,double b){return fmod(a,b);}
static double mfl_math_hypot(double a,double b){return hypot(a,b);}
static double mfl_math_pi(void){return M_PI;}
`

// regexRuntime is POSIX extended-regex (ERE) support via libc's <regex.h>,
// emitted only when a program calls regex_*. match/find/groups/replace operate
// on the subject first (like the other string builtins). A bad pattern fails
// safe: match=false, find/replace return the input/empty, groups returns [].
const regexRuntime = `#include <regex.h>

static int mfl_regex_match(const char* s, const char* pat) {
    regex_t re;
    if (regcomp(&re, pat, REG_EXTENDED | REG_NOSUB) != 0) return 0;
    int r = regexec(&re, s, 0, NULL, 0);
    regfree(&re);
    return r == 0 ? 1 : 0;
}
static char* mfl_regex_find(const char* s, const char* pat) {
    regex_t re;
    if (regcomp(&re, pat, REG_EXTENDED) != 0) return mfl_dup_arena("", 0);
    regmatch_t m[1];
    char* out = mfl_dup_arena("", 0);
    if (regexec(&re, s, 1, m, 0) == 0 && m[0].rm_so >= 0) {
        out = mfl_dup_arena(s + m[0].rm_so, (size_t)(m[0].rm_eo - m[0].rm_so));
    }
    regfree(&re);
    return out;
}
/* groups: []string of the first match — index 0 is the whole match, 1..n the
   captured subgroups (an unmatched optional group is ""). Empty if no match. */
static mfl_slice mfl_regex_groups(const char* s, const char* pat) {
    mfl_slice out = {0};
    regex_t re;
    if (regcomp(&re, pat, REG_EXTENDED) != 0) return out;
    size_t ng = re.re_nsub + 1;
    regmatch_t* m = (regmatch_t*)malloc(ng * sizeof(regmatch_t));
    if (regexec(&re, s, ng, m, 0) == 0) {
        for (size_t i = 0; i < ng; i++) {
            char* g;
            if (m[i].rm_so >= 0) g = mfl_dup_arena(s + m[i].rm_so, (size_t)(m[i].rm_eo - m[i].rm_so));
            else g = mfl_dup_arena("", 0);
            out = mfl_append(out, &g, sizeof(char*));
        }
    }
    free(m);
    regfree(&re);
    return out;
}
static char* mfl_regex_replace(const char* s, const char* pat, const char* repl) {
    regex_t re;
    if (regcomp(&re, pat, REG_EXTENDED) != 0) return mfl_dup_arena(s, strlen(s));
    size_t cap = strlen(s) + 64, len = 0, rl = strlen(repl);
    char* out = (char*)malloc(cap);
    const char* p = s;
    regmatch_t m[1];
    int eflags = 0;
    while (regexec(&re, p, 1, m, eflags) == 0) {
        size_t so = (size_t)m[0].rm_so, eo = (size_t)m[0].rm_eo;
        while (len + so + rl + 2 >= cap) { cap *= 2; out = (char*)realloc(out, cap); }
        memcpy(out + len, p, so); len += so;
        memcpy(out + len, repl, rl); len += rl;
        if (eo == so) { /* empty match: emit one char to make progress */
            if (p[eo] == 0) { p += eo; break; }
            out[len++] = p[eo];
            p += eo + 1;
        } else {
            p += eo;
        }
        eflags = REG_NOTBOL;
    }
    size_t rest = strlen(p);
    while (len + rest + 1 >= cap) { cap *= 2; out = (char*)realloc(out, cap); }
    memcpy(out + len, p, rest); len += rest;
    out[len] = 0;
    regfree(&re);
    char* r = mfl_dup_arena(out, len);
    free(out);
    return r;
}
`

// sqliteRuntime is a thin SQLite client over libsqlite3, emitted (and linked
// -lsqlite3) only when a program calls sqlite_*. A connection is an int handle
// (the sqlite3* pointer). sqlite_query returns the result set as a JSON array of
// row objects, so it composes with json_get. INTEGER/REAL are unquoted, TEXT is
// JSON-escaped, NULL is null.
const sqliteRuntime = `#include <sqlite3.h>

static int64_t mfl_sqlite_open(const char* path) {
    sqlite3* db = NULL;
    if (sqlite3_open(path, &db) != SQLITE_OK) { sqlite3_close(db); return 0; }
    return (int64_t)(intptr_t)db;
}
static int64_t mfl_sqlite_exec(int64_t h, const char* sql) {
    sqlite3* db = (sqlite3*)(intptr_t)h;
    if (!db) return -1;
    char* err = NULL;
    int r = sqlite3_exec(db, sql, NULL, NULL, &err);
    if (err) sqlite3_free(err);
    return r;
}
static int64_t mfl_sqlite_close(int64_t h) {
    return sqlite3_close((sqlite3*)(intptr_t)h);
}
/* bind a []string of params positionally to ?1, ?2, ... (all as text). */
static void mfl_sqlite_bind(sqlite3_stmt* st, mfl_slice params) {
    for (int64_t i = 0; i < params.len; i++) {
        const char* p = ((char**)params.data)[i];
        sqlite3_bind_text(st, (int)(i + 1), p ? p : "", -1, SQLITE_TRANSIENT);
    }
}
/* step a prepared statement to completion, returning a JSON array of row
   objects; finalizes the statement. */
static char* mfl_sqlite_rows_json(sqlite3_stmt* st) {
    size_t cap = 256, len = 0;
    char* out = (char*)malloc(cap);
    out[len++] = '[';
    int ncol = sqlite3_column_count(st);
    int row = 0;
    while (sqlite3_step(st) == SQLITE_ROW) {
        if (row++) { if (len + 1 >= cap) { cap *= 2; out = realloc(out, cap); } out[len++] = ','; }
        if (len + 1 >= cap) { cap *= 2; out = realloc(out, cap); }
        out[len++] = '{';
        for (int i = 0; i < ncol; i++) {
            char* cell;
            const char* name = sqlite3_column_name(st, i);
            char* jname = mfl_json_str(name ? name : "");
            int t = sqlite3_column_type(st, i);
            if (t == SQLITE_INTEGER || t == SQLITE_FLOAT) {
                const unsigned char* txt = sqlite3_column_text(st, i);
                cell = mfl_dup((const char*)(txt ? txt : (const unsigned char*)"0"));
            } else if (t == SQLITE_NULL) {
                cell = mfl_dup("null");
            } else {
                const unsigned char* txt = sqlite3_column_text(st, i);
                cell = mfl_json_str((const char*)(txt ? txt : (const unsigned char*)""));
            }
            char* piece = mfl_cat(mfl_cat(jname, ":"), cell);
            if (i) piece = mfl_cat(",", piece);
            size_t pl = strlen(piece);
            while (len + pl + 2 >= cap) { cap *= 2; out = realloc(out, cap); }
            memcpy(out + len, piece, pl); len += pl;
        }
        if (len + 1 >= cap) { cap *= 2; out = realloc(out, cap); }
        out[len++] = '}';
    }
    if (len + 1 >= cap) { cap *= 2; out = realloc(out, cap); }
    out[len++] = ']';
    sqlite3_finalize(st);
    char* r = mfl_dup_arena(out, len);
    free(out);
    return r;
}
static char* mfl_sqlite_query(int64_t h, const char* sql) {
    sqlite3* db = (sqlite3*)(intptr_t)h;
    sqlite3_stmt* st = NULL;
    if (!db || sqlite3_prepare_v2(db, sql, -1, &st, NULL) != SQLITE_OK) return mfl_dup_arena("[]", 2);
    return mfl_sqlite_rows_json(st);
}
/* parameterized variants: bind a []string of params to the ? placeholders. */
static char* mfl_sqlite_query_p(int64_t h, const char* sql, mfl_slice params) {
    sqlite3* db = (sqlite3*)(intptr_t)h;
    sqlite3_stmt* st = NULL;
    if (!db || sqlite3_prepare_v2(db, sql, -1, &st, NULL) != SQLITE_OK) return mfl_dup_arena("[]", 2);
    mfl_sqlite_bind(st, params);
    return mfl_sqlite_rows_json(st);
}
static int64_t mfl_sqlite_exec_p(int64_t h, const char* sql, mfl_slice params) {
    sqlite3* db = (sqlite3*)(intptr_t)h;
    sqlite3_stmt* st = NULL;
    if (!db || sqlite3_prepare_v2(db, sql, -1, &st, NULL) != SQLITE_OK) return -1;
    mfl_sqlite_bind(st, params);
    int r = sqlite3_step(st);
    sqlite3_finalize(st);
    return (r == SQLITE_DONE || r == SQLITE_ROW) ? 0 : r;
}
`

// cryptoRuntime wraps OpenSSL libcrypto primitives over the bytes type. Emitted
// (and linked, -lcrypto) only when a program calls a crypto builtin. All inputs
// and outputs are mfl_bytes (arena-allocated); ed25519_verify returns a bool.
const cryptoRuntime = `#include <openssl/evp.h>
#include <openssl/rand.h>
#include <openssl/hmac.h>
#include <openssl/sha.h>
#include <openssl/kdf.h>
#include <openssl/ec.h>
#include <openssl/ecdsa.h>
#include <openssl/obj_mac.h>
#include <string.h>

static mfl_bytes mfl_crypto_buf(int64_t n) {
    mfl_bytes b; b.len = n < 0 ? 0 : n; b.data = (uint8_t*)mfl_alloc(b.len ? b.len : 1); return b;
}
static mfl_bytes mfl_crypto_rand(int64_t n) {
    mfl_bytes b = mfl_crypto_buf(n);
    /* rand is genuinely nondeterministic every run — recording the drawn bytes makes a
       crypto program FAITHFULLY replayable (not best-effort); replay returns them verbatim. */
    if (mfl_rr_mode == 2) { mfl_rr_io_pop_into(b.data, (size_t)b.len); return b; }
    if (b.len > 0) RAND_bytes(b.data, (int)b.len);
    if (mfl_rr_mode == 1) mfl_rr_io_log_bytes((char*)b.data, (size_t)b.len);
    return b;
}
static mfl_bytes mfl_crypto_sha256(mfl_bytes m) {
    mfl_bytes out = mfl_crypto_buf(32);
    SHA256(m.data, (size_t)m.len, out.data);
    return out;
}
static mfl_bytes mfl_crypto_sha1(mfl_bytes m) {
    mfl_bytes out = mfl_crypto_buf(20);
    SHA1(m.data, (size_t)m.len, out.data);
    return out;
}
static mfl_bytes mfl_crypto_hmac256(mfl_bytes key, mfl_bytes msg) {
    mfl_bytes out = mfl_crypto_buf(32);
    unsigned int n = 32;
    HMAC(EVP_sha256(), key.data, (int)key.len, msg.data, (size_t)msg.len, out.data, &n);
    out.len = n;
    return out;
}
static mfl_bytes mfl_crypto_pbkdf2(mfl_bytes pw, mfl_bytes salt, int64_t iter, int64_t dklen) {
    mfl_bytes out = mfl_crypto_buf(dklen);
    if (PKCS5_PBKDF2_HMAC((const char*)pw.data, (int)pw.len, salt.data, (int)salt.len, (int)iter, EVP_sha256(), (int)out.len, out.data) != 1) out.len = 0;
    return out;
}
static mfl_bytes mfl_crypto_hkdf(mfl_bytes ikm, mfl_bytes salt, mfl_bytes info, int64_t length) {
    mfl_bytes out = mfl_crypto_buf(length);
    EVP_PKEY_CTX* c = EVP_PKEY_CTX_new_id(EVP_PKEY_HKDF, NULL);
    if (c) {
        size_t l = (size_t)out.len;
        if (EVP_PKEY_derive_init(c) > 0 &&
            EVP_PKEY_CTX_set_hkdf_md(c, EVP_sha256()) > 0 &&
            EVP_PKEY_CTX_set1_hkdf_salt(c, salt.data, (int)salt.len) > 0 &&
            EVP_PKEY_CTX_set1_hkdf_key(c, ikm.data, (int)ikm.len) > 0 &&
            EVP_PKEY_CTX_add1_hkdf_info(c, info.data, (int)info.len) > 0 &&
            EVP_PKEY_derive(c, out.data, &l) > 0) out.len = (int64_t)l;
        else out.len = 0;
        EVP_PKEY_CTX_free(c);
    }
    return out;
}
static mfl_bytes mfl_crypto_x25519_pub(mfl_bytes priv) {
    mfl_bytes out = mfl_crypto_buf(32); out.len = 0;
    EVP_PKEY* pk = EVP_PKEY_new_raw_private_key(EVP_PKEY_X25519, NULL, priv.data, (size_t)priv.len);
    if (pk) { size_t l = 32; if (EVP_PKEY_get_raw_public_key(pk, out.data, &l) > 0) out.len = (int64_t)l; EVP_PKEY_free(pk); }
    return out;
}
static mfl_bytes mfl_crypto_x25519_shared(mfl_bytes priv, mfl_bytes peer) {
    mfl_bytes out = mfl_crypto_buf(32); out.len = 0;
    EVP_PKEY* sk = EVP_PKEY_new_raw_private_key(EVP_PKEY_X25519, NULL, priv.data, (size_t)priv.len);
    EVP_PKEY* pubk = EVP_PKEY_new_raw_public_key(EVP_PKEY_X25519, NULL, peer.data, (size_t)peer.len);
    if (sk && pubk) {
        EVP_PKEY_CTX* c = EVP_PKEY_CTX_new(sk, NULL);
        if (c && EVP_PKEY_derive_init(c) > 0 && EVP_PKEY_derive_set_peer(c, pubk) > 0) {
            size_t l = 32; if (EVP_PKEY_derive(c, out.data, &l) > 0) out.len = (int64_t)l;
        }
        if (c) EVP_PKEY_CTX_free(c);
    }
    if (sk) EVP_PKEY_free(sk);
    if (pubk) EVP_PKEY_free(pubk);
    return out;
}
static mfl_bytes mfl_crypto_ed25519_pub(mfl_bytes seed) {
    mfl_bytes out = mfl_crypto_buf(32); out.len = 0;
    EVP_PKEY* pk = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, seed.data, (size_t)seed.len);
    if (pk) { size_t l = 32; if (EVP_PKEY_get_raw_public_key(pk, out.data, &l) > 0) out.len = (int64_t)l; EVP_PKEY_free(pk); }
    return out;
}
static mfl_bytes mfl_crypto_ed25519_sign(mfl_bytes seed, mfl_bytes msg) {
    mfl_bytes out = mfl_crypto_buf(64); out.len = 0;
    EVP_PKEY* pk = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, seed.data, (size_t)seed.len);
    if (pk) {
        EVP_MD_CTX* c = EVP_MD_CTX_new();
        if (c && EVP_DigestSignInit(c, NULL, NULL, NULL, pk) > 0) {
            size_t l = 64; if (EVP_DigestSign(c, out.data, &l, msg.data, (size_t)msg.len) > 0) out.len = (int64_t)l;
        }
        if (c) EVP_MD_CTX_free(c);
        EVP_PKEY_free(pk);
    }
    return out;
}
static int mfl_crypto_ed25519_verify(mfl_bytes pub, mfl_bytes msg, mfl_bytes sig) {
    int ok = 0;
    EVP_PKEY* pk = EVP_PKEY_new_raw_public_key(EVP_PKEY_ED25519, NULL, pub.data, (size_t)pub.len);
    if (pk) {
        EVP_MD_CTX* c = EVP_MD_CTX_new();
        if (c && EVP_DigestVerifyInit(c, NULL, NULL, NULL, pk) > 0)
            ok = EVP_DigestVerify(c, sig.data, (size_t)sig.len, msg.data, (size_t)msg.len) == 1;
        if (c) EVP_MD_CTX_free(c);
        EVP_PKEY_free(pk);
    }
    return ok;
}
static mfl_bytes mfl_crypto_aes_gcm_enc(mfl_bytes key, mfl_bytes iv, mfl_bytes pt, mfl_bytes aad) {
    mfl_bytes out = mfl_crypto_buf(pt.len + 16); out.len = 0;
    EVP_CIPHER_CTX* c = EVP_CIPHER_CTX_new();
    const EVP_CIPHER* ciph = (key.len == 16) ? EVP_aes_128_gcm() : EVP_aes_256_gcm();
    if (c && EVP_EncryptInit_ex(c, ciph, NULL, NULL, NULL) > 0 &&
        EVP_CIPHER_CTX_ctrl(c, EVP_CTRL_GCM_SET_IVLEN, (int)iv.len, NULL) > 0 &&
        EVP_EncryptInit_ex(c, NULL, NULL, key.data, iv.data) > 0) {
        int l = 0, t = 0;
        if (aad.len > 0) EVP_EncryptUpdate(c, NULL, &l, aad.data, (int)aad.len);
        EVP_EncryptUpdate(c, out.data, &l, pt.data, (int)pt.len); t = l;
        EVP_EncryptFinal_ex(c, out.data + t, &l); t += l;
        EVP_CIPHER_CTX_ctrl(c, EVP_CTRL_GCM_GET_TAG, 16, out.data + t);
        out.len = t + 16;
    }
    if (c) EVP_CIPHER_CTX_free(c);
    return out;
}
static mfl_bytes mfl_crypto_aes_gcm_dec(mfl_bytes key, mfl_bytes iv, mfl_bytes ct, mfl_bytes aad) {
    mfl_bytes out = mfl_crypto_buf(ct.len > 16 ? ct.len - 16 : 1); out.len = 0;
    if (ct.len < 16) return out;
    int64_t ctlen = ct.len - 16;
    EVP_CIPHER_CTX* c = EVP_CIPHER_CTX_new();
    const EVP_CIPHER* ciph = (key.len == 16) ? EVP_aes_128_gcm() : EVP_aes_256_gcm();
    if (c && EVP_DecryptInit_ex(c, ciph, NULL, NULL, NULL) > 0 &&
        EVP_CIPHER_CTX_ctrl(c, EVP_CTRL_GCM_SET_IVLEN, (int)iv.len, NULL) > 0 &&
        EVP_DecryptInit_ex(c, NULL, NULL, key.data, iv.data) > 0) {
        int l = 0, t = 0;
        if (aad.len > 0) EVP_DecryptUpdate(c, NULL, &l, aad.data, (int)aad.len);
        EVP_DecryptUpdate(c, out.data, &l, ct.data, (int)ctlen); t = l;
        EVP_CIPHER_CTX_ctrl(c, EVP_CTRL_GCM_SET_TAG, 16, ct.data + ctlen);
        if (EVP_DecryptFinal_ex(c, out.data + t, &l) > 0) out.len = t + l;  /* else stays 0: auth fail */
    }
    if (c) EVP_CIPHER_CTX_free(c);
    return out;
}
static mfl_bytes mfl_crypto_aes_cbc_enc(mfl_bytes key, mfl_bytes iv, mfl_bytes pt) {
    mfl_bytes out = mfl_crypto_buf(pt.len + 16); out.len = 0;
    EVP_CIPHER_CTX* c = EVP_CIPHER_CTX_new();
    const EVP_CIPHER* ciph = (key.len == 16) ? EVP_aes_128_cbc() : EVP_aes_256_cbc();
    if (c && EVP_EncryptInit_ex(c, ciph, NULL, key.data, iv.data) > 0) {
        int l = 0, t = 0;
        EVP_EncryptUpdate(c, out.data, &l, pt.data, (int)pt.len); t = l;
        EVP_EncryptFinal_ex(c, out.data + t, &l); out.len = t + l;
    }
    if (c) EVP_CIPHER_CTX_free(c);
    return out;
}
static mfl_bytes mfl_crypto_aes_cbc_dec(mfl_bytes key, mfl_bytes iv, mfl_bytes ct) {
    mfl_bytes out = mfl_crypto_buf(ct.len ? ct.len : 1); out.len = 0;
    EVP_CIPHER_CTX* c = EVP_CIPHER_CTX_new();
    const EVP_CIPHER* ciph = (key.len == 16) ? EVP_aes_128_cbc() : EVP_aes_256_cbc();
    if (c && EVP_DecryptInit_ex(c, ciph, NULL, key.data, iv.data) > 0) {
        int l = 0, t = 0;
        EVP_DecryptUpdate(c, out.data, &l, ct.data, (int)ct.len); t = l;
        if (EVP_DecryptFinal_ex(c, out.data + t, &l) > 0) out.len = t + l;  /* else 0: bad pad */
    }
    if (c) EVP_CIPHER_CTX_free(c);
    return out;
}

/* Keccak-256 (Ethereum's hash; NOT the NIST SHA3-256 final standard — the
   sponge padding domain byte differs: 0x01 here vs SHA3's 0x06). Compact
   Keccak-f[1600] permutation + sponge construction, little-endian hosts only
   (x86_64/arm64 — the only targets machin builds for). */
static const uint64_t mfl_keccakf_rndc[24] = {
    0x0000000000000001ULL, 0x0000000000008082ULL, 0x800000000000808aULL,
    0x8000000080008000ULL, 0x000000000000808bULL, 0x0000000080000001ULL,
    0x8000000080008081ULL, 0x8000000000008009ULL, 0x000000000000008aULL,
    0x0000000000000088ULL, 0x0000000080008009ULL, 0x000000008000000aULL,
    0x000000008000808bULL, 0x800000000000008bULL, 0x8000000000008089ULL,
    0x8000000000008003ULL, 0x8000000000008002ULL, 0x8000000000000080ULL,
    0x000000000000800aULL, 0x800000008000000aULL, 0x8000000080008081ULL,
    0x8000000000008080ULL, 0x0000000080000001ULL, 0x8000000080008008ULL
};
static const int mfl_keccakf_rotc[24] = {
    1, 3, 6, 10, 15, 21, 28, 36, 45, 55, 2, 14,
    27, 41, 56, 8, 25, 43, 62, 18, 39, 61, 20, 44
};
static const int mfl_keccakf_piln[24] = {
    10, 7, 11, 17, 18, 3, 5, 16, 8, 21, 24, 4,
    15, 23, 19, 13, 12, 2, 20, 14, 22, 9, 6, 1
};
static uint64_t mfl_rotl64(uint64_t x, int y) { return (x << y) | (x >> (64 - y)); }
static void mfl_keccakf(uint64_t st[25]) {
    int i, j, r;
    uint64_t t, bc[5];
    for (r = 0; r < 24; r++) {
        for (i = 0; i < 5; i++) bc[i] = st[i] ^ st[i+5] ^ st[i+10] ^ st[i+15] ^ st[i+20];
        for (i = 0; i < 5; i++) {
            t = bc[(i+4)%5] ^ mfl_rotl64(bc[(i+1)%5], 1);
            for (j = 0; j < 25; j += 5) st[j+i] ^= t;
        }
        t = st[1];
        for (i = 0; i < 24; i++) {
            j = mfl_keccakf_piln[i];
            bc[0] = st[j];
            st[j] = mfl_rotl64(t, mfl_keccakf_rotc[i]);
            t = bc[0];
        }
        for (j = 0; j < 25; j += 5) {
            for (i = 0; i < 5; i++) bc[i] = st[j+i];
            for (i = 0; i < 5; i++) st[j+i] ^= (~bc[(i+1)%5]) & bc[(i+2)%5];
        }
        st[0] ^= mfl_keccakf_rndc[r];
    }
}
static void mfl_keccak256_raw(const uint8_t* in, size_t inlen, uint8_t out[32]) {
    uint64_t st[25];
    const size_t rate = 136; /* 1088-bit rate for a 256-bit Keccak (512-bit capacity) */
    uint8_t* stb = (uint8_t*)st;
    size_t i;
    memset(st, 0, sizeof(st));
    while (inlen >= rate) {
        for (i = 0; i < rate; i++) stb[i] ^= in[i];
        mfl_keccakf(st);
        in += rate; inlen -= rate;
    }
    {
        uint8_t tmp[136];
        memset(tmp, 0, rate);
        memcpy(tmp, in, inlen);
        tmp[inlen] |= 0x01;      /* Keccak domain byte (SHA3 final uses 0x06) */
        tmp[rate - 1] |= 0x80;   /* pad10*1 end bit; ORs into the same byte when inlen==rate-1 */
        for (i = 0; i < rate; i++) stb[i] ^= tmp[i];
        mfl_keccakf(st);
    }
    memcpy(out, stb, 32);
}
static mfl_bytes mfl_crypto_keccak256(mfl_bytes m) {
    mfl_bytes out = mfl_crypto_buf(32);
    mfl_keccak256_raw(m.data, (size_t)m.len, out.data);
    return out;
}

/* secp256k1 (Ethereum signing) over OpenSSL's generic EC API (NID_secp256k1).
   OpenSSL has no recoverable-ECDSA entry point, so the recovery id (v) is
   derived by trying both y-parities of R and matching the resulting candidate
   public key against the real one — the standard technique for signers that
   don't link a dedicated secp256k1 library. Signatures are EIP-2 canonical
   (low-S); v is returned as 27/28 (ecrecover convention). */
static EC_POINT* mfl_secp256k1_recover_point(const EC_GROUP* group, BN_CTX* ctx, const BIGNUM* order,
                                              const BIGNUM* r, const BIGNUM* s, const uint8_t* hash32, int recId) {
    if (BN_is_zero(r) || BN_cmp(r, order) >= 0 || BN_is_zero(s) || BN_cmp(s, order) >= 0) return NULL;
    EC_POINT* R = EC_POINT_new(group);
    if (!R) return NULL;
    if (EC_POINT_set_compressed_coordinates(group, R, r, recId & 1, ctx) != 1 ||
        EC_POINT_is_on_curve(group, R, ctx) != 1) {
        EC_POINT_free(R);
        return NULL;
    }
    BIGNUM* z = BN_bin2bn(hash32, 32, NULL);
    BIGNUM* rInv = z ? BN_mod_inverse(NULL, r, order, ctx) : NULL;
    BIGNUM* tmp = BN_new();
    BIGNUM* zero = BN_new();
    BIGNUM* u1 = BN_new();
    BIGNUM* u2 = BN_new();
    EC_POINT* Q = EC_POINT_new(group);
    int ok = 0;
    if (z && rInv && tmp && zero && u1 && u2 && Q) {
        BN_zero(zero);
        ok = BN_mod_mul(tmp, z, rInv, order, ctx) == 1 &&      /* tmp = z*r^-1 mod n      */
             BN_mod_sub(u1, zero, tmp, order, ctx) == 1 &&     /* u1  = -z*r^-1 mod n     */
             BN_mod_mul(u2, s, rInv, order, ctx) == 1 &&       /* u2  = s*r^-1 mod n      */
             EC_POINT_mul(group, Q, u1, R, u2, ctx) == 1;      /* Q   = u1*G + u2*R       */
    }
    EC_POINT_free(R);
    if (z) BN_free(z);
    if (rInv) BN_free(rInv);
    if (tmp) BN_free(tmp);
    if (zero) BN_free(zero);
    if (u1) BN_free(u1);
    if (u2) BN_free(u2);
    if (!ok) { if (Q) EC_POINT_free(Q); return NULL; }
    return Q;
}
static mfl_bytes mfl_crypto_secp256k1_pubkey(mfl_bytes priv) {
    mfl_bytes out = mfl_crypto_buf(65); out.len = 0;
    if (priv.len != 32) return out;
    EC_GROUP* group = EC_GROUP_new_by_curve_name(NID_secp256k1);
    BN_CTX* ctx = BN_CTX_new();
    BIGNUM* d = BN_bin2bn(priv.data, 32, NULL);
    EC_POINT* Q = group ? EC_POINT_new(group) : NULL;
    if (group && ctx && d && Q && EC_POINT_mul(group, Q, d, NULL, NULL, ctx) == 1) {
        size_t n = EC_POINT_point2oct(group, Q, POINT_CONVERSION_UNCOMPRESSED, out.data, 65, ctx);
        if (n == 65) out.len = 65;
    }
    if (Q) EC_POINT_free(Q);
    if (d) BN_free(d);
    if (ctx) BN_CTX_free(ctx);
    if (group) EC_GROUP_free(group);
    return out;
}
static mfl_bytes mfl_crypto_secp256k1_sign_recoverable(mfl_bytes priv, mfl_bytes hash) {
    mfl_bytes out = mfl_crypto_buf(65); out.len = 0;
    if (priv.len != 32 || hash.len != 32) return out;
    EC_GROUP* group = EC_GROUP_new_by_curve_name(NID_secp256k1);
    BN_CTX* ctx = BN_CTX_new();
    EC_KEY* eckey = EC_KEY_new();
    BIGNUM* d = BN_bin2bn(priv.data, 32, NULL);
    BIGNUM* order = BN_new();
    EC_POINT* Qreal = group ? EC_POINT_new(group) : NULL;
    ECDSA_SIG* sig = NULL;

    if (group && ctx && eckey && d && order && Qreal &&
        EC_KEY_set_group(eckey, group) == 1 &&
        EC_KEY_set_private_key(eckey, d) == 1 &&
        EC_GROUP_get_order(group, order, ctx) == 1 &&
        EC_POINT_mul(group, Qreal, d, NULL, NULL, ctx) == 1 &&
        EC_KEY_set_public_key(eckey, Qreal) == 1) {
        sig = ECDSA_do_sign(hash.data, 32, eckey);
    }
    if (sig) {
        const BIGNUM *r0, *s0;
        ECDSA_SIG_get0(sig, &r0, &s0);
        BIGNUM* r = BN_dup(r0);
        BIGNUM* s = BN_dup(s0);
        BIGNUM* half = BN_new();
        if (r && s && half) {
            BN_rshift1(half, order);
            if (BN_cmp(s, half) > 0) BN_sub(s, order, s); /* EIP-2 canonical low-S */
            EC_POINT* Qmatch = NULL;
            int cand;
            for (cand = 0; cand < 2 && !Qmatch; cand++) {
                EC_POINT* Qtry = mfl_secp256k1_recover_point(group, ctx, order, r, s, hash.data, cand);
                if (!Qtry) continue;
                if (EC_POINT_cmp(group, Qtry, Qreal, ctx) == 0) {
                    Qmatch = Qtry;
                    if (BN_bn2binpad(r, out.data, 32) == 32 && BN_bn2binpad(s, out.data + 32, 32) == 32) {
                        out.data[64] = (uint8_t)(27 + cand);
                        out.len = 65;
                    }
                } else {
                    EC_POINT_free(Qtry);
                }
            }
            if (Qmatch) EC_POINT_free(Qmatch);
        }
        if (half) BN_free(half);
        if (r) BN_free(r);
        if (s) BN_free(s);
        ECDSA_SIG_free(sig);
    }
    if (Qreal) EC_POINT_free(Qreal);
    if (order) BN_free(order);
    if (d) BN_free(d);
    if (eckey) EC_KEY_free(eckey);
    if (ctx) BN_CTX_free(ctx);
    if (group) EC_GROUP_free(group);
    return out;
}
static mfl_bytes mfl_crypto_secp256k1_recover(mfl_bytes hash, mfl_bytes sig) {
    mfl_bytes out = mfl_crypto_buf(65); out.len = 0;
    if (hash.len != 32 || sig.len != 65) return out;
    int v = sig.data[64];
    int recId = (v == 27 || v == 28) ? v - 27 : v;
    if (recId != 0 && recId != 1) return out;
    EC_GROUP* group = EC_GROUP_new_by_curve_name(NID_secp256k1);
    BN_CTX* ctx = BN_CTX_new();
    BIGNUM* order = BN_new();
    BIGNUM* r = BN_bin2bn(sig.data, 32, NULL);
    BIGNUM* s = BN_bin2bn(sig.data + 32, 32, NULL);
    if (group && ctx && order && r && s && EC_GROUP_get_order(group, order, ctx) == 1) {
        EC_POINT* Q = mfl_secp256k1_recover_point(group, ctx, order, r, s, hash.data, recId);
        if (Q) {
            size_t n = EC_POINT_point2oct(group, Q, POINT_CONVERSION_UNCOMPRESSED, out.data, 65, ctx);
            if (n == 65) out.len = 65;
            EC_POINT_free(Q);
        }
    }
    if (r) BN_free(r);
    if (s) BN_free(s);
    if (order) BN_free(order);
    if (ctx) BN_CTX_free(ctx);
    if (group) EC_GROUP_free(group);
    return out;
}
`

// xeddsaRuntime implements XEdDSA (Curve25519 signatures, the scheme Signal /
// WhatsApp use for identity/device signatures) over libsodium's Ed25519 group &
// scalar ops, OpenSSL SHA-512, and TweetNaCl's public-domain field arithmetic
// (for the Montgomery->Edwards conversion in verify). Emitted and linked
// (-lsodium -lcrypto) only when a program calls xeddsa_*. Matches libsignal's
// ecc/SignCurve25519.go exactly: diversifier 0xFE||0xFF*31, sign bit in sig[63].
const xeddsaRuntime = `#include <openssl/evp.h>

/* libsodium (headers may be absent; declare the symbols we link against) */
int crypto_core_ed25519_scalar_reduce(unsigned char*, const unsigned char*);
int crypto_core_ed25519_scalar_mul(unsigned char*, const unsigned char*, const unsigned char*);
int crypto_core_ed25519_scalar_add(unsigned char*, const unsigned char*, const unsigned char*);
int crypto_scalarmult_ed25519_base_noclamp(unsigned char*, const unsigned char*);
int crypto_sign_ed25519_verify_detached(const unsigned char*, const unsigned char*, unsigned long long, const unsigned char*);

/* TweetNaCl field arithmetic (public domain) for the Montgomery->Edwards map */
typedef long long mflx_i64; typedef mflx_i64 mflx_gf[16];
static const mflx_gf mflx_gf1 = {1};
static void mflx_unpack(mflx_gf o, const unsigned char* n) { int i; for (i = 0; i < 16; i++) o[i] = n[2*i] + ((mflx_i64)n[2*i+1] << 8); o[15] &= 0x7fff; }
static void mflx_car(mflx_gf o) { int i; mflx_i64 c; for (i = 0; i < 16; i++) { o[i] += (1LL<<16); c = o[i]>>16; o[(i+1)*(i<15)] += c - 1 + 37*(c-1)*(i==15); o[i] -= c<<16; } }
static void mflx_sel(mflx_gf p, mflx_gf q, int b) { mflx_i64 t, i, c = ~(b-1); for (i = 0; i < 16; i++) { t = c & (p[i]^q[i]); p[i] ^= t; q[i] ^= t; } }
static void mflx_pack(unsigned char* o, const mflx_gf n) { int i, j, b; mflx_gf m, t; for (i = 0; i < 16; i++) t[i] = n[i]; mflx_car(t); mflx_car(t); mflx_car(t); for (j = 0; j < 2; j++) { m[0] = t[0]-0xffed; for (i = 1; i < 15; i++) { m[i] = t[i]-0xffff-((m[i-1]>>16)&1); m[i-1] &= 0xffff; } m[15] = t[15]-0x7fff-((m[14]>>16)&1); b = (m[15]>>16)&1; m[14] &= 0xffff; mflx_sel(t, m, 1-b); } for (i = 0; i < 16; i++) { o[2*i] = t[i]&0xff; o[2*i+1] = t[i]>>8; } }
static void mflx_A(mflx_gf o, const mflx_gf a, const mflx_gf b) { int i; for (i = 0; i < 16; i++) o[i] = a[i]+b[i]; }
static void mflx_Z(mflx_gf o, const mflx_gf a, const mflx_gf b) { int i; for (i = 0; i < 16; i++) o[i] = a[i]-b[i]; }
static void mflx_M(mflx_gf o, const mflx_gf a, const mflx_gf b) { mflx_i64 i, j, t[31]; for (i = 0; i < 31; i++) t[i] = 0; for (i = 0; i < 16; i++) for (j = 0; j < 16; j++) t[i+j] += a[i]*b[j]; for (i = 0; i < 15; i++) t[i] += 38*t[i+16]; for (i = 0; i < 16; i++) o[i] = t[i]; mflx_car(o); mflx_car(o); }
static void mflx_inv(mflx_gf o, const mflx_gf in) { mflx_gf c; int a; for (a = 0; a < 16; a++) c[a] = in[a]; for (a = 253; a >= 0; a--) { mflx_M(c, c, c); if (a != 2 && a != 4) mflx_M(c, c, in); } for (a = 0; a < 16; a++) o[a] = c[a]; }
static void mflx_mont_to_ed(unsigned char* aed, const unsigned char* mpub, unsigned char signbit) {
    unsigned char p[32]; memcpy(p, mpub, 32); p[31] &= 0x7f;
    mflx_gf mx, mxm1, mxp1, iv, edy; mflx_unpack(mx, p);
    mflx_Z(mxm1, mx, mflx_gf1); mflx_A(mxp1, mx, mflx_gf1); mflx_inv(iv, mxp1); mflx_M(edy, mxm1, iv);
    mflx_pack(aed, edy); aed[31] |= (signbit & 0x80);
}

static void mflx_sha512(const unsigned char* a, size_t al, const unsigned char* b, size_t bl, const unsigned char* c, size_t cl, const unsigned char* d, size_t dl, unsigned char* out) {
    size_t n = al + bl + cl + dl;
    unsigned char* buf = (unsigned char*)malloc(n ? n : 1);
    size_t o = 0;
    if (al) { memcpy(buf+o, a, al); o += al; }
    if (bl) { memcpy(buf+o, b, bl); o += bl; }
    if (cl) { memcpy(buf+o, c, cl); o += cl; }
    if (dl) { memcpy(buf+o, d, dl); o += dl; }
    unsigned int ml;
    EVP_Digest(buf, n, out, &ml, EVP_sha512(), NULL);
    free(buf);
}

/* sign: priv 32 bytes, random 64 bytes -> 64-byte signature (R||s, A sign bit in [63]) */
static mfl_bytes mfl_xeddsa_sign(mfl_bytes priv, mfl_bytes msg, mfl_bytes rnd) {
    mfl_bytes sig; sig.len = 64; sig.data = (uint8_t*)mfl_alloc(64);
    memset(sig.data, 0, 64);
    if (priv.len < 32 || rnd.len < 64) return sig;
    unsigned char a[32], a64[64], Aed[32], r[32], rh[64], R[32], h[32], hh[64], s[32], tmp[32], div[32];
    memset(a64, 0, 64); memcpy(a, priv.data, 32); a[0] &= 248; a[31] &= 127; a[31] |= 64; memcpy(a64, a, 32);
    crypto_core_ed25519_scalar_reduce(a, a64);
    crypto_scalarmult_ed25519_base_noclamp(Aed, a);
    div[0] = 0xFE; memset(div+1, 0xFF, 31);
    mflx_sha512(div, 32, priv.data, 32, msg.data, (size_t)msg.len, rnd.data, 64, rh);
    crypto_core_ed25519_scalar_reduce(r, rh);
    crypto_scalarmult_ed25519_base_noclamp(R, r);
    mflx_sha512(R, 32, Aed, 32, msg.data, (size_t)msg.len, NULL, 0, hh);
    crypto_core_ed25519_scalar_reduce(h, hh);
    crypto_core_ed25519_scalar_mul(tmp, h, a);
    crypto_core_ed25519_scalar_add(s, tmp, r);
    memcpy(sig.data, R, 32); memcpy(sig.data+32, s, 32); sig.data[63] |= Aed[31] & 0x80;
    return sig;
}

/* verify: pub 32-byte Curve25519 key, sig 64 bytes -> 1 ok / 0 bad */
static int mfl_xeddsa_verify(mfl_bytes pub, mfl_bytes msg, mfl_bytes sig) {
    if (pub.len < 32 || sig.len < 64) return 0;
    unsigned char Aed[32], s2[64];
    mflx_mont_to_ed(Aed, pub.data, sig.data[63]);
    memcpy(s2, sig.data, 64); s2[63] &= 0x7F;
    return crypto_sign_ed25519_verify_detached(s2, msg.data, (unsigned long long)msg.len, Aed) == 0 ? 1 : 0;
}
`

// isFFIScalar reports whether t is an FFI scalar type name (vs a cstruct name).
func isFFIScalar(t string) bool {
	switch t {
	case "int", "float", "bool", "string", "ptr",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64":
		return true
	}
	return false
}

// isFFINumeric reports whether t is a numeric scalar usable as a cstruct field
// (excludes string and the opaque ptr, which aren't laid out as plain numbers).
func isFFINumeric(t string) bool {
	return isFFIScalar(t) && t != "string" && t != "ptr"
}

// ffiCType maps an FFI scalar type name to its C type (for headerless prototypes
// and struct-field casts). Returns "void" for "" or anything non-scalar.
func ffiCType(t string) string {
	switch t {
	case "ptr":
		return "void*"
	case "int", "i64":
		return "int64_t"
	case "i32":
		return "int32_t"
	case "i16":
		return "int16_t"
	case "i8":
		return "int8_t"
	case "u64":
		return "uint64_t"
	case "u32":
		return "uint32_t"
	case "u16":
		return "uint16_t"
	case "u8":
		return "uint8_t"
	case "float", "f64":
		return "double"
	case "f32":
		return "float"
	case "bool":
		return "int"
	case "string":
		return "const char*"
	}
	return "void"
}

// externCType is the C type used in a foreign prototype: a scalar's C type, a
// callback's function-pointer type, the cstruct's own C name, or void.
func externCType(t string) string {
	if t == "" {
		return "void"
	}
	if isCallbackType(t) {
		return callbackCType(t)
	}
	if isFFIScalar(t) {
		return ffiCType(t)
	}
	return t // a cstruct: its C struct name
}

// isCallbackType reports whether t is a "cb(t1,t2)ret" callback parameter type
// (Phase 4a of #305: a captureless MFL function passed as a raw C fn pointer).
func isCallbackType(t string) bool { return strings.HasPrefix(t, "cb(") }

// parseCallbackType splits a "cb(t1,t2)ret" encoding into its parameter types
// and return type (ret == "" means void).
func parseCallbackType(t string) (params []string, ret string) {
	body := strings.TrimPrefix(t, "cb(")
	close := strings.Index(body, ")")
	inner := body[:close]
	ret = body[close+1:]
	if inner != "" {
		params = strings.Split(inner, ",")
	}
	return params, ret
}

// callbackCType renders a callback type as a C function-pointer type, e.g.
// "cb(int)" -> "void (*)(int64_t)".
func callbackCType(t string) string {
	params, ret := parseCallbackType(t)
	ps := make([]string, len(params))
	for i, p := range params {
		ps[i] = ffiCType(p)
	}
	plist := strings.Join(ps, ", ")
	if plist == "" {
		plist = "void"
	}
	return fmt.Sprintf("%s (*)(%s)", ffiCType(ret), plist)
}

// ffiMFLType maps an FFI scalar type to the MFL type name used for the synthesized
// struct field: every integer width is MFL int; f32/f64/float are float.
func ffiMFLType(t string) string {
	switch t {
	case "ptr":
		return "int" // an opaque handle, held as an int
	case "f32", "f64", "float":
		return "float"
	case "bool":
		return "bool"
	case "string":
		return "string"
	}
	return "int"
}

func cType(k Kind) string {
	switch k {
	case KInt:
		return "int64_t"
	case KFloat:
		return "double"
	case KBool:
		return "int"
	case KString:
		return "char*"
	case KBytes:
		return "mfl_bytes"
	case KVoid:
		return "void"
	case KSlice:
		return "mfl_slice"
	case KChan:
		return "mfl_chan*"
	case KMap:
		return "mfl_map*"
	case KFunc:
		return "mfl_closure"
	}
	return "int64_t"
}

func cZero(k Kind) string {
	switch k {
	case KFloat:
		return "0.0"
	case KString:
		return "\"\"" // the zero value of a string is "", not NULL
	case KSlice, KStruct, KFunc, KBytes:
		return "{0}"
	default:
		return "0"
	}
}

func (g *cgen) program(p *Program) (string, error) {
	// record package-global names so varRef renders them as mfl_g_<name> (this must
	// be set before any function body is emitted, since bodies may reference them).
	g.globals = map[string]bool{}
	for _, name := range g.c.GlobalOrder() {
		g.globals[name] = true
	}
	// emit one function body per instance (monomorphization); this also fills
	// g.tramp via any go statements.
	for _, inst := range g.c.Reps() {
		if err := g.function(inst); err != nil {
			return "", err
		}
	}
	var out strings.Builder
	// bodyOnly (the Stage-4 codegen oracle): skip every static runtime block and emit
	// only the program-specific C (externs, structs, functions, main). Both the Go and
	// MFL codegens produce this identically, so diffing it verifies the emission logic
	// without embedding the ~2000-line runtime prelude in MFL.
	if !g.bodyOnly {
		out.WriteString(cRuntime)
		out.WriteByte('\n')
		// POSIX socket + tty runtimes: always present for the native target; for the
		// wasm target emitted only when actually used, so a browser app pulls in no
		// socket/termios symbols (which wasi-libc does not fully provide).
		if !g.wasm() || g.usesNet {
			out.WriteString(netRuntime)
			out.WriteByte('\n')
		}
		if !g.wasm() || g.usesTTY {
			out.WriteString(ttyRuntime)
			out.WriteByte('\n')
		}
		if g.usesTLS || g.usesWSS {
			out.WriteString(tlsCoreRuntime)
			out.WriteByte('\n')
		}
		if g.usesTLS {
			out.WriteString(tlsRuntime)
			out.WriteByte('\n')
		}
		if g.usesWSS {
			out.WriteString(wssRuntime)
			out.WriteByte('\n')
		}
		if g.usesMath {
			out.WriteString(mathRuntime)
			out.WriteByte('\n')
		}
		if g.usesNoise {
			out.WriteString(noiseRuntime)
			out.WriteByte('\n')
		}
		if g.usesRegex {
			out.WriteString(regexRuntime)
			out.WriteByte('\n')
		}
		if g.usesSQLite {
			out.WriteString(sqliteRuntime)
			out.WriteByte('\n')
		}
		if g.usesCrypto {
			out.WriteString(cryptoRuntime)
			out.WriteByte('\n')
		}
		if g.usesXEdDSA {
			out.WriteString(xeddsaRuntime)
			out.WriteByte('\n')
		}
	}
	// foreign (extern) declarations. With a header, its prototypes + C structs are
	// in scope. Without one, emit C struct typedefs and function prototypes from
	// the declared signatures.
	for _, ed := range p.Externs {
		if ed.Header != "" {
			fmt.Fprintf(&out, "#include <%s>\n", ed.Header)
			continue
		}
		for _, cs := range ed.Structs {
			fmt.Fprintf(&out, "typedef struct {")
			for _, f := range cs.Fields {
				fmt.Fprintf(&out, " %s %s;", ffiCType(f.CType), f.Name)
			}
			fmt.Fprintf(&out, " } %s;\n", cs.Name)
		}
		for _, ef := range ed.Funcs {
			params := make([]string, len(ef.Params))
			for i, pt := range ef.Params {
				params[i] = externCType(pt)
			}
			ps := strings.Join(params, ", ")
			if ps == "" {
				ps = "void"
			}
			// Under the wasm target a headerless extern is a host (JS) function: tag
			// it as a wasm import so the linker leaves it undefined for the host to
			// supply. The `extern "<lib>"` name is the import module (default "env").
			var attr string
			if g.wasm() && ed.Header == "" {
				mod := ed.Lib
				if mod == "" {
					mod = "env"
				}
				attr = fmt.Sprintf("__attribute__((import_module(%q), import_name(%q))) ", mod, ef.Name)
			}
			fmt.Fprintf(&out, "%sextern %s %s(%s);\n", attr, externCType(ef.Ret), ef.Name, ps)
		}
	}
	// struct typedefs, in declaration order (a struct may reference earlier ones)
	for _, td := range p.Types {
		if td.COpaque != "" {
			// an opaque FFI handle: wrap the real C type by value in one hidden field
			fmt.Fprintf(&out, "typedef struct { %s _c; } mfl_%s;\n", td.COpaque, td.Name)
			continue
		}
		fmt.Fprintf(&out, "typedef struct {")
		for _, f := range td.Fields {
			fmt.Fprintf(&out, " %s f_%s;", cTypeName(f.Type), f.Name)
		}
		fmt.Fprintf(&out, " } mfl_%s;\n", td.Name)
	}
	// FFI struct marshaling: convert each cstruct between its MFL value (mfl_Name,
	// with int64/double/nested mfl_ fields) and the C layout (Name) at the
	// boundary. A nested cstruct field recurses through its own mfl_from_/mfl_to_.
	cstructNames := map[string]bool{}
	for _, ed := range p.Externs {
		for _, cs := range ed.Structs {
			cstructNames[cs.Name] = true
		}
	}
	for _, ed := range p.Externs {
		for _, cs := range ed.Structs {
			if cs.Opaque {
				// copy the whole C struct in/out of the one-field MFL wrapper
				fmt.Fprintf(&out, "static mfl_%s mfl_from_%s(%s c) { return (mfl_%s){ ._c = c }; }\n", cs.Name, cs.Name, cs.Name, cs.Name)
				fmt.Fprintf(&out, "static %s mfl_to_%s(mfl_%s m) { return m._c; }\n", cs.Name, cs.Name, cs.Name)
				continue
			}
			fmt.Fprintf(&out, "static mfl_%s mfl_from_%s(%s c) { return (mfl_%s){", cs.Name, cs.Name, cs.Name, cs.Name)
			for _, f := range cs.Fields {
				if cstructNames[f.CType] {
					fmt.Fprintf(&out, " .f_%s = mfl_from_%s(c.%s),", f.Name, f.CType, f.Name)
				} else if f.CType == "ptr" {
					fmt.Fprintf(&out, " .f_%s = (int64_t)(intptr_t)c.%s,", f.Name, f.Name)
				} else {
					fmt.Fprintf(&out, " .f_%s = c.%s,", f.Name, f.Name)
				}
			}
			out.WriteString(" }; }\n")
			fmt.Fprintf(&out, "static %s mfl_to_%s(mfl_%s m) { return (%s){", cs.Name, cs.Name, cs.Name, cs.Name)
			for _, f := range cs.Fields {
				if cstructNames[f.CType] {
					fmt.Fprintf(&out, " .%s = mfl_to_%s(m.f_%s),", f.Name, f.CType, f.Name)
				} else if f.CType == "ptr" {
					// a pointer field: MFL holds it as an int; void* converts to the
					// real C field type (float*, unsigned char*, ...).
					fmt.Fprintf(&out, " .%s = (void*)(intptr_t)m.f_%s,", f.Name, f.Name)
				} else {
					fmt.Fprintf(&out, " .%s = (%s)m.f_%s,", f.Name, ffiCType(f.CType), f.Name)
				}
			}
			out.WriteString(" }; }\n")
		}
	}
	// closure environment + multi-return result structs (one per instance)
	for _, inst := range g.c.Reps() {
		src := g.c.SrcFunc(inst)
		if src.IsLambda && src.NumCaptures > 0 {
			// Each environment field is a pointer to the captured variable's heap
			// box, so the closure shares storage with its enclosing scope.
			fmt.Fprintf(&out, "typedef struct {")
			for i := 0; i < src.NumCaptures; i++ {
				fmt.Fprintf(&out, " %s* f%d;", g.c.ParamCType(inst, i), i)
			}
			fmt.Fprintf(&out, " } %s_env;\n", g.c.CName(inst))
		}
		if n := g.c.RetArity(inst); n >= 2 {
			fmt.Fprintf(&out, "typedef struct {")
			for i := 0; i < n; i++ {
				fmt.Fprintf(&out, " %s r%d;", g.c.RetCTypeAt(inst, i), i)
			}
			fmt.Fprintf(&out, " } %s_ret;\n", g.c.CName(inst))
		}
	}
	if len(p.Types) > 0 {
		out.WriteByte('\n')
	}
	// JSON serializers (generated on demand by json()); reference struct typedefs
	if g.jsonFns.Len() > 0 {
		out.WriteString(g.jsonFns.String())
		out.WriteByte('\n')
	}
	// package globals: zero-initialized C statics; their MFL initializers run in a
	// constructor (emitted after the function bodies). Declared here so function
	// bodies that reference mfl_g_<name> compile.
	for _, name := range g.c.GlobalOrder() {
		fmt.Fprintf(&out, "static %s mfl_g_%s;\n", g.c.GlobalCType(name), name)
	}
	if len(g.c.GlobalOrder()) > 0 {
		out.WriteByte('\n')
	}
	for _, inst := range g.c.Reps() {
		// Under wasm, an `export func` is exported to the host under its clean source
		// name (so JS calls instance.exports.render, not the mangled C symbol). The
		// attribute on the prototype also forces the export — no linker flag needed.
		if g.wasm() {
			if src := g.c.SrcFunc(inst); g.c.exportSrc[src.Name] {
				fmt.Fprintf(&out, "__attribute__((export_name(%q))) ", src.Name)
			}
		}
		out.WriteString(g.signature(inst) + ";\n")
	}
	out.WriteByte('\n')
	out.WriteString(g.tramp.String())
	out.WriteString(g.buf.String())
	// package-global initializers run in a C constructor — before main (native) and
	// at _initialize (wasm reactor). Compile each init into a scratch buffer so any
	// helper statements land inside the constructor, in declaration order.
	if len(p.Globals) > 0 {
		g.curFn = "$globals"
		var ctor strings.Builder
		for _, gv := range p.Globals {
			saved := g.buf
			g.buf = strings.Builder{}
			e, err := g.expr(gv.Init)
			if err != nil {
				return "", err
			}
			helpers := g.buf.String()
			g.buf = saved
			ctor.WriteString(helpers)
			fmt.Fprintf(&ctor, "    mfl_g_%s = %s;\n", gv.Name, e)
		}
		out.WriteString("__attribute__((constructor)) static void mfl_globals_init(void) {\n")
		out.WriteString(ctor.String())
		out.WriteString("}\n")
	}
	// wasm reactor has no exit() to flush stdio, so buffered output is lost when an
	// exported function returns. Make stdout/stderr unbuffered at _initialize so every
	// println is written through immediately (e.g. for the browser playground). Native
	// keeps normal buffering — exit() flushes it.
	if g.wasm() {
		out.WriteString("__attribute__((constructor)) static void mfl_wasm_stdio_init(void) { setvbuf(stdout, NULL, _IONBF, 0); setvbuf(stderr, NULL, _IONBF, 0); }\n")
	}
	// record/replay determinism boundary: 1 if the program uses FFI (extern) — the call's
	// result is uncaptured, so it leaves the boundary machin controls. Everything else
	// nondeterministic is now captured: channel schedule + concurrent prints, time, stdin,
	// rand, file reads, raw fd sockets, and (this slice) the high-level HTTP/TLS/WebSocket
	// reads; `select` is gated. Program-dependent, so emitted here (body region, compared
	// by cgentest) rather than the fixed prelude; the rr runtime forward-declares it.
	rrBoundary := 0
	if len(p.Externs) > 0 {
		rrBoundary = 1
	}
	fmt.Fprintf(&out, "static int mfl_rr_prog_boundary(void) { return %d; }\n", rrBoundary)
	// Native entry point. The wasm target is a reactor module (no `int main`): the
	// host drives it through the exported functions, so emit the C main only for
	// native, and only when the program actually defines an MFL main.
	if !g.wasm() && g.c.HasMain() {
		// Ignore SIGPIPE so a write to a peer that closed the connection (e.g. an SSE
		// client that navigated away) returns -1/EPIPE instead of killing the process.
		out.WriteString("int main(int argc, char** argv) { signal(SIGPIPE, SIG_IGN); mfl_argc = argc; mfl_argv = argv; mfl_rr_init(); mfl_main(); mfl_rr_finish(); return 0; }\n")
	}
	return out.String(), nil
}

// retType is a function's C return type: void, the single value's type, or a
// generated result struct for multiple returns.
func (g *cgen) retType(inst string) string {
	switch g.c.RetArity(inst) {
	case 0:
		return "void"
	case 1:
		return g.c.RetCTypeAt(inst, 0)
	default:
		return g.c.CName(inst) + "_ret"
	}
}

func (g *cgen) signature(inst string) string {
	fn := g.c.SrcFunc(inst)
	var parts []string
	if fn.IsLambda {
		parts = append(parts, "void* _env") // captures arrive via the environment
	}
	for i := fn.NumCaptures; i < len(fn.Params); i++ {
		// A parameter captured by a nested closure is received by value under a
		// temporary name, then boxed on entry (see function()).
		cname := "v_" + fn.Params[i]
		if fn.Boxed[fn.Params[i]] {
			cname = "_arg_" + fn.Params[i]
		}
		parts = append(parts, g.c.ParamCType(inst, i)+" "+cname)
	}
	params := strings.Join(parts, ", ")
	if params == "" {
		params = "void"
	}
	return fmt.Sprintf("%s %s(%s)", g.retType(inst), g.c.CName(inst), params)
}

func (g *cgen) function(inst string) error {
	fn := g.c.SrcFunc(inst)
	g.curFn = inst
	g.buf.WriteString(g.signature(inst) + " {\n")
	// unpack captured variables from the closure environment. Each is a pointer
	// to the shared heap box; the lambda accesses it by reference (varRef).
	if fn.IsLambda && fn.NumCaptures > 0 {
		env := g.c.CName(inst) + "_env"
		fmt.Fprintf(&g.buf, "    %s* _e = (%s*)_env;\n", env, env)
		for i := 0; i < fn.NumCaptures; i++ {
			fmt.Fprintf(&g.buf, "    %s* v_%s = _e->f%d;\n", g.c.ParamCType(inst, i), fn.Params[i], i)
		}
	}
	// box any parameter captured by a nested closure: copy the by-value argument
	// into a fresh heap cell so the closure can share and mutate it.
	for i := fn.NumCaptures; i < len(fn.Params); i++ {
		name := fn.Params[i]
		if fn.Boxed[name] {
			ct := g.c.ParamCType(inst, i)
			// malloc, not mfl_alloc: this box must outlive the arena of
			// whichever call frame declares it, since a closure sharing it
			// may escape that frame (e.g. via `go`) (#314).
			fmt.Fprintf(&g.buf, "    %s* v_%s = malloc(sizeof(%s)); *v_%s = _arg_%s;\n", ct, name, ct, name, name)
		}
	}
	for _, name := range g.c.Locals(inst) {
		if fn.Boxed[name] {
			ct := g.c.VarCType(inst, name)
			// Zero the heap cell via calloc — the body assigns the real value. (A
			// `*p = {0}` assignment is invalid C for aggregate types like slices.)
			fmt.Fprintf(&g.buf, "    %s* v_%s = mfl_calloc(1, sizeof(%s));\n", ct, name, ct)
			continue
		}
		fmt.Fprintf(&g.buf, "    %s v_%s = %s;\n", g.c.VarCType(inst, name), name, cZero(g.c.VarKind(inst, name)))
	}
	for _, s := range fn.Body {
		if err := g.stmt(s, 1); err != nil {
			return err
		}
	}
	// a named-return function may fall off the end, yielding the named values
	if len(fn.Returns) > 0 {
		g.buf.WriteString("    ")
		g.emitNamedReturn(inst)
	}
	g.buf.WriteString("}\n\n")
	return nil
}

// emitNamedReturn writes a return of the function's named return locals.
func (g *cgen) emitNamedReturn(inst string) {
	names := g.c.RetNames(inst)
	switch len(names) {
	case 0:
		g.buf.WriteString("return;\n")
	case 1:
		fmt.Fprintf(&g.buf, "return %s;\n", g.varRef(names[0]))
	default:
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = g.varRef(n)
		}
		fmt.Fprintf(&g.buf, "return (%s_ret){ %s };\n", g.c.CName(inst), strings.Join(parts, ", "))
	}
}

func indentC(b *strings.Builder, n int) {
	for i := 0; i < n; i++ {
		b.WriteString("    ")
	}
}

// emitProbe instruments an assignment for `machin replay --print <name>`: after the store,
// print the variable's current value. Because replay is deterministic, this is a faithful
// value history — the last line before a panic is the variable's state at the crash. Emitted
// only when the var is watched, so a normal build (and the cgentest oracle-diff) carry
// nothing. Scalars + strings only; slice/map/struct/chan are skipped (no cheap str form).
func (g *cgen) emitProbe(name string, kind Kind, depth int) {
	if !g.wantsProbe(name) {
		return
	}
	ref := g.varRef(name)
	var val string
	switch kind {
	case KFloat:
		val = fmt.Sprintf("mfl_str_d(%s)", ref)
	case KBool:
		val = fmt.Sprintf("mfl_str_b(%s)", ref)
	case KString:
		val = ref
	case KInt:
		val = fmt.Sprintf("mfl_str_i(%s)", ref)
	default:
		return
	}
	indentC(&g.buf, depth)
	fmt.Fprintf(&g.buf, "mfl_rr_probe(%q, %s);\n", name, val)
}

func (g *cgen) stmt(s Stmt, depth int) error {
	indentC(&g.buf, depth)
	switch st := s.(type) {
	case *ExprStmt:
		if call, ok := st.X.(*Call); ok && (call.Callee == "print" || call.Callee == "println") {
			return g.printCall(call, depth)
		}
		e, err := g.expr(st.X)
		if err != nil {
			return err
		}
		g.buf.WriteString(e + ";\n")
	case *AssignStmt:
		e, err := g.expr(st.Val)
		if err != nil {
			return err
		}
		fmt.Fprintf(&g.buf, "%s = %s;\n", g.varRef(st.Name), e)
		g.emitProbe(st.Name, g.c.NodeKind(g.curFn, st.Val), depth)
	case *ReturnStmt:
		if len(st.Vals) == 0 {
			// a bare return yields the named return locals (or nothing for void)
			g.emitNamedReturn(g.curFn)
			return nil
		}
		if len(st.Vals) == 1 {
			e, err := g.expr(st.Vals[0])
			if err != nil {
				return err
			}
			g.buf.WriteString("return (" + e + ");\n")
		} else {
			// multiple return values: sequence them left-to-right (a C aggregate
			// initializer's elements are otherwise unordered).
			ret, err := g.seqExprs(st.Vals, func(names []string) (string, error) {
				return fmt.Sprintf("(%s_ret){ %s }", g.c.CName(g.curFn), strings.Join(names, ", ")), nil
			})
			if err != nil {
				return err
			}
			g.buf.WriteString("return " + ret + ";\n")
		}
	case *BreakStmt:
		g.buf.WriteString("break;\n")
	case *ContinueStmt:
		g.buf.WriteString("continue;\n")
	case *MultiAssign:
		return g.multiAssign(st, depth)
	case *IfStmt:
		cond, err := g.expr(st.Cond)
		if err != nil {
			return err
		}
		g.buf.WriteString("if (" + cond + ") {\n")
		for _, t := range st.Then {
			if err := g.stmt(t, depth+1); err != nil {
				return err
			}
		}
		indentC(&g.buf, depth)
		if st.Else != nil {
			g.buf.WriteString("} else {\n")
			for _, e := range st.Else {
				if err := g.stmt(e, depth+1); err != nil {
					return err
				}
			}
			indentC(&g.buf, depth)
		}
		g.buf.WriteString("}\n")
	case *WhileStmt:
		cond, err := g.expr(st.Cond)
		if err != nil {
			return err
		}
		g.buf.WriteString("while (" + cond + ") {\n")
		for _, t := range st.Body {
			if err := g.stmt(t, depth+1); err != nil {
				return err
			}
		}
		indentC(&g.buf, depth)
		g.buf.WriteString("}\n")
	case *IndexAssign:
		x, err := g.expr(st.Target.X)
		if err != nil {
			return err
		}
		idx, err := g.expr(st.Target.Idx)
		if err != nil {
			return err
		}
		val, err := g.expr(st.Val)
		if err != nil {
			return err
		}
		if g.c.NodeKind(g.curFn, st.Target.X) == KMap {
			ik, sk := g.mapKeyArgs(st.Target.X, idx)
			vt := g.c.MapValCType(g.curFn, st.Target.X)
			fmt.Fprintf(&g.buf, "mfl_map_set(%s, %s, %s, &((%s[1]){%s})[0]);\n", x, ik, sk, vt, val)
		} else {
			fmt.Fprintf(&g.buf, "((%s*)(%s).data)[%s] = %s;\n", g.c.ElemCType(g.curFn, st.Target.X), x, g.boundsIdx(idx, x), val)
		}
	case *FieldAssign:
		x, err := g.expr(st.Target.X)
		if err != nil {
			return err
		}
		val, err := g.expr(st.Val)
		if err != nil {
			return err
		}
		fmt.Fprintf(&g.buf, "(%s).f_%s = %s;\n", x, st.Target.Name, val)
	case *SendStmt:
		ch, err := g.expr(st.Ch)
		if err != nil {
			return err
		}
		val, err := g.expr(st.Val)
		if err != nil {
			return err
		}
		ct := g.c.ElemCType(g.curFn, st.Ch)
		fmt.Fprintf(&g.buf, "mfl_chan_send(%s, &((%s[1]){%s})[0]);\n", ch, ct, val)
	case *SelectStmt:
		return g.selectStmt(st, depth)
	case *RangeStmt:
		return g.rangeStmt(st, depth)
	case *ArenaStmt:
		// install a fresh arena for the block's allocations, then free it in bulk
		// on exit and restore the enclosing arena.
		id := g.arenaID
		g.arenaID++
		fmt.Fprintf(&g.buf, "{ mfl_arena _sa%d = {0}; mfl_arena* _sp%d = mfl_arena_cur; mfl_arena_cur = &_sa%d;\n", id, id, id)
		for _, b := range st.Body {
			if err := g.stmt(b, depth+1); err != nil {
				return err
			}
		}
		indentC(&g.buf, depth)
		fmt.Fprintf(&g.buf, "mfl_arena_cur = _sp%d; mfl_arena_free(&_sa%d); }\n", id, id)
	case *GoStmt:
		return g.goStmt(st)
	default:
		return fmt.Errorf("codegen: unknown statement %T", s)
	}
	return nil
}

// multiAssign emits `a, b := rhs`: destructure a multi-return call via its
// result struct, or evaluate parallel RHS expressions into temps first (so
// `a, b = b, a` works) and then assign.
// multiRetBuiltinC maps a multi-return builtin to its C function, result struct
// type, the struct field names (in return order), and whether it needs OpenSSL.
func multiRetBuiltinC(name string) (cfn, ctype string, fields []string, needsTLS, ok bool) {
	switch name {
	case "http_get":
		return "mfl_http_get", "mfl_http_result", []string{"status", "body", "err"}, true, true
	case "http_request":
		return "mfl_http_request", "mfl_http_result", []string{"status", "body", "err"}, true, true
	case "json_get":
		return "mfl_json_get", "mfl_json_result", []string{"value", "err"}, false, true
	case "exec":
		return "mfl_exec", "mfl_exec_result", []string{"code", "out", "err"}, false, true
	case "mmap_file":
		return "mfl_mmap_file", "mfl_mmap_result", []string{"ptr", "len"}, false, true
	}
	return "", "", nil, false, false
}

// selectStmt compiles a select to a poll over its cases. Channel operands and
// send values are evaluated once up front (Go order); the loop tries each case
// in source order (receives via non-blocking tryrecv, sends are always ready on
// machin's unbounded channels) and takes the first ready one. With no ready case
// and no default, it polls (1ms). The chosen body runs OUTSIDE the poll loop, so
// break/continue/return inside a case affect the enclosing loop/function.
func (g *cgen) selectStmt(st *SelectStmt, depth int) error {
	g.usesSelect = true // record/replay gates the select (chosen case recorded + replayed)
	id := g.tmpID
	g.tmpID++
	g.buf.WriteString("{\n")
	for i := range st.Cases {
		sc := &st.Cases[i]
		indentC(&g.buf, depth+1)
		if sc.RecvCh != nil {
			ch, err := g.expr(sc.RecvCh)
			if err != nil {
				return err
			}
			et := g.c.ElemCType(g.curFn, sc.RecvCh)
			fmt.Fprintf(&g.buf, "mfl_chan* _sc%d_%d = %s; %s _sv%d_%d; int _sok%d_%d = 0;\n", id, i, ch, et, id, i, id, i)
		} else {
			ch, err := g.expr(sc.SendCh)
			if err != nil {
				return err
			}
			val, err := g.expr(sc.SendVal)
			if err != nil {
				return err
			}
			et := g.c.ElemCType(g.curFn, sc.SendCh)
			fmt.Fprintf(&g.buf, "mfl_chan* _sc%d_%d = %s; %s _sv%d_%d = %s;\n", id, i, ch, et, id, i, val)
		}
	}
	indentC(&g.buf, depth+1)
	fmt.Fprintf(&g.buf, "int _sel%d = -1;\n", id)
	// record/replay gating: on replay we do NOT poll — we pop the recorded case index
	// and force exactly that case with a BLOCKING op (recv2/send), which waits its turn
	// in the replayed schedule so the value & ordering match the recording byte-for-byte.
	// A recorded select is therefore faithful, not best-effort.
	indentC(&g.buf, depth+1)
	fmt.Fprintf(&g.buf, "if (mfl_rr_mode == 2) {\n")
	indentC(&g.buf, depth+2)
	fmt.Fprintf(&g.buf, "_sel%d = (int)mfl_rr_io_pop_i64();\n", id)
	for i := range st.Cases {
		sc := &st.Cases[i]
		indentC(&g.buf, depth+2)
		if sc.RecvCh != nil {
			fmt.Fprintf(&g.buf, "if (_sel%d == %d) { if (!(_sok%d_%d = mfl_chan_recv2(_sc%d_%d, &_sv%d_%d))) memset(&_sv%d_%d, 0, sizeof(_sv%d_%d)); }\n", id, i, id, i, id, i, id, i, id, i, id, i)
		} else {
			fmt.Fprintf(&g.buf, "if (_sel%d == %d) { mfl_chan_send(_sc%d_%d, &_sv%d_%d); }\n", id, i, id, i, id, i)
		}
	}
	indentC(&g.buf, depth+1)
	g.buf.WriteString("} else {\n")
	indentC(&g.buf, depth+2)
	g.buf.WriteString("for (;;) {\n")
	for i := range st.Cases {
		sc := &st.Cases[i]
		indentC(&g.buf, depth+3)
		if sc.RecvCh != nil {
			fmt.Fprintf(&g.buf, "if (mfl_chan_tryrecv2(_sc%d_%d, &_sv%d_%d, &_sok%d_%d)) { _sel%d = %d; break; }\n", id, i, id, i, id, i, id, i)
		} else {
			fmt.Fprintf(&g.buf, "{ mfl_chan_send(_sc%d_%d, &_sv%d_%d); _sel%d = %d; break; }\n", id, i, id, i, id, i)
		}
	}
	indentC(&g.buf, depth+3)
	if st.HasDefault {
		fmt.Fprintf(&g.buf, "{ _sel%d = %d; break; }\n", id, len(st.Cases))
	} else {
		g.buf.WriteString("mfl_sleep(1);\n")
	}
	indentC(&g.buf, depth+2)
	g.buf.WriteString("}\n")
	// record the chosen case index so replay can force it (I/O queue, per goroutine).
	indentC(&g.buf, depth+2)
	fmt.Fprintf(&g.buf, "if (mfl_rr_mode == 1) mfl_rr_io_log_i64(_sel%d);\n", id)
	indentC(&g.buf, depth+1)
	g.buf.WriteString("}\n")
	for i := range st.Cases {
		sc := &st.Cases[i]
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "if (_sel%d == %d) {\n", id, i)
		if sc.RecvCh != nil && sc.Name != "" && sc.Name != "_" {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s = _sv%d_%d;\n", g.varRef(sc.Name), id, i)
		}
		if sc.RecvCh != nil && sc.OkName != "" && sc.OkName != "_" {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s = _sok%d_%d;\n", g.varRef(sc.OkName), id, i)
		}
		for _, s := range sc.Body {
			if err := g.stmt(s, depth+2); err != nil {
				return err
			}
		}
		indentC(&g.buf, depth+1)
		g.buf.WriteString("}\n")
	}
	if st.HasDefault {
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "if (_sel%d == %d) {\n", id, len(st.Cases))
		for _, s := range st.Default {
			if err := g.stmt(s, depth+2); err != nil {
				return err
			}
		}
		indentC(&g.buf, depth+1)
		g.buf.WriteString("}\n")
	}
	indentC(&g.buf, depth)
	g.buf.WriteString("}\n")
	return nil
}

func (g *cgen) multiAssign(st *MultiAssign, depth int) error {
	assign := func(name, val string) {
		if name == "_" {
			return
		}
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "%s = %s;\n", g.varRef(name), val)
	}

	if len(st.Rhs) == 1 {
		// comma-ok receive: v, ok := <-ch  (ok is false when closed and drained)
		if recv, isRecv := st.Rhs[0].(*Recv); isRecv {
			ch, err := g.expr(recv.Ch)
			if err != nil {
				return err
			}
			et := g.c.ElemCType(g.curFn, recv.Ch)
			id := g.tmpID
			g.tmpID++
			g.buf.WriteString("{\n")
			indentC(&g.buf, depth+1)
			fmt.Fprintf(&g.buf, "%s _rv%d; int _rok%d = mfl_chan_recv2(%s, &_rv%d);\n", et, id, id, ch, id)
			indentC(&g.buf, depth+1)
			fmt.Fprintf(&g.buf, "if (!_rok%d) memset(&_rv%d, 0, sizeof(_rv%d));\n", id, id, id)
			assign(st.Names[0], fmt.Sprintf("_rv%d", id))
			assign(st.Names[1], fmt.Sprintf("_rok%d", id))
			indentC(&g.buf, depth)
			g.buf.WriteString("}\n")
			return nil
		}
		// multi-return builtin (the `v, err :=` idiom): emit the result struct
		// and destructure its fields across the assigned names.
		if call, isCall := st.Rhs[0].(*Call); isCall {
			if cfn, ctype, fields, needsTLS, ok := multiRetBuiltinC(call.Callee); ok {
				if needsTLS {
					g.usesTLS = true
				}
				args := make([]string, len(call.Args))
				for i, a := range call.Args {
					e, err := g.expr(a)
					if err != nil {
						return err
					}
					args[i] = e
				}
				id := g.tmpID
				g.tmpID++
				g.buf.WriteString("{\n")
				indentC(&g.buf, depth+1)
				fmt.Fprintf(&g.buf, "%s _t%d = %s(%s);\n", ctype, id, cfn, strings.Join(args, ", "))
				for i, name := range st.Names {
					assign(name, fmt.Sprintf("_t%d.%s", id, fields[i]))
				}
				indentC(&g.buf, depth)
				g.buf.WriteString("}\n")
				return nil
			}
		}
		call, isCall := st.Rhs[0].(*Call)
		if isCall && g.c.CalleeInst(g.curFn, call) != "" && g.c.RetArity(g.c.CalleeInst(g.curFn, call)) >= 2 {
			args := make([]string, len(call.Args))
			for i, a := range call.Args {
				e, err := g.expr(a)
				if err != nil {
					return err
				}
				args[i] = e
			}
			id := g.tmpID
			g.tmpID++
			cn := g.c.CalleeCName(g.curFn, call)
			g.buf.WriteString("{\n")
			indentC(&g.buf, depth+1)
			fmt.Fprintf(&g.buf, "%s_ret _t%d = %s(%s);\n", cn, id, cn, strings.Join(args, ", "))
			for i, name := range st.Names {
				assign(name, fmt.Sprintf("_t%d.r%d", id, i))
			}
			indentC(&g.buf, depth)
			g.buf.WriteString("}\n")
			return nil
		}
		// single value to a single name
		e, err := g.expr(st.Rhs[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(&g.buf, "%s = %s;\n", g.varRef(st.Names[0]), e)
		return nil
	}

	// parallel assignment: evaluate all RHS into temps, then assign
	id := g.tmpID
	g.tmpID++
	g.buf.WriteString("{\n")
	for i, e := range st.Rhs {
		ce, err := g.expr(e)
		if err != nil {
			return err
		}
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "%s _t%d_%d = %s;\n", g.c.NodeCType(g.curFn, e), id, i, ce)
	}
	for i, name := range st.Names {
		assign(name, fmt.Sprintf("_t%d_%d", id, i))
	}
	indentC(&g.buf, depth)
	g.buf.WriteString("}\n")
	return nil
}

// rangeStmt desugars `for k, v := range x` to a C loop over a slice, map, or
// string. The loop variables are ordinary function locals (declared at the top
// of the function); here they are assigned each iteration.
func (g *cgen) rangeStmt(st *RangeStmt, depth int) error {
	id := g.rangeID
	g.rangeID++
	x, err := g.expr(st.X)
	if err != nil {
		return err
	}
	hasKey := st.Key != "" && st.Key != "_"
	hasVal := st.Val != "" && st.Val != "_"

	emitBody := func() error {
		for _, s := range st.Body {
			if err := g.stmt(s, depth+2); err != nil {
				return err
			}
		}
		return nil
	}

	g.buf.WriteString("{\n")
	switch g.c.NodeKind(g.curFn, st.X) {
	case KSlice:
		ect := g.c.ElemCType(g.curFn, st.X)
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "mfl_slice _r%d = %s;\n", id, x)
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "for (int64_t _i%d = 0; _i%d < _r%d.len; _i%d++) {\n", id, id, id, id)
		if hasKey {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s = _i%d;\n", g.varRef(st.Key), id)
		}
		if hasVal {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s = ((%s*)_r%d.data)[_i%d];\n", g.varRef(st.Val), ect, id, id)
		}
	case KString:
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "const char* _s%d = %s;\n", id, x)
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "for (int64_t _i%d = 0; _s%d[_i%d]; _i%d++) {\n", id, id, id, id)
		if hasKey {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s = _i%d;\n", g.varRef(st.Key), id)
		}
		if hasVal {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s = mfl_charat(_s%d, _i%d);\n", g.varRef(st.Val), id, id)
		}
	case KMap:
		kct, vct := g.c.MapKeyCType(g.curFn, st.X), g.c.MapValCType(g.curFn, st.X)
		ik, sk := "_k"+fmt.Sprint(id), "NULL"
		if g.c.MapKeyKind(g.curFn, st.X) == KString {
			ik, sk = "0", "_k"+fmt.Sprint(id)
		}
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "mfl_map* _m%d = %s;\n", id, x)
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "mfl_slice _ks%d = mfl_map_keys(_m%d);\n", id, id)
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "for (int64_t _i%d = 0; _i%d < _ks%d.len; _i%d++) {\n", id, id, id, id)
		indentC(&g.buf, depth+2)
		fmt.Fprintf(&g.buf, "%s _k%d = ((%s*)_ks%d.data)[_i%d];\n", kct, id, kct, id, id)
		if hasKey {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s = _k%d;\n", g.varRef(st.Key), id)
		}
		if hasVal {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s _v%d; mfl_map_get(_m%d, %s, %s, &_v%d); %s = _v%d;\n", vct, id, id, ik, sk, id, g.varRef(st.Val), id)
		}
	case KChan:
		ect := g.c.ElemCType(g.curFn, st.X)
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "mfl_chan* _ch%d = %s;\n", id, x)
		indentC(&g.buf, depth+1)
		// receive into the loop variable each iteration; stop when the channel is
		// closed and drained (mfl_chan_recv2 returns 0).
		if hasKey {
			fmt.Fprintf(&g.buf, "while (mfl_chan_recv2(_ch%d, &%s)) {\n", id, g.varRef(st.Key))
		} else {
			fmt.Fprintf(&g.buf, "%s _cd%d; while (mfl_chan_recv2(_ch%d, &_cd%d)) {\n", ect, id, id, id)
		}
	default:
		return fmt.Errorf("codegen: cannot range over %s", g.c.NodeKind(g.curFn, st.X))
	}
	if err := emitBody(); err != nil {
		return err
	}
	indentC(&g.buf, depth+1)
	g.buf.WriteString("}\n")
	indentC(&g.buf, depth)
	g.buf.WriteString("}\n")
	return nil
}

// goStmt spawns a pthread. For each go-call site it emits a per-site arg struct
// and trampoline, then a detached pthread_create at the call site.
//
// #310: the spawned goroutine gets its own fresh arena (mfl_arena_cur = &_a
// below), reclaimed independently of the spawning goroutine's. An argument
// holding a pointer into the SPAWNING goroutine's arena -- a string, or a
// struct containing one -- dangles once that arena is freed, which can
// happen before or during the new goroutine's run (e.g. a machweb handler
// that does `go background_work(ag, conv)` then returns: the response is
// sent and the request arena reclaimed while background_work may still be
// starting up). This is exactly what channel sends already protect against
// (mfl_chan_freeze/thaw for strings, a JSON round-trip for slices/maps) --
// reused here for `go` call arguments. Scalars need neither: they are not
// heap-allocated (SPEC.md 12).
func (g *cgen) goStmt(st *GoStmt) error {
	id := g.goID
	g.goID++
	inst := g.c.CalleeInst(g.curFn, st.Call)
	cname := g.c.CName(inst)
	n := len(st.Call.Args)

	// Classify each argument up front: JSON round-trip (contains a slice/map),
	// string-offset freeze/thaw (a string, or a struct of strings only), or
	// neither (a scalar, passed through unchanged). Iterate by index (not a
	// map) so the emitted C is deterministic across compiler runs.
	type jsonArg struct{ ser, des string }
	jsonArgs := make(map[int]jsonArg)
	var strOffs []string
	for i, a := range st.Call.Args {
		argType := g.c.TypeString(g.curFn, a)
		if g.chanNeedsJSON(argType) {
			ser, err := g.jsonSerializer(argType)
			if err != nil {
				return err
			}
			des, err := g.jsonParser(argType)
			if err != nil {
				return err
			}
			jsonArgs[i] = jsonArg{ser, des}
			continue
		}
		strOffs = append(strOffs, g.chanStrOffsets(argType, fmt.Sprintf("offsetof(struct mfl_go_%d, a%d) + ", id, i))...)
	}

	// #314: an argument that is a closure LITERAL (a *MakeClosure with
	// captures) needs one more level of the same protection. Since #376 the
	// env struct and each captured variable's box are plain malloc (stable
	// across the arena boundary), but the DATA a box holds still lives in the
	// spawning goroutine's arena: a captured string's bytes, a captured
	// struct's string fields, a captured slice/map's backing. Freeze/thaw
	// those through the env->box indirection, per capture. After the go
	// statement the spawner must not keep mutating a captured variable (the
	// same ownership-moves rule as a channel send); a closure passed as a
	// plain VARIABLE has an unknowable env layout at this call site and keeps
	// the documented shared-value caveat (SPEC.md 12).
	type capStrOp struct {
		ci   int
		offs []string
	}
	type capJSONOp struct {
		ci       int
		ser, des string
	}
	type closureOps struct {
		env  string
		strs []capStrOp
		js   []capJSONOp
	}
	closArgs := make(map[int]closureOps)
	for i, a := range st.Call.Args {
		mc, ok := a.(*MakeClosure)
		if !ok || len(mc.Captures) == 0 {
			continue
		}
		ops := closureOps{env: g.c.ClosureCName(g.curFn, mc) + "_env"}
		for ci, cap := range mc.Captures {
			capType := g.c.VarTypeString(g.curFn, cap)
			if g.chanNeedsJSON(capType) {
				ser, err := g.jsonSerializer(capType)
				if err != nil {
					return err
				}
				des, err := g.jsonParser(capType)
				if err != nil {
					return err
				}
				ops.js = append(ops.js, capJSONOp{ci, ser, des})
				continue
			}
			if offs := g.chanStrOffsets(capType, ""); len(offs) > 0 {
				ops.strs = append(ops.strs, capStrOp{ci, offs})
			}
		}
		if len(ops.strs) > 0 || len(ops.js) > 0 {
			closArgs[i] = ops
		}
	}

	// arg struct + trampoline (a leading dummy field avoids an empty struct).
	// A JSON-mode argument gets an extra _jN field carrying the malloc'd JSON
	// blob across the arena boundary; its real aN field is populated inside
	// the trampoline, once mfl_arena_cur is the NEW goroutine's arena.
	fmt.Fprintf(&g.tramp, "struct mfl_go_%d { char _; char* _ppath; int _cidx;", id)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&g.tramp, " %s a%d;", g.c.ParamCType(inst, i), i)
		if _, ok := jsonArgs[i]; ok {
			fmt.Fprintf(&g.tramp, " char* _j%d;", i)
		}
		if ops, ok := closArgs[i]; ok {
			for _, j := range ops.js {
				fmt.Fprintf(&g.tramp, " char* _cb%d_%d;", i, j.ci)
			}
		}
	}
	g.tramp.WriteString(" };\n")
	fmt.Fprintf(&g.tramp, "static void* mfl_go_run_%d(void* p) { mfl_arena _a = {0}; mfl_arena_cur = &_a; struct mfl_go_%d* s = (struct mfl_go_%d*)p; mfl_gid_path = mfl_path_child(s->_ppath, s->_cidx); mfl_spawn_ctr = 0; free(s->_ppath);\n",
		id, id, id)
	for i := 0; i < n; i++ {
		if j, ok := jsonArgs[i]; ok {
			fmt.Fprintf(&g.tramp, "    { const char* _p = s->_j%d; s->a%d = %s(&_p); } free(s->_j%d);\n", i, i, j.des, i)
		}
	}
	if len(strOffs) > 0 {
		fmt.Fprintf(&g.tramp, "    mfl_thaw_strs(%d, (int[]){%s}, s);\n", len(strOffs), strings.Join(strOffs, ", "))
	}
	for i := 0; i < n; i++ {
		ops, ok := closArgs[i]
		if !ok {
			continue
		}
		fmt.Fprintf(&g.tramp, "    { %s* _ce = (%s*)s->a%d.env;\n", ops.env, ops.env, i)
		for _, so := range ops.strs {
			fmt.Fprintf(&g.tramp, "      mfl_thaw_strs(%d, (int[]){%s}, _ce->f%d);\n", len(so.offs), strings.Join(so.offs, ", "), so.ci)
		}
		for _, j := range ops.js {
			fmt.Fprintf(&g.tramp, "      { const char* _p = s->_cb%d_%d; *_ce->f%d = %s(&_p); } free(s->_cb%d_%d);\n", i, j.ci, j.ci, j.des, i, j.ci)
		}
		g.tramp.WriteString("    }\n")
	}
	g.tramp.WriteString("    " + cname + "(")
	for i := 0; i < n; i++ {
		if i > 0 {
			g.tramp.WriteString(", ")
		}
		fmt.Fprintf(&g.tramp, "s->a%d", i)
	}
	g.tramp.WriteString("); free(s); mfl_arena_free(&_a); return NULL; }\n")

	// call site: populate every field from the SPAWNING goroutine's (current)
	// arena, then freeze it against that arena being freed before the new
	// goroutine gets a chance to thaw it back out.
	g.buf.WriteString("{\n")
	fmt.Fprintf(&g.buf, "        struct mfl_go_%d* s = malloc(sizeof(*s));\n", id)
	// record/replay: assign a stable parent-relative goroutine path in the
	// SPAWNING goroutine's program order (not in the new thread, whose start
	// races), so a goroutine's id is identical across record and replay even under
	// concurrent nested spawns. The child gets (parent path, its spawn index).
	g.buf.WriteString("        s->_cidx = ++mfl_spawn_ctr; s->_ppath = strdup(mfl_gid_path ? mfl_gid_path : \"0\");\n")
	for i, a := range st.Call.Args {
		e, err := g.expr(a)
		if err != nil {
			return err
		}
		if j, ok := jsonArgs[i]; ok {
			fmt.Fprintf(&g.buf, "        { char* _j = %s(%s); size_t _n = strlen(_j); s->_j%d = malloc(_n + 1); memcpy(s->_j%d, _j, _n + 1); }\n", j.ser, e, i, i)
		} else {
			fmt.Fprintf(&g.buf, "        s->a%d = (%s);\n", i, e)
		}
	}
	if len(strOffs) > 0 {
		fmt.Fprintf(&g.buf, "        mfl_freeze_strs(%d, (int[]){%s}, s);\n", len(strOffs), strings.Join(strOffs, ", "))
	}
	for i := 0; i < n; i++ {
		ops, ok := closArgs[i]
		if !ok {
			continue
		}
		fmt.Fprintf(&g.buf, "        { %s* _ce = (%s*)s->a%d.env;\n", ops.env, ops.env, i)
		for _, so := range ops.strs {
			fmt.Fprintf(&g.buf, "          mfl_freeze_strs(%d, (int[]){%s}, _ce->f%d);\n", len(so.offs), strings.Join(so.offs, ", "), so.ci)
		}
		for _, j := range ops.js {
			fmt.Fprintf(&g.buf, "          { char* _j = %s(*_ce->f%d); size_t _n = strlen(_j); s->_cb%d_%d = malloc(_n + 1); memcpy(s->_cb%d_%d, _j, _n + 1); }\n", j.ser, j.ci, i, j.ci, i, j.ci)
		}
		g.buf.WriteString("        }\n")
	}
	fmt.Fprintf(&g.buf, "        pthread_t t; pthread_create(&t, NULL, mfl_go_run_%d, s); pthread_detach(t);\n", id)
	g.buf.WriteString("    }\n")
	return nil
}

// printCall emits one print per argument, with single-space separators, so no
// runtime variadic machinery is needed.
func (g *cgen) printCall(call *Call, depth int) error {
	// gate the whole print statement so concurrent output interleaving is
	// captured + replayed (a no-op unless recording/replaying).
	g.buf.WriteString("mfl_rr_print_begin();\n")
	indentC(&g.buf, depth)
	for i, a := range call.Args {
		if i > 0 {
			g.buf.WriteString("fputs(\" \", stdout); ")
		}
		e, err := g.expr(a)
		if err != nil {
			return err
		}
		switch g.c.NodeKind(g.curFn, a) {
		case KInt:
			fmt.Fprintf(&g.buf, "printf(\"%%lld\", (long long)(%s));", e)
		case KFloat:
			fmt.Fprintf(&g.buf, "printf(\"%%g\", (double)(%s));", e)
		case KBool:
			fmt.Fprintf(&g.buf, "fputs((%s) ? \"true\" : \"false\", stdout);", e)
		case KString:
			fmt.Fprintf(&g.buf, "{ const char* _s = (%s); fputs(_s ? _s : \"\", stdout); }", e)
		case KBytes:
			fmt.Fprintf(&g.buf, "fputs(mfl_bytes_hex(%s), stdout);", e) // print bytes as hex
		case KSlice, KStruct, KChan, KMap:
			return fmt.Errorf("cannot print a %s value", g.c.NodeKind(g.curFn, a))
		default:
			fmt.Fprintf(&g.buf, "printf(\"%%lld\", (long long)(%s));", e)
		}
		g.buf.WriteByte('\n')
		if i < len(call.Args)-1 {
			indentC(&g.buf, depth)
		}
	}
	if len(call.Args) == 0 {
		// keep alignment for a bare println()
	}
	indentC(&g.buf, depth)
	if call.Callee == "println" {
		g.buf.WriteString("fputs(\"\\n\", stdout);\n")
	} else {
		g.buf.WriteString("fflush(stdout);\n")
	}
	indentC(&g.buf, depth)
	g.buf.WriteString("mfl_rr_print_end();\n")
	return nil
}

func (g *cgen) expr(e Expr) (string, error) {
	switch ex := e.(type) {
	case *IntLit:
		return strconv.FormatInt(ex.Val, 10), nil
	case *FloatLit:
		s := strconv.FormatFloat(ex.Val, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s, nil
	case *StringLit:
		return strconv.Quote(ex.Val), nil
	case *BoolLit:
		if ex.Val {
			return "1", nil
		}
		return "0", nil
	case *NilLit:
		return "0", nil
	case *Ident:
		return g.varRef(ex.Name), nil
	case *Unary:
		x, err := g.expr(ex.X)
		if err != nil {
			return "", err
		}
		op := ex.Op
		if op == "^" { // MFL bitwise complement -> C's ~
			op = "~"
		}
		return "(" + op + x + ")", nil
	case *Binary:
		return g.binary(ex)
	case *Call:
		return g.call(ex)
	case *SliceLit:
		return g.sliceLit(ex)
	case *Index:
		x, err := g.expr(ex.X)
		if err != nil {
			return "", err
		}
		idx, err := g.expr(ex.Idx)
		if err != nil {
			return "", err
		}
		if g.c.NodeKind(g.curFn, ex.X) == KMap {
			ik, sk := g.mapKeyArgs(ex.X, idx)
			// a missing string value zero-fills to NULL; surface it as ""
			tail := "_g;"
			if g.c.NodeKind(g.curFn, ex) == KString {
				tail = "_g ? _g : \"\";"
			}
			return fmt.Sprintf("({ %s _g; mfl_map_get(%s, %s, %s, &_g); %s })", g.c.NodeCType(g.curFn, ex), x, ik, sk, tail), nil
		}
		return fmt.Sprintf("((%s*)(%s).data)[%s]", g.c.ElemCType(g.curFn, ex.X), x, g.boundsIdx(idx, x)), nil
	case *StructLit:
		return g.structLit(ex)
	case *FieldAccess:
		x, err := g.expr(ex.X)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s).f_%s", x, ex.Name), nil
	case *MakeClosure:
		name := g.c.ClosureCName(g.curFn, ex)
		if len(ex.Captures) == 0 {
			return fmt.Sprintf("(mfl_closure){ (void*)%s, NULL }", name), nil
		}
		id := g.tmpID
		g.tmpID++
		var b strings.Builder
		// malloc, not mfl_alloc: the env must outlive the arena of whichever
		// goroutine constructs this closure (e.g. a `go f(func(){...})` call
		// captures/allocates in the spawner, which may return and free its
		// arena before the spawned goroutine invokes the closure) (#314).
		fmt.Fprintf(&b, "({ %s_env* _e%d = malloc(sizeof(%s_env));", name, id, name)
		for i, cap := range ex.Captures {
			fmt.Fprintf(&b, " _e%d->f%d = v_%s;", id, i, cap)
		}
		fmt.Fprintf(&b, " (mfl_closure){ (void*)%s, _e%d }; })", name, id)
		return b.String(), nil
	case *CallValue:
		clos, err := g.expr(ex.Fn)
		if err != nil {
			return "", err
		}
		args := make([]string, len(ex.Args))
		for i, a := range ex.Args {
			e, err := g.expr(a)
			if err != nil {
				return "", err
			}
			args[i] = e
		}
		params, ret := g.c.NodeFuncSig(g.curFn, ex.Fn)
		return g.closureCall(clos, params, ret, args), nil
	case *MakeChan:
		ect := g.c.ElemCType(g.curFn, ex)
		et := g.c.ElemTypeString(g.curFn, ex)
		// elements with a slice or map are deep-copied via JSON round-trip; plain
		// strings via the fast offset path; scalars need nothing.
		if g.chanNeedsJSON(et) {
			ser, des, err := g.chanJSONFns(et)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("mfl_make_chan(sizeof(%s), %s, %s, 0)", ect, ser, des), nil
		}
		offs := g.chanStrOffsets(et, "")
		if len(offs) == 0 {
			return fmt.Sprintf("mfl_make_chan(sizeof(%s), 0, 0, 0)", ect), nil
		}
		casted := make([]string, len(offs))
		for i, o := range offs {
			casted[i] = "(int)(" + o + ")"
		}
		return fmt.Sprintf("mfl_make_chan(sizeof(%s), 0, 0, %d, %s)", ect, len(offs), strings.Join(casted, ", ")), nil
	case *MakeMap:
		keyIsStr := 0
		if g.c.MapKeyKind(g.curFn, ex) == KString {
			keyIsStr = 1
		}
		return fmt.Sprintf("mfl_make_map(%d, sizeof(%s))", keyIsStr, g.c.MapValCType(g.curFn, ex)), nil
	case *Recv:
		ch, err := g.expr(ex.Ch)
		if err != nil {
			return "", err
		}
		// statement-expression yields the received value (gcc/clang)
		return fmt.Sprintf("({ %s _r; mfl_chan_recv(%s, &_r); _r; })", g.c.NodeCType(g.curFn, ex), ch), nil
	}
	return "", fmt.Errorf("codegen: unknown expression %T", e)
}

// exprHasSideEffect reports whether evaluating e could have an observable side
// effect (or observe one) — i.e. it contains a call or a channel receive. Used
// to decide when operand evaluation order is significant.
func exprHasSideEffect(e Expr) bool {
	switch x := e.(type) {
	case *Call, *CallValue, *Recv:
		return true
	case *Unary:
		return exprHasSideEffect(x.X)
	case *Binary:
		return exprHasSideEffect(x.L) || exprHasSideEffect(x.R)
	case *Index:
		return exprHasSideEffect(x.X) || exprHasSideEffect(x.Idx)
	case *FieldAccess:
		return exprHasSideEffect(x.X)
	case *SliceLit:
		for _, el := range x.Elems {
			if exprHasSideEffect(el) {
				return true
			}
		}
	case *StructLit:
		for _, v := range x.Vals {
			if exprHasSideEffect(v) {
				return true
			}
		}
	}
	return false
}

// seqExprs evaluates exprs in source order and passes their C strings to build.
// C leaves operand/argument evaluation order unspecified, but Go (and MFL) fixes
// it left-to-right; so when any sub-expression has a side effect and there is
// more than one, the values are hoisted into temporaries inside a GNU statement-
// expression (whose declarations are sequenced) before being combined.
func (g *cgen) seqExprs(exprs []Expr, build func([]string) (string, error)) (string, error) {
	strs := make([]string, len(exprs))
	impure := false
	for i, e := range exprs {
		s, err := g.expr(e)
		if err != nil {
			return "", err
		}
		strs[i] = s
		if exprHasSideEffect(e) {
			impure = true
		}
	}
	if !impure || len(exprs) < 2 {
		return build(strs)
	}
	id := g.tmpID
	g.tmpID++
	var decls strings.Builder
	names := make([]string, len(exprs))
	for i, e := range exprs {
		n := fmt.Sprintf("_sq%d_%d", id, i)
		names[i] = n
		fmt.Fprintf(&decls, "%s %s = %s; ", g.c.NodeCType(g.curFn, e), n, strs[i])
	}
	body, err := build(names)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("({ %s%s; })", decls.String(), body), nil
}

func (g *cgen) binary(ex *Binary) (string, error) {
	// Logical && / || must short-circuit: the right operand is evaluated only
	// when the left does not already decide the result. seqExprs would hoist
	// both operands into temporaries whenever either has a side effect, forcing
	// the right operand to be evaluated unconditionally (#437). C's && / ||
	// already guarantee left-to-right short-circuit evaluation with a sequence
	// point between the operands, so emit them directly instead.
	if ex.Op == "&&" || ex.Op == "||" {
		l, err := g.expr(ex.L)
		if err != nil {
			return "", err
		}
		r, err := g.expr(ex.R)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s %s %s)", l, ex.Op, r), nil
	}
	return g.seqExprs([]Expr{ex.L, ex.R}, func(n []string) (string, error) {
		return g.binaryCombine(ex, n[0], n[1]), nil
	})
}

func (g *cgen) binaryCombine(ex *Binary, l, r string) string {
	if ex.Op == "+" && g.c.NodeKind(g.curFn, ex) == KString {
		return fmt.Sprintf("mfl_cat(%s, %s)", l, r)
	}
	// Compare strings by value, not by pointer. C's relational operators on char*
	// compare addresses, so equal-but-distinct strings would wrongly differ and
	// ordering (< <= > >=) would be meaningless; route all six through strcmp.
	if g.c.NodeKind(g.curFn, ex.L) == KString {
		switch ex.Op {
		case "==", "!=", "<", "<=", ">", ">=":
			return fmt.Sprintf("(mfl_strcmp(%s, %s) %s 0)", l, r, ex.Op)
		}
	}
	// --safe: checked integer arithmetic (overflow) and division (by zero)
	if g.safe && g.c.NodeKind(g.curFn, ex) == KInt {
		switch ex.Op {
		case "+":
			return fmt.Sprintf("mfl_iadd(%s, %s)", l, r)
		case "-":
			return fmt.Sprintf("mfl_isub(%s, %s)", l, r)
		case "*":
			return fmt.Sprintf("mfl_imul(%s, %s)", l, r)
		case "/":
			return fmt.Sprintf("mfl_idiv(%s, %s)", l, r)
		case "%":
			return fmt.Sprintf("mfl_imod(%s, %s)", l, r)
		}
	}
	return fmt.Sprintf("(%s %s %s)", l, ex.Op, r)
}

// isBoxed reports whether a variable in the current instance is captured by a
// closure and therefore heap-boxed (captured by reference).
func (g *cgen) isBoxed(name string) bool {
	return g.c.SrcFunc(g.curFn).Boxed[name]
}

// varRef is the C lvalue/rvalue for a variable: a plain local, or a dereference
// of its heap box when the variable is captured by reference. `v_name` always
// names the storage (the box pointer for boxed variables); varRef names the
// value through it, so both the enclosing scope and the closure see mutations.
func (g *cgen) varRef(name string) string {
	if g.isBoxed(name) {
		return "(*v_" + name + ")"
	}
	// a package global, unless shadowed by a local/param of the current function
	if g.globals[name] && !g.c.IsLocal(g.curFn, name) {
		return "mfl_g_" + name
	}
	return "v_" + name
}

// boundsIdx wraps a slice index with a bounds check when --safe is set.
func (g *cgen) boundsIdx(idx, sliceExpr string) string {
	if !g.safe {
		return idx
	}
	return fmt.Sprintf("mfl_bounds(%s, (%s).len)", idx, sliceExpr)
}

func (g *cgen) sliceLit(ex *SliceLit) (string, error) {
	if len(ex.Elems) == 0 {
		return "(mfl_slice){0}", nil
	}
	ek := g.c.ElemKindOf(g.curFn, ex)
	var builder, cast string
	switch ek {
	case KFloat:
		builder, cast = "mfl_lit_f64", "double"
	case KString:
		builder, cast = "mfl_lit_str", "char*"
	case KInt, KBool:
		builder, cast = "mfl_lit_i64", "int64_t"
	default:
		// struct / nested-slice elements: build empty + append in source instead
		return "", fmt.Errorf("non-empty []%s literals are not supported; build with append", ek)
	}
	return g.seqExprs(ex.Elems, func(names []string) (string, error) {
		parts := []string{strconv.Itoa(len(ex.Elems))}
		for _, n := range names {
			parts = append(parts, fmt.Sprintf("(%s)(%s)", cast, n))
		}
		return builder + "(" + strings.Join(parts, ", ") + ")", nil
	})
}

// stringZeroInits returns designated initializers that set every string reachable in
// a struct value to "" (recursing nested structs). A string's zero value is "", but
// C zeroes an omitted compound-literal field to a NULL char* — which crashes the
// string ops — so omitted string fields are made "" explicitly. Other field types
// (int/float/slice/map/bytes/func) C-zero correctly and are left out.
func (g *cgen) stringZeroInits(typeStr string) []string {
	td, ok := g.c.StructTypes()[typeStr]
	if !ok {
		return nil
	}
	var inits []string
	for _, f := range td.Fields {
		if f.Type == "string" {
			inits = append(inits, fmt.Sprintf(".f_%s = \"\"", f.Name))
		} else if _, isStruct := g.c.StructTypes()[f.Type]; isStruct {
			if sub := g.stringZeroInits(f.Type); len(sub) > 0 {
				inits = append(inits, fmt.Sprintf(".f_%s = (mfl_%s){%s}", f.Name, f.Type, strings.Join(sub, ", ")))
			}
		}
	}
	return inits
}

// structLit emits a C compound literal: (mfl_Point){ .f_x = (1), .f_y = (2) }
// for keyed literals, or positional (mfl_Point){ (1), (2) }. Omitted keyed fields
// are zero-filled, with string fields explicitly "" (not NULL) per stringZeroInits.
func (g *cgen) structLit(ex *StructLit) (string, error) {
	return g.seqExprs(ex.Vals, func(names []string) (string, error) {
		// positional literal: every field supplied in order — emit verbatim.
		if len(names) > 0 && len(ex.FieldNames) == 0 {
			parts := make([]string, len(names))
			for i, n := range names {
				parts[i] = "(" + n + ")"
			}
			return fmt.Sprintf("(mfl_%s){%s}", ex.Type, strings.Join(parts, ", ")), nil
		}
		// keyed or empty literal: provided fields, plus "" for any omitted string field.
		provided := make(map[string]bool, len(ex.FieldNames))
		var parts []string
		for i, fn := range ex.FieldNames {
			provided[fn] = true
			parts = append(parts, fmt.Sprintf(".f_%s = (%s)", fn, names[i]))
		}
		if td, ok := g.c.StructTypes()[ex.Type]; ok {
			for _, f := range td.Fields {
				if provided[f.Name] {
					continue
				}
				if f.Type == "string" {
					parts = append(parts, fmt.Sprintf(".f_%s = \"\"", f.Name))
				} else if _, isStruct := g.c.StructTypes()[f.Type]; isStruct {
					if sub := g.stringZeroInits(f.Type); len(sub) > 0 {
						parts = append(parts, fmt.Sprintf(".f_%s = (mfl_%s){%s}", f.Name, f.Type, strings.Join(sub, ", ")))
					}
				}
			}
		}
		if len(parts) == 0 {
			return fmt.Sprintf("(mfl_%s){0}", ex.Type), nil
		}
		return fmt.Sprintf("(mfl_%s){%s}", ex.Type, strings.Join(parts, ", ")), nil
	})
}

// mapKeyArgs returns the (int-key, string-key) C arguments for a map op: a
// string-keyed map passes (0, key); an int-keyed map passes (key, NULL).
func (g *cgen) mapKeyArgs(mapNode Node, keyExpr string) (string, string) {
	if g.c.MapKeyKind(g.curFn, mapNode) == KString {
		return "0", keyExpr
	}
	return keyExpr, "NULL"
}

// chanStrOffsets returns C offset expressions for every string (char*) reachable
// by value inside a channel element of the given MFL type — a bare `string`
// (offset 0) or each string field of a struct (recursing into nested structs).
// Slices/maps inside an element are not deep-copied (their backing stays shared).
func (g *cgen) chanStrOffsets(typeStr, base string) []string {
	if typeStr == "string" {
		return []string{base + "0"}
	}
	td, ok := g.c.StructTypes()[typeStr]
	if !ok {
		return nil
	}
	cs := "mfl_" + typeStr
	var offs []string
	for _, f := range td.Fields {
		fo := base + fmt.Sprintf("offsetof(%s, f_%s)", cs, f.Name)
		if f.Type == "string" {
			offs = append(offs, fo)
		} else if _, isStruct := g.c.StructTypes()[f.Type]; isStruct {
			offs = append(offs, g.chanStrOffsets(f.Type, fo+" + ")...)
		}
	}
	return offs
}

// chanNeedsJSON reports whether a channel element of this type contains a slice
// or map (reachable by value), which the flat string-offset path can't deep-copy
// — those elements go through a JSON serialize/parse round-trip instead.
func (g *cgen) chanNeedsJSON(typeStr string) bool {
	if strings.HasPrefix(typeStr, "[]") || strings.HasPrefix(typeStr, "map[") {
		return true
	}
	if td, ok := g.c.StructTypes()[typeStr]; ok {
		for _, f := range td.Fields {
			if g.chanNeedsJSON(f.Type) {
				return true
			}
		}
	}
	return false
}

// chanJSONFns emits (once per type) the serialize/parse wrappers a JSON-mode
// channel calls, returning their names. The wrappers adapt the generated
// per-type JSON serializer/parser to the channel's void* calling convention.
func (g *cgen) chanJSONFns(typeStr string) (string, string, error) {
	if n, ok := g.chanJSONMemo[typeStr]; ok {
		return n[0], n[1], nil
	}
	ser, err := g.jsonSerializer(typeStr)
	if err != nil {
		return "", "", err
	}
	des, err := g.jsonParser(typeStr)
	if err != nil {
		return "", "", err
	}
	ct := cTypeName(typeStr)
	id := g.jsonID
	g.jsonID++
	sName := fmt.Sprintf("mfl_chanser_%d", id)
	dName := fmt.Sprintf("mfl_chandes_%d", id)
	fmt.Fprintf(&g.jsonFns, "static char* %s(const void* _e) { return %s(*(const %s*)_e); }\n", sName, ser, ct)
	fmt.Fprintf(&g.jsonFns, "static void %s(const char* _j, void* _o) { const char* _p = _j; *(%s*)_o = %s(&_p); }\n", dName, ct, des)
	g.chanJSONMemo[typeStr] = [2]string{sName, dName}
	return sName, dName, nil
}

// jsonSerializer ensures a C function exists that serializes a value of the
// given MFL type to a JSON string, returning the function name. It recurses
// into element/field/value types, emitting children before parents.
func (g *cgen) jsonSerializer(typeStr string) (string, error) {
	if name, ok := g.jsonMemo[typeStr]; ok {
		return name, nil
	}
	name := fmt.Sprintf("mfl_json_v%d", g.jsonID)
	g.jsonID++
	g.jsonMemo[typeStr] = name // reserve before recursion
	ct := cTypeName(typeStr)
	var body string

	switch {
	case typeStr == "int":
		body = "return mfl_str_i(v);"
	case typeStr == "float":
		body = "return mfl_str_d(v);"
	case typeStr == "bool":
		body = `return mfl_dup(v ? "true" : "false");`
	case typeStr == "string":
		body = "return mfl_json_str(v);"
	case strings.HasPrefix(typeStr, "[]"):
		elem := typeStr[2:]
		es, err := g.jsonSerializer(elem)
		if err != nil {
			return "", err
		}
		ect := cTypeName(elem)
		body = fmt.Sprintf(`char* out = mfl_dup("[");
    for (int64_t i = 0; i < v.len; i++) {
        if (i) out = mfl_cat(out, ",");
        out = mfl_cat(out, %s(((%s*)v.data)[i]));
    }
    return mfl_cat(out, "]");`, es, ect)
	case strings.HasPrefix(typeStr, "map["):
		kt, vt, err := splitMapType(typeStr)
		if err != nil {
			return "", err
		}
		vs, err := g.jsonSerializer(vt)
		if err != nil {
			return "", err
		}
		kct, vct := cTypeName(kt), cTypeName(vt)
		var keyJSON, getCall string
		if kt == "string" {
			keyJSON = "mfl_json_str(_k)"
			getCall = "mfl_map_get(v, 0, _k, &_val);"
		} else {
			keyJSON = "mfl_json_str(mfl_str_i(_k))"
			getCall = "mfl_map_get(v, _k, NULL, &_val);"
		}
		body = fmt.Sprintf(`mfl_slice _ks = mfl_map_keys(v);
    char* out = mfl_dup("{");
    for (int64_t i = 0; i < _ks.len; i++) {
        if (i) out = mfl_cat(out, ",");
        %s _k = ((%s*)_ks.data)[i];
        %s _val; %s
        out = mfl_cat(out, %s);
        out = mfl_cat(out, ":");
        out = mfl_cat(out, %s(_val));
    }
    return mfl_cat(out, "}");`, kct, kct, vct, getCall, keyJSON, vs)
	default:
		td, ok := g.c.StructTypes()[typeStr]
		if !ok {
			return "", fmt.Errorf("json: cannot serialize type %q", typeStr)
		}
		var b strings.Builder
		b.WriteString(`char* out = mfl_dup("{");` + "\n")
		for i, f := range td.Fields {
			fs, err := g.jsonSerializer(f.Type)
			if err != nil {
				return "", err
			}
			prefix := `"\"` + f.Name + `\":"`
			if i > 0 {
				prefix = `",\"` + f.Name + `\":"`
			}
			fmt.Fprintf(&b, "    out = mfl_cat(out, %s);\n", prefix)
			fmt.Fprintf(&b, "    out = mfl_cat(out, %s(v.f_%s));\n", fs, f.Name)
		}
		b.WriteString(`    return mfl_cat(out, "}");`)
		body = b.String()
	}

	fmt.Fprintf(&g.jsonFns, "static char* %s(%s v) {\n    %s\n}\n", name, ct, body)
	return name, nil
}

// jsonParser ensures a C function exists that parses JSON (via a cursor) into a
// value of the given type, returning the function name. Mirrors jsonSerializer.
func (g *cgen) jsonParser(typeStr string) (string, error) {
	if name, ok := g.parseMemo[typeStr]; ok {
		return name, nil
	}
	name := fmt.Sprintf("mfl_jp_v%d", g.jsonID)
	g.jsonID++
	g.parseMemo[typeStr] = name
	ct := cTypeName(typeStr)
	var body string

	switch {
	case typeStr == "int":
		body = "return mfl_js_int(p);"
	case typeStr == "float":
		body = "return mfl_js_float(p);"
	case typeStr == "bool":
		body = "return mfl_js_bool(p);"
	case typeStr == "string":
		body = "return mfl_js_str(p);"
	case strings.HasPrefix(typeStr, "[]"):
		elem := typeStr[2:]
		ep, err := g.jsonParser(elem)
		if err != nil {
			return "", err
		}
		ect := cTypeName(elem)
		body = fmt.Sprintf(`mfl_slice s = {0};
    mfl_js_ws(p);
    if (**p == '[') {
        (*p)++; mfl_js_ws(p);
        if (**p != ']') {
            while (1) {
                %s _e = %s(p);
                s = mfl_append(s, &_e, sizeof(%s));
                if (mfl_js_more(p)) continue;
                break;
            }
        }
        mfl_js_ws(p); if (**p == ']') (*p)++;
    }
    return s;`, ect, ep, ect)
	case strings.HasPrefix(typeStr, "map["):
		kt, vt, err := splitMapType(typeStr)
		if err != nil {
			return "", err
		}
		vp, err := g.jsonParser(vt)
		if err != nil {
			return "", err
		}
		vct := cTypeName(vt)
		keyIsStr, setCall := 0, "mfl_map_set(m, strtoll(_k, 0, 10), 0, &_val);"
		if kt == "string" {
			keyIsStr, setCall = 1, "mfl_map_set(m, 0, _k, &_val);"
		}
		body = fmt.Sprintf(`mfl_map* m = mfl_make_map(%d, sizeof(%s));
    mfl_js_ws(p);
    if (**p == '{') {
        (*p)++; mfl_js_ws(p);
        if (**p != '}') {
            while (1) {
                char* _k = mfl_js_str(p); mfl_js_ws(p); if (**p == ':') (*p)++;
                %s _val = %s(p);
                %s
                if (mfl_js_more(p)) continue;
                break;
            }
        }
        mfl_js_ws(p); if (**p == '}') (*p)++;
    }
    return m;`, keyIsStr, vct, vct, vp, setCall)
	default:
		td, ok := g.c.StructTypes()[typeStr]
		if !ok {
			return "", fmt.Errorf("parse: cannot parse into type %q", typeStr)
		}
		var b strings.Builder
		// Omitted string fields must default to "" not NULL — an absent JSON key
		// leaves the field C-zeroed (NULL char*), which crashes len()/concat. Seed
		// them (recursively) exactly like a struct literal does. See stringZeroInits.
		if inits := g.stringZeroInits(typeStr); len(inits) > 0 {
			fmt.Fprintf(&b, "%s out = {%s};\n", ct, strings.Join(inits, ", "))
		} else {
			fmt.Fprintf(&b, "%s out = {0};\n", ct)
		}
		b.WriteString(`    mfl_js_ws(p);
    if (**p == '{') {
        (*p)++; mfl_js_ws(p);
        if (**p != '}') {
            while (1) {
                char* _k = mfl_js_str(p); mfl_js_ws(p); if (**p == ':') (*p)++;
`)
		for i, f := range td.Fields {
			fp, err := g.jsonParser(f.Type)
			if err != nil {
				return "", err
			}
			kw := "if"
			if i > 0 {
				kw = "else if"
			}
			fmt.Fprintf(&b, "                %s (strcmp(_k, %q) == 0) out.f_%s = %s(p);\n", kw, f.Name, f.Name, fp)
		}
		b.WriteString(`                else mfl_js_skip(p);
                if (mfl_js_more(p)) continue;
                break;
            }
        }
        mfl_js_ws(p); if (**p == '}') (*p)++;
    }
    return out;`)
		body = b.String()
	}

	fmt.Fprintf(&g.jsonFns, "static %s %s(const char** p) {\n    %s\n}\n", ct, name, body)
	return name, nil
}

func (g *cgen) call(ex *Call) (string, error) {
	return g.seqExprs(ex.Args, func(args []string) (string, error) {
		return g.callBody(ex, args)
	})
}

// callbackTrampoline emits a tiny static C wrapper matching a "cb(...)ret"
// extern parameter's raw function-pointer signature and returns its name.
//
// A closure literal's generated C function always takes a leading `void*
// _env` (codegen's signature(), for uniform dispatch through mfl_closure),
// even when it captures nothing and that parameter goes unused. A raw C
// callback type has no such slot, so the two signatures never match — the
// checker already rejected anything but a captureless *MakeClosure (#305
// Phase 4a), so we know statically which lifted function to call and can
// wrap it in a same-shaped function that just drops in NULL for the env.
func (g *cgen) callbackTrampoline(arg Expr, cbType string) (string, error) {
	mc, ok := arg.(*MakeClosure)
	if !ok {
		return "", fmt.Errorf("callback argument must be a captureless function literal")
	}
	name := g.c.ClosureCName(g.curFn, mc)
	params, ret := parseCallbackType(cbType)
	id := g.tmpID
	g.tmpID++
	var plist, callArgs []string
	for i, p := range params {
		plist = append(plist, fmt.Sprintf("%s _p%d", ffiCType(p), i))
		callArgs = append(callArgs, fmt.Sprintf("_p%d", i))
	}
	sig := strings.Join(plist, ", ")
	if sig == "" {
		sig = "void"
	}
	call := fmt.Sprintf("%s(NULL%s)", name, prependEach(", ", callArgs))
	retC := ffiCType(ret)
	body := call + ";"
	if retC != "void" {
		body = "return " + call + ";"
	}
	fn := fmt.Sprintf("_mfl_cb%d", id)
	fmt.Fprintf(&g.tramp, "static %s %s(%s) { %s }\n", retC, fn, sig, body)
	return fn, nil
}

// prependEach joins parts with sep and, if non-empty, prefixes the joined
// result with sep too (so a variadic call can splice it after a fixed arg).
func prependEach(sep string, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return sep + strings.Join(parts, sep)
}

func (g *cgen) callBody(ex *Call, args []string) (string, error) {
	// An explicit extern declaration shadows a builtin of the same name (matches
	// the type-checker), so a declared `fn sqrt(float) float` is called, not the
	// math builtin.
	if ef := g.c.ExternFn(ex.Callee); ef != nil {
		parts := make([]string, len(args))
		var pre, post []string // for inout (Name*) params: temp + writeback
		for i, a := range args {
			switch pt := ef.Params[i]; {
			case pt == "ptr": // MFL int -> opaque void*
				parts[i] = fmt.Sprintf("(void*)(intptr_t)(%s)", a)
			case isCallbackType(pt): // a captureless MFL closure -> its raw C fn pointer
				w, err := g.callbackTrampoline(ex.Args[i], pt)
				if err != nil {
					return "", err
				}
				parts[i] = w
			case strings.HasPrefix(pt, "*"): // *Name: deref an MFL int (ptr) to a C struct, by value
				parts[i] = fmt.Sprintf("(*(%s*)(intptr_t)(%s))", pt[1:], a)
			case strings.HasSuffix(pt, "*"): // Name*: pass an MFL cstruct by pointer, writing back (inout)
				base := pt[:len(pt)-1]
				tmp := fmt.Sprintf("_io%d", i)
				pre = append(pre, fmt.Sprintf("%s %s = mfl_to_%s(%s);", base, tmp, base, a))
				post = append(post, fmt.Sprintf("%s = mfl_from_%s(%s);", a, base, tmp))
				parts[i] = "&" + tmp
			case isFFIScalar(pt):
				parts[i] = fmt.Sprintf("(%s)(%s)", ffiCType(pt), a)
			default: // a cstruct, marshaled into the C layout
				parts[i] = fmt.Sprintf("mfl_to_%s(%s)", pt, a)
			}
		}
		call := fmt.Sprintf("%s(%s)", ef.Name, strings.Join(parts, ", "))
		if len(pre) > 0 {
			// an inout call: temp(s), the call, then write the modified struct(s)
			// back to the MFL variable(s). A GNU statement-expression (the call is
			// void in practice — UploadMesh etc.).
			return fmt.Sprintf("({ %s %s; %s })", strings.Join(pre, " "), call, strings.Join(post, " ")), nil
		}
		switch {
		case ef.Ret == "ptr": // void* -> MFL int
			return fmt.Sprintf("(int64_t)(intptr_t)(%s)", call), nil
		case ef.Ret != "" && !isFFIScalar(ef.Ret):
			return fmt.Sprintf("mfl_from_%s(%s)", ef.Ret, call), nil
		}
		return call, nil
	}
	switch ex.Callee {
	case "len":
		switch g.c.NodeKind(g.curFn, ex.Args[0]) {
		case KSlice, KBytes:
			return fmt.Sprintf("((%s).len)", args[0]), nil
		case KMap:
			return fmt.Sprintf("mfl_map_len(%s)", args[0]), nil
		}
		return fmt.Sprintf("((int64_t)strlen(%s))", args[0]), nil
	case "bytes":
		return fmt.Sprintf("mfl_bytes_from_str(%s)", args[0]), nil
	case "bytes_str":
		return fmt.Sprintf("mfl_bytes_str(%s)", args[0]), nil
	case "to_hex":
		return fmt.Sprintf("mfl_bytes_hex(%s)", args[0]), nil
	case "from_hex":
		return fmt.Sprintf("mfl_bytes_unhex(%s)", args[0]), nil
	case "byte_at":
		return fmt.Sprintf("mfl_byte_at(%s, %s)", args[0], args[1]), nil
	case "bytes_sub":
		return fmt.Sprintf("mfl_bytes_sub(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "bytes_index":
		return fmt.Sprintf("mfl_bytes_index(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "bytes_concat":
		return fmt.Sprintf("mfl_bytes_concat(%s, %s)", args[0], args[1]), nil
	case "rand_bytes":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_rand(%s)", args[0]), nil
	case "sha256_bytes":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_sha256(%s)", args[0]), nil
	case "sha1_bytes":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_sha1(%s)", args[0]), nil
	case "hmac_sha256_bytes":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_hmac256(%s, %s)", args[0], args[1]), nil
	case "hkdf_sha256":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_hkdf(%s, %s, %s, %s)", args[0], args[1], args[2], args[3]), nil
	case "pbkdf2_sha256":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_pbkdf2(%s, %s, %s, %s)", args[0], args[1], args[2], args[3]), nil
	case "x25519_pub":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_x25519_pub(%s)", args[0]), nil
	case "x25519_shared":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_x25519_shared(%s, %s)", args[0], args[1]), nil
	case "ed25519_pub":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_ed25519_pub(%s)", args[0]), nil
	case "ed25519_sign":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_ed25519_sign(%s, %s)", args[0], args[1]), nil
	case "ed25519_verify":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_ed25519_verify(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "aes_gcm_encrypt":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_aes_gcm_enc(%s, %s, %s, %s)", args[0], args[1], args[2], args[3]), nil
	case "aes_gcm_decrypt":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_aes_gcm_dec(%s, %s, %s, %s)", args[0], args[1], args[2], args[3]), nil
	case "aes_cbc_encrypt":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_aes_cbc_enc(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "aes_cbc_decrypt":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_aes_cbc_dec(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "xeddsa_sign":
		g.usesXEdDSA = true
		return fmt.Sprintf("mfl_xeddsa_sign(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "xeddsa_verify":
		g.usesXEdDSA = true
		return fmt.Sprintf("mfl_xeddsa_verify(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "keccak256":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_keccak256(%s)", args[0]), nil
	case "secp256k1_pubkey":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_secp256k1_pubkey(%s)", args[0]), nil
	case "secp256k1_sign_recoverable":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_secp256k1_sign_recoverable(%s, %s)", args[0], args[1]), nil
	case "secp256k1_recover":
		g.usesCrypto = true
		return fmt.Sprintf("mfl_crypto_secp256k1_recover(%s, %s)", args[0], args[1]), nil
	case "has":
		ik, sk := g.mapKeyArgs(ex.Args[0], args[1])
		return fmt.Sprintf("mfl_map_has(%s, %s, %s)", args[0], ik, sk), nil
	case "delete":
		ik, sk := g.mapKeyArgs(ex.Args[0], args[1])
		return fmt.Sprintf("mfl_map_del(%s, %s, %s)", args[0], ik, sk), nil
	case "keys":
		return fmt.Sprintf("mfl_map_keys(%s)", args[0]), nil
	case "json":
		name, err := g.jsonSerializer(g.c.TypeString(g.curFn, ex.Args[0]))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", name, args[0]), nil
	case "parse":
		// the witness (Args[1]) supplies the target type; its value is unused
		name, err := g.jsonParser(g.c.TypeString(g.curFn, ex.Args[1]))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("({ const char* _p = (%s); %s(&_p); })", args[0], name), nil
	case "http_body":
		return fmt.Sprintf("mfl_http_body(%s)", args[0]), nil
	case "to_upper":
		return fmt.Sprintf("mfl_to_upper(%s)", args[0]), nil
	case "to_lower":
		return fmt.Sprintf("mfl_to_lower(%s)", args[0]), nil
	case "trim":
		return fmt.Sprintf("mfl_trim(%s)", args[0]), nil
	case "contains":
		return fmt.Sprintf("mfl_contains(%s, %s)", args[0], args[1]), nil
	case "has_prefix":
		return fmt.Sprintf("mfl_has_prefix(%s, %s)", args[0], args[1]), nil
	case "has_suffix":
		return fmt.Sprintf("mfl_has_suffix(%s, %s)", args[0], args[1]), nil
	case "index":
		return fmt.Sprintf("mfl_index(%s, %s)", args[0], args[1]), nil
	case "substr":
		return fmt.Sprintf("mfl_substr(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "charat":
		return fmt.Sprintf("mfl_charat(%s, %s)", args[0], args[1]), nil
	case "replace":
		return fmt.Sprintf("mfl_replace(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "split":
		return fmt.Sprintf("mfl_split(%s, %s)", args[0], args[1]), nil
	case "join":
		return fmt.Sprintf("mfl_join(%s, %s)", args[0], args[1]), nil
	case "append":
		// &((T[1]){v})[0] yields a T* for any element type, including structs
		// (a plain &(T){v} would mis-init an aggregate field-by-field).
		ct := g.c.ElemCType(g.curFn, ex.Args[0])
		return fmt.Sprintf("mfl_append(%s, &((%s[1]){%s})[0], sizeof(%s))", args[0], ct, args[1], ct), nil
	case "sleep":
		return fmt.Sprintf("mfl_sleep(%s)", args[0]), nil
	case "exit":
		return fmt.Sprintf("mfl_exit(%s)", args[0]), nil
	case "flush":
		return "mfl_flush()", nil
	case "raw_mode":
		g.usesTTY = true
		return fmt.Sprintf("mfl_raw_mode(%s)", args[0]), nil
	case "read_key":
		g.usesTTY = true
		return "mfl_read_key()", nil
	case "base64_encode":
		return fmt.Sprintf("mfl_base64_encode(%s)", args[0]), nil
	case "base64_decode":
		return fmt.Sprintf("mfl_base64_decode(%s)", args[0]), nil
	case "base64_encode_bytes":
		return fmt.Sprintf("mfl_base64_encode_bytes(%s)", args[0]), nil
	case "base64_decode_bytes":
		return fmt.Sprintf("mfl_base64_decode_bytes(%s)", args[0]), nil
	case "url_encode":
		return fmt.Sprintf("mfl_url_encode(%s)", args[0]), nil
	case "url_decode":
		return fmt.Sprintf("mfl_url_decode(%s)", args[0]), nil
	case "sha256":
		return fmt.Sprintf("mfl_sha256(%s)", args[0]), nil
	case "hmac_sha256":
		return fmt.Sprintf("mfl_hmac_sha256(%s, %s)", args[0], args[1]), nil
	case "sqlite_open":
		g.usesSQLite = true
		return fmt.Sprintf("mfl_sqlite_open(%s)", args[0]), nil
	case "sqlite_exec":
		g.usesSQLite = true
		if len(args) == 3 {
			return fmt.Sprintf("mfl_sqlite_exec_p(%s, %s, %s)", args[0], args[1], args[2]), nil
		}
		return fmt.Sprintf("mfl_sqlite_exec(%s, %s)", args[0], args[1]), nil
	case "sqlite_query":
		g.usesSQLite = true
		if len(args) == 3 {
			return fmt.Sprintf("mfl_sqlite_query_p(%s, %s, %s)", args[0], args[1], args[2]), nil
		}
		return fmt.Sprintf("mfl_sqlite_query(%s, %s)", args[0], args[1]), nil
	case "sqlite_close":
		g.usesSQLite = true
		return fmt.Sprintf("mfl_sqlite_close(%s)", args[0]), nil
	case "regex_match":
		g.usesRegex = true
		return fmt.Sprintf("mfl_regex_match(%s, %s)", args[0], args[1]), nil
	case "regex_find":
		g.usesRegex = true
		return fmt.Sprintf("mfl_regex_find(%s, %s)", args[0], args[1]), nil
	case "regex_replace":
		g.usesRegex = true
		return fmt.Sprintf("mfl_regex_replace(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "regex_groups":
		g.usesRegex = true
		return fmt.Sprintf("mfl_regex_groups(%s, %s)", args[0], args[1]), nil
	case "str":
		switch g.c.NodeKind(g.curFn, ex.Args[0]) {
		case KFloat:
			return fmt.Sprintf("mfl_str_d(%s)", args[0]), nil
		case KBool:
			return fmt.Sprintf("mfl_str_b(%s)", args[0]), nil
		case KString:
			return args[0], nil
		}
		return fmt.Sprintf("mfl_str_i(%s)", args[0]), nil
	case "int":
		return fmt.Sprintf("((int64_t)(%s))", args[0]), nil
	case "float":
		return fmt.Sprintf("((double)(%s))", args[0]), nil
	case "f64_bits":
		return fmt.Sprintf("mfl_f64_bits((double)(%s))", args[0]), nil
	case "f64_from_bits":
		return fmt.Sprintf("mfl_f64_from_bits((int64_t)(%s))", args[0]), nil
	case "sin", "cos", "tan", "asin", "acos", "atan", "exp", "log", "log2", "log10", "sqrt", "cbrt", "floor", "ceil", "round", "trunc", "abs":
		g.usesMath = true
		fn := ex.Callee
		if fn == "abs" {
			fn = "fabs"
		}
		return fmt.Sprintf("mfl_math_%s((double)(%s))", fn, args[0]), nil
	case "pow", "atan2", "fmod", "hypot":
		g.usesMath = true
		return fmt.Sprintf("mfl_math_%s((double)(%s), (double)(%s))", ex.Callee, args[0], args[1]), nil
	case "pi":
		g.usesMath = true
		return "mfl_math_pi()", nil
	case "noise2":
		g.usesNoise = true
		return fmt.Sprintf("mfl_noise2((double)(%s), (double)(%s))", args[0], args[1]), nil
	case "noise3":
		g.usesNoise = true
		return fmt.Sprintf("mfl_noise3((double)(%s), (double)(%s), (double)(%s))", args[0], args[1], args[2]), nil
	case "alloc":
		return fmt.Sprintf("mfl_raw_alloc(%s)", args[0]), nil
	case "free":
		return fmt.Sprintf("mfl_raw_free(%s)", args[0]), nil
	case "madvise_free":
		return fmt.Sprintf("mfl_madvise_free(%s, %s)", args[0], args[1]), nil
	case "poke_f32", "poke_i32", "poke_u8", "poke_u16", "poke_ptr":
		return fmt.Sprintf("mfl_%s(%s, %s, %s)", ex.Callee, args[0], args[1], args[2]), nil
	case "peek_f32", "peek_i32", "peek_i8", "peek_u8":
		return fmt.Sprintf("mfl_%s(%s, %s)", ex.Callee, args[0], args[1]), nil
	case "dot_i8":
		return fmt.Sprintf("mfl_dot_i8(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "dot_q8":
		return fmt.Sprintf("mfl_dot_q8(%s, %s, %s, %s, %s, %s)", args[0], args[1], args[2], args[3], args[4], args[5]), nil
	case "matmul_q8_batch":
		return fmt.Sprintf("mfl_matmul_q8_batch(%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)", args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7], args[8], args[9], args[10]), nil
	case "dot_q4":
		return fmt.Sprintf("mfl_dot_q4(%s, %s, %s, %s, %s, %s)", args[0], args[1], args[2], args[3], args[4], args[5]), nil
	case "dot_f32":
		return fmt.Sprintf("mfl_dot_f32(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "axpy_f32":
		return fmt.Sprintf("mfl_axpy_f32(%s, %s, %s, %s)", args[0], args[1], args[2], args[3]), nil
	case "ptr_str":
		return fmt.Sprintf("mfl_ptr_str(%s)", args[0]), nil
	case "dial":
		g.usesNet = true
		return fmt.Sprintf("mfl_dial(%s, %s)", args[0], args[1]), nil
	case "listen":
		g.usesNet = true
		return fmt.Sprintf("mfl_listen(%s)", args[0]), nil
	case "accept":
		g.usesNet = true
		return fmt.Sprintf("mfl_accept(%s)", args[0]), nil
	case "peer_addr":
		g.usesNet = true
		return fmt.Sprintf("mfl_peer_addr(%s)", args[0]), nil
	case "socket_timeout":
		g.usesNet = true
		return fmt.Sprintf("mfl_socket_timeout(%s, %s)", args[0], args[1]), nil
	case "read":
		g.usesNet = true
		return fmt.Sprintf("mfl_read(%s)", args[0]), nil
	case "read_bytes":
		g.usesNet = true
		return fmt.Sprintf("mfl_read_bytes(%s)", args[0]), nil
	case "write":
		g.usesNet = true
		return fmt.Sprintf("mfl_write(%s, %s)", args[0], args[1]), nil
	case "close":
		if g.c.NodeKind(g.curFn, ex.Args[0]) == KChan {
			return fmt.Sprintf("mfl_chan_close(%s)", args[0]), nil
		}
		g.usesNet = true
		return fmt.Sprintf("mfl_close(%s)", args[0]), nil
	case "input":
		return "mfl_input()", nil
	case "read_stdin":
		return "mfl_read_stdin()", nil
	case "args":
		return "mfl_args()", nil
	case "env":
		return fmt.Sprintf("mfl_env(%s)", args[0]), nil
	case "now":
		return "mfl_now()", nil
	case "now_ms":
		return "mfl_now_ms()", nil
	case "time_fields":
		return fmt.Sprintf("mfl_time_fields(%s)", args[0]), nil
	case "time_format":
		return fmt.Sprintf("mfl_time_format(%s, %s)", args[0], args[1]), nil
	case "time_format_utc":
		return fmt.Sprintf("mfl_time_format_utc(%s, %s)", args[0], args[1]), nil
	case "time_make":
		return fmt.Sprintf("mfl_time_make(%s, %s, %s, %s, %s, %s)", args[0], args[1], args[2], args[3], args[4], args[5]), nil
	case "parse_int":
		return fmt.Sprintf("mfl_parse_int(%s)", args[0]), nil
	case "parse_float":
		return fmt.Sprintf("mfl_parse_float(%s)", args[0]), nil
	case "read_file":
		return fmt.Sprintf("mfl_read_file(%s)", args[0]), nil
	case "read_file_bytes":
		return fmt.Sprintf("mfl_read_file_bytes(%s)", args[0]), nil
	case "write_bytes":
		g.usesNet = true
		return fmt.Sprintf("mfl_write_bytes(%s, %s)", args[0], args[1]), nil
	case "write_file":
		return fmt.Sprintf("mfl_write_file(%s, %s)", args[0], args[1]), nil
	case "write_file_bytes":
		return fmt.Sprintf("mfl_write_file_bytes(%s, %s)", args[0], args[1]), nil
	case "write_file_raw":
		return fmt.Sprintf("mfl_write_file_raw(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "read_file_raw":
		return fmt.Sprintf("mfl_read_file_raw(%s, %s, %s)", args[0], args[1], args[2]), nil
	case "remove":
		return fmt.Sprintf("mfl_remove_file(%s)", args[0]), nil
	case "list_dir":
		return fmt.Sprintf("mfl_list_dir(%s)", args[0]), nil
	case "system":
		return fmt.Sprintf("mfl_system(%s)", args[0]), nil
	case "mkdir":
		return fmt.Sprintf("mfl_mkdir(%s)", args[0]), nil
	case "https_get":
		g.usesTLS = true
		return fmt.Sprintf("mfl_https_get(%s)", args[0]), nil
	case "https_post":
		g.usesTLS = true
		return fmt.Sprintf("mfl_https_post(%s, %s)", args[0], args[1]), nil
	case "wss_open":
		g.usesWSS = true
		return fmt.Sprintf("mfl_wss_open(%s)", args[0]), nil
	case "wss_send":
		g.usesWSS = true
		return fmt.Sprintf("mfl_wss_send(%s, %s)", args[0], args[1]), nil
	case "wss_recv":
		g.usesWSS = true
		return fmt.Sprintf("mfl_wss_recv(%s)", args[0]), nil
	case "wss_send_bin":
		g.usesWSS = true
		return fmt.Sprintf("mfl_wss_send_bin(%s, %s)", args[0], args[1]), nil
	case "wss_recv_bin":
		g.usesWSS = true
		return fmt.Sprintf("mfl_wss_recv_bin(%s)", args[0]), nil
	case "wss_close":
		g.usesWSS = true
		return fmt.Sprintf("mfl_wss_close(%s)", args[0]), nil
	case "tls_client_fd":
		g.usesTLS = true
		return fmt.Sprintf("mfl_tls_client_fd(%s, %s)", args[0], args[1]), nil
	case "tls_server_ctx":
		g.usesTLS = true
		return fmt.Sprintf("mfl_tls_server_ctx(%s, %s)", args[0], args[1]), nil
	case "tls_accept":
		g.usesTLS = true
		return fmt.Sprintf("mfl_tls_accept(%s, %s)", args[0], args[1]), nil
	case "tls_read":
		g.usesTLS = true
		return fmt.Sprintf("mfl_tls_read_str(%s)", args[0]), nil
	case "tls_read_bytes":
		g.usesTLS = true
		return fmt.Sprintf("mfl_tls_read_bytes_h(%s)", args[0]), nil
	case "tls_write":
		g.usesTLS = true
		return fmt.Sprintf("mfl_tls_write_str(%s, %s)", args[0], args[1]), nil
	case "tls_write_bytes":
		g.usesTLS = true
		return fmt.Sprintf("mfl_tls_write_bytes_h(%s, %s)", args[0], args[1]), nil
	case "tls_close":
		g.usesTLS = true
		return fmt.Sprintf("mfl_tls_close_h(%s)", args[0]), nil
	case "print", "println":
		return "", fmt.Errorf("print/println may only be used as a statement")
	}
	if !g.c.IsTopFunc(ex.Callee) {
		// a function-valued local variable, called by name
		params, ret := g.c.VarFuncSig(g.curFn, ex.Callee)
		return g.closureCall(g.varRef(ex.Callee), params, ret, args), nil
	}
	cname := g.c.CalleeCName(g.curFn, ex)
	inst := g.c.CalleeInst(g.curFn, ex)
	if g.c.SrcFunc(inst).Variadic {
		nfixed := len(g.c.SrcFunc(inst).Params) - 1
		call := append([]string{}, args[:nfixed]...)
		if ex.Spread {
			call = append(call, args[nfixed]) // pass the spread slice directly
		} else {
			// build a slice from the trailing arguments
			ect := g.c.ParamElemCType(inst, nfixed)
			id := g.tmpID
			g.tmpID++
			var b strings.Builder
			fmt.Fprintf(&b, "({ mfl_slice _v%d = {0};", id)
			for _, a := range args[nfixed:] {
				fmt.Fprintf(&b, " _v%d = mfl_append(_v%d, &((%s[1]){%s})[0], sizeof(%s));", id, id, ect, a, ect)
			}
			fmt.Fprintf(&b, " _v%d; })", id)
			call = append(call, b.String())
		}
		return fmt.Sprintf("%s(%s)", cname, strings.Join(call, ", ")), nil
	}
	return fmt.Sprintf("%s(%s)", cname, strings.Join(args, ", ")), nil
}

// closureCall invokes a function value: cast its fn pointer to the right
// signature and pass the environment as the leading argument.
func (g *cgen) closureCall(clos string, paramCTypes []string, retCType string, args []string) string {
	id := g.tmpID
	g.tmpID++
	fnSig := "(void*"
	for _, p := range paramCTypes {
		fnSig += ", " + p
	}
	fnSig += ")"
	callArgs := fmt.Sprintf("_c%d.env", id)
	for _, a := range args {
		callArgs += ", " + a
	}
	return fmt.Sprintf("({ mfl_closure _c%d = %s; ((%s(*)%s)_c%d.fn)(%s); })", id, clos, retCType, fnSig, id, callArgs)
}
