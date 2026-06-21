package main

// Node is the base interface for AST nodes.
type Node interface{ node() }

// Program is a whole MFL program: struct type declarations plus functions.
type Program struct {
	Types []*TypeDecl
	Funcs []*FuncDecl
}

// Field is one struct field with an explicit type (int, float, bool, string,
// []elem, or another struct name).
type Field struct {
	Name string
	Type string
}

// TypeDecl is a struct type declaration: type Name struct { ... }.
type TypeDecl struct {
	Name   string
	Fields []Field
}

func (TypeDecl) node() {}

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

// StructLit constructs a struct value: Point{x: 1, y: 2} or Point{1, 2}.
// FieldNames is nil for positional literals.
type StructLit struct {
	Type       string
	FieldNames []string
	Vals       []Expr
}

// FieldAccess reads a struct field: p.x.
type FieldAccess struct {
	X    Expr
	Name string
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
func (SliceLit) node()    {}
func (Index) node()       {}
func (StructLit) node()   {}
func (FieldAccess) node() {}

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

// FieldAssign is assignment to a struct field: p.x = v.
type FieldAssign struct {
	Target *FieldAccess
	Val    Expr
}

// GoStmt spawns a goroutine: go f(args).
type GoStmt struct{ Call *Call }

func (ExprStmt) node()    {}
func (AssignStmt) node()  {}
func (ReturnStmt) node()  {}
func (IfStmt) node()      {}
func (WhileStmt) node()   {}
func (IndexAssign) node() {}
func (FieldAssign) node() {}
func (GoStmt) node()      {}

// ---- Top level ----

type FuncDecl struct {
	Name   string
	Params []string
	Body   []Stmt
}

func (FuncDecl) node() {}
