package main

// --target android-lib. The generated C, JNI bridge and Java are the same for
// any JVM, so the behaviour is tested on the desktop with the host compiler;
// only the last step (the NDK link) needs Android tooling.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const androidTestLib = `
var events = make(chan string)

export func greet(name) (r) { r = "hello " + name }
export func add(a, b) (r) { r = a + b }
export func half(x) (r) { r = x / 2.0 }
export func is_even(n) (r) { r = n % 2 == 0 }
export func flip(data) (r) { r = bytes_concat(data, from_hex("00ff")) }
export func note(s) { if s == "never" { println(s) } }
export func at(i) (r) { xs := []int{1, 2, 3}  r = xs[i] }
export func next_event() (r) { r = <-events }
export func post_event(s) { events <- s }

func die(i) { xs := []int{1}  println(xs[i]) }
func die_after(ms, i) { sleep(ms)  die(i) }
func relay(a, b) { b <- <-a }
export func chain() (r) { a := make(chan int)  b := make(chan int)  go relay(a, b)  go die_after(20, 5)  r = <-b }
export func in_select() (r) {
    a := make(chan int)
    b := make(chan int)
    go die_after(20, 6)
    select {
    case v := <-a:
        r = v
    case v := <-b:
        r = v
    }
}
export func fire_and_forget() (r) { go die(7)  sleep(150)  r = 1 }
export func later() (r) { go die_after(100, 8)  r = 1 }
func job(i, out) { xs := []int{0, 1, 2, 3, 4, 5, 6}  out <- xs[i] }
export func pool(n) (r) {
    out := make(chan int)
    i := 0
    while i < n { go job(i, out)  i = i + 1 }
    i = 0
    while i < n { r = r + <-out  i = i + 1 }
}
func rec(n) (r) { r = rec(n + 1) + 1 }
func overflow(c) { c <- rec(0) }
export func recurse() (r) { r = rec(0) }
export func recurse_goroutine() (r) { c := make(chan int)  go overflow(c)  r = <-c }
`

const androidTestJava = `
import com.example.my_app.Mfl;
import com.example.my_app.MflPanic;
import java.util.concurrent.atomic.AtomicInteger;

