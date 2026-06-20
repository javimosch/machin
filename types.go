package main

import "fmt"

// MFL is statically typed, but types are inferred (no annotations) so the
// surface stays minimal. Inference is unification over a union-find of slots;
// every parameter, local, function return, and expression gets a slot.
//
// Kinds. KNum is the "flexible numeric" of an integer literal: it defaults to
// int but unifies up to float on contact with a float. Unresolved slots default
// to int (values) or void (a function that never returns one).

type Kind int

const (
	KVar    Kind = iota // unbound
	KNum                // numeric literal, int unless unified with float
	KInt
	KFloat
	KBool
	KString
	KVoid
	KSlice // []elem; element kind stored in the slot's elem slot
)

func (k Kind) String() string {
	switch k {
	case KVar:
		return "var"
	case KNum:
		return "num"
	case KInt:
		return "int"
	case KFloat:
		return "float"
	case KBool:
		return "bool"
	case KString:
		return "string"
	case KVoid:
		return "void"
	case KSlice:
		return "slice"
	}
	return "?"
}

func kindFromName(name string) (Kind, error) {
	switch name {
	case "int":
		return KInt, nil
	case "float":
		return KFloat, nil
	case "string":
		return KString, nil
	case "bool":
		return KBool, nil
	}
	return KVar, fmt.Errorf("unknown type %q", name)
}

func isNumeric(k Kind) bool { return k == KInt || k == KFloat || k == KNum }

type Checker struct {
	parent []int
	kind   []Kind
	elem   []int // for KSlice slots: the element slot; -1 otherwise

	funcs      map[string]*FuncDecl
	funcParam  map[string][]int
	funcRet    map[string]int
	funcHasVal map[string]bool

	vars       map[string]map[string]int // func -> var name -> slot
	localOrder map[string][]string       // func -> locals in declaration order
	nodeSlot   map[Node]int

	// shared concrete slots (no inner type vars, safe to share)
	cBool, cString, cVoid, cInt int

	pairs   []int      // flattened pairs: pairs[2i], pairs[2i+1]
	plus    []plusCons // overloaded '+' constraints, resolved by fixpoint
	lenArgs []int      // arg slots passed to len(); must resolve to string/slice
}

type plusCons struct{ l, r, res int }

func newSlot(c *Checker, k Kind) int {
	c.parent = append(c.parent, len(c.parent))
	c.kind = append(c.kind, k)
	c.elem = append(c.elem, -1)
	return len(c.parent) - 1
}

// newSliceSlot makes a KSlice slot whose element is the given slot.
func newSliceSlot(c *Checker, elemSlot int) int {
	s := newSlot(c, KSlice)
	c.elem[s] = elemSlot
	return s
}

// sliceElem forces slot to be a slice and returns its element slot.
func (c *Checker) sliceElem(slot int) (int, error) {
	r := c.find(slot)
	if c.kind[r] == KSlice && c.elem[r] >= 0 {
		return c.elem[r], nil
	}
	e := newSlot(c, KVar)
	s := newSliceSlot(c, e)
	if _, err := c.union(slot, s); err != nil {
		return 0, err
	}
	return c.elem[c.find(slot)], nil
}

func (c *Checker) find(i int) int {
	for c.parent[i] != i {
		c.parent[i] = c.parent[c.parent[i]]
		i = c.parent[i]
	}
	return i
}

// reconcile merges two kinds, returning the merged kind or an error.
func reconcile(a, b Kind) (Kind, error) {
	if a == b {
		return a, nil
	}
	if a == KVar {
		return b, nil
	}
	if b == KVar {
		return a, nil
	}
	// numeric flex
	if a == KNum && isNumeric(b) {
		return b, nil
	}
	if b == KNum && isNumeric(a) {
		return a, nil
	}
	if a == KSlice && b == KSlice {
		return KSlice, nil // element slots reconciled by union
	}
	return KVar, fmt.Errorf("type mismatch: %s vs %s", a, b)
}

