package main

// Slice 1.1 — inferred data-race detection (type-aware, reads + writes).
//
// The "better than Rust" concurrency guarantee, with ZERO Send/Sync annotations:
// infer which heap locations are shared *and* concurrently accessed across goroutine
// boundaries, at least one access a write, and report the race with a counterexample.
//
// Two-stage design:
//   1. A structural, per-function ACCESS SUMMARY (fixed-point over the call graph):
//      for each function, which access PATHS (index / field chains) through each
//      parameter are read or written — directly or transitively via callees.
//   2. A per-instance RACE PASS: at each `go f(args)` the goroutine's param accesses
//      are re-rooted onto the caller's argument; combined with the spawner's own
//      accesses, a root reached by >=2 concurrent accessors (>=1 write) is a race —
//      but only if the access TYPE actually reaches shared heap (a slice/map indirection).
//
// Sharing is reachability-based (empirically grounded): structs pass by value, so a
// scalar-field write on a struct arg races nothing; but a slice *inside* that struct
// keeps its shared backing array, so `s.items[i] = v` does race. `typeShared` walks the
// access path against the argument's resolved type to decide.
//
// Soundness stance (a borrow checker's): concurrency is over-approximated (every
// goroutine in a scope is treated as live alongside the spawner) and slice indices are
// ignored — sound, may over-report, never under-reports. Happens-before precision
// (channel joins) lands in Slice 1.4.

import (
	"fmt"
	"sort"
	"strings"
)

// ── access paths ────────────────────────────────────────────────────────────
// An accStep is one navigation from a root: an index step ([], field == "") or a
// struct-field step (field name). A path is the chain from the root outward.
type accStep struct{ field string }
type accPath []accStep

func clonePath(p accPath) accPath {
	if len(p) == 0 {
		return nil
	}
	q := make(accPath, len(p))
	copy(q, p)
	return q
}

func pathEq(a, b accPath) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// placeOf decomposes a place expression into (rootIdent, path). Returns ok=false
// when the expression is not rooted in a named variable.
func placeOf(e Expr) (string, accPath, bool) {
	switch t := e.(type) {
	case *Ident:
		return t.Name, nil, true
	case *Index:
		r, p, ok := placeOf(t.X)
		if !ok {
			return "", nil, false
		}
		return r, append(clonePath(p), accStep{}), true
	case *FieldAccess:
		r, p, ok := placeOf(t.X)
		if !ok {
			return "", nil, false
		}
		return r, append(clonePath(p), accStep{field: t.Name}), true
	}
	return "", nil, false
}

// ── stage 1: the per-function access summary ────────────────────────────────
type paramAcc struct {
	idx   int
	path  accPath
	write bool
}

// accessSummary computes, for every function, the parameter accesses it performs —
// directly and transitively through calls / goroutine spawns — to a fixed point.
func accessSummary(prog *Program) map[string][]paramAcc {
	sum := map[string][]paramAcc{}
	for _, f := range prog.Funcs {
		sum[f.Name] = nil
	}
	add := func(fn string, a paramAcc) bool {
		for _, e := range sum[fn] {
			if e.idx == a.idx && e.write == a.write && pathEq(e.path, a.path) {
				return false
			}
		}
		sum[fn] = append(sum[fn], paramAcc{a.idx, clonePath(a.path), a.write})
		return true
	}

	for changed := true; changed; {
		changed = false
		for _, f := range prog.Funcs {
			pidx := map[string]int{}
			for i, p := range f.Params {
				pidx[p] = i
			}
			record := func(root string, path accPath, write bool) {
				if i, ok := pidx[root]; ok {
					if add(f.Name, paramAcc{i, path, write}) {
						changed = true
					}
				}
			}
			// a call/go: re-root each callee param access onto our matching arg.
			propCall := func(callee string, args []Expr) {
				for j, a := range args {
					r, pfx, ok := placeOf(a)
					if !ok {
						continue
					}
					pi, isParam := pidx[r]
					if !isParam {
						continue
					}
					for _, ca := range sum[callee] {
						if ca.idx != j {
							continue
						}
						full := append(clonePath(pfx), ca.path...)
						if add(f.Name, paramAcc{pi, full, ca.write}) {
							changed = true
						}
					}
				}
			}
			var visitExpr func(e Expr)
			visitExpr = func(e Expr) {
				switch t := e.(type) {
				case nil:
					return
				case *Index:
					if r, p, ok := placeOf(t); ok {
						record(r, p, false) // element read
					}
					visitExpr(t.X)
					visitExpr(t.Idx)
				case *FieldAccess:
					if r, p, ok := placeOf(t); ok {
						record(r, p, false)
					}
					visitExpr(t.X)
				case *Call:
					propCall(t.Callee, t.Args)
					for _, a := range t.Args {
						visitExpr(a)
					}
				case *Binary:
					visitExpr(t.L)
					visitExpr(t.R)
				case *Unary:
					visitExpr(t.X)
				case *SliceLit:
					for _, el := range t.Elems {
						visitExpr(el)
					}
				case *StructLit:
					for _, v := range t.Vals {
						visitExpr(v)
					}
				}
			}
			var visit func(ss []Stmt)
			visit = func(ss []Stmt) {
				for _, s := range ss {
					switch st := s.(type) {
					case *IndexAssign:
						if r, p, ok := placeOf(st.Target); ok {
							record(r, p, true)
						}
						visitExpr(st.Target.Idx)
						visitExpr(st.Val)
					case *FieldAssign:
						if r, p, ok := placeOf(st.Target); ok {
							record(r, p, true)
						}
						visitExpr(st.Val)
					case *AssignStmt:
						visitExpr(st.Val)
					case *MultiAssign:
						for _, e := range st.Rhs {
							visitExpr(e)
						}
					case *ExprStmt:
						visitExpr(st.X)
					case *ReturnStmt:
						for _, e := range st.Vals {
							visitExpr(e)
						}
					case *SendStmt:
						visitExpr(st.Ch)
						visitExpr(st.Val)
					case *GoStmt:
						propCall(st.Call.Callee, st.Call.Args)
						for _, a := range st.Call.Args {
							visitExpr(a)
						}
					case *IfStmt:
						visitExpr(st.Cond)
						visit(st.Then)
						visit(st.Else)
					case *WhileStmt:
						visitExpr(st.Cond)
						visit(st.Body)
					case *RangeStmt:
						visitExpr(st.X)
						visit(st.Body)
					case *ArenaStmt:
						visit(st.Body)
					}
				}
			}
			visit(f.Body)
		}
	}
	return sum
}

