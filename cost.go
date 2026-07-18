package main

// cost.go — the static cost model that quantifies a proven rewrite (Slice 3 of provable
// superoptimization: propose → prove → QUANTIFY).
//
// Why a cost model and not a wall-clock benchmark: `machin build` compiles through `cc -O2`,
// so the C backend already applies the same peephole rewrites (x*8 -> x<<3, constant folding).
// Micro-benchmarking those at -O2 measures noise, not the rewrite. A static latency-weighted
// operation count is exactly how real superoptimizers RANK equivalent instruction sequences —
// it is deterministic, backend-independent, and reflects the work the optimized form does. It
// is an ESTIMATE (labeled as such), not a wall-clock claim; the honesty note in the report says
// so, and points out that the large wall-clock wins come from algorithmic rewrites.
//
// Operations inside a loop body are weighted by loopFactor to reflect that hot code dominates —
// a strength reduction inside a `for` counts for more than one in straight-line code.

const loopFactor = 10

// opCost is the relative latency weight of a binary operator (rough, in the spirit of an
// instruction-cost table: multiply and especially divide are dear; shifts/adds/bitwise are cheap).
func opCost(op string) int {
	switch op {
	case "*":
		return 4
	case "/", "%":
		return 5
	case "<<", ">>", "&", "|", "^":
		return 1
	case "+", "-":
		return 1
	case "==", "!=", "<", "<=", ">", ">=":
		return 1
	case "&&", "||":
		return 1
	}
	return 1
}

func exprCost(e Expr) int {
	switch x := e.(type) {
	case *Binary:
		return opCost(x.Op) + exprCost(x.L) + exprCost(x.R)
	case *Unary:
		return 1 + exprCost(x.X)
	case *Call:
		c := 5 // a call is not free; identical before/after so it doesn't bias a delta
		for _, a := range x.Args {
			c += exprCost(a)
		}
		return c
	case *CallValue:
		c := 5 + exprCost(x.Fn)
		for _, a := range x.Args {
			c += exprCost(a)
		}
		return c
	case *Index:
		return 2 + exprCost(x.X) + exprCost(x.Idx)
	case *FieldAccess:
		return 1 + exprCost(x.X)
	case *SliceLit:
		return exprsCost(x.Elems)
	case *StructLit:
		return exprsCost(x.Vals)
	case *Recv:
		return 5 + exprCost(x.Ch)
	default:
		return 0 // literals, idents, make*, funclit leaves
	}
}

func exprsCost(es []Expr) int {
	c := 0
	for _, e := range es {
		c += exprCost(e)
	}
	return c
}

func stmtsCost(ss []Stmt) int {
	c := 0
	for _, s := range ss {
		c += stmtCost(s)
	}
	return c
}

func stmtCost(s Stmt) int {
	switch x := s.(type) {
	case *ExprStmt:
		return exprCost(x.X)
	case *AssignStmt:
		return exprCost(x.Val)
	case *MultiAssign:
		return exprsCost(x.Rhs)
	case *ReturnStmt:
		return exprsCost(x.Vals)
	case *IfStmt:
		return exprCost(x.Cond) + stmtsCost(x.Then) + stmtsCost(x.Else)
	case *WhileStmt:
		return exprCost(x.Cond) + loopFactor*stmtsCost(x.Body)
	case *RangeStmt:
		return loopFactor * (exprCost(x.X) + stmtsCost(x.Body))
	case *IndexAssign:
		return 2 + exprCost(x.Target.X) + exprCost(x.Target.Idx) + exprCost(x.Val)
	case *FieldAssign:
		return 1 + exprCost(x.Target.X) + exprCost(x.Val)
	case *SendStmt:
		return 5 + exprCost(x.Ch) + exprCost(x.Val)
	case *ArenaStmt:
		return stmtsCost(x.Body)
	default:
		return 0 // break/continue/go/select
	}
}

func funcCost(f *FuncDecl) int { return stmtsCost(f.Body) }

// costPct is the percentage reduction from before to after (0 if before is 0).
func costPct(before, after int) int {
	if before <= 0 {
		return 0
	}
	return (before - after) * 100 / before
}
