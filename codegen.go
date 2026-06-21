package main

import (
	"fmt"
	"strconv"
	"strings"
)

// CompileToC type-checks the program and emits standalone C99. The C is fed to
// `cc -O2`, so MFL's runtime cost is whatever the C compiler's optimizer
// produces — native, on par with C/Rust/Zig for scalar code.
func CompileToC(p *Program) (string, error) {
	c, err := Check(p)
	if err != nil {
		return "", err
	}
	g := &cgen{c: c, jsonMemo: map[string]string{}, parseMemo: map[string]string{}}
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
	curFn     string // name of the function currently being emitted
}

const cRuntime = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdarg.h>
#include <ctype.h>
#include <unistd.h>
#include <time.h>
#include <pthread.h>
#include <sys/socket.h>
#include <netinet/in.h>

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
static void mfl_sleep(int64_t ms) {
    struct timespec ts = { ms / 1000, (ms % 1000) * 1000000L };
    nanosleep(&ts, NULL);
}

/* channels: a mutex + condvar FIFO carrying fixed-size elements */
typedef struct mfl_cnode { struct mfl_cnode* next; void* data; } mfl_cnode;
typedef struct {
    pthread_mutex_t mu; pthread_cond_t cnd;
    mfl_cnode *head, *tail; int64_t es;
} mfl_chan;
static mfl_chan* mfl_make_chan(int64_t es) {
    mfl_chan* c = malloc(sizeof(mfl_chan));
    pthread_mutex_init(&c->mu, NULL); pthread_cond_init(&c->cnd, NULL);
    c->head = c->tail = NULL; c->es = es;
    return c;
}
static void mfl_chan_send(mfl_chan* c, const void* v) {
    mfl_cnode* n = malloc(sizeof(mfl_cnode));
    n->data = malloc(c->es); memcpy(n->data, v, c->es); n->next = NULL;
    pthread_mutex_lock(&c->mu);
    if (c->tail) c->tail->next = n; else c->head = n;
    c->tail = n;
    pthread_cond_signal(&c->cnd);
    pthread_mutex_unlock(&c->mu);
}
static void mfl_chan_recv(mfl_chan* c, void* out) {
    pthread_mutex_lock(&c->mu);
    while (!c->head) pthread_cond_wait(&c->cnd, &c->mu);
    mfl_cnode* n = c->head; c->head = n->next;
    if (!c->head) c->tail = NULL;
    pthread_mutex_unlock(&c->mu);
    memcpy(out, n->data, c->es);
    free(n->data); free(n);
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
static char* mfl_read(int64_t fd) {
    char* buf = mfl_alloc(65536);
    ssize_t n = read((int)fd, buf, 65535);
    if (n < 0) n = 0;
    buf[n] = 0;
    return buf;
}
static int64_t mfl_write(int64_t fd, const char* s) { return (int64_t)write((int)fd, s, strlen(s)); }
static void mfl_close(int64_t fd) { close((int)fd); }
`

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
	// struct typedefs, in declaration order (a struct may reference earlier ones)
	for _, td := range p.Types {
		fmt.Fprintf(&out, "typedef struct {")
		for _, f := range td.Fields {
			fmt.Fprintf(&out, " %s f_%s;", cTypeName(f.Type), f.Name)
		}
		fmt.Fprintf(&out, " } mfl_%s;\n", td.Name)
	}
	// closure environment + multi-return result structs (one per instance)
	for _, inst := range g.c.Reps() {
		src := g.c.SrcFunc(inst)
		if src.IsLambda && src.NumCaptures > 0 {
			fmt.Fprintf(&out, "typedef struct {")
			for i := 0; i < src.NumCaptures; i++ {
				fmt.Fprintf(&out, " %s f%d;", g.c.ParamCType(inst, i), i)
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
	out.WriteString("int main(void) { mfl_main(); return 0; }\n")
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
		parts = append(parts, g.c.ParamCType(inst, i)+" v_"+fn.Params[i])
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
	// unpack captured variables from the closure environment
	if fn.IsLambda && fn.NumCaptures > 0 {
		env := g.c.CName(inst) + "_env"
		fmt.Fprintf(&g.buf, "    %s* _e = (%s*)_env;\n", env, env)
		for i := 0; i < fn.NumCaptures; i++ {
			fmt.Fprintf(&g.buf, "    %s v_%s = _e->f%d;\n", g.c.ParamCType(inst, i), fn.Params[i], i)
		}
	}
	for _, name := range g.c.Locals(inst) {
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
		fmt.Fprintf(&g.buf, "return v_%s;\n", names[0])
	default:
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = "v_" + n
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
		fmt.Fprintf(&g.buf, "v_%s = %s;\n", st.Name, e)
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
			fmt.Fprintf(&g.buf, "((%s*)(%s).data)[%s] = %s;\n", g.c.ElemCType(g.curFn, st.Target.X), x, idx, val)
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
	case *RangeStmt:
		return g.rangeStmt(st, depth)
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
func (g *cgen) multiAssign(st *MultiAssign, depth int) error {
	assign := func(name, val string) {
		if name == "_" {
			return
		}
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "v_%s = %s;\n", name, val)
	}

	if len(st.Rhs) == 1 {
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
		fmt.Fprintf(&g.buf, "v_%s = %s;\n", st.Names[0], e)
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
			fmt.Fprintf(&g.buf, "v_%s = _i%d;\n", st.Key, id)
		}
		if hasVal {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "v_%s = ((%s*)_r%d.data)[_i%d];\n", st.Val, ect, id, id)
		}
	case KString:
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "const char* _s%d = %s;\n", id, x)
		indentC(&g.buf, depth+1)
		fmt.Fprintf(&g.buf, "for (int64_t _i%d = 0; _s%d[_i%d]; _i%d++) {\n", id, id, id, id)
		if hasKey {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "v_%s = _i%d;\n", st.Key, id)
		}
		if hasVal {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "v_%s = mfl_charat(_s%d, _i%d);\n", st.Val, id, id)
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
			fmt.Fprintf(&g.buf, "v_%s = _k%d;\n", st.Key, id)
		}
		if hasVal {
			indentC(&g.buf, depth+2)
			fmt.Fprintf(&g.buf, "%s _v%d; mfl_map_get(_m%d, %s, %s, &_v%d); v_%s = _v%d;\n", vct, id, id, ik, sk, id, st.Val, id)
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
		return "v_" + ex.Name, nil
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
		return fmt.Sprintf("((%s*)(%s).data)[%s]", g.c.ElemCType(g.curFn, ex.X), x, idx), nil
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
		return fmt.Sprintf("mfl_make_chan(sizeof(%s))", g.c.ElemCType(g.curFn, ex)), nil
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
	return fmt.Sprintf("(%s %s %s)", l, ex.Op, r), nil
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
	case "str":
		if g.c.NodeKind(g.curFn, ex.Args[0]) == KFloat {
			return fmt.Sprintf("mfl_str_d(%s)", args[0]), nil
		}
		return fmt.Sprintf("mfl_str_i(%s)", args[0]), nil
	case "int":
		return fmt.Sprintf("((int64_t)(%s))", args[0]), nil
	case "listen":
		return fmt.Sprintf("mfl_listen(%s)", args[0]), nil
	case "accept":
		return fmt.Sprintf("mfl_accept(%s)", args[0]), nil
	case "read":
		return fmt.Sprintf("mfl_read(%s)", args[0]), nil
	case "write":
		return fmt.Sprintf("mfl_write(%s, %s)", args[0], args[1]), nil
	case "close":
		return fmt.Sprintf("mfl_close(%s)", args[0]), nil
	case "print", "println":
		return "", fmt.Errorf("print/println may only be used as a statement")
	}
	if !g.c.IsTopFunc(ex.Callee) {
		// a function-valued local variable, called by name
		params, ret := g.c.VarFuncSig(g.curFn, ex.Callee)
		return g.closureCall("v_"+ex.Callee, params, ret, args), nil
	}
	return fmt.Sprintf("%s(%s)", g.c.CalleeCName(g.curFn, ex), strings.Join(args, ", ")), nil
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