// ── stage 2: the race pass ──────────────────────────────────────────────────
type raceFinding struct {
	Root    string   // the shared variable
	Decl    string   // source function the concurrent accesses live in
	Kind    string   // "write/write" | "read/write"
	Writers []string // counterexample: each concurrent accessor
}

type accessor struct {
	write bool
	byGo  bool
	mult  int // concurrency multiplicity: 2 for a goroutine spawned inside a loop
	//         (N concurrent instances from one site), else 1
	desc string
}

// slotTypeName renders a checker type slot as an MFL type string ("[]int",
// "map[string]int", "Box", ...) so reachability can walk it uniformly with the
// field-type strings the struct table returns.
func slotTypeName(c *Checker, slot int) string {
	s := c.find(slot)
	switch c.kind[s] {
	case KInt, KNum:
		return "int"
	case KFloat:
		return "float"
	case KBool:
		return "bool"
	case KString:
		return "string"
	case KBytes:
		return "bytes"
	case KSlice:
		if c.elem[s] >= 0 {
			return "[]" + slotTypeName(c, c.elem[s])
		}
		return "[]?"
	case KMap:
		if c.mkey[s] >= 0 && c.mval[s] >= 0 {
			return "map[" + slotTypeName(c, c.mkey[s]) + "]" + slotTypeName(c, c.mval[s])
		}
		return "map[?]?"
	case KChan:
		if c.elem[s] >= 0 {
			return "chan " + slotTypeName(c, c.elem[s])
		}
		return "chan ?"
	case KStruct:
		return c.sname[s]
	}
	return "?"
}

// typeShared reports whether an access path, applied to a value of type tname,
// reaches SHARED heap — i.e. it dereferences through a slice/map (whose backing
// store is aliased across the goroutine boundary) at or before the accessed
// location. Struct navigation on a by-value copy stays private until it crosses
// such an indirection.
func typeShared(c *Checker, tname string, path accPath) bool {
	cur := tname
	shared := false
	for _, step := range path {
		if step.field == "" { // index step
			switch {
			case strings.HasPrefix(cur, "[]"):
				shared = true
				cur = cur[2:]
			case strings.HasPrefix(cur, "map["):
				shared = true
				cur = mapValType(cur)
			default:
				return shared // indexing a string/bytes: no struct sharing beyond here
			}
		} else { // field step
			ft, ok := c.fieldType(cur, step.field)
			if !ok {
				return shared
			}
			cur = ft
		}
	}
	return shared
}

// mapValType extracts V from "map[K]V" (K has no unbalanced brackets in practice).
func mapValType(t string) string {
	depth := 0
	for i := 4; i < len(t); i++ {
		switch t[i] {
		case '[':
			depth++
		case ']':
			if depth == 0 {
				return t[i+1:]
			}
			depth--
		}
	}
	return "?"
}

// goAcc is one access a goroutine performs on a caller variable.
type goAcc struct {
	root  string
	path  accPath
	write bool
	mult  int
	desc  string
}

// goAccessorsOf re-roots a goroutine's parameter accesses onto the caller's args.
func goAccessorsOf(st *GoStmt, inLoop bool, sum map[string][]paramAcc) []goAcc {
	g := st.Call.Callee
	mult, label := 1, "goroutine"
	if inLoop {
		mult, label = 2, "loop-spawned goroutine"
	}
	var out []goAcc
	for j, a := range st.Call.Args {
		r, pfx, ok := placeOf(a)
		if !ok {
			continue
		}
		for _, ca := range sum[g] {
			if ca.idx != j {
				continue
			}
			full := append(clonePath(pfx), ca.path...)
			verb := "reads"
			if ca.write {
				verb = "writes"
			}
			out = append(out, goAcc{r, full, ca.write, mult,
				fmt.Sprintf("%s `go %s(...)` %s `%s`", label, g, verb, r)})
		}
	}
	return out
}

