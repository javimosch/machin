package main

// --target android-lib: an MFL program as a shared library inside an Android
// app. The output is libmfl.so per ABI plus the Java class that calls it.
//
// The library is always built --safe: without the checks a bad index is a
// segfault, which no trap catches. See MFL_LIBRARY in the runtime for how a
// panic (on the calling thread or on a goroutine) becomes a Java exception.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// androidAPI is the lowest Android API level the library is built for (8.1).
const androidAPI = 27

// androidABIs maps an Android ABI name to its clang target triple.
var androidABIs = map[string]string{
	"arm64-v8a":   "aarch64-linux-android",
	"armeabi-v7a": "armv7a-linux-androideabi",
	"x86_64":      "x86_64-linux-android",
}

// jniType is how one MFL kind crosses the boundary.
type jniType struct {
	java, native string // public Java type, type of the native method
	c            string // JNI C type
}

var jniTypes = map[Kind]jniType{
	KInt:    {"long", "long", "jlong"},
	KFloat:  {"double", "double", "jdouble"},
	KBool:   {"boolean", "boolean", "jboolean"},
	KString: {"String", "byte[]", "jbyteArray"},
	KBytes:  {"byte[]", "byte[]", "jbyteArray"},
}

type libExport struct {
	name, cname string
	params      []string
	kinds       []Kind
	ret         Kind // KVoid when nothing is returned
}

