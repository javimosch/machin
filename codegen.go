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
	liftClosures(p) // idempotent: a no-op once literals are already lifted
	c, err := Check(p)
	if err != nil {
		return "", err
	}
	g := &cgen{c: c, safe: safe, jsonMemo: map[string]string{}, parseMemo: map[string]string{}}
	return g.program(p)
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
	c        *Checker
	buf      strings.Builder // function bodies
	tramp    strings.Builder // goroutine trampolines
	goID     int
	jsonFns   strings.Builder   // generated per-type JSON serializers + parsers
	jsonMemo  map[string]string // type string -> serializer function name
	parseMemo map[string]string // type string -> parser function name
	jsonID    int
	rangeID   int    // unique temp names for for-range loops
	tmpID     int    // unique temp names for multi-assignment
	arenaID   int    // unique temp names for scoped-arena blocks
	curFn     string // name of the function currently being emitted
	safe      bool   // emit runtime bounds / div-by-zero / overflow checks
	usesTLS   bool   // program calls https_get/https_post -> emit + link OpenSSL
	usesWSS   bool   // program calls wss_* -> emit WebSocket runtime + link OpenSSL
}

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
#include <errno.h>

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
static void* mfl_calloc(size_t n, size_t sz) { void* p = mfl_alloc(n * sz); memset(p, 0, n * sz); return p; }
static void* mfl_realloc(void* old, size_t sz) {
    void* p = mfl_alloc(sz);
    if (old) { size_t o = ((mfl_blk*)old - 1)->size; memcpy(p, old, o < sz ? o : sz); }
    return p; /* old reclaimed with its arena */
}
static void mfl_arena_free(mfl_arena* a) {
    mfl_blk* b = a->head;
    while (b) { mfl_blk* n = b->next; free(b); b = n; }
    a->head = NULL;
}

/* --safe runtime checks (used only when the program is built with --safe) */
static void mfl_panic(const char* msg) { fputs("panic: ", stderr); fputs(msg, stderr); fputc('\n', stderr); exit(1); }
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
static void mfl_sleep(int64_t ms) {
    struct timespec ts = { ms / 1000, (ms % 1000) * 1000000L };
    nanosleep(&ts, NULL);
}

/* channels: a mutex + condvar FIFO carrying fixed-size elements.
   stroff[] holds the byte offsets of every string (char*) inside an element, so
   a sent value's strings can be deep-copied out of the (possibly short-lived)
   sender arena on send and adopted into the receiver arena on receive. Without
   this, the channel copies only the char*, dangling once the sender's goroutine
   arena is reclaimed. */
static char* mfl_dup_arena(const char* s, size_t n); /* defined later in the runtime */
typedef struct mfl_cnode { struct mfl_cnode* next; void* data; } mfl_cnode;
typedef struct {
    pthread_mutex_t mu; pthread_cond_t cnd;
    mfl_cnode *head, *tail; int64_t es; int closed;
    int nstr; int* stroff;
} mfl_chan;
static mfl_chan* mfl_make_chan(int64_t es, int nstr, ...) {
    mfl_chan* c = malloc(sizeof(mfl_chan));
    pthread_mutex_init(&c->mu, NULL); pthread_cond_init(&c->cnd, NULL);
    c->head = c->tail = NULL; c->es = es; c->closed = 0;
    c->nstr = nstr; c->stroff = NULL;
    if (nstr > 0) {
        c->stroff = (int*)malloc((size_t)nstr * sizeof(int));
        va_list ap; va_start(ap, nstr);
        for (int i = 0; i < nstr; i++) c->stroff[i] = va_arg(ap, int);
        va_end(ap);
    }
    return c;
}
/* freeze: replace each string field with a stable malloc'd copy (the value is
   being handed off to the channel, away from the sender's arena). */
static void mfl_chan_freeze(mfl_chan* c, void* elem) {
    for (int i = 0; i < c->nstr; i++) {
        char** p = (char**)((char*)elem + c->stroff[i]);
        if (*p) { size_t n = strlen(*p); char* d = (char*)malloc(n + 1); memcpy(d, *p, n + 1); *p = d; }
    }
}
/* thaw: move each frozen string into the receiver's arena, freeing the malloc'd
   copy. After this the value's strings live exactly as long as the receiver. */