// sigCompleteParams returns the parameter indices a function "signals complete" on:
// its LAST statement is `p <- ...`. Because the send is last, every access in the
// body happens-before it, so a caller that receives from that channel has provably
// waited for the whole body — the sound basis for a channel-join barrier.
func sigCompleteParams(f *FuncDecl) map[int]bool {
	out := map[int]bool{}
	if f == nil || len(f.Body) == 0 {
		return out
	}
	if ss, ok := f.Body[len(f.Body)-1].(*SendStmt); ok {
		if id, ok := ss.Ch.(*Ident); ok {
			for i, p := range f.Params {
				if p == id.Name {
					out[i] = true
				}
			}
		}
	}
	return out
}

// recvChanName returns the channel a bare `<-ch` receive reads.
func recvChanName(e Expr) (string, bool) {
	if r, ok := e.(*Recv); ok {
		if id, ok := r.Ch.(*Ident); ok {
			return id.Name, true
		}
	}
	return "", false
}

// markLoopSpawns pre-marks roots touched by goroutines inside a loop body as live,
// so every this-thread access in the loop is treated as concurrent (a later
// iteration's access races an earlier iteration's goroutine).
func markLoopSpawns(ss []Stmt, live map[string]int, sum map[string][]paramAcc) {
	for _, s := range ss {
		switch st := s.(type) {
		case *GoStmt:
			for _, a := range goAccessorsOf(st, true, sum) {
				live[a.root]++
			}
		case *IfStmt:
			markLoopSpawns(st.Then, live, sum)
			markLoopSpawns(st.Else, live, sum)
		case *WhileStmt:
			markLoopSpawns(st.Body, live, sum)
		case *RangeStmt:
			markLoopSpawns(st.Body, live, sum)
		case *ArenaStmt:
			markLoopSpawns(st.Body, live, sum)
		}
	}
}

// collectClosureVars maps each variable bound to a closure literal to its
// MakeClosure (lifted lambda + captured outer-variable names). Closures are the
// only way captured state reaches a goroutine in MFL: `go` rejects lambdas and
// closure-valued callees, so a capturing closure must travel as a func-valued
// argument to a `go`-spawned function that invokes it (Slice 1.3).
func collectClosureVars(ss []Stmt, into map[string]*MakeClosure) {
	for _, s := range ss {
		switch st := s.(type) {
		case *AssignStmt:
			if mc, ok := st.Val.(*MakeClosure); ok {
				into[st.Name] = mc
			}
		case *IfStmt:
			collectClosureVars(st.Then, into)
			collectClosureVars(st.Else, into)
		case *WhileStmt:
			collectClosureVars(st.Body, into)
		case *RangeStmt:
			collectClosureVars(st.Body, into)
		case *ArenaStmt:
			collectClosureVars(st.Body, into)
		}
	}
}

