package main

import "sort"

// arena_prov.go — interprocedural provenance for arena escape analysis (Slice 2).
//
// v1 of the escape analysis treated ANY heap-returning call inside an arena block as a fresh
// arena allocation, so a pass-through helper — `func id(s) (r) { r = s }` — made `outer = id(p)`
// look like an escape even though it just returns its (pre-existing) argument. This computes a
// per-function return summary so a call is tainted only if the callee actually ALLOCATES fresh
// heap, or PASSES THROUGH an argument that is itself tainted.
//
// The summary `provSummary{fresh, pass}` answers, for a function's return value: can it carry
// heap freshly allocated inside the function (`fresh`), and/or is it (a copy of) parameter i
// unchanged (`pass[i]`)? Computed to a fixpoint over the call graph (a call composes with its
// callee's summary), with an inner fixpoint over locals (so `a := p; b := a; return b` traces
// b -> a -> param 0). Sound and monotone: provenance sets only grow, so it converges; an
// unknown callee (builtin / not-yet-summarized) is conservatively `fresh`.

type provSummary struct {
	fresh bool
	pass  map[int]bool // parameter indices the return may pass through unchanged
}

// computeRetProv returns a return-provenance summary per source function name. It is only called
// when the program actually contains an arena block (see detectArenaEscapes).
func computeRetProv(prog *Program, c *Checker) map[string]*provSummary {
	sum := map[string]*provSummary{}
	for _, f := range prog.Funcs {
		sum[f.Name] = &provSummary{pass: map[int]bool{}}
	}
	for iter := 0; iter < 40; iter++ { // outer fixpoint over the call graph
		changed := false
		for _, inst := range c.reps {
			f := c.instFn[inst]
			if f == nil || sum[f.Name] == nil {
				continue
			}
			paramIdx := map[string]int{}
			for i, p := range f.Params {
				paramIdx[p] = i
			}
			localFresh := map[string]bool{}
			localPass := map[string]map[int]bool{}

			var prov func(e Expr) (bool, map[int]bool)
			prov = func(e Expr) (bool, map[int]bool) {
				// a scalar result carries no heap, so it can neither be fresh nor pass a value through
				if slot, ok := c.nodeSlot[inst][e]; ok && !arenaHeapKind(c, slot) {
					return false, nil
				}
				switch t := e.(type) {
				case *Ident:
					if i, ok := paramIdx[t.Name]; ok {
						return false, map[int]bool{i: true}
					}
					return localFresh[t.Name], localPass[t.Name] // local; globals => (false,nil) = safe
				case *Binary:
					return true, nil // heap-kind binary = string concat, allocated here
				case *SliceLit:
					return true, nil
				case *Unary:
					return prov(t.X)
				case *Index:
					return prov(t.X)
				case *FieldAccess:
					return prov(t.X)
				case *StructLit:
					fr := false
					ps := map[int]bool{}
					for _, v := range t.Vals {
						f2, p2 := prov(v)
						fr = fr || f2
						for k := range p2 {
							ps[k] = true
						}
					}
					return fr, ps
				case *Call:
					cs := sum[t.Callee]
					if cs == nil {
						return true, nil // builtin / unknown callee: conservatively a fresh allocation
					}
					fr := cs.fresh
					ps := map[int]bool{}
					for i := range cs.pass {
						if i < len(t.Args) {
							f2, p2 := prov(t.Args[i])
							fr = fr || f2
							for k := range p2 {
								ps[k] = true
							}
						}
					}
					return fr, ps
				}
				return false, nil
			}

			// inner fixpoint: join each local's provenance over all its assignments
			for li := 0; li < 30; li++ {
				lchg := false
				provWalkAssignments(f.Body, func(name string, val Expr) {
					fr, ps := prov(val)
					if fr && !localFresh[name] {
						localFresh[name] = true
						lchg = true
					}
					if len(ps) > 0 {
						if localPass[name] == nil {
							localPass[name] = map[int]bool{}
						}
						for k := range ps {
							if !localPass[name][k] {
								localPass[name][k] = true
								lchg = true
							}
						}
					}
				})
				if !lchg {
					break
				}
			}

			// return provenance = join over explicit `return e` plus the named returns' locals
			retFresh := false
			retPass := map[int]bool{}
			add := func(fr bool, ps map[int]bool) {
				retFresh = retFresh || fr
				for k := range ps {
					retPass[k] = true
				}
			}
			provWalkReturns(f.Body, func(vals []Expr) {
				for _, v := range vals {
					add(prov(v))
				}
			})
			for _, rn := range f.Returns {
				add(localFresh[rn], localPass[rn])
			}

			s := sum[f.Name]
			if retFresh && !s.fresh {
				s.fresh = true
				changed = true
			}
			for k := range retPass {
				if !s.pass[k] {
					s.pass[k] = true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return sum
}

// provWalkAssignments invokes fn on every `name [:=|=] val` (and each leg of a parallel
// multi-assign) anywhere in body, recursing into nested blocks.
func provWalkAssignments(body []Stmt, fn func(name string, val Expr)) {
	for _, s := range body {
		switch st := s.(type) {
		case *AssignStmt:
			fn(st.Name, st.Val)
		case *MultiAssign:
			if len(st.Rhs) == len(st.Names) {
				for i, n := range st.Names {
					fn(n, st.Rhs[i])
				}
			}
		case *IfStmt:
			provWalkAssignments(st.Then, fn)
			provWalkAssignments(st.Else, fn)
		case *WhileStmt:
			provWalkAssignments(st.Body, fn)
		case *RangeStmt:
			provWalkAssignments(st.Body, fn)
		case *ArenaStmt:
			provWalkAssignments(st.Body, fn)
		}
	}
}

// provWalkReturns invokes fn on every ReturnStmt's value list anywhere in body.
func provWalkReturns(body []Stmt, fn func(vals []Expr)) {
	for _, s := range body {
		switch st := s.(type) {
		case *ReturnStmt:
			if len(st.Vals) > 0 {
				fn(st.Vals)
			}
		case *IfStmt:
			provWalkReturns(st.Then, fn)
			provWalkReturns(st.Else, fn)
		case *WhileStmt:
			provWalkReturns(st.Body, fn)
		case *RangeStmt:
			provWalkReturns(st.Body, fn)
		case *ArenaStmt:
			provWalkReturns(st.Body, fn)
		}
	}
}

// ---- global-write provenance (Slice 3, issue #539) ----
//
// computeRetProv answers "does calling f RETURN arena memory". This answers the
// other direction: "does calling f STORE arena memory somewhere that outlives the
// block" — specifically, into a package global.
//
// Why it is needed: ARENA001 catches `g = out` written literally inside an
// `arena { }`, but not the identical assignment one call deeper —
//
//	func build() { out := []string{}  ...  g_cache = out }
//	func main()  { arena { build() }  println(g_cache[0]) }   // reads freed memory
//
// which is the normal way to write MFL, and produced silent wrong data with a
// clean `machin check` (#539). The analysis was sound for what it inspected; the
// boundary was one stack frame deep, which is narrower than the claim reads.
//
// Summary: function name -> the globals it may assign FRESHLY-ALLOCATED heap to,
// directly or through a call. Fixpoint over the call graph, monotone (sets only
// grow) so it converges. The `fresh` gate is what keeps this from flagging every
// global write: assigning a global a parameter, another global, or a literal
// allocates nothing in the block, so it does not dangle.
func computeGlobalWrites(prog *Program, c *Checker, retProv map[string]*provSummary) map[string]map[string]bool {
	writes := map[string]map[string]bool{}
	for _, f := range prog.Funcs {
		writes[f.Name] = map[string]bool{}
	}
	for iter := 0; iter < 40; iter++ { // fixpoint over the call graph
		changed := false
		for _, inst := range c.reps {
			f := c.instFn[inst]
			if f == nil || writes[f.Name] == nil {
				continue
			}
			isParam := map[string]bool{}
			for _, p := range f.Params {
				isParam[p] = true
			}
			localFresh := map[string]bool{}

			// fresh reports whether e evaluates to heap allocated in THIS call —
			// the same question `prov` asks for returns, minus the pass-through
			// bookkeeping (a parameter or a global is pre-existing, so neither
			// can dangle when the block is reclaimed).
			var fresh func(e Expr) bool
			fresh = func(e Expr) bool {
				if slot, ok := c.nodeSlot[inst][e]; ok && !arenaHeapKind(c, slot) {
					return false
				}
				switch t := e.(type) {
				case *Ident:
					if isParam[t.Name] {
						return false
					}
					if _, isGlobal := c.globalSlot[t.Name]; isGlobal {
						return false
					}
					return localFresh[t.Name]
				case *Binary:
					return true // heap-kind binary = string concat, allocated here
				case *SliceLit:
					return true
				case *Unary:
					return fresh(t.X)
				case *Index:
					return fresh(t.X)
				case *FieldAccess:
					return fresh(t.X)
				case *StructLit:
					for _, v := range t.Vals {
						if fresh(v) {
							return true
						}
					}
					return false
				case *Call:
					if t.Callee == "escape" {
						return false // escape() copies into the enclosing arena
					}
					if s := retProv[t.Callee]; s != nil {
						if s.fresh {
							return true
						}
						for i := range s.pass { // passes an argument through
							if i < len(t.Args) && fresh(t.Args[i]) {
								return true
							}
						}
						return false
					}
					return true // builtin / unknown callee: conservatively fresh
				}
				// Default FALSE, matching carriesArena in arena_check.go: a literal
				// is static storage, not arena memory, and ARENA001 is only worth
				// having if every finding is real. An allocating form not listed
				// above is a false negative, which is the trade this analysis has
				// always made.
				return false
			}

			var walk func(body []Stmt)
			walk = func(body []Stmt) {
				for _, s := range body {
					switch st := s.(type) {
					case *AssignStmt:
						if _, isGlobal := c.globalSlot[st.Name]; isGlobal {
							if _, shadowed := c.vars[inst][st.Name]; !shadowed {
								if fresh(st.Val) && !writes[f.Name][st.Name] {
									writes[f.Name][st.Name] = true
									changed = true
								}
							}
						} else if fresh(st.Val) != localFresh[st.Name] {
							localFresh[st.Name] = fresh(st.Val)
							changed = true
						}
					case *ExprStmt:
						if call, ok := st.X.(*Call); ok {
							for g := range writes[call.Callee] {
								if !writes[f.Name][g] {
									writes[f.Name][g] = true
									changed = true
								}
							}
						}
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
		if !changed {
			break
		}
	}
	return writes
}

// arenaCallWrites returns the globals a call made inside an arena block may fill
// with memory from that block, sorted for a stable message.
func arenaCallWrites(callee string, writes map[string]map[string]bool) []string {
	var gs []string
	for g := range writes[callee] {
		gs = append(gs, g)
	}
	sort.Strings(gs)
	return gs
}