public class Main {
    interface Body { Object run(); }
    static void show(String what, Body b) {
        try { System.out.println(what + ": " + b.run()); }
        catch (MflPanic e) { System.out.println(what + ": panic " + e.getMessage().replaceAll(" \\((out of stack|depth).*", "")); }
        catch (NullPointerException e) { System.out.println(what + ": NullPointerException"); }
    }
    public static void main(String[] args) throws Exception {
        show("greet", () -> Mfl.greet("é 𝄞"));
        show("add", () -> Mfl.add(40, 2));
        show("half", () -> Mfl.half(5));
        show("even", () -> Mfl.is_even(4) + " " + Mfl.is_even(5));
        show("bytes", () -> java.util.Arrays.toString(Mfl.flip(new byte[]{1, 0, 2})));
        show("void", () -> { Mfl.note("x"); return "ok"; });
        show("null string", () -> Mfl.greet(null));
        show("null bytes", () -> Mfl.flip(null));

        final String[] got = {null};
        Thread waiter = new Thread(() -> got[0] = Mfl.next_event()); // waits on Java: not a deadlock
        waiter.start();
        Thread.sleep(300);

        show("caller", () -> Mfl.at(99));
        show("two hops", Mfl::chain);
        show("select", Mfl::in_select);
        show("unwatched", Mfl::fire_and_forget);
        show("fan-out", () -> Mfl.pool(20));
        show("healthy fan-out", () -> Mfl.pool(7));
        show("overflow", Mfl::recurse);
        show("goroutine overflow", Mfl::recurse_goroutine);
        show("later", Mfl::later);
        Thread.sleep(500);
        System.out.println("background: " + Mfl.mflBackgroundPanicCount() + " " + Mfl.mflLastBackgroundPanic());

        Mfl.post_event("still here");
        waiter.join(5000);
        System.out.println("unrelated waiter: " + got[0]);

        AtomicInteger right = new AtomicInteger();
        Thread[] threads = new Thread[8];
        for (int t = 0; t < threads.length; t++) {
            final int kind = t % 4;
            threads[t] = new Thread(() -> {
                for (int i = 0; i < 100; i++) {
                    try {
                        long v = kind == 0 ? Mfl.at(1) : kind == 1 ? Mfl.pool(7) : kind == 2 ? Mfl.at(99) : Mfl.pool(20);
                        if ((kind == 0 && v == 2) || (kind == 1 && v == 21)) right.incrementAndGet();
                    } catch (MflPanic e) { if (kind >= 2) right.incrementAndGet(); }
                }
            });
            threads[t].start();
        }
        for (Thread t : threads) t.join();
        System.out.println("concurrent: " + right.get() + "/800");
    }
}
`

// runAndroidLibOnJVM builds a library for the desktop JVM and returns a runner
// for Main.java; env is added to the Java process's environment.
func runAndroidLibOnJVM(t *testing.T, mflSrc, javaMain string) func(env ...string) string {
	t.Helper()
	javac, err := exec.LookPath("javac")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs a JDK on a unix host")
	}
	real, err := filepath.EvalSymlinks(javac)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Dir(filepath.Dir(real))
	include := filepath.Join(home, "include")
	if _, err := os.Stat(filepath.Join(include, "jni.h")); err != nil {
		t.Skip("JDK has no jni.h")
	}
	prog, err := wallsCompose(t, mflSrc)
	if err != nil {
		t.Fatal(err)
	}
	// an underscore in the package exercises JNI name mangling
	lib, err := generateAndroidLibrary(prog, "com.example.my_app")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "com", "example", "my_app")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(dir, "lib.c"):            lib.csrc,
		filepath.Join(pkgDir, "Mfl.java"):      lib.mflJava,
		filepath.Join(pkgDir, "MflPanic.java"): lib.panicJava,
		filepath.Join(dir, "Main.java"):        javaMain,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var env []string
	run := func(name string, args ...string) string {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		var out, errs strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errs
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s: %v\n%s\n%s", name, err, out.String(), errs.String())
		}
		return out.String()
	}
	osInclude := filepath.Join(include, runtime.GOOS)
	run(ccPath(), "-shared", "-fPIC", "-O2", "-std=c11", "-pthread", "-fwrapv", "-fno-strict-aliasing",
		"-I"+include, "-I"+osInclude, "lib.c", "-o", "libmfl.so", "-lm")
	run(javac, "-encoding", "UTF-8", "-d", ".", "Main.java", "com/example/my_app/Mfl.java", "com/example/my_app/MflPanic.java")
	if runtime.GOOS == "darwin" {
		if err := os.Rename(filepath.Join(dir, "libmfl.so"), filepath.Join(dir, "libmfl.dylib")); err != nil {
			t.Fatal(err)
		}
	}
	return func(extra ...string) string {
		env = extra
		return run(filepath.Join(home, "bin", "java"), "-Xcheck:jni", "-Dfile.encoding=UTF-8", "-Dstdout.encoding=UTF-8", "-Djava.library.path=.", "-cp", ".", "Main")
	}
}

func TestAndroidLibOnDesktopJVM(t *testing.T) {
	got := runAndroidLibOnJVM(t, androidTestLib, androidTestJava)()
	want := `greet: hello é 𝄞
add: 42
half: 2.5
even: true false
bytes: [1, 0, 2, 0, -1]
void: ok
null string: NullPointerException
null bytes: NullPointerException
caller: panic index out of range [99] with length 3
two hops: panic goroutine panicked: index out of range [5] with length 1
select: panic goroutine panicked: index out of range [6] with length 1
unwatched: panic goroutine panicked: index out of range [7] with length 1
fan-out: panic goroutine panicked: index out of range [P] with length 7
healthy fan-out: 21
overflow: panic stack overflow
goroutine overflow: panic goroutine panicked: stack overflow
later: 1
background: 1 index out of range [8] with length 1
unrelated waiter: still here
concurrent: 800/800
`
	// which fan-out worker panics first is a race: any of the bad indexes
	lines := strings.Split(got, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "fan-out: ") {
			for p := 7; p < 20; p++ {
				l = strings.Replace(l, "["+itoa(p)+"]", "[P]", 1)
			}
			lines[i] = l
		}
	}
	if got = strings.Join(lines, "\n"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAndroidLibRejectsWhatJavaCannotCall(t *testing.T) {
	for src, want := range map[string]string{
		`func main() { }`:                              "no exported functions",
		`export func f(xs) (r) { r = xs[0] + 1 }`:      `parameter "xs" is a`,
		`export func f(n) (r) { r = []int{n} }`:        "returns a",
		`export func f(n) (a, b) { a = n + 1  b = n }`: "returns 2 values",
		`export func new(n) (r) { r = n + 1 }`:         "cannot be a Java method name",
		`export func mflX(n) (r) { r = n + 1 }`:        "cannot be a Java method name",
	} {
		prog, err := wallsCompose(t, src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if _, err := generateAndroidLibrary(prog, "com.example.app"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", src, err, want)
		}
	}
	for _, pkg := range []string{"", "app", "com.example.new", "com..x", "com.1x"} {
		prog, _ := wallsCompose(t, `export func f(n) (r) { r = n + 1 }`)
		if _, err := generateAndroidLibrary(prog, pkg); err == nil {
			t.Errorf("package %q was accepted", pkg)
		}
	}
	prog, _ := wallsCompose(t, `export func f(n) (r) { r = n + 1 }`)
	if err := BuildAndroidLib(prog, t.TempDir(), "com.example.app", []string{"mips"}, LinkOptions{}); err == nil || !strings.Contains(err.Error(), "unknown --abi") {
		t.Errorf("abi: %v", err)
	}
	t.Setenv("ANDROID_NDK_HOME", "")
	t.Setenv("ANDROID_SDK_ROOT", "")
	t.Setenv("ANDROID_HOME", t.TempDir())
	if _, err := findNDK(); err == nil {
		t.Error("an SDK without an NDK was accepted")
	}
	if !versionLess("9.1.0", "28.2.1") || versionLess("28.2.1", "28.2.1") {
		t.Error("NDK versions compare as numbers")
	}
}

// A normal build must not change: library code is behind MFL_LIBRARY.
func TestAndroidLibLeavesNativeBuildsAlone(t *testing.T) {
	prog, err := wallsCompose(t, `func w(c) { c <- 1 }