func copyIntMap(m map[string]int) map[string]int {
	n := make(map[string]int, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}

// detectRaces runs the inferred data-race analysis over a type-checked program.
func detectRaces(prog *Program, c *Checker) []raceFinding {
	sum := accessSummary(prog)
	byName := map[string]*FuncDecl{}
	for _, fn := range prog.Funcs {
		byName[fn.Name] = fn
	}
	var out []raceFinding
	seen := map[string]bool{}

	for _, inst := range c.reps {
		f := c.instFn[inst]
		if f == nil {
			continue
		}
		// Order-sensitive accessor gathering (Slice 1.4 happens-before). `live[root]`
		// counts spawned-but-not-yet-joined goroutines touching root; a THIS-THREAD
		// access is concurrent only while live>0. Accesses before the first relevant
		// spawn are ordered-before it; accesses after a channel-join barrier that
		// drains all such goroutines are ordered-after — both are safe. Goroutine
		// accessors are always recorded (they overlap each other and post-spawn code).
		accs := map[string][]accessor{}
		closureVars := map[string]*MakeClosure{}
		collectClosureVars(f.Body, closureVars)
		recordGo := func(a goAcc) {
			slot, ok := c.vars[inst][a.root]
			if !ok || !typeShared(c, slotTypeName(c, slot), a.path) {
				return
			}
			accs[a.root] = append(accs[a.root], accessor{a.write, true, a.mult, a.desc})
		}
		recordThis := func(root string, path accPath, write bool, live map[string]int, desc string) {
			if live[root] <= 0 { // ordered-before, or post-join: not concurrent
				return
			}
			slot, ok := c.vars[inst][root]
			if !ok || !typeShared(c, slotTypeName(c, slot), path) {
				return
			}
			accs[root] = append(accs[root], accessor{write, false, 1, desc})
		}

		var visit func(ss []Stmt, inLoop, top bool, live, spawnCnt, recvCnt map[string]int, pending map[string]map[string]int)
		visit = func(ss []Stmt, inLoop, top bool, live, spawnCnt, recvCnt map[string]int, pending map[string]map[string]int) {
			reads := func(e Expr) {
				noteReads(e, func(root string, path accPath, write, byGo bool, desc string) {
					recordThis(root, path, write, live, desc)
				})
			}
			// a top-level receive on a join channel: once received >= spawned, every
			// goroutine that signals-complete on it has finished -> drain their roots.
			joinRecv := func(e Expr) {
				if !top || inLoop {
					return
				}
				ch, ok := recvChanName(e)
				if !ok {
					return
				}
				recvCnt[ch]++
				if spawnCnt[ch] >= 1 && recvCnt[ch] >= spawnCnt[ch] {
					for root, n := range pending[ch] {
						if live[root] -= n; live[root] < 0 {
							live[root] = 0
						}
					}
					pending[ch] = map[string]int{}
					spawnCnt[ch] = 0
					recvCnt[ch] = 0
				}
			}
			for _, s := range ss {
				switch st := s.(type) {
				case *GoStmt:
					mult, label := 1, "goroutine"
					if inLoop {
						mult, label = 2, "loop-spawned goroutine"
					}
					touched := map[string]bool{}
					for _, a := range goAccessorsOf(st, inLoop, sum) {
						recordGo(a)
						touched[a.root] = true
					}
					// closure captures escaping to the goroutine (Slice 1.3): a closure
					// arg re-roots its lifted lambda's accesses onto the CAPTURED outer
					// variables, which are shared by-reference with this scope.
					for _, arg := range st.Call.Args {
						var mc *MakeClosure
						switch av := arg.(type) {
						case *MakeClosure:
							mc = av
						case *Ident:
							mc = closureVars[av.Name]
						}
						if mc == nil {
							continue
						}
						for _, ca := range sum[mc.FuncName] {
							if ca.idx >= len(mc.Captures) {
								continue
							}
							outer := mc.Captures[ca.idx]
							verb := "reads"
							if ca.write {
								verb = "writes"
							}
							recordGo(goAcc{outer, ca.path, ca.write, mult,
								fmt.Sprintf("%s closure in `go %s(...)` %s captured `%s`", label, st.Call.Callee, verb, outer)})
							touched[outer] = true
						}
					}
					for r := range touched {
						live[r]++
					}
					if top && !inLoop { // join bookkeeping (sound only on the linear path)
						sig := sigCompleteParams(byName[st.Call.Callee])
						for j, arg := range st.Call.Args {
							if !sig[j] {
								continue
							}
							if id, ok := arg.(*Ident); ok {
								spawnCnt[id.Name]++
								if pending[id.Name] == nil {
									pending[id.Name] = map[string]int{}
								}
								for r := range touched {
									pending[id.Name][r]++
								}
							}
						}
					}
				case *IndexAssign:
					if r, p, ok := placeOf(st.Target); ok {
						recordThis(r, p, true, live, fmt.Sprintf("this thread writes `%s`", r))
					}
				case *FieldAssign:
					if r, p, ok := placeOf(st.Target); ok {
						recordThis(r, p, true, live, fmt.Sprintf("this thread writes `%s`", r))
					}
				case *ExprStmt:
					joinRecv(st.X)
					reads(st.X)
				case *AssignStmt:
					joinRecv(st.Val)
					reads(st.Val)
				case *MultiAssign:
					for _, e := range st.Rhs {
						joinRecv(e)
						reads(e)
					}
				case *ReturnStmt:
					for _, e := range st.Vals {
						reads(e)
					}
				case *IfStmt:
					reads(st.Cond)
					thenLive, elseLive := copyIntMap(live), copyIntMap(live)
					visit(st.Then, inLoop, false, thenLive, spawnCnt, recvCnt, pending)
					visit(st.Else, inLoop, false, elseLive, spawnCnt, recvCnt, pending)
					for k, v := range thenLive { // conservative merge: live if either branch
						if v > live[k] {
							live[k] = v
						}
					}
					for k, v := range elseLive {
						if v > live[k] {
							live[k] = v
						}
					}
				case *WhileStmt:
					reads(st.Cond)
					markLoopSpawns(st.Body, live, sum)
					visit(st.Body, true, false, live, spawnCnt, recvCnt, pending)
				case *RangeStmt:
					reads(st.X)
					markLoopSpawns(st.Body, live, sum)
					visit(st.Body, true, false, live, spawnCnt, recvCnt, pending)
				case *ArenaStmt:
					visit(st.Body, inLoop, top, live, spawnCnt, recvCnt, pending)
				}
			}
		}
		visit(f.Body, false, true, map[string]int{}, map[string]int{}, map[string]int{}, map[string]map[string]int{})

		// a race: >=2 concurrent "threads" reach a shared root, >=1 of them a write,
		// >=1 a goroutine. Multiplicity counts a loop-spawned goroutine as 2 threads,
		// so a single loop-spawned writer races itself (write/write).
		for root, as := range accs {
			goCount, threads, writeThreads := 0, 0, 0
			for _, a := range as {
				threads += a.mult
				if a.byGo {
					goCount++
				}
				if a.write {
					writeThreads += a.mult
				}
			}
			if goCount >= 1 && threads >= 2 && writeThreads >= 1 {
				kind := "read/write"
				if writeThreads >= 2 {
					kind = "write/write"
				}
				descs := make([]string, len(as))
				for i, a := range as {
					descs[i] = a.desc
				}
				sort.Strings(descs)
				key := f.Name + "|" + root + "|" + strings.Join(descs, "|")
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, raceFinding{Root: root, Decl: f.Name, Kind: kind, Writers: descs})
			}
		}
	}
	out = append(out, globalRaces(prog, c)...)
	out = append(out, useAfterSend(prog, c)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Decl != out[j].Decl {
			return out[i].Decl < out[j].Decl
		}
		return out[i].Root < out[j].Root
	})
	return out
}

