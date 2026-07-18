package main

// arena_check.go — compile-time arena escape analysis (ARENA001).
//
// An `arena { }` block reclaims everything allocated inside it in bulk when the block ends
// (codegen swaps a fresh arena in, runs the body, then frees the whole chain). That is sound
// ONLY if nothing allocated inside the block is still reachable afterwards — otherwise the
// surviving reference dangles into freed memory. Until now that "nothing escapes" contract was
// the machine-author's word (see the ArenaStmt doc comment); this proves it.
//
// The analysis is SOUND and conservative in the same spirit as the data-race pass: a clean
// result means no arena-allocated value reaches a location that outlives the block, so the bulk
// reclamation is safe — memory safety WITHOUT a borrow checker or lifetime annotations, all
// inferred. When it cannot prove safety it reports ARENA001 (advisory warning, phase "arena"),
// naming the escaping assignment.
//
// Taint tracks PROVENANCE: taint originates only from values ALLOCATED INSIDE the block (string
// concatenation, a heap-returning call, a slice literal, …) and propagates variable→variable.
// Pre-existing values — parameters, variables declared outside the block, globals — are not
// tainted, so passing them out is correctly safe. The crux of precision is a result-kind gate:
// a value can carry arena memory only if its own result kind is heap-carrying, so `len(s)` (a
// KInt) does not escape even though it consumes an arena string — which keeps the canonical
// accumulator `total = total + len(s)` clean.
//
// v1 scope / honest limits: intraprocedural. A heap-returning call is treated conservatively as
// a fresh arena allocation (so a pass-through helper that merely returns its argument may
// over-report — interprocedural provenance is a later slice). Channel sends are NOT escapes: the
// runtime deep-copies values crossing a channel (string freeze/thaw + JSON ser/des for
// aggregates), so the receiver never observes the arena buffer. Struct-field granularity is
// coarse (a struct built from any tainted field is tainted).

import "sort"

type arenaFinding struct {
	Decl   string // enclosing function
	Code   string // ARENA001
	Detail string // human-readable escaping-site message
}

func (af arenaFinding) toDiagnostic() Diagnostic {
	return Diagnostic{
		Severity: "warning", // advisory: an escape finding never fails check/build
		Phase:    "arena",
		Code:     af.Code,
		Message:  af.Detail,
	}
}

// arenaHeapKind reports whether a slot's kind carries a pointer into an arena (so a value of
// that kind, freshly allocated, would dangle after the block's reclamation). KFunc is included
// because a closure value can hold arena memory through a captured variable (see the
// *MakeClosure case in carriesArena): its captures are read at the make site (inside the block)
// and stored in the closure's block-outliving environment.
func arenaHeapKind(c *Checker, slot int) bool {
	switch c.kindOf(slot) {
	case KString, KSlice, KBytes, KMap, KFunc:
		return true
	case KStruct:
		return typeSharesHeap(c, c.sname[c.find(slot)], 0)
	}
	return false
}

// arenaPlaceString encodes an assignable place as a path string: a bare variable is its name,
// a field access appends ".field", an index appends "#" (all indices of a container share one
// key). ok=false for a place not rooted in a plain variable. This is the taint key: tracking
// taint per place (not per whole variable) lets a field-assign of arena memory into an inner
// struct taint just that field, so escaping the struct is caught while extracting a different,
// clean field is not.
func arenaPlaceString(e Expr) (string, bool) {
	switch t := e.(type) {
	case *Ident:
		return t.Name, true
	case *FieldAccess:
		if p, ok := arenaPlaceString(t.X); ok {
			return p + "." + t.Name, true
		}
	case *Index:
		if p, ok := arenaPlaceString(t.X); ok {
			return p + "#", true
		}
	}
	return "", false
}

// arenaIsPrefixPath reports whether path a is an ancestor-or-equal of path b — a == b, or b
// continues a at a place boundary (a ".field" or "#" step). Delimiter-aware so "p" is an
// ancestor of "p.items" and "p#" but not of "ptr".
func arenaIsPrefixPath(a, b string) bool {
	if len(a) > len(b) || b[:len(a)] != a {
		return false
	}
	if len(a) == len(b) {
		return true
	}
	c := b[len(a)]
	return c == '.' || c == '#'
}