func main() { c := make(chan int)  go w(c)  select { case v := <-c: println(v) } }`)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := CompileToCTarget(prog, false, targetNative)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c, "#define MFL_LIBRARY") {
		t.Fatal("native build defines MFL_LIBRARY")
	}
	// emitted program code, after the runtime, carries no library calls
	tail := c[strings.Index(c, "struct mfl_go_0 {"):]
	for _, s := range []string{"mfl_scope", "mfl_go_guard", "mfl_go_body_"} {
		if strings.Contains(tail, s) {
			t.Errorf("native program code mentions %s", s)
		}
	}
}

// A JVM adopts the user's locale, and an app can set any. MFL must behave as it
// does standalone ("C"): under en_US.UTF-8, [a-z] otherwise matches é, which
// turns a validation pattern into a weaker one without a word.
func TestAndroidLibIgnoresHostLocale(t *testing.T) {
	java := runAndroidLibOnJVM(t, `
var at_load = regex_match("ja-é", "^[a-z-]+$")

func check(c, s) { c <- regex_match(s, "^[a-z0-9-]+$") }

export func valid(s) (r) { r = regex_match(s, "^[a-z0-9-]+$") }
export func valid_on_goroutine(s) (r) { c := make(chan bool)  go check(c, s)  r = <-c }
export func slug(s) (r) { r = regex_replace(s, "[^a-z0-9]+", "-") }
export func upper(s) (r) { r = to_upper(s) }
export func number(x) (r) { r = json(x / 2.0) + " " + str(parse_float("1.5")) }
export func loaded() (r) { r = at_load }
export func boom(i) (r) { xs := []int{1}  r = xs[i] }
`, `
import com.example.my_app.Mfl;
import com.example.my_app.MflPanic;

public class Main {
    public static void main(String[] args) {
        System.out.println(Mfl.valid("ja-\u00e9") + " " + Mfl.valid_on_goroutine("ja-\u00e9") + " " + Mfl.valid("ja-ok"));
        System.out.println(Mfl.slug("\u00c9t\u00e9 2026"));
        System.out.println(Mfl.upper("a\u00e9i"));
        System.out.println(Mfl.number(5) + " " + Mfl.loaded());
        try { Mfl.boom(9); } catch (MflPanic e) { System.out.println("panic"); }
        System.out.println(Mfl.valid("ja-\u00e9"));
    }
}
`)
	want := "false false true\n-t-2026\nAéI\n2.5 1.5 false\npanic\nfalse\n"
	tried := []string{"C"}
	if out, err := exec.Command("locale", "-a").Output(); err == nil {
		have := strings.ToLower(string(out))
		for _, l := range []string{"en_US", "fr_FR", "de_DE", "tr_TR"} {
			if strings.Contains(have, strings.ToLower(l)+".utf") {
				tried = append(tried, l+".UTF-8")
			}
		}
	}
	if os.Getenv("MFL_REQUIRE_LOCALES") != "" && !strings.Contains(strings.Join(tried, " "), "fr_FR") {
		t.Fatalf("MFL_REQUIRE_LOCALES is set but only %v are available", tried)
	}
	for _, l := range tried {
		if got := java("LC_ALL=" + l); got != want {
			t.Errorf("host locale %s: got\n%s\nwant\n%s", l, got, want)
		}
	}
	t.Logf("host locales tried: %v", tried)
}