// ── globals (Slice 1.2) ─────────────────────────────────────────────────────
// A package-level `var` is a single cell shared by every goroutine — so unlike a
// parameter (copied at the call boundary), even a SCALAR global races when two
// concurrent threads touch it and one writes. Sharing is unconditional; no
// reachability gate. This catches the canonical shared-counter race.

type gAccess struct{ read, write bool }
type goSite struct {
	callee string
	inLoop bool
}

// collectLocals records the names a function binds (params + `:=` decls + range
// vars), which SHADOW a package global of the same name (MFL has flat scope).
func collectLocals(ss []Stmt, into map[string]bool) {
	for _, s := range ss {
		switch st := s.(type) {
		case *AssignStmt:
			if st.Op == ":=" {
				into[st.Name] = true
			}
		case *MultiAssign:
			if st.Op == ":=" {
				for _, n := range st.Names {
					into[n] = true
				}
			}
		case *RangeStmt:
			if st.Key != "" {
				into[st.Key] = true
			}
			if st.Val != "" {
				into[st.Val] = true
			}
			collectLocals(st.Body, into)
		case *IfStmt:
			collectLocals(st.Then, into)
			collectLocals(st.Else, into)
		case *WhileStmt:
			collectLocals(st.Body, into)
		case *ArenaStmt:
			collectLocals(st.Body, into)
		}
	}
}

// mergeGAcc ORs an access into a per-global map; returns true if it changed.
func mergeGAcc(m map[string]*gAccess, g string, a *gAccess) bool {
	e := m[g]
	if e == nil {
		m[g] = &gAccess{a.read, a.write}
		return true
	}
	changed := false
	if a.read && !e.read {
		e.read = true
		changed = true
	}
	if a.write && !e.write {
		e.write = true
		changed = true
	}
	return changed
}

// collectGlobalAccess records direct reads/writes of package globals in a body,
// skipping names shadowed by a local. `sink` receives (global, read, write).
func collectGlobalAccess(ss []Stmt, gset, local map[string]bool, sink func(g string, read, write bool)) {
	isG := func(root string) bool { return gset[root] && !local[root] }
	var rd func(e Expr)
	rd = func(e Expr) {
		switch t := e.(type) {
		case nil:
			return
		case *Ident:
			if isG(t.Name) {
				sink(t.Name, true, false)
			}
		case *Index:
			if r, _, ok := placeOf(t); ok && isG(r) {
				sink(r, true, false)
			}
			rd(t.X)
			rd(t.Idx)
		case *FieldAccess:
			if r, _, ok := placeOf(t); ok && isG(r) {
				sink(r, true, false)
			}
			rd(t.X)
		case *Call:
			for _, a := range t.Args {
				rd(a)
			}
		case *Binary:
			rd(t.L)
			rd(t.R)
		case *Unary:
			rd(t.X)
		case *SliceLit:
			for _, el := range t.Elems {
				rd(el)
			}
		case *StructLit:
			for _, v := range t.Vals {
				rd(v)
			}
		}
	}
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch st := s.(type) {
			case *AssignStmt:
				if st.Op == "=" && isG(st.Name) {
					sink(st.Name, false, true) // whole-cell write
				}
				rd(st.Val)
			case *MultiAssign:
				for _, e := range st.Rhs {
					rd(e)
				}
			case *IndexAssign:
				if r, _, ok := placeOf(st.Target); ok && isG(r) {
					sink(r, false, true)
				}
				rd(st.Target.Idx)
				rd(st.Val)
			case *FieldAssign:
				if r, _, ok := placeOf(st.Target); ok && isG(r) {
					sink(r, false, true)
				}
				rd(st.Val)
			case *ExprStmt:
				rd(st.X)
			case *ReturnStmt:
				for _, e := range st.Vals {
					rd(e)
				}
			case *SendStmt:
				rd(st.Ch)
				rd(st.Val)
			case *GoStmt:
				for _, a := range st.Call.Args {
					rd(a)
				}
			case *IfStmt:
				rd(st.Cond)
				walk(st.Then)
				walk(st.Else)
			case *WhileStmt:
				rd(st.Cond)
				walk(st.Body)
			case *RangeStmt:
				rd(st.X)
				walk(st.Body)
			case *ArenaStmt:
				walk(st.Body)
			}
		}
	}
	walk(ss)
}

