package main

import (
	"fmt"
	"strings"
)

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
	KSlice  // []elem; element kind stored in the slot's elem slot
	KStruct // a named struct; name stored in the slot's sname
	KChan   // chan elem; element kind stored in the slot's elem slot
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
	case KStruct:
		return "struct"
	case KChan:
		return "chan"
	}
	return "?"
}

func isNumeric(k Kind) bool { return k == KInt || k == KFloat || k == KNum }

type Checker struct {
	parent []int
	kind   []Kind
	elem   []int    // for KSlice slots: the element slot; -1 otherwise
	sname  []string // for KStruct slots: the struct type name; "" otherwise

	structs map[string]*TypeDecl // declared struct types

	funcs      map[string]*FuncDecl
	funcParam  map[string][]int
	funcRet    map[string]int
	funcHasVal map[string]bool

	vars       map[string]map[string]int // func -> var name -> slot
	localOrder map[string][]string       // func -> locals in declaration order
	nodeSlot   map[Node]int

	// shared concrete slots (no inner type vars, safe to share)
	cBool, cString, cVoid, cInt int

	pairs []int      // flattened pairs: pairs[2i], pairs[2i+1]
	plus  []plusCons // overloaded '+' constraints, resolved by fixpoint

	lenArgs   []int      // slots passed to len(); must resolve to string or slice
	fieldUses []fieldUse // struct field access/assign, resolved after solve
}

type plusCons struct{ l, r, res int }

// fieldUse defers a struct field access/assignment: once base resolves to a
// struct, result is unified with the field's declared type.
type fieldUse struct {
	base   int
	field  string
	result int
}

func newSlot(c *Checker, k Kind) int {
	c.parent = append(c.parent, len(c.parent))
	c.kind = append(c.kind, k)
	c.elem = append(c.elem, -1)
	c.sname = append(c.sname, "")
	return len(c.parent) - 1
}

// newSliceSlot makes a KSlice slot whose element is the given slot.
func newSliceSlot(c *Checker, elemSlot int) int {
	s := newSlot(c, KSlice)
	c.elem[s] = elemSlot
	return s
}

// newStructSlot makes a KStruct slot for the named struct type.
func newStructSlot(c *Checker, name string) int {
	s := newSlot(c, KStruct)
	c.sname[s] = name
	return s
}

// newChanSlot makes a KChan slot whose element is the given slot.
func newChanSlot(c *Checker, elemSlot int) int {
	s := newSlot(c, KChan)
	c.elem[s] = elemSlot
	return s
}

// chanElem forces slot to be a channel and returns its element slot.
func (c *Checker) chanElem(slot int) (int, error) {
	r := c.find(slot)
	if c.kind[r] == KChan && c.elem[r] >= 0 {
		return c.elem[r], nil
	}
	e := newSlot(c, KVar)
	s := newChanSlot(c, e)
	if _, err := c.union(slot, s); err != nil {
		return 0, err
	}
	return c.elem[c.find(slot)], nil
}

// typeSlot builds a fresh slot for a declared type string: int, float, bool,
// string, a struct name, or []elem.
func (c *Checker) typeSlot(t string) (int, error) {
	if strings.HasPrefix(t, "[]") {
		e, err := c.typeSlot(t[2:])
		if err != nil {
			return 0, err
		}
		return newSliceSlot(c, e), nil
	}
	if strings.HasPrefix(t, "chan ") {
		e, err := c.typeSlot(strings.TrimPrefix(t, "chan "))
		if err != nil {
			return 0, err
		}
		return newChanSlot(c, e), nil
	}
	switch t {
	case "int":
		return newSlot(c, KInt), nil
	case "float":
		return newSlot(c, KFloat), nil
	case "bool":
		return newSlot(c, KBool), nil
	case "string":
		return newSlot(c, KString), nil
	}
	if _, ok := c.structs[t]; ok {
		return newStructSlot(c, t), nil
	}
	return 0, fmt.Errorf("unknown type %q", t)
}

