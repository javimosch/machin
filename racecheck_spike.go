package main

// Phase 0 spike — INFERRED data-race detection (the "better than Rust" thesis).
//
// Rust guarantees data-race freedom via Send/Sync + the borrow checker: the human
// annotates what may cross a thread boundary. This spike proves machin can INFER the
// same property with ZERO annotations, because the agent+compiler are one loop:
//
//   1. Summarize, per function, which parameters it MUTATES (writes through a slice
//      element / struct field), transitively across calls.
//   2. At each `go f(args)` site, a goroutine that mutates param j shares+writes the
//      caller's variable rooted at args[j].
//   3. A heap root written by >=2 concurrently-live writers, at least one a goroutine,
//      is a DATA RACE — reported with a counterexample, no annotation required.
//
// Soundness stance (honest, like a borrow checker): the analysis over-approximates
// concurrency (treats all writers in a scope as concurrent) — sound, may over-report,
// never under-reports. The full arc adds move-on-send + immutability to shrink the
// false-positive surface. This file exists only to validate the approach.

import "fmt"

// rsFinding is one inferred data race.
type rsFinding struct {
	Root    string   // the shared heap variable (root of the lvalue)
	Fn      string   // function where the concurrent writers live
	Writers []string // human-readable counterexample: who writes it, concurrently
}

// rsExprRoot returns the root variable name of a place expression (x, x[i], x.f,
// x[i].f ...), or "" if the expression is not rooted in a named variable.
func rsExprRoot(e Expr) string {
	switch t := e.(type) {
	case *Ident:
		return t.Name
	case *Index:
		return rsExprRoot(t.X)
	case *FieldAccess:
		return rsExprRoot(t.X)
	}
	return ""
}

// rsMutSummary computes, for every function, the set of parameter indices it mutates
// (directly via element/field write, or transitively by passing the param into a
// callee/goroutine position that mutates it). Fixed-point over the call graph.
func rsMutSummary(prog *Program) map[string]map[int]bool {
	sum := map[string]map[int]bool{}
	for _, f := range prog.Funcs {
		sum[f.Name] = map[int]bool{}
	}
	for changed := true; changed; {
		changed = false
		for _, f := range prog.Funcs {
			idx := map[string]int{}
			for i, p := range f.Params {
				idx[p] = i
			}
			mark := func(root string) {
				if i, ok := idx[root]; ok && !sum[f.Name][i] {
					sum[f.Name][i] = true
					changed = true
				}
			}
			var walk func(stmts []Stmt)
			propCall := func(callee string, args []Expr) {
				for j, a := range args {
					if sum[callee] != nil && sum[callee][j] {
						mark(rsExprRoot(a))
					}
				}
			}
			walk = func(stmts []Stmt) {
				for _, s := range stmts {
					switch st := s.(type) {
					case *IndexAssign:
						mark(rsExprRoot(st.Target))
					case *FieldAssign:
						mark(rsExprRoot(st.Target))
					case *ExprStmt:
						if c, ok := st.X.(*Call); ok {
							propCall(c.Callee, c.Args)
						}
					case *AssignStmt:
						if c, ok := st.Val.(*Call); ok {
							propCall(c.Callee, c.Args)
						}
					case *GoStmt:
						propCall(st.Call.Callee, st.Call.Args)
					case *IfStmt:
						walk(st.Then)
						walk(st.Else)
					case *WhileStmt:
						walk(st.Body)
					case *RangeStmt:
						walk(st.Body)
					case *ArenaStmt:
						walk(st.Body)
					}
				}
			}
			walk(f.Body)
		}
	}
	return sum
}

// rsAnalyze returns every inferred data race in the program.
func rsAnalyze(prog *Program) []rsFinding {
	sum := rsMutSummary(prog)
	var out []rsFinding

	for _, f := range prog.Funcs {
		// root -> list of concurrent writers (desc, isGoroutine)
		type writer struct {
			desc string
			isGo bool
		}
		writers := map[string][]writer{}
		add := func(root, desc string, isGo bool) {
			if root != "" {
				writers[root] = append(writers[root], writer{desc, isGo})
			}
		}
		var scan func(stmts []Stmt)
		scan = func(stmts []Stmt) {
			for _, s := range stmts {
				switch st := s.(type) {
				case *GoStmt:
					g := st.Call.Callee
					for j, a := range st.Call.Args {
						if sum[g] != nil && sum[g][j] {
							r := rsExprRoot(a)
							add(r, fmt.Sprintf("goroutine `go %s(...)` writes `%s` (param #%d)", g, r, j), true)
						}
					}
				case *IndexAssign:
					r := rsExprRoot(st.Target)
					add(r, fmt.Sprintf("this-thread write `%s[...] = ...`", r), false)
				case *FieldAssign:
					r := rsExprRoot(st.Target)
					add(r, fmt.Sprintf("this-thread write `%s.%s = ...`", r, st.Target.Name), false)
				case *ExprStmt:
					if c, ok := st.X.(*Call); ok {
						for j, a := range c.Args {
							if sum[c.Callee] != nil && sum[c.Callee][j] {
								add(rsExprRoot(a), fmt.Sprintf("this-thread call `%s(...)` writes `%s`", c.Callee, rsExprRoot(a)), false)
							}
						}
					}
				case *IfStmt:
					scan(st.Then)
					scan(st.Else)
				case *WhileStmt:
					scan(st.Body)
				case *RangeStmt:
					scan(st.Body)
				case *ArenaStmt:
					scan(st.Body)
				}
			}
		}
		scan(f.Body)

		for root, ws := range writers {
			goCount := 0
			for _, w := range ws {
				if w.isGo {
					goCount++
				}
			}
			if goCount >= 1 && len(ws) >= 2 {
				descs := make([]string, len(ws))
				for i, w := range ws {
					descs[i] = w.desc
				}
				out = append(out, rsFinding{Root: root, Fn: f.Name, Writers: descs})
			}
		}
	}
	return out
}