// collectCallGraph gathers, per function body, its normal callees and its `go`
// spawn sites (with loop multiplicity).
func collectCallGraph(ss []Stmt, inLoop bool, normal *[]string, gos *[]goSite) {
	var rd func(e Expr)
	rd = func(e Expr) {
		switch t := e.(type) {
		case *Call:
			*normal = append(*normal, t.Callee)
			for _, a := range t.Args {
				rd(a)
			}
		case *Binary:
			rd(t.L)
			rd(t.R)
		case *Unary:
			rd(t.X)
		case *Index:
			rd(t.X)
			rd(t.Idx)
		case *FieldAccess:
			rd(t.X)
		case *SliceLit:
			for _, el := range t.Elems {
				rd(el)
			}
		case *StructLit:
			for _, v := range t.Vals {
				rd(v)
			}
		}
	}
	for _, s := range ss {
		switch st := s.(type) {
		case *GoStmt:
			*gos = append(*gos, goSite{st.Call.Callee, inLoop})
			for _, a := range st.Call.Args {
				rd(a)
			}
		case *AssignStmt:
			rd(st.Val)
		case *MultiAssign:
			for _, e := range st.Rhs {
				rd(e)
			}
		case *IndexAssign:
			rd(st.Val)
		case *FieldAssign:
			rd(st.Val)
		case *ExprStmt:
			rd(st.X)
		case *ReturnStmt:
			for _, e := range st.Vals {
				rd(e)
			}
		case *SendStmt:
			rd(st.Val)
		case *IfStmt:
			rd(st.Cond)
			collectCallGraph(st.Then, inLoop, normal, gos)
			collectCallGraph(st.Else, inLoop, normal, gos)
		case *WhileStmt:
			collectCallGraph(st.Body, true, normal, gos)
		case *RangeStmt:
			collectCallGraph(st.Body, true, normal, gos)
		case *ArenaStmt:
			collectCallGraph(st.Body, inLoop, normal, gos)
		}
	}
}

// mainPostGlobals returns main's DIRECT global accesses that occur after its first
// `go` spawn — the ones concurrent with the goroutines. Accesses strictly before
// the first spawn are ordered-before (goroutine creation is a happens-before edge)
// and excluded, so the common "init a global, then spawn readers" pattern is safe.
func mainPostGlobals(body []Stmt, gset, local map[string]bool) map[string]*gAccess {
	out := map[string]*gAccess{}
	spawned := false
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			if _, ok := s.(*GoStmt); ok {
				spawned = true
				continue
			}
			if !spawned {
				// descend to catch a spawn nested in an early block, but don't collect
				if b, ok := blockOf(s); ok {
					walk(b)
				}
				continue
			}
			collectGlobalAccess([]Stmt{s}, gset, local, func(g string, r, w bool) {
				mergeGAcc(out, g, &gAccess{r, w})
			})
		}
	}
	walk(body)
	return out
}

// mainPostCallees returns the set of functions main invokes as NORMAL (non-`go`)
// calls at or after its first `go` spawn. A callee invoked strictly before the
// first spawn runs entirely before goroutine creation — a happens-before edge — so
// its global writes are ordered with respect to the spawned goroutines and must not
// be treated as concurrent. Excluding those callees propagates the caller's program
// order through the call, so the canonical "init globals in a helper, then spawn a
// worker pool" pattern is race-free (issue #434: interprocedural happens-before).
func mainPostCallees(body []Stmt) map[string]bool {
	out := map[string]bool{}
	spawned := false
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			if _, ok := s.(*GoStmt); ok {
				spawned = true
				continue
			}
			if !spawned {
				// descend to catch a spawn nested in an early block, but don't collect
				if b, ok := blockOf(s); ok {
					walk(b)
				}
				continue
			}
			var normal []string
			var gos []goSite
			collectCallGraph([]Stmt{s}, false, &normal, &gos)
			for _, n := range normal {
				out[n] = true
			}
		}
	}
	walk(body)
	return out
}

func blockOf(s Stmt) ([]Stmt, bool) {
	switch st := s.(type) {
	case *IfStmt:
		return append(append([]Stmt{}, st.Then...), st.Else...), true
	case *WhileStmt:
		return st.Body, true
	case *RangeStmt:
		return st.Body, true
	case *ArenaStmt:
		return st.Body, true
	}
	return nil, false
}