// detectArenaEscapes returns one finding per arena block from which an allocated value escapes.
func detectArenaEscapes(prog *Program, c *Checker) []arenaFinding {
	// Nothing to do (and no summary to compute) unless the program uses an arena.
	hasArena := false
	for _, f := range prog.Funcs {
		arenaFindEach(f.Body, func(*ArenaStmt) { hasArena = true })
	}
	if !hasArena {
		return nil
	}
	retProv := computeRetProv(prog, c) // interprocedural return-provenance summary

	var out []arenaFinding
	seen := map[string]bool{} // dedup per (function, detail)
	for _, inst := range c.reps {
		f := c.instFn[inst]
		if f == nil {
			continue
		}
		arenaFindEach(f.Body, func(a *ArenaStmt) {
			inner := arenaInnerDeclared(a.Body)
			// taint is keyed by PLACE path ("p", "p.items", "p#"), not by whole variable, so a
			// field/element holding arena memory is tracked at that granularity.
			tainted := map[string]bool{}
			// placeCarries: does any tainted place lie on the same chain as `path`? A tainted
			// ancestor means the whole value is arena (reading a sub-part carries); a tainted
			// descendant means some part is arena (reading the whole value carries).
			placeCarries := func(path string) bool {
				for k := range tainted {
					if arenaIsPrefixPath(k, path) || arenaIsPrefixPath(path, k) {
						return true
					}
				}
				return false
			}
			// setPlace assigns `path` wholesale: drop `path` and every descendant (they are
			// replaced), then re-taint `path` if the new value carries arena memory. Ancestors and
			// siblings are untouched (assigning p.a says nothing about p.b or p as a whole).
			setPlace := func(path string, tv bool) {
				for k := range tainted {
					if arenaIsPrefixPath(path, k) {
						delete(tainted, k)
					}
				}
				if tv {
					tainted[path] = true
				}
			}

			var carriesArena func(e Expr) bool
			carriesArena = func(e Expr) bool {
				// result-kind gate: only a heap-carrying result can hold arena memory
				if slot, ok := c.nodeSlot[inst][e]; ok && !arenaHeapKind(c, slot) {
					return false
				}
				switch t := e.(type) {
				case *Ident:
					return placeCarries(t.Name) // params/outer/globals are pre-existing, never tainted
				case *Binary:
					return true // a heap-kind binary is a string concatenation — allocated here
				case *Call:
					// interprocedural: the call carries arena memory only if the callee allocates
					// fresh heap, or passes through an argument that is itself arena-tainted
					cs := retProv[t.Callee]
					if cs == nil {
						return true // builtin / unknown callee: conservatively a fresh allocation
					}
					if cs.fresh {
						return true
					}
					for i := range cs.pass {
						if i < len(t.Args) && carriesArena(t.Args[i]) {
							return true
						}
					}
					return false
				case *SliceLit:
					return true
				case *MakeClosure:
					// a closure created here dangles if it captures a variable that currently
					// holds arena memory: the capture is read at this make site (inside the
					// block) but lives on in the closure's malloc'd, block-outliving environment,
					// so calling the closure after the block reads freed arena memory. (The
					// closure's own env is stable; it is the captured VALUE that dangles.)
					for _, cap := range t.Captures {
						if placeCarries(cap) {
							return true
						}
					}
					return false
				case *StructLit:
					for _, v := range t.Vals {
						if carriesArena(v) {
							return true
						}
					}
					return false
				case *Index:
					if p, ok := arenaPlaceString(t); ok {
						return placeCarries(p)
					}
					return carriesArena(t.X)
				case *FieldAccess:
					if p, ok := arenaPlaceString(t); ok {
						return placeCarries(p)
					}
					return carriesArena(t.X)
				case *Unary:
					return carriesArena(t.X)
				}
				return false
			}
			flag := func(code, detail string) {
				key := f.Name + "\x00" + code + "\x00" + detail
				if seen[key] {
					return
				}
				seen[key] = true
				out = append(out, arenaFinding{Decl: f.Name, Code: code, Detail: detail})
			}
			var scan func(body []Stmt)
			scan = func(body []Stmt) {
				for _, s := range body {
					switch st := s.(type) {
					case *AssignStmt:
						tv := carriesArena(st.Val)
						if inner[st.Name] {
							setPlace(st.Name, tv) // propagate within the block
						} else if tv {
							flag("ARENA001", "`"+st.Name+"` outlives this arena block but is assigned a value allocated inside it — it dangles after the block's memory is reclaimed")
						}
					case *MultiAssign:
						for i, n := range st.Names {
							var v Expr
							if len(st.Rhs) == len(st.Names) {
								v = st.Rhs[i]
							} else if len(st.Rhs) == 1 {
								v = st.Rhs[0]
							}
							tv := v != nil && carriesArena(v)
							if inner[n] {
								setPlace(n, tv)
							} else if tv {
								flag("ARENA001", "`"+n+"` outlives this arena block but is assigned a value allocated inside it — it dangles after the block's memory is reclaimed")
							}
						}
					case *ReturnStmt:
						// ARENA002: ANY return inside an arena block is a control-flow bug — the
						// generated code returns before the block's `mfl_arena_cur = _sp; arena_free`
						// cleanup, so it leaks the block's allocations AND leaves the current-arena
						// pointer dangling into the freed stack frame (later allocations are UB).
						// Independent of the returned value; false-positive-free.
						flag("ARENA002", "a `return` inside this arena block skips the block's cleanup — the block's memory is leaked and the current-arena pointer is left dangling (later allocations corrupt); move the `return` after the arena block")
					case *IndexAssign:
						if r, _, ok := placeOf(st.Target); ok {
							if inner[r] {
								// an inner container gains arena memory at this element — taint that
								// place so escaping the whole container later is caught.
								if p, ok2 := arenaPlaceString(st.Target); ok2 {
									setPlace(p, carriesArena(st.Val))
								}
							} else if carriesArena(st.Val) {
								flag("ARENA001", "`"+r+"` outlives this arena block but is stored an element allocated inside it — it dangles after the block's memory is reclaimed")
							}
						}
					case *FieldAssign:
						if r, _, ok := placeOf(st.Target); ok {
							if inner[r] {
								if p, ok2 := arenaPlaceString(st.Target); ok2 {
									setPlace(p, carriesArena(st.Val))
								}
							} else if carriesArena(st.Val) {
								flag("ARENA001", "`"+r+"` outlives this arena block but is stored a field allocated inside it — it dangles after the block's memory is reclaimed")
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
			scan(a.Body)
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Decl != out[j].Decl {
			return out[i].Decl < out[j].Decl
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

// arenaFindEach invokes fn on every ArenaStmt reachable in body, recursing into nested blocks.
func arenaFindEach(body []Stmt, fn func(*ArenaStmt)) {
	for _, s := range body {
		switch st := s.(type) {
		case *ArenaStmt:
			fn(st)
			arenaFindEach(st.Body, fn)
		case *IfStmt:
			arenaFindEach(st.Then, fn)
			arenaFindEach(st.Else, fn)
		case *WhileStmt:
			arenaFindEach(st.Body, fn)
		case *RangeStmt:
			arenaFindEach(st.Body, fn)
		}
	}
}

// arenaInnerDeclared collects names bound (:=, range key/val, multi :=) lexically inside body —
// the variables local to the arena block, which die with it and so cannot escape.
func arenaInnerDeclared(body []Stmt) map[string]bool {
	m := map[string]bool{}
	var walk func([]Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch st := s.(type) {
			case *AssignStmt:
				if st.Op == ":=" {
					m[st.Name] = true
				}
			case *MultiAssign:
				if st.Op == ":=" {
					for _, n := range st.Names {
						m[n] = true
					}
				}
			case *RangeStmt:
				if st.Key != "" && st.Key != "_" {
					m[st.Key] = true
				}
				if st.Val != "" && st.Val != "_" {
					m[st.Val] = true
				}
				walk(st.Body)
			case *IfStmt:
				walk(st.Then)
				walk(st.Else)
			case *WhileStmt:
				walk(st.Body)
			case *ArenaStmt:
				walk(st.Body)
			}
		}
	}
	walk(body)
	return m
}
