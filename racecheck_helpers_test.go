package main

import (
	"strings"
	"testing"
)

func TestClonePath(t *testing.T) {
	if got := clonePath(nil); got != nil {
		t.Fatalf("clonePath(nil) = %v, want nil", got)
	}
	if got := clonePath(accPath{}); got != nil {
		t.Fatalf("clonePath(empty) = %v, want nil", got)
	}

	orig := accPath{{field: "a"}, {field: ""}, {field: "b"}}
	clone := clonePath(orig)
	if !pathEq(orig, clone) {
		t.Fatalf("clonePath(%v) = %v, want equal path", orig, clone)
	}

	// Mutating the clone must not affect the original — it's a deep copy of the slice.
	clone[0].field = "z"
	if orig[0].field != "a" {
		t.Fatalf("mutating clone affected original: %v", orig)
	}
}

func TestPathEq(t *testing.T) {
	tests := []struct {
		name string
		a, b accPath
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, accPath{}, true},
		{"equal single field", accPath{{field: "x"}}, accPath{{field: "x"}}, true},
		{"equal index step", accPath{{field: ""}}, accPath{{field: ""}}, true},
		{"different field name", accPath{{field: "x"}}, accPath{{field: "y"}}, false},
		{"different length", accPath{{field: "x"}}, accPath{{field: "x"}, {field: "y"}}, false},
		{"different order", accPath{{field: "x"}, {field: "y"}}, accPath{{field: "y"}, {field: "x"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathEq(tt.a, tt.b); got != tt.want {
				t.Fatalf("pathEq(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMapValType(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"map[string]int", "int"},
		{"map[int]string", "string"},
		{"map[string]map[int]bool", "map[int]bool"},
		{"map[string][]int", "[]int"},
		{"map[string]", ""},
		{"nope", "?"},
	}
	for _, tt := range tests {
		if got := mapValType(tt.in); got != tt.want {
			t.Fatalf("mapValType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCopyIntMap(t *testing.T) {
	orig := map[string]int{"a": 1, "b": 2}
	clone := copyIntMap(orig)
	if len(clone) != len(orig) {
		t.Fatalf("copyIntMap(%v) = %v, want same length", orig, clone)
	}
	for k, v := range orig {
		if clone[k] != v {
			t.Fatalf("copyIntMap(%v)[%q] = %d, want %d", orig, k, clone[k], v)
		}
	}

	// Mutating the clone must not affect the original.
	clone["a"] = 99
	if orig["a"] != 1 {
		t.Fatalf("mutating clone affected original: %v", orig)
	}

	empty := copyIntMap(nil)
	if len(empty) != 0 {
		t.Fatalf("copyIntMap(nil) = %v, want empty map", empty)
	}
}

func TestSigCompleteParams(t *testing.T) {
	if got := sigCompleteParams(nil); len(got) != 0 {
		t.Fatalf("sigCompleteParams(nil) = %v, want empty", got)
	}
	if got := sigCompleteParams(&FuncDecl{}); len(got) != 0 {
		t.Fatalf("sigCompleteParams(empty body) = %v, want empty", got)
	}

	// Last statement isn't a send: no param is signal-complete.
	notSend := &FuncDecl{Params: []string{"ch"}, Body: []Stmt{&ExprStmt{X: &Ident{Name: "ch"}}}}
	if got := sigCompleteParams(notSend); len(got) != 0 {
		t.Fatalf("sigCompleteParams(no trailing send) = %v, want empty", got)
	}

	// Last statement sends on a param: that param's index is marked complete.
	fn := &FuncDecl{
		Params: []string{"a", "ch", "b"},
		Body: []Stmt{
			&AssignStmt{Name: "x", Op: ":=", Val: &Ident{Name: "a"}},
			&SendStmt{Ch: &Ident{Name: "ch"}, Val: &Ident{Name: "x"}},
		},
	}
	got := sigCompleteParams(fn)
	if !got[1] || len(got) != 1 {
		t.Fatalf("sigCompleteParams(%v) = %v, want {1: true}", fn, got)
	}

	// Sending on a non-param channel expression yields no matches.
	notParam := &FuncDecl{
		Params: []string{"a"},
		Body:   []Stmt{&SendStmt{Ch: &Ident{Name: "other"}, Val: &Ident{Name: "a"}}},
	}
	if got := sigCompleteParams(notParam); len(got) != 0 {
		t.Fatalf("sigCompleteParams(send on non-param) = %v, want empty", got)
	}
}

func TestRecvChanName(t *testing.T) {
	name, ok := recvChanName(&Recv{Ch: &Ident{Name: "ch"}})
	if !ok || name != "ch" {
		t.Fatalf("recvChanName(<-ch) = (%q, %v), want (\"ch\", true)", name, ok)
	}

	if _, ok := recvChanName(&Ident{Name: "notarecv"}); ok {
		t.Fatalf("recvChanName(non-Recv expr) should return ok=false")
	}

	if _, ok := recvChanName(&Recv{Ch: &Call{Callee: "getch"}}); ok {
		t.Fatalf("recvChanName(<-getch()) should return ok=false: channel isn't a bare ident")
	}
}

func TestBlockOf(t *testing.T) {
	then := []Stmt{&AssignStmt{Name: "a", Op: ":=", Val: &Ident{Name: "1"}}}
	els := []Stmt{&AssignStmt{Name: "b", Op: ":=", Val: &Ident{Name: "2"}}}
	if body, ok := blockOf(&IfStmt{Then: then, Else: els}); !ok || len(body) != 2 {
		t.Fatalf("blockOf(IfStmt) = (%v, %v), want 2 combined stmts", body, ok)
	}

	whileBody := []Stmt{&AssignStmt{Name: "c", Op: ":=", Val: &Ident{Name: "3"}}}
	if body, ok := blockOf(&WhileStmt{Body: whileBody}); !ok || len(body) != 1 {
		t.Fatalf("blockOf(WhileStmt) = (%v, %v), want its body", body, ok)
	}

	if _, ok := blockOf(&ReturnStmt{}); ok {
		t.Fatalf("blockOf(ReturnStmt) should return ok=false: not a block-bearing stmt")
	}
}

func TestPlaceOf(t *testing.T) {
	root, path, ok := placeOf(&Ident{Name: "x"})
	if !ok || root != "x" || path != nil {
		t.Fatalf("placeOf(Ident) = (%q, %v, %v), want (\"x\", nil, true)", root, path, ok)
	}

	root, path, ok = placeOf(&Index{X: &Ident{Name: "arr"}, Idx: &Ident{Name: "i"}})
	if !ok || root != "arr" || !pathEq(path, accPath{{field: ""}}) {
		t.Fatalf("placeOf(Index) = (%q, %v, %v), want (\"arr\", [index], true)", root, path, ok)
	}

	root, path, ok = placeOf(&FieldAccess{X: &Ident{Name: "p"}, Name: "y"})
	if !ok || root != "p" || !pathEq(path, accPath{{field: "y"}}) {
		t.Fatalf("placeOf(FieldAccess) = (%q, %v, %v), want (\"p\", [.y], true)", root, path, ok)
	}

	root, path, ok = placeOf(&FieldAccess{X: &Index{X: &Ident{Name: "s"}, Idx: &Ident{Name: "0"}}, Name: "z"})
	if !ok || root != "s" || !pathEq(path, accPath{{field: ""}, {field: "z"}}) {
		t.Fatalf("placeOf(nested Index.Field) = (%q, %v, %v), want (\"s\", [index,.z], true)", root, path, ok)
	}

	if _, _, ok := placeOf(&Call{Callee: "f"}); ok {
		t.Fatalf("placeOf(Call) should return ok=false: not a place expression")
	}

	if _, _, ok := placeOf(&FieldAccess{X: &Call{Callee: "f"}, Name: "y"}); ok {
		t.Fatalf("placeOf(FieldAccess on non-place) should return ok=false")
	}
}

func TestSlotTypeName(t *testing.T) {
	c := &Checker{}

	scalars := []struct {
		kind Kind
		want string
	}{
		{KInt, "int"}, {KNum, "int"}, {KFloat, "float"}, {KBool, "bool"},
		{KString, "string"}, {KBytes, "bytes"}, {KVoid, "?"},
	}
	for _, tt := range scalars {
		s := newSlot(c, tt.kind)
		if got := slotTypeName(c, s); got != tt.want {
			t.Fatalf("slotTypeName(%v) = %q, want %q", tt.kind, got, tt.want)
		}
	}

	elemSlot := newSlot(c, KInt)
	sliceSlot := newSlot(c, KSlice)
	c.elem[sliceSlot] = elemSlot
	if got := slotTypeName(c, sliceSlot); got != "[]int" {
		t.Fatalf("slotTypeName(slice of int) = %q, want []int", got)
	}
	emptySlice := newSlot(c, KSlice)
	if got := slotTypeName(c, emptySlice); got != "[]?" {
		t.Fatalf("slotTypeName(slice, no elem) = %q, want []?", got)
	}

	keySlot := newSlot(c, KString)
	valSlot := newSlot(c, KInt)
	mapSlot := newSlot(c, KMap)
	c.mkey[mapSlot] = keySlot
	c.mval[mapSlot] = valSlot
	if got := slotTypeName(c, mapSlot); got != "map[string]int" {
		t.Fatalf("slotTypeName(map) = %q, want map[string]int", got)
	}
	emptyMap := newSlot(c, KMap)
	if got := slotTypeName(c, emptyMap); got != "map[?]?" {
		t.Fatalf("slotTypeName(map, no key/val) = %q, want map[?]?", got)
	}

	chanElem := newSlot(c, KBool)
	chanSlot := newSlot(c, KChan)
	c.elem[chanSlot] = chanElem
	if got := slotTypeName(c, chanSlot); got != "chan bool" {
		t.Fatalf("slotTypeName(chan) = %q, want chan bool", got)
	}
	emptyChan := newSlot(c, KChan)
	if got := slotTypeName(c, emptyChan); got != "chan ?" {
		t.Fatalf("slotTypeName(chan, no elem) = %q, want chan ?", got)
	}

	structSlot := newSlot(c, KStruct)
	c.sname[structSlot] = "Box"
	if got := slotTypeName(c, structSlot); got != "Box" {
		t.Fatalf("slotTypeName(struct) = %q, want Box", got)
	}
}

func TestTypeShared(t *testing.T) {
	c := &Checker{
		structs: map[string]*TypeDecl{
			"Box":   {Name: "Box", Fields: []Field{{Name: "n", Type: "int"}, {Name: "items", Type: "[]int"}}},
			"Outer": {Name: "Outer", Fields: []Field{{Name: "b", Type: "Box"}}},
		},
	}

	// Empty path: never crosses an indirection.
	if got := typeShared(c, "Box", accPath{}); got {
		t.Fatalf("typeShared(Box, empty path) = %v, want false", got)
	}

	// Field navigation on a by-value struct stays private.
	if got := typeShared(c, "Outer", accPath{{field: "b"}}); got {
		t.Fatalf("typeShared(Outer.b) = %v, want false", got)
	}

	// Indexing a slice crosses into shared heap.
	if got := typeShared(c, "[]int", accPath{{field: ""}}); !got {
		t.Fatalf("typeShared([]int, index) = %v, want true", got)
	}

	// Indexing a map value crosses into shared heap.
	if got := typeShared(c, "map[string]int", accPath{{field: ""}}); !got {
		t.Fatalf("typeShared(map[string]int, index) = %v, want true", got)
	}

	// Field access after crossing a slice index stays "shared" (sticky).
	if got := typeShared(c, "[]Box", accPath{{field: ""}, {field: "n"}}); !got {
		t.Fatalf("typeShared([]Box, index then field) = %v, want true", got)
	}

	// Unknown field on a struct: stops early, reports whatever was accumulated.
	if got := typeShared(c, "Box", accPath{{field: "missing"}}); got {
		t.Fatalf("typeShared(Box.missing) = %v, want false", got)
	}

	// Indexing a string/bytes (no "[]" or "map[" prefix): no sharing, stops early.
	if got := typeShared(c, "string", accPath{{field: ""}}); got {
		t.Fatalf("typeShared(string, index) = %v, want false", got)
	}
}

// TestTypeSharesHeap covers typeSharesHeap (distinct from typeShared above): it
// decides whether SENDING a value of this type on a channel aliases shared heap,
// by name prefix or by recursing into struct fields, with a depth cutoff against
// runaway/cyclic struct definitions.
func TestTypeSharesHeap(t *testing.T) {
	c := &Checker{
		structs: map[string]*TypeDecl{
			"Box":       {Name: "Box", Fields: []Field{{Name: "n", Type: "int"}, {Name: "items", Type: "[]int"}}},
			"Plain":     {Name: "Plain", Fields: []Field{{Name: "n", Type: "int"}, {Name: "s", Type: "string"}}},
			"Wraps":     {Name: "Wraps", Fields: []Field{{Name: "b", Type: "Box"}}},
			"Recursive": {Name: "Recursive", Fields: []Field{{Name: "next", Type: "Recursive"}}},
		},
	}

	if !typeSharesHeap(c, "[]int", 0) {
		t.Fatalf("typeSharesHeap([]int) = false, want true")
	}
	if !typeSharesHeap(c, "map[string]int", 0) {
		t.Fatalf("typeSharesHeap(map[string]int) = false, want true")
	}
	if !typeSharesHeap(c, "Box", 0) {
		t.Fatalf("typeSharesHeap(Box) = false, want true (has a []int field)")
	}
	if typeSharesHeap(c, "Plain", 0) {
		t.Fatalf("typeSharesHeap(Plain) = true, want false (no shared-heap fields)")
	}
	if !typeSharesHeap(c, "Wraps", 0) {
		t.Fatalf("typeSharesHeap(Wraps) = false, want true (transitively wraps Box)")
	}
	if typeSharesHeap(c, "unknown", 0) {
		t.Fatalf("typeSharesHeap(unknown type) = true, want false")
	}
	// Depth cutoff: guards against runaway recursion on a self-referential struct.
	if typeSharesHeap(c, "Recursive", 9) {
		t.Fatalf("typeSharesHeap(depth>8) = true, want false (cutoff hit)")
	}
}

func TestGoAccessorsOf(t *testing.T) {
	sum := map[string][]paramAcc{
		"worker": {
			{idx: 0, path: accPath{{field: "n"}}, write: true},
			{idx: 1, path: nil, write: false},
		},
	}
	st := &GoStmt{Call: &Call{Callee: "worker", Args: []Expr{
		&Ident{Name: "box"},
		&Ident{Name: "counter"},
	}}}

	got := goAccessorsOf(st, false, sum)
	if len(got) != 2 {
		t.Fatalf("goAccessorsOf(not in loop) = %d accesses, want 2: %+v", len(got), got)
	}
	if got[0].root != "box" || !got[0].write || got[0].mult != 1 {
		t.Fatalf("goAccessorsOf[0] = %+v, want root=box write=true mult=1", got[0])
	}
	if got[1].root != "counter" || got[1].write || got[1].mult != 1 {
		t.Fatalf("goAccessorsOf[1] = %+v, want root=counter write=false mult=1", got[1])
	}

	// Inside a loop, the multiplier doubles and the description labels it as such.
	loopGot := goAccessorsOf(st, true, sum)
	if len(loopGot) != 2 || loopGot[0].mult != 2 {
		t.Fatalf("goAccessorsOf(in loop)[0].mult = %d, want 2", loopGot[0].mult)
	}
	if !strings.Contains(loopGot[0].desc, "loop-spawned goroutine") {
		t.Fatalf("goAccessorsOf(in loop) desc = %q, want it to mention loop-spawned goroutine", loopGot[0].desc)
	}

	// An argument that isn't a place expression (e.g. a call result) is skipped.
	nonPlace := &GoStmt{Call: &Call{Callee: "worker", Args: []Expr{&Call{Callee: "f"}, &Ident{Name: "counter"}}}}
	if got := goAccessorsOf(nonPlace, false, sum); len(got) != 1 || got[0].root != "counter" {
		t.Fatalf("goAccessorsOf(non-place arg) = %+v, want only the counter access", got)
	}

	// A callee with no recorded summary yields no accesses.
	if got := goAccessorsOf(&GoStmt{Call: &Call{Callee: "unknown", Args: []Expr{&Ident{Name: "x"}}}}, false, sum); len(got) != 0 {
		t.Fatalf("goAccessorsOf(unknown callee) = %+v, want none", got)
	}
}

// TestMarkLoopSpawns covers every Stmt case it recurses through (If/While/Range/
// Arena) plus the GoStmt leaf that actually bumps `live`, mirroring the shape of
// TestCollectLocals above.
func TestMarkLoopSpawns(t *testing.T) {
	sum := map[string][]paramAcc{
		"worker": {{idx: 0, path: nil, write: true}},
	}
	goCall := func() Stmt {
		return &GoStmt{Call: &Call{Callee: "worker", Args: []Expr{&Ident{Name: "box"}}}}
	}

	body := []Stmt{
		&IfStmt{Then: []Stmt{goCall()}, Else: []Stmt{goCall()}},
		&WhileStmt{Body: []Stmt{goCall()}},
		&RangeStmt{Body: []Stmt{goCall()}},
		&ArenaStmt{Body: []Stmt{goCall()}},
	}

	live := map[string]int{}
	markLoopSpawns(body, live, sum)

	if live["box"] != 5 {
		t.Fatalf("markLoopSpawns: live[box] = %d, want 5 (if-then + if-else + while + range + arena)", live["box"])
	}
}

func TestMergeGAcc(t *testing.T) {
	m := map[string]*gAccess{}

	if changed := mergeGAcc(m, "g", &gAccess{read: true, write: false}); !changed {
		t.Fatalf("mergeGAcc(new entry) should report changed=true")
	}
	if got := m["g"]; !got.read || got.write {
		t.Fatalf("m[g] = %+v, want {read:true write:false}", got)
	}

	if changed := mergeGAcc(m, "g", &gAccess{read: true, write: false}); changed {
		t.Fatalf("mergeGAcc(no new bits) should report changed=false")
	}

	if changed := mergeGAcc(m, "g", &gAccess{read: false, write: true}); !changed {
		t.Fatalf("mergeGAcc(adding write) should report changed=true")
	}
	if got := m["g"]; !got.read || !got.write {
		t.Fatalf("m[g] = %+v, want {read:true write:true}", got)
	}

	if changed := mergeGAcc(m, "g", &gAccess{read: true, write: true}); changed {
		t.Fatalf("mergeGAcc(already set bits) should report changed=false")
	}
}

// TestRaceFindingToDiagnostic covers all three raceFinding.Kind branches:
// write/write and use-after-move each pick a distinct code/message shape,
// and anything else (e.g. read/write) falls through to the default RACE002.
// TestCollectLocals covers every Stmt case collectLocals recurses through: a
// top-level `:=`, a `:=` MultiAssign, a range's key/val vars (recursing into its
// body), and nested If/While/Arena bodies. Plain `=` (not `:=`) must NOT bind a
// local, since it targets an existing name rather than shadowing a global.
func TestCollectLocals(t *testing.T) {
	body := []Stmt{
		&AssignStmt{Name: "a", Op: ":=", Val: &IntLit{}},
		&AssignStmt{Name: "existing", Op: "=", Val: &IntLit{}},
		&MultiAssign{Names: []string{"b", "c"}, Op: ":="},
		&RangeStmt{
			Key: "i", Val: "v",
			Body: []Stmt{&AssignStmt{Name: "inner", Op: ":=", Val: &IntLit{}}},
		},
		&IfStmt{
			Then: []Stmt{&AssignStmt{Name: "then_local", Op: ":=", Val: &IntLit{}}},
			Else: []Stmt{&AssignStmt{Name: "else_local", Op: ":=", Val: &IntLit{}}},
		},
		&WhileStmt{
			Body: []Stmt{&AssignStmt{Name: "while_local", Op: ":=", Val: &IntLit{}}},
		},
		&ArenaStmt{
			Body: []Stmt{&AssignStmt{Name: "arena_local", Op: ":=", Val: &IntLit{}}},
		},
	}

	into := map[string]bool{}
	collectLocals(body, into)

	want := []string{"a", "b", "c", "i", "v", "inner", "then_local", "else_local", "while_local", "arena_local"}
	for _, name := range want {
		if !into[name] {
			t.Errorf("collectLocals: missing %q in %v", name, into)
		}
	}
	if into["existing"] {
		t.Errorf("collectLocals: plain `=` must not bind a local, got %v", into)
	}
	if len(into) != len(want) {
		t.Errorf("collectLocals: got %d locals %v, want exactly %v", len(into), into, want)
	}
}

func TestRaceFindingToDiagnostic(t *testing.T) {
	cases := []struct {
		kind     string
		wantCode string
		wantMsg  string
	}{
		{"write/write", "RACE001", "data race on `x` (write/write): a; b"},
		{"use-after-move", "RACE004", "use after move: a; b"},
		{"read/write", "RACE002", "data race on `x` (read/write): a; b"},
	}
	for _, c := range cases {
		rf := raceFinding{Root: "x", Decl: "f", Kind: c.kind, Writers: []string{"a", "b"}}
		d := rf.toDiagnostic()
		if d.Code != c.wantCode {
			t.Errorf("Kind %q: Code = %q, want %q", c.kind, d.Code, c.wantCode)
		}
		if d.Message != c.wantMsg {
			t.Errorf("Kind %q: Message = %q, want %q", c.kind, d.Message, c.wantMsg)
		}
		if d.Severity != "error" || d.Phase != "race" || d.Decl != "f" {
			t.Errorf("Kind %q: unexpected Diagnostic shape: %+v", c.kind, d)
		}
	}
}

// accessSummary must record a function's direct param accesses plus, via its
// call-graph fixed point, the accesses its callees perform transitively.
func TestAccessSummary(t *testing.T) {
	prog, err := ParseProgram([]string{
		"func poke(ys, k){ys[k]=1}",
		"func worker(xs, id){poke(xs, id)}",
		"func reader(zs){v:=zs[0] print(v)}",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sum := accessSummary(prog)

	poke := sum["poke"]
	if len(poke) != 1 || poke[0].idx != 0 || !poke[0].write || len(poke[0].path) != 1 {
		t.Fatalf("poke: direct write access = %+v, want one write on param 0 with an index step", poke)
	}

	worker := sum["worker"]
	if len(worker) != 1 || worker[0].idx != 0 || !worker[0].write {
		t.Fatalf("worker: transitive access via poke = %+v, want one write on param 0", worker)
	}

	reader := sum["reader"]
	if len(reader) != 1 || reader[0].idx != 0 || reader[0].write {
		t.Fatalf("reader: direct read access = %+v, want one read on param 0", reader)
	}
}

// noteReads records every shared element/field READ inside an expression.
func TestNoteReads(t *testing.T) {
	var calls []struct {
		root string
		path accPath
		write bool
		byGo bool
		desc string
	}
	note := func(root string, path accPath, write, byGo bool, desc string) {
		calls = append(calls, struct {
			root string
			path accPath
			write bool
			byGo bool
			desc string
		}{root, path, write, byGo, desc})
	}

	// Index expression: arr[i] should note a read of arr at index path
	noteReads(&Index{X: &Ident{Name: "arr"}, Idx: &Ident{Name: "i"}}, note)
	if len(calls) != 1 || calls[0].root != "arr" || len(calls[0].path) != 1 || calls[0].write {
		t.Fatalf("noteReads(Index) = %+v, want one read of arr with index step", calls)
	}

	calls = nil
	// Field access: p.x should note a read of p at field path
	noteReads(&FieldAccess{X: &Ident{Name: "p"}, Name: "x"}, note)
	if len(calls) != 1 || calls[0].root != "p" || len(calls[0].path) != 1 || calls[0].write {
		t.Fatalf("noteReads(FieldAccess) = %+v, want one read of p with field step", calls)
	}

	calls = nil
	// Binary: x + y should note reads of both operands
	noteReads(&Binary{Op: "+", L: &Ident{Name: "x"}, R: &Ident{Name: "y"}}, note)
	// Neither x nor y are place expressions (just idents), so no calls expected
	if len(calls) != 0 {
		t.Fatalf("noteReads(Binary with plain idents) = %+v, want no calls (idents are not place expressions)", calls)
	}

	calls = nil
	// Nested: arr[i].x should note reads of both the index and field access (two calls due to recursion)
	noteReads(&FieldAccess{X: &Index{X: &Ident{Name: "arr"}, Idx: &Ident{Name: "i"}}, Name: "x"}, note)
	if len(calls) != 2 {
		t.Fatalf("noteReads(nested Index.Field) = %+v, want two calls (one for Index, one for FieldAccess)", calls)
	}
	// First call is for the FieldAccess on the Index result (path: index + field)
	if calls[0].root != "arr" || len(calls[0].path) != 2 || calls[0].write {
		t.Fatalf("noteReads(nested Index.Field) first call = %+v, want read of arr with index+field steps", calls[0])
	}
	// Second call is for the Index itself (path: index only)
	if calls[1].root != "arr" || len(calls[1].path) != 1 || calls[1].write {
		t.Fatalf("noteReads(nested Index.Field) second call = %+v, want read of arr with index step", calls[1])
	}

	calls = nil
	// nil expression should not panic
	noteReads(nil, note)
	if len(calls) != 0 {
		t.Fatalf("noteReads(nil) = %+v, want no calls", calls)
	}
}