// globalRaces detects data races on package globals across the whole program.
func globalRaces(prog *Program, c *Checker) []raceFinding {
	gset := map[string]bool{}
	for _, g := range prog.Globals {
		gset[g.Name] = true
	}
	if len(gset) == 0 {
		return nil
	}
	local := map[string]map[string]bool{}
	gdirect := map[string]map[string]*gAccess{}
	normalCallees := map[string][]string{}
	var allGo []goSite
	var mainFn *FuncDecl
	for _, f := range prog.Funcs {
		ln := map[string]bool{}
		for _, p := range f.Params {
			ln[p] = true
		}
		collectLocals(f.Body, ln)
		local[f.Name] = ln
		gd := map[string]*gAccess{}
		collectGlobalAccess(f.Body, gset, ln, func(g string, r, w bool) { mergeGAcc(gd, g, &gAccess{r, w}) })
		gdirect[f.Name] = gd
		var normal []string
		var gos []goSite
		collectCallGraph(f.Body, false, &normal, &gos)
		normalCallees[f.Name] = normal
		allGo = append(allGo, gos...)
		if f.Name == "main" {
			mainFn = f
		}
	}
	// transitive global footprint over NORMAL calls (a goroutine is a separate thread)
	gall := map[string]map[string]*gAccess{}
	for n, gd := range gdirect {
		m := map[string]*gAccess{}
		for g, a := range gd {
			m[g] = &gAccess{a.read, a.write}
		}
		gall[n] = m
	}
	for changed := true; changed; {
		changed = false
		for fn, cs := range normalCallees {
			for _, ce := range cs {
				for g, a := range gall[ce] {
					if mergeGAcc(gall[fn], g, a) {
						changed = true
					}
				}
			}
		}
	}
	// main-thread concurrent footprint: direct post-spawn + normal-callee transitive
	mainConc := map[string]*gAccess{}
	if mainFn != nil {
		for g, a := range mainPostGlobals(mainFn.Body, gset, local["main"]) {
			mergeGAcc(mainConc, g, a)
		}
		// Only callees invoked at/after the first spawn are concurrent; a callee
		// completed before the spawn happens-before the goroutines (issue #434).
		postCallees := mainPostCallees(mainFn.Body)
		for _, ce := range normalCallees["main"] {
			if !postCallees[ce] {
				continue
			}
			for g, a := range gall[ce] {
				mergeGAcc(mainConc, g, a)
			}
		}
	}

	var out []raceFinding
	for g := range gset {
		type th struct {
			write bool
			mult  int
			desc  string
		}
		var threads []th
		for _, gs := range allGo {
			a := gall[gs.callee][g]
			if a == nil {
				continue
			}
			mult := 1
			label := "goroutine"
			if gs.inLoop {
				mult = 2
				label = "loop-spawned goroutine"
			}
			verb := "reads"
			if a.write {
				verb = "writes"
			}
			threads = append(threads, th{a.write, mult, fmt.Sprintf("%s `go %s(...)` %s global `%s`", label, gs.callee, verb, g)})
		}
		goThreads := len(threads)
		if a := mainConc[g]; a != nil {
			verb := "reads"
			if a.write {
				verb = "writes"
			}
			threads = append(threads, th{a.write, 1, fmt.Sprintf("main thread %s global `%s`", verb, g)})
		}
		total, writes := 0, 0
		for _, t := range threads {
			total += t.mult
			if t.write {
				writes += t.mult
			}
		}
		if goThreads >= 1 && total >= 2 && writes >= 1 {
			kind := "read/write"
			if writes >= 2 {
				kind = "write/write"
			}
			descs := make([]string, len(threads))
			for i, t := range threads {
				descs[i] = t.desc
			}
			sort.Strings(descs)
			// no single enclosing decl — the message already says "global `g`"
			out = append(out, raceFinding{Root: g, Decl: "", Kind: kind, Writers: descs})
		}
	}
	return out
}

// ── move-on-send (Slice 1.2) ─────────────────────────────────────────────────
// `ch <- v` TRANSFERS ownership of a shared value v to the receiver. Touching v on
// the sender after the send is a use-after-move: the receiver may now be mutating
// it concurrently. Enforcing this is the formal backbone of "share by communicating"
// — send it, then let it go. A flow-sensitive scan per function (moves are cleared
// when the variable is rebound), shared-typed values only.

// typeSharesHeap reports whether a value of this type carries shared heap (a slice/
// map, directly or inside a struct) — i.e. sending it aliases, so it must be moved.
func typeSharesHeap(c *Checker, tname string, depth int) bool {
	if depth > 8 {
		return false
	}
	if strings.HasPrefix(tname, "[]") || strings.HasPrefix(tname, "map[") {
		return true
	}
	if td := c.structs[tname]; td != nil {
		for _, fld := range td.Fields {
			if typeSharesHeap(c, fld.Type, depth+1) {
				return true
			}
		}
	}
	return false
}

