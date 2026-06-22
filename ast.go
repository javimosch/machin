package main

// Node is the base interface for AST nodes.
type Node interface{ node() }

// Program is a whole MFL program: struct type declarations plus functions.
type Program struct {
	Types   []*TypeDecl
	Funcs   []*FuncDecl
	Externs []*ExternDecl
}

// ExternDecl declares foreign C functions and how to compile/link against them:
//   extern "m" { header "math.h" link "m" fn sqrt(float) float }
// Calls to the declared names compile to direct C calls; the header supplies the
// real prototype and `link`/`cflags` are threaded into the cc invocation.
type ExternDecl struct {
	Lib     string // informational name after `extern`
	Header  string // #include <Header> ("" if none)
	Link    string // -l<Link> ("" if none)
	CFlags  string // extra cc flags ("" if none)
	Structs []ExternStruct
	Funcs   []ExternFunc
}

// ExternStruct declares a C struct's layout for by-value marshaling:
//   cstruct Color { r u8  g u8  b u8  a u8 }
// machin synthesizes a matching MFL struct (int/float fields) and marshals
// between the MFL value and the C struct at the FFI boundary.
type ExternStruct struct {
	Name   string
	Fields []ExternField
}

// ExternField is one C struct field with a sized C scalar type (i8..u64, f32, f64).
type ExternField struct {
	Name  string
	CType string
}

// ExternFunc is one foreign function signature. Param/return types are FFI
// scalar type names (int, float, bool, string, i8..u64, f32, f64) or the name of
// a declared cstruct; Ret "" means void.
type ExternFunc struct {
	Name   string
	Params []string
	Ret    string
}

func (ExternDecl) node() {}

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
	// Spread marks the final argument as a slice to spread into a variadic
	// parameter: f(xs...).
	Spread bool
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

// FuncLit is a function literal (lambda): func(a, b) { ... }.
type FuncLit struct {
	Params []string
	Body   []Stmt
}

// CallValue calls a function-valued expression: f(args), (g())(args), fs[i](x).
type CallValue struct {
	Fn   Expr
	Args []Expr
}

// MakeClosure is produced by lambda-lifting: it builds a closure value over a
// lifted top-level function, capturing the named variables by reference
// (pointers to their heap boxes, so the closure shares mutable state).
type MakeClosure struct {
	FuncName string
	Captures []string
}

// MakeChan constructs a channel: make(chan T).
type MakeChan struct{ Elem string }

// MakeMap constructs a map: make(map[K]V).
type MakeMap struct{ Key, Val string }

// Recv receives from a channel: <-ch.
type Recv struct{ Ch Expr }

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
func (FuncLit) node()     {}
func (CallValue) node()   {}
func (MakeClosure) node() {}
func (MakeChan) node()    {}
func (MakeMap) node()     {}
func (Recv) node()        {}

// ---- Statements ----

type Stmt interface{ Node }

type ExprStmt struct{ X Expr }

type AssignStmt struct {
	Name    string
	Op      string // ":=" or "="
	Val     Expr
}

type ReturnStmt struct{ Vals []Expr } // empty for a bare return

// MultiAssign is `a, b := rhs` / `a, b = rhs`, where rhs is either a single call
// returning len(Names) values, or len(Names) parallel expressions.
type MultiAssign struct {
	Names []string
	Op    string // ":=" or "="
	Rhs   []Expr
}

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

// SendStmt sends on a channel: ch <- v.
type SendStmt struct {
	Ch  Expr
	Val Expr
}

// RangeStmt iterates a slice, map, or string:
//   for i, v := range xs   for k, v := range m   for c := range s
// Key is the index/key var; Val is the value var ("" if absent). Either may be
// "_" to ignore.
type RangeStmt struct {
	Key  string
	Val  string
	X    Expr
	Body []Stmt
}

// GoStmt spawns a goroutine: go f(args).
type GoStmt struct{ Call *Call }

// ArenaStmt is a scoped-arena block: arena { ... }. Allocations made inside the
// block are reclaimed in bulk when the block ends, bounding the memory of a
// long-lived loop. The contract is that nothing allocated inside escapes the
// block (the machine author guarantees no escape, as with a stack frame).
type ArenaStmt struct{ Body []Stmt }

func (ExprStmt) node()    {}
func (AssignStmt) node()  {}
func (MultiAssign) node() {}
func (ReturnStmt) node()  {}
func (IfStmt) node()      {}
func (WhileStmt) node()   {}
func (IndexAssign) node() {}
func (FieldAssign) node() {}
func (SendStmt) node()    {}
func (RangeStmt) node()   {}
func (GoStmt) node()      {}
func (ArenaStmt) node()   {}

// ---- Top level ----

type FuncDecl struct {
	Name   string
	Params []string
	// Returns are named return values: locals, zero-initialized, returned by a
	// bare `return`. Empty when the function has no named returns.
	Returns []string
	Body    []Stmt
	// Variadic marks the last parameter as variadic: it collects trailing call
	// arguments into a slice.
	Variadic bool
	// IsLambda marks a lifted lambda: it is always invoked via the closure
	// convention (a leading void* env), and its first NumCaptures params are
	// captured variables, supplied at runtime from that heap environment.
	IsLambda    bool
	NumCaptures int
	// Boxed names the variables (locals or params) that are captured by a
	// closure and therefore heap-boxed, so captures are by reference.
	Boxed map[string]bool
}

func (FuncDecl) node() {}