static void mfl_chan_thaw(mfl_chan* c, void* elem) {
    for (int i = 0; i < c->nstr; i++) {
        char** p = (char**)((char*)elem + c->stroff[i]);
        if (*p) { char* a = mfl_dup_arena(*p, strlen(*p)); free(*p); *p = a; }
    }
}
/* close a channel: receivers drain the buffer then get "not ok". Wakes every
   blocked receiver so range/recv stop instead of hanging forever. */
static void mfl_chan_close(mfl_chan* c) {
    pthread_mutex_lock(&c->mu);
    c->closed = 1;
    pthread_cond_broadcast(&c->cnd);
    pthread_mutex_unlock(&c->mu);
}
static void mfl_chan_send(mfl_chan* c, const void* v) {
    mfl_cnode* n = malloc(sizeof(mfl_cnode));
    n->data = malloc(c->es); memcpy(n->data, v, c->es);
    mfl_chan_freeze(c, n->data);
    n->next = NULL;
    pthread_mutex_lock(&c->mu);
    if (c->tail) c->tail->next = n; else c->head = n;
    c->tail = n;
    pthread_cond_signal(&c->cnd);
    pthread_mutex_unlock(&c->mu);
}
/* blocking receive with ok: 1 and fills out if a value arrived; 0 if the channel
   is closed and drained (out left untouched). The primitive behind range-over-
   channel and the comma-ok receive. */
static int mfl_chan_recv2(mfl_chan* c, void* out) {
    pthread_mutex_lock(&c->mu);
    while (!c->head && !c->closed) pthread_cond_wait(&c->cnd, &c->mu);
    mfl_cnode* n = c->head;
    if (!n) { pthread_mutex_unlock(&c->mu); return 0; }
    c->head = n->next;
    if (!c->head) c->tail = NULL;
    pthread_mutex_unlock(&c->mu);
    memcpy(out, n->data, c->es);
    mfl_chan_thaw(c, out);
    free(n->data); free(n);
    return 1;
}
static void mfl_chan_recv(mfl_chan* c, void* out) {
    if (!mfl_chan_recv2(c, out)) memset(out, 0, c->es);
}
/* non-blocking receive: 1 and fills out if an element was ready, else 0. The
   primitive behind select's poll over multiple channels. */