// useAfterSend flags accessing a shared value after it has been sent on a channel.
func useAfterSend(prog *Program, c *Checker) []raceFinding {
	var out []raceFinding
	for _, inst := range c.reps {
		f := c.instFn[inst]
		if f == nil {
			continue
		}
		moved := map[string]bool{} // var -> currently moved (owned by a receiver)
		done := map[string]bool{}  // dedup per var
		isShared := func(name string) bool {
			slot, ok := c.vars[inst][name]
			return ok && typeSharesHeap(c, slotTypeName(c, slot), 0)
		}
		flag := func(name string) {
			if moved[name] && !done[name] {
				done[name] = true
				out = append(out, raceFinding{
					Root: name, Decl: f.Name, Kind: "use-after-move",
					Writers: []string{fmt.Sprintf("`%s` is used after it was sent on a channel (ownership moved to the receiver)", name)},
				})
			}
		}
		var uses func(e Expr)
		uses = func(e Expr) {
			switch t := e.(type) {
			case nil:
				return
			case *Ident:
				flag(t.Name)
			case *Index:
				if r, _, ok := placeOf(t); ok {
					flag(r)
				}
				uses(t.X)
				uses(t.Idx)
			case *FieldAccess:
				if r, _, ok := placeOf(t); ok {
					flag(r)
				}
				uses(t.X)
			case *Call:
				for _, a := range t.Args {
					uses(a)
				}
			case *Binary:
				uses(t.L)
				uses(t.R)
			case *Unary:
				uses(t.X)
			case *SliceLit:
				for _, el := range t.Elems {
					uses(el)
				}
			case *StructLit:
				for _, v := range t.Vals {
					uses(v)
				}
			}
		}
		var scan func(ss []Stmt)
		scan = func(ss []Stmt) {
			for _, s := range ss {
				switch st := s.(type) {
				case *SendStmt:
					uses(st.Ch)
					// the sent value: an already-moved var re-sent is use-after-move;
					// otherwise a shared var becomes moved.
					if id, ok := st.Val.(*Ident); ok {
						if moved[id.Name] {
							flag(id.Name)
						} else if isShared(id.Name) {
							moved[id.Name] = true
						}
					} else {
						uses(st.Val)
					}
				case *AssignStmt:
					uses(st.Val)
					delete(moved, st.Name) // rebound -> fresh value
				case *MultiAssign:
					for _, e := range st.Rhs {
						uses(e)
					}
					for _, n := range st.Names {
						delete(moved, n)
					}
				case *IndexAssign:
					if r, _, ok := placeOf(st.Target); ok {
						flag(r) // writing through a moved handle
					}
					uses(st.Target.Idx)
					uses(st.Val)
				case *FieldAssign:
					if r, _, ok := placeOf(st.Target); ok {
						flag(r)
					}
					uses(st.Val)
				case *ExprStmt:
					uses(st.X)
				case *ReturnStmt:
					for _, e := range st.Vals {
						uses(e)
					}
				case *GoStmt:
					for _, a := range st.Call.Args {
						uses(a)
					}
				case *IfStmt:
					uses(st.Cond)
					scan(st.Then)
					scan(st.Else)
				case *WhileStmt:
					uses(st.Cond)
					scan(st.Body)
				case *RangeStmt:
					uses(st.X)
					delete(moved, st.Key)
					delete(moved, st.Val)
					scan(st.Body)
				case *ArenaStmt:
					scan(st.Body)
				}
			}
		}
		scan(f.Body)
	}
	return out
}

// noteReads records every shared element/field READ inside an expression.
func noteReads(e Expr, note func(root string, path accPath, write, byGo bool, desc string)) {
	switch t := e.(type) {
	case nil:
		return
	case *Index:
		if r, p, ok := placeOf(t); ok {
			note(r, p, false, false, fmt.Sprintf("this thread reads `%s`", r))
		}
		noteReads(t.X, note)
		noteReads(t.Idx, note)
	case *FieldAccess:
		if r, p, ok := placeOf(t); ok {
			note(r, p, false, false, fmt.Sprintf("this thread reads `%s`", r))
		}
		noteReads(t.X, note)
	case *Call:
		for _, a := range t.Args {
			noteReads(a, note)
		}
	case *Binary:
		noteReads(t.L, note)
		noteReads(t.R, note)
	case *Unary:
		noteReads(t.X, note)
	case *SliceLit:
		for _, el := range t.Elems {
			noteReads(el, note)
		}
	case *StructLit:
		for _, v := range t.Vals {
			noteReads(v, note)
		}
	}
}

// toDiagnostic renders a finding as a check.go Diagnostic (Slice 1.5 will extend
// the code taxonomy and JSON surface; option (a): errors in `machin check`).
func (rf raceFinding) toDiagnostic() Diagnostic {
	code := "RACE002" // read/write
	msg := fmt.Sprintf("data race on `%s` (%s): %s", rf.Root, rf.Kind, strings.Join(rf.Writers, "; "))
	switch rf.Kind {
	case "write/write":
		code = "RACE001"
	case "use-after-move":
		code = "RACE004"
		msg = fmt.Sprintf("use after move: %s", strings.Join(rf.Writers, "; "))
	}
	return Diagnostic{
		Severity: "error",
		Phase:    "race",
		Code:     code,
		Message:  msg,
		Decl:     rf.Decl,
	}
}