func (c *Checker) union(a, b int) (bool, error) {
	ra, rb := c.find(a), c.find(b)
	if ra == rb {
		return false, nil
	}
	merged, err := reconcile(c.kind[ra], c.kind[rb])
	if err != nil {
		return false, err
	}
	// merge element slots when joining slices
	var ea, eb int = -1, -1
	if merged == KSlice {
		ea, eb = c.elem[ra], c.elem[rb]
	}
	c.parent[rb] = ra
	c.kind[ra] = merged
	if merged == KSlice {
		keep := ea
		if keep < 0 {
			keep = eb
		}
		c.elem[ra] = keep
		if ea >= 0 && eb >= 0 && ea != eb {
			if _, err := c.union(ea, eb); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func (c *Checker) kindOf(slot int) Kind { return c.kind[c.find(slot)] }

// Check infers types for all functions, returning an error on a type clash.
func Check(funcs []*FuncDecl) (*Checker, error) {
	c := &Checker{
		funcs:      map[string]*FuncDecl{},
		funcParam:  map[string][]int{},
		funcRet:    map[string]int{},
		funcHasVal: map[string]bool{},
		vars:       map[string]map[string]int{},
		localOrder: map[string][]string{},
		nodeSlot:   map[Node]int{},
	}
	c.cBool = newSlot(c, KBool)
	c.cString = newSlot(c, KString)
	c.cVoid = newSlot(c, KVoid)
	c.cInt = newSlot(c, KInt)

	// 1. signatures
	for _, fn := range funcs {
		c.funcs[fn.Name] = fn
		params := make([]int, len(fn.Params))
		env := map[string]int{}
		for i, p := range fn.Params {
			params[i] = newSlot(c, KVar)
			env[p] = params[i]
		}
		c.funcParam[fn.Name] = params
		c.funcRet[fn.Name] = newSlot(c, KVar)
		c.vars[fn.Name] = env
	}
	// 2. constraints
	for _, fn := range funcs {
		for _, s := range fn.Body {
			if err := c.genStmt(fn, s); err != nil {
				return nil, err
			}
		}
	}
	// 3. solve
	if err := c.solve(); err != nil {
		return nil, err
	}
	// 3b. validate len() arguments: only strings and slices have a length.
	// Checked before defaults so an unconstrained/numeric arg (e.g. len(5))
	// is rejected rather than silently defaulted to int (#3).
	for _, slot := range c.lenArgs {
		if k := c.kindOf(slot); k != KString && k != KSlice {
			return nil, fmt.Errorf("type mismatch: len expects string or slice, got %s", k)
		}
	}
	// 4. defaults
	for name, ret := range c.funcRet {
		r := c.find(ret)
		if c.kind[r] == KVar && !c.funcHasVal[name] {
			c.kind[r] = KVoid
		}
	}
	for i := range c.parent {
		r := c.find(i)
		if c.kind[r] == KVar || c.kind[r] == KNum {
			c.kind[r] = KInt
		}
	}
	return c, nil
}

func (c *Checker) addPair(a, b int) { c.pairs = append(c.pairs, a, b) }

func (c *Checker) solve() error {
	for {
		changed := false
		for i := 0; i+1 < len(c.pairs); i += 2 {
			ch, err := c.union(c.pairs[i], c.pairs[i+1])
			if err != nil {
				return err
			}
			changed = changed || ch
		}
		for _, p := range c.plus {
			kl, kr := c.kindOf(p.l), c.kindOf(p.r)
			var a, b int = -1, -1
			if kl == KString || kr == KString {
				a, b = p.l, p.r
			} else if isNumeric(kl) || isNumeric(kr) {
				a, b = p.l, p.r
			} else {
				continue // undecided this round
			}
			ch1, err := c.union(a, b)
			if err != nil {
				return err
			}
			ch2, err := c.union(p.res, p.l)
			if err != nil {
				return err
			}
			changed = changed || ch1 || ch2
		}
		if !changed {
			return nil
		}
	}
}

// ---- constraint generation ----

func (c *Checker) genStmt(fn *FuncDecl, s Stmt) error {
	switch st := s.(type) {
	case *ExprStmt:
		_, err := c.genExpr(fn, st.X)
		return err
	case *AssignStmt:
		vs, err := c.genExpr(fn, st.Val)
		if err != nil {
			return err
		}
		env := c.vars[fn.Name]
		slot, ok := env[st.Name]
		if !ok {
			slot = newSlot(c, KVar)
			env[st.Name] = slot
			c.localOrder[fn.Name] = append(c.localOrder[fn.Name], st.Name)
		}
		c.addPair(slot, vs)
		return nil
	case *ReturnStmt:
		if st.Val == nil {
			c.addPair(c.funcRet[fn.Name], c.cVoid)
			return nil
		}
		vs, err := c.genExpr(fn, st.Val)
		if err != nil {
			return err
		}
		c.funcHasVal[fn.Name] = true
		c.addPair(c.funcRet[fn.Name], vs)
		return nil
	case *IfStmt:
		cs, err := c.genExpr(fn, st.Cond)
		if err != nil {
			return err
		}
		c.addPair(cs, c.cBool)
		for _, t := range st.Then {
			if err := c.genStmt(fn, t); err != nil {
				return err
			}
		}
		for _, e := range st.Else {
			if err := c.genStmt(fn, e); err != nil {
				return err
			}
		}
		return nil
	case *WhileStmt:
		cs, err := c.genExpr(fn, st.Cond)
		if err != nil {
			return err
		}
		c.addPair(cs, c.cBool)
		for _, t := range st.Body {
			if err := c.genStmt(fn, t); err != nil {
				return err
			}
		}
		return nil
	case *IndexAssign:
		xs, err := c.genExpr(fn, st.Target.X)
		if err != nil {
			return err
		}
		eslot, err := c.sliceElem(xs)
		if err != nil {
			return err
		}
		is, err := c.genExpr(fn, st.Target.Idx)
		if err != nil {
			return err
		}
		c.addPair(is, c.cInt)
		vs, err := c.genExpr(fn, st.Val)
		if err != nil {
			return err
		}
		c.addPair(vs, eslot)
		return nil
	case *GoStmt:
		if _, ok := c.funcs[st.Call.Callee]; !ok {
			return fmt.Errorf("go: %q is not a user function", st.Call.Callee)
		}
		_, err := c.genExpr(fn, st.Call)
		return err
	}
	return fmt.Errorf("typecheck: unknown statement %T", s)
}

func (c *Checker) genExpr(fn *FuncDecl, e Expr) (int, error) {
	if s, ok := c.nodeSlot[e]; ok {
		return s, nil
	}
	slot, err := c.genExprInner(fn, e)
	if err != nil {
		return 0, err
	}
	c.nodeSlot[e] = slot
	return slot, nil
}

func (c *Checker) genExprInner(fn *FuncDecl, e Expr) (int, error) {
	switch ex := e.(type) {
	case *IntLit:
		return newSlot(c, KNum), nil
	case *FloatLit:
		return newSlot(c, KFloat), nil
	case *StringLit:
		return c.cString, nil
	case *BoolLit:
		return c.cBool, nil
	case *NilLit:
		return c.cVoid, nil
	case *Ident:
		if s, ok := c.vars[fn.Name][ex.Name]; ok {
			return s, nil
		}
		return 0, fmt.Errorf("%s: undefined variable %q", fn.Name, ex.Name)
	case *Unary:
		xs, err := c.genExpr(fn, ex.X)
		if err != nil {
			return 0, err
		}
		if ex.Op == "!" {
			c.addPair(xs, c.cBool)
			return c.cBool, nil
		}
		c.addPair(xs, newSlot(c, KNum))
		return xs, nil
	case *Binary:
		return c.genBinary(fn, ex)
	case *Call:
		return c.genCall(fn, ex)
	case *SliceLit:
		ek, err := kindFromName(ex.Elem)
		if err != nil {
			return 0, err
		}
		eslot := newSlot(c, ek)
		s := newSliceSlot(c, eslot)
		for _, el := range ex.Elems {
			es, err := c.genExpr(fn, el)
			if err != nil {
				return 0, err
			}
			c.addPair(es, eslot)
		}
		return s, nil
	case *Index:
		xs, err := c.genExpr(fn, ex.X)
		if err != nil {
			return 0, err
		}
		eslot, err := c.sliceElem(xs)
		if err != nil {
			return 0, err
		}
		is, err := c.genExpr(fn, ex.Idx)
		if err != nil {
			return 0, err
		}
		c.addPair(is, c.cInt)
		return eslot, nil
	}
	return 0, fmt.Errorf("typecheck: unknown expression %T", e)
}

func (c *Checker) genBinary(fn *FuncDecl, ex *Binary) (int, error) {
	ls, err := c.genExpr(fn, ex.L)
	if err != nil {
		return 0, err
	}
	rs, err := c.genExpr(fn, ex.R)
	if err != nil {
		return 0, err
	}
	switch ex.Op {
	case "+":
		res := newSlot(c, KVar)
		c.plus = append(c.plus, plusCons{l: ls, r: rs, res: res})
		return res, nil
	case "-", "*", "/":
		c.addPair(ls, rs)
		c.addPair(ls, newSlot(c, KNum))
		return ls, nil
	case "%":
		// C's '%' is integer-only, so constrain BOTH operands to int. This
		// turns float modulo into a clean MFL type error at check time
		// instead of leaking a raw cc error from invalid generated C (#2).
		c.addPair(ls, c.cInt)
		c.addPair(rs, c.cInt)
		return c.cInt, nil
	case "==", "!=", "<", "<=", ">", ">=":
		c.addPair(ls, rs)
		return c.cBool, nil
	case "&&", "||":
		c.addPair(ls, c.cBool)
		c.addPair(rs, c.cBool)
		return c.cBool, nil
	}
	return 0, fmt.Errorf("unknown operator %q", ex.Op)
}

func (c *Checker) genCall(fn *FuncDecl, ex *Call) (int, error) {
	argSlots := make([]int, len(ex.Args))
	for i, a := range ex.Args {
		s, err := c.genExpr(fn, a)
		if err != nil {
			return 0, err
		}
		argSlots[i] = s
	}
	switch ex.Callee {
	case "print", "println":
		return c.cVoid, nil
	case "len":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("len: 1 arg")
		}
		// len works on strings or slices; codegen reads the resolved kind.
		// Record the arg so we can reject len(int) etc. after solving (#3).
		c.lenArgs = append(c.lenArgs, argSlots[0])
		return c.cInt, nil
	case "append":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("append: 2 args")
		}
		eslot, err := c.sliceElem(argSlots[0])
		if err != nil {
			return 0, err
		}
		c.addPair(argSlots[1], eslot)
		return argSlots[0], nil
	case "sleep":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("sleep: 1 arg")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cVoid, nil
	case "str":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("str: 1 arg")
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		return c.cString, nil
	case "int":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("int: 1 arg")
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		return c.cInt, nil
	case "listen", "accept":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("%s: 1 arg", ex.Callee)
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cInt, nil
	case "read":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("read: 1 arg")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cString, nil
	case "write":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("write: 2 args")
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cString)
		return c.cInt, nil
	case "close":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("close: 1 arg")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cVoid, nil
	}
	params, ok := c.funcParam[ex.Callee]
	if !ok {
		return 0, fmt.Errorf("%s: call to undefined function %q", fn.Name, ex.Callee)
	}
	if len(params) != len(argSlots) {
		return 0, fmt.Errorf("%s: expected %d args, got %d", ex.Callee, len(params), len(argSlots))
	}
	for i := range params {
		c.addPair(params[i], argSlots[i])
	}
	return c.funcRet[ex.Callee], nil
}

// ---- queries used by codegen ----

func (c *Checker) RetKind(fn string) Kind   { return c.kindOf(c.funcRet[fn]) }
func (c *Checker) ParamKind(fn string, i int) Kind {
	return c.kindOf(c.funcParam[fn][i])
}
func (c *Checker) VarKind(fn, name string) Kind { return c.kindOf(c.vars[fn][name]) }
func (c *Checker) NodeKind(n Node) Kind         { return c.kindOf(c.nodeSlot[n]) }
func (c *Checker) Locals(fn string) []string    { return c.localOrder[fn] }

// ElemKindOf returns the element kind of a slice-typed expression node.
func (c *Checker) ElemKindOf(n Node) Kind {
	r := c.find(c.nodeSlot[n])
	if c.kind[r] == KSlice && c.elem[r] >= 0 {
		return c.kindOf(c.elem[r])
	}
	return KInt
}