static int mfl_chan_tryrecv(mfl_chan* c, void* out) {
    pthread_mutex_lock(&c->mu);
    mfl_cnode* n = c->head;
    if (n) { c->head = n->next; if (!c->head) c->tail = NULL; }
    pthread_mutex_unlock(&c->mu);
    if (!n) return 0;
    memcpy(out, n->data, c->es);
    mfl_chan_thaw(c, out);
    free(n->data); free(n);
    return 1;
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
static void mfl_map_set(mfl_map* m, int64_t ik, const char* sk, const void* val) {
    mfl_ment** pp = mfl_map_at(m, ik, sk);
    if (*pp) { memcpy((*pp)->val, val, m->vs); return; }
    mfl_ment* e = malloc(sizeof(mfl_ment)); e->next=NULL; e->ik=ik; e->sk=NULL;
    if (m->sk) { e->sk = malloc(strlen(sk)+1); strcpy(e->sk, sk); }
    e->val = malloc(m->vs); memcpy(e->val, val, m->vs);
    *pp = e; m->count++;
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

static char* mfl_cat(const char* a, const char* b) {
    size_t la = strlen(a), lb = strlen(b);
    char* r = mfl_alloc(la + lb + 1);
    memcpy(r, a, la); memcpy(r + la, b, lb); r[la + lb] = 0;
    return r;
}
static char* mfl_str_i(int64_t v) { char* b = mfl_alloc(24); snprintf(b, 24, "%lld", (long long)v); return b; }
static char* mfl_str_d(double v)  { char* b = mfl_alloc(32); snprintf(b, 32, "%g", v); return b; }
static char* mfl_dup(const char* s) { size_t n = strlen(s); char* r = mfl_alloc(n+1); memcpy(r, s, n+1); return r; }
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
static char* mfl_js_str(const char** p) {
    mfl_js_ws(p);
    if (**p != '"') return mfl_dup("");
    (*p)++;
    char* out = mfl_alloc(strlen(*p) + 1); size_t j = 0;
    while (**p && **p != '"') {
        char c = **p;
        if (c == '\\') {
            (*p)++; char e = **p;
            if (e=='n') out[j++]='\n'; else if (e=='t') out[j++]='\t'; else if (e=='r') out[j++]='\r';
            else out[j++] = e;
        } else out[j++] = c;
        (*p)++;
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
    int64_t n = strlen(s);
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
    char* r = mfl_dup(((char**)xs.data)[0]);
    for (int64_t i = 1; i < xs.len; i++) { r = mfl_cat(r, sep); r = mfl_cat(r, ((char**)xs.data)[i]); }
    return r;
}

/* networking: the low-level shape of Go's net package */
static int64_t mfl_listen(int64_t port) {
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    int opt = 1; setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
    struct sockaddr_in a; memset(&a, 0, sizeof(a));
    a.sin_family = AF_INET; a.sin_addr.s_addr = INADDR_ANY; a.sin_port = htons((uint16_t)port);
    if (bind(fd, (struct sockaddr*)&a, sizeof(a)) < 0) { perror("bind"); exit(1); }
    if (listen(fd, 64) < 0) { perror("listen"); exit(1); }
    return fd;
}
static int64_t mfl_accept(int64_t fd) { return accept((int)fd, NULL, NULL); }
/* dial: connect a TCP socket to host:port, returning an fd (-1 on failure).
   The fd is used with the same read/write/close as an accepted connection. */
static int64_t mfl_dial(const char* host, int64_t port) {
    struct addrinfo hints, *res, *rp;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_UNSPEC; hints.ai_socktype = SOCK_STREAM;
    char ps[16]; snprintf(ps, sizeof(ps), "%lld", (long long)port);
    if (getaddrinfo(host, ps, &hints, &res) != 0) return -1;
    int fd = -1;
    for (rp = res; rp; rp = rp->ai_next) {
        fd = socket(rp->ai_family, rp->ai_socktype, rp->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, rp->ai_addr, rp->ai_addrlen) == 0) break;
        close(fd); fd = -1;
    }
    freeaddrinfo(res);
    return fd;
}
static char* mfl_read(int64_t fd) {
    char* buf = mfl_alloc(65536);
    ssize_t n = read((int)fd, buf, 65535);
    if (n < 0) n = 0;
    buf[n] = 0;
    return buf;
}
static int64_t mfl_write(int64_t fd, const char* s) { return (int64_t)write((int)fd, s, strlen(s)); }
static void mfl_close(int64_t fd) { close((int)fd); }

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

/* command-line arguments, environment, and wall-clock time */
static int mfl_argc = 0;
static char** mfl_argv = NULL;
static mfl_slice mfl_args(void) {
    mfl_slice s = { mfl_argc ? mfl_alloc(mfl_argc * sizeof(char*)) : NULL, mfl_argc, mfl_argc };
    for (int i = 0; i < mfl_argc; i++) ((char**)s.data)[i] = mfl_argv[i];
    return s;
}
static char* mfl_env(const char* k) { char* v = getenv(k); return v ? v : ""; }
static int64_t mfl_now(void) { return (int64_t)time(NULL); }
static int64_t mfl_now_ms(void) { struct timeval tv; gettimeofday(&tv, NULL); return (int64_t)tv.tv_sec * 1000 + tv.tv_usec / 1000; }
static int64_t mfl_parse_int(const char* s) { return (int64_t)strtoll(s, NULL, 10); }

/* file system: read/write whole files, list a directory, make a directory */
static char* mfl_read_file(const char* path) {
    FILE* f = fopen(path, "rb");
    if (!f) return mfl_dup("");
    fseek(f, 0, SEEK_END); long n = ftell(f); fseek(f, 0, SEEK_SET);
    if (n < 0) n = 0;
    char* buf = mfl_alloc((size_t)n + 1);
    size_t r = fread(buf, 1, (size_t)n, f); buf[r] = 0;
    fclose(f); return buf;
}
static int64_t mfl_write_file(const char* path, const char* content) {
    FILE* f = fopen(path, "wb");
    if (!f) return -1;
    size_t len = strlen(content);
    size_t w = fwrite(content, 1, len, f);
    fclose(f); return (int64_t)w;
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

// tlsCoreRuntime holds the shared OpenSSL plumbing (a verified TLS dial) used by
// both the HTTPS client and the WebSocket client. Emitted whenever a program
// uses native TLS (https_* or wss_*); linked against -lssl -lcrypto. A single
// process-global SSL_CTX is shared across all connections (OpenSSL makes SSL_new
// on a shared CTX thread-safe), so per-connection setup is just SSL_new+connect.
const tlsCoreRuntime = `#include <openssl/ssl.h>
#include <openssl/err.h>
#include <openssl/rand.h>

static SSL_CTX* mfl_ssl_ctx(void) {
    static SSL_CTX* ctx = NULL;
    static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
    pthread_mutex_lock(&mu);
    if (!ctx) {
        ctx = SSL_CTX_new(TLS_client_method());
        if (ctx) SSL_CTX_set_default_verify_paths(ctx);
    }
    pthread_mutex_unlock(&mu);
    return ctx;
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
    SSL_CTX* ctx = mfl_ssl_ctx();
    if (!ctx) { if (stage) *stage = "tls"; close(fd); return NULL; }
    SSL* ssl = SSL_new(ctx);
    SSL_set_fd(ssl, fd);
    SSL_set_tlsext_host_name(ssl, host);
    SSL_set1_host(ssl, host);
    SSL_set_verify(ssl, SSL_VERIFY_PEER, NULL);
    if (SSL_connect(ssl) != 1) { if (stage) *stage = "tls"; SSL_free(ssl); close(fd); return NULL; }
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

static mfl_http_result mfl_http_do(const char* method, const char* url, const char* reqbody, const char* ctype, int redirects);

static mfl_http_result mfl_http_do(const char* method, const char* url, const char* reqbody, const char* ctype, int redirects) {
    mfl_http_result R;
    R.status = 0;
    R.body = mfl_dup_arena("", 0);
    R.err = mfl_dup_arena("", 0);

    char host[256] = {0}, path[2048] = {0};
    int port = 443;
    const char* p = url;
    if (strncmp(p, "https://", 8) == 0) p += 8;
    else if (strncmp(p, "http://", 7) == 0) { R.err = mfl_dup_arena("scheme", 6); return R; } /* TLS only */
    /* else: no scheme — assume https */
    int i = 0;
    while (*p && *p != '/' && *p != ':' && i < 255) host[i++] = *p++;
    host[i] = 0;
    if (*p == ':') { p++; port = atoi(p); while (*p && *p != '/') p++; }
    if (*p == '/') strncpy(path, p, sizeof(path) - 1);
    else { path[0] = '/'; path[1] = 0; }

    const char* stage = "connect";
    SSL* ssl = mfl_tls_dial_e(host, port, &stage);
    if (!ssl) { R.err = mfl_dup_arena(stage, strlen(stage)); return R; }

    size_t blen = reqbody ? strlen(reqbody) : 0;
    size_t reqcap = blen + strlen(path) + strlen(host) + 256 + (ctype ? strlen(ctype) : 0);
    char* req = (char*)malloc(reqcap);
    int rl;
    if (blen > 0) {
        rl = snprintf(req, reqcap,
            "%s %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: machin/0.8\r\nAccept: */*\r\nContent-Type: %s\r\nContent-Length: %zu\r\nConnection: close\r\n\r\n",
            method, path, host, ctype ? ctype : "application/octet-stream", blen);
        memcpy(req + rl, reqbody, blen);
        rl += (int)blen;
    } else {
        rl = snprintf(req, reqcap,
            "%s %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: machin/0.8\r\nAccept: */*\r\nConnection: close\r\n\r\n",
            method, path, host);
    }
    SSL_write(ssl, req, rl);
    free(req);

    size_t rlen;
    char* raw = mfl_tls_readall(ssl, &rlen);
    mfl_tls_hangup(ssl);

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
                return mfl_http_do(m, locurl, rb, ctype, redirects - 1);
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

static char* mfl_https_get(const char* url) { return mfl_http_do("GET", url, NULL, NULL, 5).body; }
static char* mfl_https_post(const char* url, const char* body) { return mfl_http_do("POST", url, body, "application/json", 5).body; }
static mfl_http_result mfl_http_get(const char* url) { return mfl_http_do("GET", url, NULL, NULL, 5); }
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

static int64_t mfl_wss_open(const char* url) {
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

static int64_t mfl_wss_send(int64_t h, const char* msg) {
    mfl_ws* w = (mfl_ws*)(intptr_t)h;
    if (!w) return -1;
    mfl_ws_frame(w, 0x1, (const unsigned char*)msg, strlen(msg));
    return 0;
}

/* block until a full text/binary message arrives; "" on close or error.
   Replies to pings, ignores pongs, and reassembles fragmented messages. */
static char* mfl_wss_recv(int64_t h) {
    mfl_ws* w = (mfl_ws*)(intptr_t)h;
    if (!w) return mfl_dup_arena("", 0);
    unsigned char* msg = NULL;
    size_t mlen = 0;
    for (;;) {
        unsigned char hd[2];
        if (mfl_ssl_read_n(w->ssl, hd, 2) < 0) { free(msg); return mfl_dup_arena("", 0); }
        int fin = hd[0] & 0x80;
        int opcode = hd[0] & 0x0f;
        int masked = hd[1] & 0x80;
        uint64_t len = hd[1] & 0x7f;
        if (len == 126) {
            unsigned char e[2];
            if (mfl_ssl_read_n(w->ssl, e, 2) < 0) { free(msg); return mfl_dup_arena("", 0); }
            len = ((uint64_t)e[0] << 8) | e[1];
        } else if (len == 127) {
            unsigned char e[8];
            if (mfl_ssl_read_n(w->ssl, e, 8) < 0) { free(msg); return mfl_dup_arena("", 0); }
            len = 0;
            for (int s = 0; s < 8; s++) len = (len << 8) | e[s];
        }
        unsigned char mk[4] = {0,0,0,0};
        if (masked && mfl_ssl_read_n(w->ssl, mk, 4) < 0) { free(msg); return mfl_dup_arena("", 0); }
        unsigned char* pl = (unsigned char*)malloc(len ? len : 1);
        if (len && mfl_ssl_read_n(w->ssl, pl, len) < 0) { free(pl); free(msg); return mfl_dup_arena("", 0); }
        if (masked) for (uint64_t k = 0; k < len; k++) pl[k] ^= mk[k & 3];

        if (opcode == 0x9) { mfl_ws_frame(w, 0xA, pl, len); free(pl); continue; } /* ping -> pong */
        if (opcode == 0xA) { free(pl); continue; }                                /* pong */
        if (opcode == 0x8) { mfl_ws_frame(w, 0x8, pl, len); free(pl); free(msg); return mfl_dup_arena("", 0); } /* close */

        unsigned char* nm = (unsigned char*)realloc(msg, mlen + len);
        msg = nm;
        if (len) memcpy(msg + mlen, pl, len);
        mlen += len;
        free(pl);
        if (fin) { char* r = mfl_dup_arena((char*)msg, mlen); free(msg); return r; }
    }
}

static int64_t mfl_wss_close(int64_t h) {
    mfl_ws* w = (mfl_ws*)(intptr_t)h;
    if (!w) return -1;
    mfl_ws_frame(w, 0x8, NULL, 0);
    mfl_tls_hangup(w->ssl);
    free(w);
    return 0;
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

// externCType is the C type used in a foreign prototype: a scalar's C type, the
// cstruct's own C name, or void.
func externCType(t string) string {
	if t == "" {
		return "void"
	}
	if isFFIScalar(t) {
		return ffiCType(t)
	}
	return t // a cstruct: its C struct name
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
	case KSlice, KStruct, KFunc:
		return "{0}"
	default:
		return "0"
	}
}

func (g *cgen) program(p *Program) (string, error) {
	// emit one function body per instance (monomorphization); this also fills
	// g.tramp via any go statements.
	for _, inst := range g.c.Reps() {
		if err := g.function(inst); err != nil {
			return "", err
		}
	}
	var out strings.Builder
	out.WriteString(cRuntime)
	out.WriteByte('\n')
	if g.usesTLS || g.usesWSS {
		// OpenSSL plumbing — emitted (and linked) only when a program uses native
		// TLS (https_* or wss_*), so TLS-free programs stay libc-only.
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
			fmt.Fprintf(&out, "extern %s %s(%s);\n", externCType(ef.Ret), ef.Name, ps)
		}
	}
	// struct typedefs, in declaration order (a struct may reference earlier ones)
	for _, td := range p.Types {
		fmt.Fprintf(&out, "typedef struct {")
		for _, f := range td.Fields {
			fmt.Fprintf(&out, " %s f_%s;", cTypeName(f.Type), f.Name)
		}
		fmt.Fprintf(&out, " } mfl_%s;\n", td.Name)
	}
	// FFI struct marshaling: convert each cstruct between its MFL value (mfl_Name,
	// with int64/double fields) and the C layout (Name) at the boundary.
	for _, ed := range p.Externs {
		for _, cs := range ed.Structs {
			fmt.Fprintf(&out, "static mfl_%s mfl_from_%s(%s c) { return (mfl_%s){", cs.Name, cs.Name, cs.Name, cs.Name)
			for _, f := range cs.Fields {
				fmt.Fprintf(&out, " .f_%s = c.%s,", f.Name, f.Name)
			}
			out.WriteString(" }; }\n")
			fmt.Fprintf(&out, "static %s mfl_to_%s(mfl_%s m) { return (%s){", cs.Name, cs.Name, cs.Name, cs.Name)
			for _, f := range cs.Fields {
				fmt.Fprintf(&out, " .%s = (%s)m.f_%s,", f.Name, ffiCType(f.CType), f.Name)
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
	for _, inst := range g.c.Reps() {
		out.WriteString(g.signature(inst) + ";\n")
	}
	out.WriteByte('\n')
	out.WriteString(g.tramp.String())
	out.WriteString(g.buf.String())
	out.WriteString("int main(int argc, char** argv) { mfl_argc = argc; mfl_argv = argv; mfl_main(); return 0; }\n")
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
			fmt.Fprintf(&g.buf, "    %s* v_%s = mfl_alloc(sizeof(%s)); *v_%s = _arg_%s;\n", ct, name, ct, name, name)
		}
	}
	for _, name := range g.c.Locals(inst) {
		if fn.Boxed[name] {
			ct := g.c.VarCType(inst, name)
			fmt.Fprintf(&g.buf, "    %s* v_%s = mfl_alloc(sizeof(%s)); *v_%s = %s;\n", ct, name, ct, name, cZero(g.c.VarKind(inst, name)))
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
	case *ReturnStmt:
		if len(st.Vals) == 0 {
			// a bare return yields the named return locals (or nothing for void)
			g.emitNamedReturn(g.curFn)
			return nil
		}
		exprs := make([]string, len(st.Vals))
		for i, v := range st.Vals {
			e, err := g.expr(v)
			if err != nil {
				return err
			}
			exprs[i] = "(" + e + ")"
		}
		if len(exprs) == 1 {
			g.buf.WriteString("return " + exprs[0] + ";\n")
		} else {
			fmt.Fprintf(&g.buf, "return (%s_ret){ %s };\n", g.c.CName(g.curFn), strings.Join(exprs, ", "))
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
	case "json_get":
		return "mfl_json_get", "mfl_json_result", []string{"value", "err"}, false, true
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
			fmt.Fprintf(&g.buf, "mfl_chan* _sc%d_%d = %s; %s _sv%d_%d;\n", id, i, ch, et, id, i)
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
	indentC(&g.buf, depth+1)
	g.buf.WriteString("for (;;) {\n")
	for i := range st.Cases {
		sc := &st.Cases[i]
		indentC(&g.buf, depth+2)
		if sc.RecvCh != nil {
			fmt.Fprintf(&g.buf, "if (mfl_chan_tryrecv(_sc%d_%d, &_sv%d_%d)) { _sel%d = %d; break; }\n", id, i, id, i, id, i)
		} else {
			fmt.Fprintf(&g.buf, "{ mfl_chan_send(_sc%d_%d, &_sv%d_%d); _sel%d = %d; break; }\n", id, i, id, i, id, i)
		}
	}
	indentC(&g.buf, depth+2)
	if st.HasDefault {
		fmt.Fprintf(&g.buf, "{ _sel%d = %d; break; }\n", id, len(st.Cases))
	} else {
		g.buf.WriteString("mfl_sleep(1);\n")
	}
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
func (g *cgen) goStmt(st *GoStmt) error {
	id := g.goID
	g.goID++
	inst := g.c.CalleeInst(g.curFn, st.Call)
	cname := g.c.CName(inst)
	n := len(st.Call.Args)

	// arg struct + trampoline (a leading dummy field avoids an empty struct)
	fmt.Fprintf(&g.tramp, "struct mfl_go_%d { char _;", id)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&g.tramp, " %s a%d;", g.c.ParamCType(inst, i), i)
	}
	g.tramp.WriteString(" };\n")
	fmt.Fprintf(&g.tramp, "static void* mfl_go_run_%d(void* p) { mfl_arena _a = {0}; mfl_arena_cur = &_a; struct mfl_go_%d* s = (struct mfl_go_%d*)p; %s(",
		id, id, id, cname)
	for i := 0; i < n; i++ {
		if i > 0 {
			g.tramp.WriteString(", ")
		}
		fmt.Fprintf(&g.tramp, "s->a%d", i)
	}
	g.tramp.WriteString("); free(s); mfl_arena_free(&_a); return NULL; }\n")

	// call site
	g.buf.WriteString("{\n")
	fmt.Fprintf(&g.buf, "        struct mfl_go_%d* s = malloc(sizeof(*s));\n", id)
	for i, a := range st.Call.Args {
		e, err := g.expr(a)
		if err != nil {
			return err
		}
		fmt.Fprintf(&g.buf, "        s->a%d = (%s);\n", i, e)
	}
	fmt.Fprintf(&g.buf, "        pthread_t t; pthread_create(&t, NULL, mfl_go_run_%d, s); pthread_detach(t);\n", id)
	g.buf.WriteString("    }\n")
	return nil
}

// printCall emits one print per argument, with single-space separators, so no
// runtime variadic machinery is needed.
func (g *cgen) printCall(call *Call, depth int) error {
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
		return "(" + ex.Op + x + ")", nil
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
		fmt.Fprintf(&b, "({ %s_env* _e%d = mfl_alloc(sizeof(%s_env));", name, id, name)
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
		offs := g.chanStrOffsets(g.c.ElemTypeString(g.curFn, ex), "")
		if len(offs) == 0 {
			return fmt.Sprintf("mfl_make_chan(sizeof(%s), 0)", ect), nil
		}
		casted := make([]string, len(offs))
		for i, o := range offs {
			casted[i] = "(int)(" + o + ")"
		}
		return fmt.Sprintf("mfl_make_chan(sizeof(%s), %d, %s)", ect, len(offs), strings.Join(casted, ", ")), nil
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

func (g *cgen) binary(ex *Binary) (string, error) {
	l, err := g.expr(ex.L)
	if err != nil {
		return "", err
	}
	r, err := g.expr(ex.R)
	if err != nil {
		return "", err
	}
	if ex.Op == "+" && g.c.NodeKind(g.curFn, ex) == KString {
		return fmt.Sprintf("mfl_cat(%s, %s)", l, r), nil
	}
	// Compare strings by value, not by pointer. C's == on char* compares
	// addresses, so equal-but-distinct strings would wrongly differ.
	if (ex.Op == "==" || ex.Op == "!=") && g.c.NodeKind(g.curFn, ex.L) == KString {
		return fmt.Sprintf("(strcmp(%s, %s) %s 0)", l, r, ex.Op), nil
	}
	// --safe: checked integer arithmetic (overflow) and division (by zero)
	if g.safe && g.c.NodeKind(g.curFn, ex) == KInt {
		switch ex.Op {
		case "+":
			return fmt.Sprintf("mfl_iadd(%s, %s)", l, r), nil
		case "-":
			return fmt.Sprintf("mfl_isub(%s, %s)", l, r), nil
		case "*":
			return fmt.Sprintf("mfl_imul(%s, %s)", l, r), nil
		case "/":
			return fmt.Sprintf("mfl_idiv(%s, %s)", l, r), nil
		case "%":
			return fmt.Sprintf("mfl_imod(%s, %s)", l, r), nil
		}
	}
	return fmt.Sprintf("(%s %s %s)", l, ex.Op, r), nil
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
	parts := []string{strconv.Itoa(len(ex.Elems))}
	for _, el := range ex.Elems {
		e, err := g.expr(el)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("(%s)(%s)", cast, e))
	}
	return builder + "(" + strings.Join(parts, ", ") + ")", nil
}

// structLit emits a C compound literal: (mfl_Point){ .f_x = (1), .f_y = (2) }
// for keyed literals, or positional (mfl_Point){ (1), (2) }.
func (g *cgen) structLit(ex *StructLit) (string, error) {
	parts := make([]string, len(ex.Vals))
	for i, v := range ex.Vals {
		e, err := g.expr(v)
		if err != nil {
			return "", err
		}
		if len(ex.FieldNames) > 0 {
			parts[i] = fmt.Sprintf(".f_%s = (%s)", ex.FieldNames[i], e)
		} else {
			parts[i] = "(" + e + ")"
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("(mfl_%s){0}", ex.Type), nil
	}
	return fmt.Sprintf("(mfl_%s){%s}", ex.Type, strings.Join(parts, ", ")), nil
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
		fmt.Fprintf(&b, "%s out = {0};\n", ct)
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
	args := make([]string, len(ex.Args))
	for i, a := range ex.Args {
		s, err := g.expr(a)
		if err != nil {
			return "", err
		}
		args[i] = s
	}
	switch ex.Callee {
	case "len":
		switch g.c.NodeKind(g.curFn, ex.Args[0]) {
		case KSlice:
			return fmt.Sprintf("((%s).len)", args[0]), nil
		case KMap:
			return fmt.Sprintf("mfl_map_len(%s)", args[0]), nil
		}
		return fmt.Sprintf("((int64_t)strlen(%s))", args[0]), nil
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
	case "str":
		if g.c.NodeKind(g.curFn, ex.Args[0]) == KFloat {
			return fmt.Sprintf("mfl_str_d(%s)", args[0]), nil
		}
		return fmt.Sprintf("mfl_str_i(%s)", args[0]), nil
	case "int":
		return fmt.Sprintf("((int64_t)(%s))", args[0]), nil
	case "dial":
		return fmt.Sprintf("mfl_dial(%s, %s)", args[0], args[1]), nil
	case "listen":
		return fmt.Sprintf("mfl_listen(%s)", args[0]), nil
	case "accept":
		return fmt.Sprintf("mfl_accept(%s)", args[0]), nil
	case "read":
		return fmt.Sprintf("mfl_read(%s)", args[0]), nil
	case "write":
		return fmt.Sprintf("mfl_write(%s, %s)", args[0], args[1]), nil
	case "close":
		if g.c.NodeKind(g.curFn, ex.Args[0]) == KChan {
			return fmt.Sprintf("mfl_chan_close(%s)", args[0]), nil
		}
		return fmt.Sprintf("mfl_close(%s)", args[0]), nil
	case "input":
		return "mfl_input()", nil
	case "args":
		return "mfl_args()", nil
	case "env":
		return fmt.Sprintf("mfl_env(%s)", args[0]), nil
	case "now":
		return "mfl_now()", nil
	case "now_ms":
		return "mfl_now_ms()", nil
	case "parse_int":
		return fmt.Sprintf("mfl_parse_int(%s)", args[0]), nil
	case "read_file":
		return fmt.Sprintf("mfl_read_file(%s)", args[0]), nil
	case "write_file":
		return fmt.Sprintf("mfl_write_file(%s, %s)", args[0], args[1]), nil
	case "list_dir":
		return fmt.Sprintf("mfl_list_dir(%s)", args[0]), nil
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
	case "wss_close":
		g.usesWSS = true
		return fmt.Sprintf("mfl_wss_close(%s)", args[0]), nil
	case "print", "println":
		return "", fmt.Errorf("print/println may only be used as a statement")
	}
	if ef := g.c.ExternFn(ex.Callee); ef != nil {
		// a foreign C function: cast scalar args to their C type and marshal
		// struct args into the C layout; marshal a struct return back to MFL.
		parts := make([]string, len(args))
		for i, a := range args {
			switch pt := ef.Params[i]; {
			case pt == "ptr": // MFL int -> opaque void*
				parts[i] = fmt.Sprintf("(void*)(intptr_t)(%s)", a)
			case isFFIScalar(pt):
				parts[i] = fmt.Sprintf("(%s)(%s)", ffiCType(pt), a)
			default: // a cstruct, marshaled into the C layout
				parts[i] = fmt.Sprintf("mfl_to_%s(%s)", pt, a)
			}
		}
		call := fmt.Sprintf("%s(%s)", ef.Name, strings.Join(parts, ", "))
		switch {
		case ef.Ret == "ptr": // void* -> MFL int
			return fmt.Sprintf("(int64_t)(intptr_t)(%s)", call), nil
		case ef.Ret != "" && !isFFIScalar(ef.Ret):
			return fmt.Sprintf("mfl_from_%s(%s)", ef.Ret, call), nil
		}
		return call, nil
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
