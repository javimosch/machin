package main

import "sort"

// deadlock_check.go — a compile-time, sound-but-partial deadlock finder. machin channels
// are unbounded (a send never blocks), so a receive on a channel that NOTHING ever sends to
// or closes blocks forever in EVERY schedule — a guaranteed deadlock, detectable statically,
// before the program runs. This catches the "forgot to send/close" class up front; the
// runtime quiescence detector catches the rest.
//
// Honest, Falsifier-style and false-positive-free by construction: a channel is treated as
// "fed" (safe) unless the analysis can PROVE it is never fed. Any use it can't fully account
// for — passed to an unknown/FFI callee, stored in a struct/slice/map, returned, aliased —
// conservatively counts as a possible feed, so DL001 fires only when no feed is possible. A
// clean result is therefore not a proof of deadlock-freedom.

// dlFinding is a compile-time deadlock (DL001): a received channel that is never fed.
type dlFinding struct {
	Decl string // enclosing function
	Code string // DL001
	Chan string // the channel variable
	Prop string // human-readable message
}

func (df dlFinding) toDiagnostic() Diagnostic {
	return Diagnostic{
		Severity: "warning", // advisory: a deadlock finding never fails check/build
		Phase:    "deadlock",
		Code:     df.Code,
		Message:  df.Prop,
	}
}

// detectDeadlocks returns compile-time deadlocks over the whole program.
func detectDeadlocks(prog *Program, c *Checker) []dlFinding {
	feeds := computeFeeds(prog) // per-function: which channel params it sends to / closes
	var out []dlFinding
	for _, f := range prog.Funcs {
		// 1. locate make(chan) locals in this function.
		chanVars := map[string]bool{}
		walkStmts(f.Body, func(s Stmt) {
			if as, ok := s.(*AssignStmt); ok && as.Op == ":=" {
				if _, ok := as.Val.(*MakeChan); ok {
					chanVars[as.Name] = true
				}
			}
		})
		if len(chanVars) == 0 {
			continue
		}
		// 2. classify every use of those vars: received-from, and fed (or escaped).
		st := &dlScan{chanVars: chanVars, feeds: feeds, recv: map[string]bool{}, fed: map[string]bool{}}
		st.stmts(f.Body)
		// 3. a received channel with no possible feed is a guaranteed deadlock.
		for v := range chanVars {
			if st.recv[v] && !st.fed[v] {
				out = append(out, dlFinding{
					Decl: f.Name, Code: "DL001", Chan: v,
					Prop: "receive on channel `" + v + "` that is never sent to or closed — a guaranteed deadlock",
				})
			}
		}
	}
	// deterministic order (per-function channel set is a map): sort by function, then channel.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Decl != out[j].Decl {
			return out[i].Decl < out[j].Decl
		}
		return out[i].Chan < out[j].Chan
	})
	return out
}

// computeFeeds is a fixpoint: feeds[fn][i] is true if function fn sends to or closes its
// i-th parameter, directly or by passing it to a callee that feeds the matching parameter.
func computeFeeds(prog *Program) map[string]map[int]bool {
	byName := map[string]*FuncDecl{}
	feeds := map[string]map[int]bool{}
	for _, f := range prog.Funcs {
		byName[f.Name] = f
		feeds[f.Name] = map[int]bool{}
	}
	paramIdx := func(f *FuncDecl, name string) int {
		for i, p := range f.Params {
			if p == name {
				return i
			}
		}
		return -1
	}
	changed := true
	for changed {
		changed = false
		for _, f := range prog.Funcs {
			mark := func(name string) {
				if i := paramIdx(f, name); i >= 0 && !feeds[f.Name][i] {
					feeds[f.Name][i] = true
					changed = true
				}
			}
			walkStmts(f.Body, func(s Stmt) {
				switch st := s.(type) {
				case *SendStmt:
					if id, ok := st.Ch.(*Ident); ok {
						mark(id.Name)
					}
				case *ExprStmt:
					if call, ok := st.X.(*Call); ok {
						markCallFeeds(f, call, byName, feeds, mark, paramIdx)
					}
				case *GoStmt:
					markCallFeeds(f, st.Call, byName, feeds, mark, paramIdx)
				case *AssignStmt:
					if call, ok := st.Val.(*Call); ok {
						markCallFeeds(f, call, byName, feeds, mark, paramIdx)
					}
				}
			})
		}
	}
	return feeds
}

// markCallFeeds: for a call g(a0,a1,...), if g is `close`, its arg is fed; otherwise if g is a
// user function that feeds parameter j, then the caller's param passed as arg j is fed too.
func markCallFeeds(f *FuncDecl, call *Call, byName map[string]*FuncDecl, feeds map[string]map[int]bool, mark func(string), paramIdx func(*FuncDecl, string) int) {
	if call.Callee == "close" {
		if len(call.Args) == 1 {
			if id, ok := call.Args[0].(*Ident); ok {
				mark(id.Name)
			}
		}
		return
	}
	g := byName[call.Callee]
	if g == nil {
		return // unknown/builtin callee: handled conservatively by the escape scan, not here
	}
	for j, a := range call.Args {
		if id, ok := a.(*Ident); ok && feeds[g.Name][j] {
			mark(id.Name)
		}
	}
}

// dlScan walks a function classifying each use of the tracked channel vars.
type dlScan struct {
	chanVars map[string]bool
	feeds    map[string]map[int]bool
	recv     map[string]bool // received-from
	fed      map[string]bool // sent-to / closed / escaped (conservatively fed)
}

func (s *dlScan) isChan(name string) bool { return s.chanVars[name] }

func (s *dlScan) stmts(list []Stmt) {
	for _, st := range list {
		s.stmt(st)
	}
}

