package main

import "testing"

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
