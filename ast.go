package main

// Node is the base interface for AST nodes.
type Node interface{ node() }

// ---- Expressions ----

type Expr interface{ Node }

type IntLit struct{ Val int64 }
type FloatLit struct{ Val float64 }
type StringLit struct{ Val string }
type BoolLit struct{ Val bool }
type NilLit struct{}
type Ident struct{ Name string }

type Unary struct {
	Op  string
	X   Expr
}

type Binary struct {
	Op   string
	L, R Expr
}

type Call struct {
	Callee string
	Args   []Expr
}

// SliceLit is a typed slice literal: []int{1, 2, 3} or []string{}.
type SliceLit struct {
	Elem  string // element type name: int, float, string, bool
	Elems []Expr
}

// Index is slice indexing: x[i].
type Index struct {
	X   Expr
	Idx Expr
}

func (IntLit) node()    {}
func (FloatLit) node()  {}
func (StringLit) node() {}
func (BoolLit) node()   {}
func (NilLit) node()    {}
func (Ident) node()     {}
func (Unary) node()     {}
func (Binary) node()    {}
func (Call) node()      {}
func (SliceLit) node()  {}
func (Index) node()     {}

// ---- Statements ----

type Stmt interface{ Node }

type ExprStmt struct{ X Expr }

type AssignStmt struct {
	Name    string
	Op      string // ":=" or "="
	Val     Expr
}

type ReturnStmt struct{ Val Expr } // Val may be nil

type IfStmt struct {
	Cond Expr
	Then []Stmt
	Else []Stmt // may be nil
}

type WhileStmt struct {
	Cond Expr
	Body []Stmt
}

// IndexAssign is assignment to a slice element: x[i] = v.
type IndexAssign struct {
	Target *Index
	Val    Expr
}

// GoStmt spawns a goroutine: go f(args).
type GoStmt struct{ Call *Call }

// BreakStmt and ContinueStmt are loop control-flow statements. They are only
// valid inside a while/for body; the parser rejects them elsewhere.
type BreakStmt struct{}
type ContinueStmt struct{}

func (ExprStmt) node()     {}
func (AssignStmt) node()   {}
func (ReturnStmt) node()   {}
func (IfStmt) node()       {}
func (WhileStmt) node()    {}
func (IndexAssign) node()  {}
func (GoStmt) node()       {}
func (BreakStmt) node()    {}
func (ContinueStmt) node() {}

// ---- Top level ----

type FuncDecl struct {
	Name   string
	Params []string
	Body   []Stmt
}

func (FuncDecl) node() {}