// fieldType returns the declared type string of a struct field.
func (c *Checker) fieldType(structName, field string) (string, bool) {
	td, ok := c.structs[structName]
	if !ok {
		return "", false
	}
	for _, f := range td.Fields {
		if f.Name == field {
			return f.Type, true
		}
	}
	return "", false
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
	if a == KStruct && b == KStruct {
		return KStruct, nil // names reconciled by union
	}
	if a == KChan && b == KChan {
		return KChan, nil // element slots reconciled by union
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
	// merge element slots when joining slices or channels
	var ea, eb int = -1, -1
	if merged == KSlice || merged == KChan {
		ea, eb = c.elem[ra], c.elem[rb]
	}
	if merged == KStruct {
		na, nb := c.sname[ra], c.sname[rb]
		if na != "" && nb != "" && na != nb {
			return false, fmt.Errorf("type mismatch: struct %s vs %s", na, nb)
		}
	}
	c.parent[rb] = ra
	c.kind[ra] = merged
	if merged == KSlice || merged == KChan {
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
	if merged == KStruct {
		if c.sname[ra] == "" {
			c.sname[ra] = c.sname[rb]
		}
	}
	return true, nil
}

func (c *Checker) kindOf(slot int) Kind { return c.kind[c.find(slot)] }

// Check infers types for the program, returning an error on a type clash.
func Check(p *Program) (*Checker, error) {
	c := &Checker{
		funcs:      map[string]*FuncDecl{},
		funcParam:  map[string][]int{},
		funcRet:    map[string]int{},
		funcHasVal: map[string]bool{},
		vars:       map[string]map[string]int{},
		localOrder: map[string][]string{},
		nodeSlot:   map[Node]int{},
		structs:    map[string]*TypeDecl{},
	}
	c.cBool = newSlot(c, KBool)
	c.cString = newSlot(c, KString)
	c.cVoid = newSlot(c, KVoid)
	c.cInt = newSlot(c, KInt)

	// 0. register struct types and validate their field types
	for _, td := range p.Types {
		if _, dup := c.structs[td.Name]; dup {
			return nil, fmt.Errorf("duplicate type %q", td.Name)
		}
		c.structs[td.Name] = td
	}
	for _, td := range p.Types {
		for _, f := range td.Fields {
			if err := c.checkTypeName(f.Type); err != nil {
				return nil, fmt.Errorf("type %s field %s: %w", td.Name, f.Name, err)
			}
		}
	}

	// 1. signatures
	for _, fn := range p.Funcs {
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
	for _, fn := range p.Funcs {
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
	// 3b. resolve struct field accesses/assignments now that bases have a type
	if err := c.resolveFields(); err != nil {
		return nil, err
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
	// 5. validate len() arguments now that kinds are fully resolved.
	for _, slot := range c.lenArgs {
		if k := c.kindOf(slot); k != KString && k != KSlice {
			return nil, fmt.Errorf("len: argument must be a string or slice, got %s", k)
		}
	}
	return c, nil
}

func (c *Checker) addPair(a, b int) { c.pairs = append(c.pairs, a, b) }

// checkTypeName validates that a declared type string is known.
func (c *Checker) checkTypeName(t string) error {
	if strings.HasPrefix(t, "[]") {
		return c.checkTypeName(t[2:])
	}
	switch t {
	case "int", "float", "bool", "string":
		return nil
	}
	if _, ok := c.structs[t]; ok {
		return nil
	}
	return fmt.Errorf("unknown type %q", t)
}

// resolveFields ties off deferred struct field accesses: for each, once its
// base expression has resolved to a struct, unify the result with the field's
// declared type, then re-solve. Repeats to a fixpoint (chained access p.a.b).
func (c *Checker) resolveFields() error {
	done := make([]bool, len(c.fieldUses))
	for {
		progressed := false
		for i, fu := range c.fieldUses {
			if done[i] || c.kindOf(fu.base) != KStruct {
				continue
			}
			name := c.sname[c.find(fu.base)]
			ftype, ok := c.fieldType(name, fu.field)
			if !ok {
				return fmt.Errorf("struct %s has no field %q", name, fu.field)
			}
			fs, err := c.typeSlot(ftype)
			if err != nil {
				return err
			}
			if _, err := c.union(fu.result, fs); err != nil {
				return err
			}
			done[i] = true
			progressed = true
		}
		if !progressed {
			break
		}
		if err := c.solve(); err != nil {
			return err
		}
	}
	for i, fu := range c.fieldUses {
		if !done[i] {
			return fmt.Errorf("cannot infer struct type for field .%s", fu.field)
		}
	}
	return nil
}

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
	case *FieldAssign:
		xs, err := c.genExpr(fn, st.Target.X)
		if err != nil {
			return err
		}
		vs, err := c.genExpr(fn, st.Val)
		if err != nil {
			return err
		}
		c.fieldUses = append(c.fieldUses, fieldUse{base: xs, field: st.Target.Name, result: vs})
		return nil
	case *SendStmt:
		cs, err := c.genExpr(fn, st.Ch)
		if err != nil {
			return err
		}
		eslot, err := c.chanElem(cs)
		if err != nil {
			return err
		}
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
		eslot, err := c.typeSlot(ex.Elem)
		if err != nil {
			return 0, err
		}
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
	case *MakeChan:
		eslot, err := c.typeSlot(ex.Elem)
		if err != nil {
			return 0, err
		}
		return newChanSlot(c, eslot), nil
	case *Recv:
		cs, err := c.genExpr(fn, ex.Ch)
		if err != nil {
			return 0, err
		}
		return c.chanElem(cs)
	case *StructLit:
		return c.genStructLit(fn, ex)
	case *FieldAccess:
		xs, err := c.genExpr(fn, ex.X)
		if err != nil {
			return 0, err
		}
		res := newSlot(c, KVar)
		c.fieldUses = append(c.fieldUses, fieldUse{base: xs, field: ex.Name, result: res})
		return res, nil
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
		// C's % is integer-only; reject float operands at type-check time
		// so the user gets a clean MFL error instead of leaked cc output.
		c.addPair(ls, rs)
		c.addPair(ls, c.cInt)
		return ls, nil
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

func (c *Checker) genStructLit(fn *FuncDecl, ex *StructLit) (int, error) {
	td, ok := c.structs[ex.Type]
	if !ok {
		return 0, fmt.Errorf("unknown struct type %q", ex.Type)
	}
	res := newStructSlot(c, ex.Type)
	if len(ex.FieldNames) > 0 {
		for i, fname := range ex.FieldNames {
			ftype, ok := c.fieldType(ex.Type, fname)
			if !ok {
				return 0, fmt.Errorf("struct %s has no field %q", ex.Type, fname)
			}
			fs, err := c.typeSlot(ftype)
			if err != nil {
				return 0, err
			}
			vs, err := c.genExpr(fn, ex.Vals[i])
			if err != nil {
				return 0, err
			}
			c.addPair(vs, fs)
		}
	} else if len(ex.Vals) > 0 {
		if len(ex.Vals) != len(td.Fields) {
			return 0, fmt.Errorf("struct %s: expected %d fields, got %d", ex.Type, len(td.Fields), len(ex.Vals))
		}
		for i, v := range ex.Vals {
			fs, err := c.typeSlot(td.Fields[i].Type)
			if err != nil {
				return 0, err
			}
			vs, err := c.genExpr(fn, v)
			if err != nil {
				return 0, err
			}
			c.addPair(vs, fs)
		}
	}
	return res, nil
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
		// Record the arg so we can reject non-string/non-slice values (e.g.
		// an int) after solving, instead of emitting strlen() on a scalar.
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

// ElemKindOf returns the element kind of a slice- or channel-typed node.
func (c *Checker) ElemKindOf(n Node) Kind {
	r := c.find(c.nodeSlot[n])
	if (c.kind[r] == KSlice || c.kind[r] == KChan) && c.elem[r] >= 0 {
		return c.kindOf(c.elem[r])
	}
	return KInt
}

// ctypeSlot renders the C type of a slot, resolving struct names and slices.
func (c *Checker) ctypeSlot(slot int) string {
	r := c.find(slot)
	switch c.kind[r] {
	case KFloat:
		return "double"
	case KBool:
		return "int"
	case KString:
		return "char*"
	case KVoid:
		return "void"
	case KSlice:
		return "mfl_slice"
	case KStruct:
		return "mfl_" + c.sname[r]
	case KChan:
		return "mfl_chan*"
	}
	return "int64_t"
}

func (c *Checker) RetCType(fn string) string       { return c.ctypeSlot(c.funcRet[fn]) }
func (c *Checker) ParamCType(fn string, i int) string { return c.ctypeSlot(c.funcParam[fn][i]) }
func (c *Checker) VarCType(fn, name string) string { return c.ctypeSlot(c.vars[fn][name]) }
func (c *Checker) NodeCType(n Node) string         { return c.ctypeSlot(c.nodeSlot[n]) }

// ElemCType renders the C type of a slice- or channel-node's element.
func (c *Checker) ElemCType(n Node) string {
	r := c.find(c.nodeSlot[n])
	if (c.kind[r] == KSlice || c.kind[r] == KChan) && c.elem[r] >= 0 {
		return c.ctypeSlot(c.elem[r])
	}
	return "int64_t"
}

// Types returns the declared struct types (codegen emits a C typedef per type).
func (c *Checker) StructTypes() map[string]*TypeDecl { return c.structs }