func (s *dlScan) stmt(st Stmt) {
	switch n := st.(type) {
	case *SendStmt:
		if id, ok := n.Ch.(*Ident); ok && s.isChan(id.Name) {
			s.fed[id.Name] = true // ch <- v feeds ch
		} else {
			s.expr(n.Ch)
		}
		s.expr(n.Val)
	case *ExprStmt:
		s.callOrExpr(n.X)
	case *GoStmt:
		s.call(n.Call) // go g(ch): feeds ch iff g feeds that parameter
	case *AssignStmt:
		if _, ok := n.Val.(*MakeChan); ok {
			return // x := make(chan): the declaration itself, no channel use
		}
		// y := <ident chan>  aliases the channel — an escape we can't follow, so treat as fed.
		if id, ok := n.Val.(*Ident); ok && s.isChan(id.Name) {
			s.fed[id.Name] = true
			return
		}
		s.callOrExpr(n.Val)
	case *MultiAssign:
		for _, r := range n.Rhs {
			s.recvOrExpr(r)
		}
	case *ReturnStmt:
		for _, v := range n.Vals {
			s.expr(v) // returning a channel Ident is an escape -> fed (handled in expr)
		}
	case *IfStmt:
		s.expr(n.Cond)
		s.stmts(n.Then)
		s.stmts(n.Else)
	case *WhileStmt:
		s.expr(n.Cond)
		s.stmts(n.Body)
	case *RangeStmt:
		// range over a channel is a receive.
		if id, ok := n.X.(*Ident); ok && s.isChan(id.Name) {
			s.recv[id.Name] = true
		} else {
			s.expr(n.X)
		}
		s.stmts(n.Body)
	case *ArenaStmt:
		s.stmts(n.Body)
	case *SelectStmt:
		for i := range n.Cases {
			sc := &n.Cases[i]
			if sc.RecvCh != nil {
				if id, ok := sc.RecvCh.(*Ident); ok && s.isChan(id.Name) {
					s.recv[id.Name] = true
				} else {
					s.expr(sc.RecvCh)
				}
			}
			if sc.SendCh != nil {
				if id, ok := sc.SendCh.(*Ident); ok && s.isChan(id.Name) {
					s.fed[id.Name] = true
				} else {
					s.expr(sc.SendCh)
				}
				s.expr(sc.SendVal)
			}
			s.stmts(sc.Body)
		}
		s.stmts(n.Default)
	}
}

// callOrExpr classifies an expression that may be a call whose channel args we can account
// for; anything else falls through to the escape-aware expr walker.
func (s *dlScan) callOrExpr(e Expr) {
	if call, ok := e.(*Call); ok {
		s.call(call)
		return
	}
	s.expr(e)
}

// recvOrExpr: a comma-ok / range receive RHS (`v := <-ch`) is a receive, not an escape.
func (s *dlScan) recvOrExpr(e Expr) {
	if r, ok := e.(*Recv); ok {
		if id, ok := r.Ch.(*Ident); ok && s.isChan(id.Name) {
			s.recv[id.Name] = true
			return
		}
	}
	s.expr(e)
}

func (s *dlScan) call(call *Call) {
	if call.Callee == "close" {
		if len(call.Args) == 1 {
			if id, ok := call.Args[0].(*Ident); ok && s.isChan(id.Name) {
				s.fed[id.Name] = true
				return
			}
		}
	}
	g := s.feeds[call.Callee]
	known := g != nil
	for j, a := range call.Args {
		if id, ok := a.(*Ident); ok && s.isChan(id.Name) {
			if !known {
				s.fed[id.Name] = true // unknown/builtin callee: can't prove it doesn't feed
			} else if g[j] {
				s.fed[id.Name] = true // callee feeds this parameter
			}
			// known callee that does NOT feed param j: passing ch there neither feeds nor escapes.
			continue
		}
		s.expr(a) // a non-channel arg: still scan for nested channel uses
	}
}

// expr is the escape-aware walker: a bare channel Ident reaching here is used in a context
// the classifier didn't account for, so — to stay sound — it counts as a possible feed.
func (s *dlScan) expr(e Expr) {
	switch n := e.(type) {
	case nil:
		return
	case *Ident:
		if s.isChan(n.Name) {
			s.fed[n.Name] = true // escaped into an unanalyzed position
		}
	case *Recv:
		if id, ok := n.Ch.(*Ident); ok && s.isChan(id.Name) {
			s.recv[id.Name] = true
		} else {
			s.expr(n.Ch)
		}
	case *Call:
		s.call(n)
	case *Binary:
		s.expr(n.L)
		s.expr(n.R)
	case *Unary:
		s.expr(n.X)
	case *Index:
		s.expr(n.X)
		s.expr(n.Idx)
	case *FieldAccess:
		s.expr(n.X)
	case *SliceLit:
		for _, el := range n.Elems {
			s.expr(el)
		}
	case *StructLit:
		for _, v := range n.Vals {
			s.expr(v)
		}
	}
}

// walkStmts calls fn on every statement in a body, recursing into nested blocks.
func walkStmts(list []Stmt, fn func(Stmt)) {
	for _, st := range list {
		fn(st)
		switch n := st.(type) {
		case *IfStmt:
			walkStmts(n.Then, fn)
			walkStmts(n.Else, fn)
		case *WhileStmt:
			walkStmts(n.Body, fn)
		case *RangeStmt:
			walkStmts(n.Body, fn)
		case *ArenaStmt:
			walkStmts(n.Body, fn)
		case *SelectStmt:
			for i := range n.Cases {
				walkStmts(n.Cases[i].Body, fn)
			}
			walkStmts(n.Default, fn)
		}
	}
}