var (
	javaIdent   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	javaPackage = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+$`)
)

var javaReserved = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`abstract assert boolean break byte case catch char class const continue
		default do double else enum extends final finally float for goto if implements import instanceof int
		interface long native new package private protected public return short static strictfp super switch
		synchronized this throw throws transient try void volatile while true false null _`) {
		m[w] = true
	}
	return m
}()

// libraryExports checks that every `export func` can be called from Java.
func libraryExports(c *Checker) ([]libExport, error) {
	byName := map[string][]string{}
	for _, inst := range c.Reps() {
		if n := c.SrcFunc(inst).Name; c.exportSrc[n] {
			byName[n] = append(byName[n], inst)
		}
	}
	if len(byName) == 0 {
		return nil, fmt.Errorf("android-lib target: no exported functions — mark at least one with `export func`")
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []libExport
	for _, name := range names {
		if !javaIdent.MatchString(name) || javaReserved[name] || strings.HasPrefix(name, "mfl") || name == "main" {
			return nil, fmt.Errorf("android-lib target: export %q cannot be a Java method name (ASCII, not a Java keyword, not starting with \"mfl\")", name)
		}
		if len(byName[name]) != 1 {
			return nil, fmt.Errorf("android-lib target: export %q is used with more than one set of types; an export needs exactly one", name)
		}
		inst := byName[name][0]
		fn := c.SrcFunc(inst)
		e := libExport{name: name, cname: c.CName(inst), ret: KVoid}
		for i, p := range fn.Params {
			k := c.ParamKind(inst, i)
			if _, ok := jniTypes[k]; !ok {
				return nil, fmt.Errorf("android-lib target: export %q: parameter %q is a %s; only int, float, bool, string and bytes cross to Java (pass JSON in a string)", name, p, k)
			}
			e.params = append(e.params, p)
			e.kinds = append(e.kinds, k)
		}
		if len(c.funcRets[inst]) > 1 {
			return nil, fmt.Errorf("android-lib target: export %q returns %d values; Java takes one (return JSON in a string)", name, len(c.funcRets[inst]))
		}
		if len(c.funcRets[inst]) == 1 {
			if k := c.RetKindAt(inst, 0); k != KVoid {
				if _, ok := jniTypes[k]; !ok {
					return nil, fmt.Errorf("android-lib target: export %q returns a %s; only int, float, bool, string and bytes cross to Java (return JSON in a string)", name, k)
				}
				e.ret = k
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// jniMangle escapes one component of a JNI symbol name.
func jniMangle(s string) string { return strings.ReplaceAll(s, "_", "_1") }

const jniHelpers = `
/* ---- JNI bridge (generated) ---- */
#include <jni.h>
#define MFL_JAVA_PKG "%[1]s"
static void mfl_jni_throw(JNIEnv* env, const char* cls_name, const char* msg) {
    char ascii[320]; size_t i = 0; /* ThrowNew wants modified UTF-8: send ASCII */
    for (; msg[i] && i < sizeof ascii - 1; i++) ascii[i] = (unsigned char)msg[i] < 128 ? msg[i] : '?';
    ascii[i] = 0;
    jclass cls = (*env)->FindClass(env, cls_name);
    if (cls) (*env)->ThrowNew(env, cls, ascii);
}
static void mfl_jni_panic(JNIEnv* env, const char* msg) { mfl_jni_throw(env, MFL_JAVA_PKG "/MflPanic", msg); }
/* Strings cross as UTF-8 byte[]: NewStringUTF expects "modified UTF-8". */
static char* mfl_jni_str(JNIEnv* env, jbyteArray a) {
    jsize n = (*env)->GetArrayLength(env, a);
    char* s = (char*)mfl_alloc((size_t)n + 1);
    (*env)->GetByteArrayRegion(env, a, 0, n, (jbyte*)s);
    s[n] = 0;
    return s;
}
static mfl_bytes mfl_jni_bytes(JNIEnv* env, jbyteArray a) {
    mfl_bytes b; b.len = (*env)->GetArrayLength(env, a);
    b.data = (uint8_t*)mfl_alloc((size_t)b.len + 1);
    (*env)->GetByteArrayRegion(env, a, 0, (jsize)b.len, (jbyte*)b.data);
    return b;
}
static jbyteArray mfl_jni_array(JNIEnv* env, const void* p, size_t n) {
    jbyteArray a = (*env)->NewByteArray(env, (jsize)n);
    if (a && n) (*env)->SetByteArrayRegion(env, a, 0, (jsize)n, (const jbyte*)p);
    return a;
}
`

const jniBackground = `
JNIEXPORT jlong JNICALL Java_%[1]s_Mfl_n_1mflBackgroundPanicCount(JNIEnv* env, jclass cls) {
    pthread_mutex_lock(&mfl_bg_mu); int64_t n = mfl_bg_count; pthread_mutex_unlock(&mfl_bg_mu);
    return (jlong)n;
}
JNIEXPORT jbyteArray JNICALL Java_%[1]s_Mfl_n_1mflLastBackgroundPanic(JNIEnv* env, jclass cls) {
    char b[sizeof mfl_bg_msg];
    pthread_mutex_lock(&mfl_bg_mu); memcpy(b, mfl_bg_msg, sizeof b); pthread_mutex_unlock(&mfl_bg_mu);
    return mfl_jni_array(env, b, strlen(b));
}
`

// jniBridge writes one C entry point per export.
func jniBridge(pkg string, exports []libExport) string {
	parts := strings.Split(pkg, ".")
	for i := range parts {
		parts[i] = jniMangle(parts[i])
	}
	prefix := strings.Join(parts, "_")
	var b strings.Builder
	fmt.Fprintf(&b, jniHelpers, strings.ReplaceAll(pkg, ".", "/"))
	for _, e := range exports {
		ret, zero := "void", ""
		if e.ret != KVoid {
			ret, zero = jniTypes[e.ret].c, "0"
		}
		fmt.Fprintf(&b, "\nJNIEXPORT %s JNICALL Java_%s_Mfl_n_1%s(JNIEnv* env, jclass cls", ret, prefix, jniMangle(e.name))
		var args []string
		for i, k := range e.kinds {
			fmt.Fprintf(&b, ", %s p%d", jniTypes[k].c, i)
		}
		b.WriteString(") {\n")
		fmt.Fprintf(&b, "    jmp_buf jb; mfl_call* const call = mfl_call_begin(&jb);\n")
		fmt.Fprintf(&b, "    if (!call) { mfl_jni_panic(env, \"out of memory\"); return %s; }\n", zero)
		fmt.Fprintf(&b, "    if (_setjmp(jb)) { mfl_call_end(call); mfl_jni_panic(env, mfl_trap_msg); return %s; }\n", zero)
		for i, k := range e.kinds {
			expr := fmt.Sprintf("(int64_t)p%d", i)
			ctype := "int64_t"
			switch k {
			case KFloat:
				ctype, expr = "double", fmt.Sprintf("(double)p%d", i)
			case KBool:
				ctype, expr = "int", fmt.Sprintf("p%d != 0", i)
			case KString:
				ctype, expr = "char*", fmt.Sprintf("mfl_jni_str(env, p%d)", i)
			case KBytes:
				ctype, expr = "mfl_bytes", fmt.Sprintf("mfl_jni_bytes(env, p%d)", i)
			}
			// one statement each: conversion order is then fixed
			fmt.Fprintf(&b, "    %s a%d = %s;\n", ctype, i, expr)
			args = append(args, "a"+strconv.Itoa(i))
		}
		invoke := e.cname + "(" + strings.Join(args, ", ") + ")"
		// the result is copied into the Java heap before the call's arena goes
		switch e.ret {
		case KVoid:
			fmt.Fprintf(&b, "    %s;\n", invoke)
		case KString:
			fmt.Fprintf(&b, "    const char* s = %s; if (!s) s = \"\";\n    jbyteArray result = mfl_jni_array(env, s, strlen(s));\n", invoke)
		case KBytes:
			fmt.Fprintf(&b, "    mfl_bytes v = %s;\n    jbyteArray result = mfl_jni_array(env, v.data, (size_t)v.len);\n", invoke)
		default:
			fmt.Fprintf(&b, "    %s result = (%s)%s;\n", ret, ret, invoke)
		}
		b.WriteString("    const char* failed = mfl_call_end(call);\n")
		fmt.Fprintf(&b, "    if (failed) { mfl_jni_panic(env, failed); return %s; }\n", zero)
		if e.ret != KVoid {
			b.WriteString("    return result;\n")
		}
		b.WriteString("}\n")
	}
	fmt.Fprintf(&b, jniBackground, prefix)
	return b.String()
}

// javaBinding writes Mfl.java.
func javaBinding(pkg string, exports []libExport) string {
	var b strings.Builder
	fmt.Fprintf(&b, `// Generated by machin build --target android-lib. Do not edit.
package %s;

import java.nio.charset.StandardCharsets;

/**
 * The exported MFL functions. Calls block the calling thread, so keep slow ones
 * off the UI thread. A panic during a call, on this thread or on a goroutine it
 * started, is thrown as MflPanic. Strings end at the first NUL character.
 */
public final class Mfl {
    static { System.loadLibrary("mfl"); }

    private Mfl() { }

    private static byte[] utf8(String s) { return s.getBytes(StandardCharsets.UTF_8); }
    private static String string(byte[] b) { return b == null ? null : new String(b, StandardCharsets.UTF_8); }
    private static byte[] nonNull(byte[] b) {
        if (b == null) throw new NullPointerException();
        return b;
    }

    /** Panics of goroutines that outlived the call that started them. */
    public static long mflBackgroundPanicCount() { return n_mflBackgroundPanicCount(); }
    /** The latest such panic, or "". */
    public static String mflLastBackgroundPanic() { return string(n_mflLastBackgroundPanic()); }
    private static native long n_mflBackgroundPanicCount();
    private static native byte[] n_mflLastBackgroundPanic();
`, pkg)
	for _, e := range exports {
		var params, args, natives []string
		for i, k := range e.kinds {
			name := e.params[i]
			if !javaIdent.MatchString(name) || javaReserved[name] {
				name = "p" + strconv.Itoa(i)
			}
			params = append(params, jniTypes[k].java+" "+name)
			natives = append(natives, jniTypes[k].native+" "+name)
			switch k {
			case KString: // a null String throws in utf8, before native code
				args = append(args, "utf8("+name+")")
			case KBytes:
				args = append(args, "nonNull("+name+")")
			default:
				args = append(args, name)
			}
		}
		javaRet, nativeRet := "void", "void"
		if e.ret != KVoid {
			javaRet, nativeRet = jniTypes[e.ret].java, jniTypes[e.ret].native
		}
		call := "n_" + e.name + "(" + strings.Join(args, ", ") + ")"
		switch {
		case e.ret == KString:
			call = "return string(" + call + ");"
		case e.ret != KVoid:
			call = "return " + call + ";"
		default:
			call += ";"
		}
		fmt.Fprintf(&b, "\n    public static %s %s(%s) { %s }\n    private static native %s n_%s(%s);\n",
			javaRet, e.name, strings.Join(params, ", "), call, nativeRet, e.name, strings.Join(natives, ", "))
	}
	b.WriteString("}\n")
	return b.String()
}

func javaPanicClass(pkg string) string {
	return fmt.Sprintf(`// Generated by machin build --target android-lib. Do not edit.
package %s;

/** A panic inside an MFL call. Globals the call was changing may be half updated. */
public final class MflPanic extends RuntimeException {
    MflPanic(String message) { super(message); }
}
`, pkg)
}

// androidLibrary is everything generated for a library, before any C compiler.
type androidLibrary struct {
	csrc, mflJava, panicJava string
}

func generateAndroidLibrary(prog *Program, pkg string) (androidLibrary, error) {
	var lib androidLibrary
	if !javaPackage.MatchString(pkg) {
		return lib, fmt.Errorf("android-lib target: --java-package needs a dotted Java package name such as com.example.app, got %q", pkg)
	}
	for _, p := range strings.Split(pkg, ".") {
		if javaReserved[p] {
			return lib, fmt.Errorf("android-lib target: %q is a Java keyword and cannot be part of a package name", p)
		}
	}
	liftClosures(prog)
	c, err := Check(prog)
	if err != nil {
		return lib, err
	}
	exports, err := libraryExports(c)
	if err != nil {
		return lib, err
	}
	csrc, _, err := CompileToCTarget(prog, true, targetAndroidLib)
	if err != nil {
		return lib, err
	}
	lib.csrc = csrc + jniBridge(pkg, exports)
	lib.mflJava = javaBinding(pkg, exports)
	lib.panicJava = javaPanicClass(pkg)
	return lib, nil
}

// findNDK returns the NDK directory: $ANDROID_NDK_HOME, else the newest one
// under $ANDROID_HOME (or $ANDROID_SDK_ROOT).
func findNDK() (string, error) {
	if d := os.Getenv("ANDROID_NDK_HOME"); d != "" {
		return d, nil
	}
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		sdk := os.Getenv(env)
		if sdk == "" {
			continue
		}
		found, _ := filepath.Glob(filepath.Join(sdk, "ndk", "*"))
		if len(found) > 0 {
			sort.Slice(found, func(i, j int) bool { return versionLess(filepath.Base(found[i]), filepath.Base(found[j])) })
			return found[len(found)-1], nil
		}
	}
	return "", fmt.Errorf("android-lib target: Android NDK not found — set ANDROID_NDK_HOME, or ANDROID_HOME to an SDK that has ndk/<version>")
}

func versionLess(a, b string) bool {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		m, _ := strconv.Atoi(x[i])
		n, _ := strconv.Atoi(y[i])
		if m != n {
			return m < n
		}
	}
	return len(x) < len(y)
}

// ndkClang is the NDK's compiler driver for one ABI.
func ndkClang(ndk, triple string) (string, error) {
	host := map[string]string{"linux": "linux-x86_64", "darwin": "darwin-x86_64", "windows": "windows-x86_64"}[runtime.GOOS]
	p := filepath.Join(ndk, "toolchains", "llvm", "prebuilt", host, "bin", fmt.Sprintf("%s%d-clang", triple, androidAPI))
	if runtime.GOOS == "windows" {
		p += ".cmd"
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("android-lib target: no NDK compiler at %s", p)
	}
	return p, nil
}

// BuildAndroidLib writes outDir/jniLibs/<abi>/libmfl.so for each ABI and
// outDir/java/<package>/{Mfl,MflPanic}.java: the two source directories an
// Android project adds.
func BuildAndroidLib(prog *Program, outDir, pkg string, abis []string, link LinkOptions) error {
	if len(abis) == 0 {
		abis = []string{"arm64-v8a"}
	}
	for _, abi := range abis {
		if androidABIs[abi] == "" {
			return fmt.Errorf("android-lib target: unknown --abi %q (want arm64-v8a, armeabi-v7a or x86_64)", abi)
		}
	}
	lib, err := generateAndroidLibrary(prog, pkg)
	if err != nil {
		return err
	}
	// what the NDK does not ship cannot be linked
	for needle, what := range map[string]string{"mfl_tls_dial": "TLS (https/wss)", "mfl_crypto_": "OpenSSL crypto", "mfl_xeddsa_": "XEdDSA", "mfl_sqlite_": "SQLite"} {
		if strings.Contains(lib.csrc, needle) {
			return fmt.Errorf("android-lib target: %s builtins are not supported yet (the NDK does not ship the library)", what)
		}
	}
	ndk, err := findNDK()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "mfl-android-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	cfile := filepath.Join(tmp, "mfl_android.c")
	if err := os.WriteFile(cfile, []byte(lib.csrc), 0o644); err != nil {
		return err
	}
	var libs []string
	if strings.Contains(lib.csrc, "mfl_math_") || strings.Contains(lib.csrc, "mfl_noise") {
		libs = append(libs, "-lm")
	}
	if strings.Contains(lib.csrc, "mfl_zlib_") {
		libs = append(libs, "-lz")
	}
	for _, eb := range prog.Externs {
		for _, l := range eb.Links {
			libs = append(libs, "-l"+l)
		}
	}
	for _, abi := range abis {
		cc, err := ndkClang(ndk, androidABIs[abi])
		if err != nil {
			return err
		}
		so := filepath.Join(outDir, "jniLibs", abi, "libmfl.so")
		if err := os.MkdirAll(filepath.Dir(so), 0o755); err != nil {
			return err
		}
		args := []string{"-shared", "-fPIC", "-O2", "-fno-strict-aliasing", "-fwrapv", "-std=c11", "-pthread",
			"-fvisibility=hidden", "-ffunction-sections", "-fdata-sections",
			"-Wl,--gc-sections", "-Wl,--no-undefined", "-Wl,-s", "-Wl,-z,max-page-size=16384"}
		args = append(args, link.CFlags...)
		args = append(args, cfile, "-o", so)
		args = append(args, libs...)
		args = append(args, link.LDFlags...)
		if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s failed: %v\n%s", cc, err, out)
		}
	}
	javaDir := filepath.Join(append([]string{outDir, "java"}, strings.Split(pkg, ".")...)...)
	if err := os.MkdirAll(javaDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(javaDir, "Mfl.java"), []byte(lib.mflJava), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(javaDir, "MflPanic.java"), []byte(lib.panicJava), 0o644)
}
