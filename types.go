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
	KMap    // map[k]v; key/value kinds stored in mkey/mval slots
	KFunc   // a function value; signature stored in the slot's fsig
	KBytes  // a NUL-safe binary buffer (pointer + length)
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
	case KMap:
		return "map"
	case KFunc:
		return "func"
	case KBytes:
		return "bytes"
	}
	return "?"
}

func isNumeric(k Kind) bool { return k == KInt || k == KFloat || k == KNum }

type Checker struct {
	parent []int
	kind   []Kind
	elem   []int    // for KSlice/KChan slots: the element slot; -1 otherwise
	sname  []string // for KStruct slots: the struct type name; "" otherwise
	mkey   []int       // for KMap slots: the key slot; -1 otherwise
	mval   []int       // for KMap slots: the value slot; -1 otherwise
	fsig   []*funcSig  // for KFunc slots: the signature; nil otherwise

	structs map[string]*TypeDecl // declared struct types

	funcs     map[string]*FuncDecl
	funcParam map[string][]int
	funcRets  map[string][]int // one slot per return value (empty = void)

	vars       map[string]map[string]int // func -> var name -> slot
	localOrder map[string][]string       // func -> locals in declaration order
	nodeSlot   map[string]map[Node]int // instance -> expr node -> slot

	// shared concrete slots (no inner type vars, safe to share)
	cBool, cString, cVoid, cInt, cFloat, cBytes int

	externs map[string]*ExternFunc // foreign C functions, by name

	pairs []int      // flattened pairs: pairs[2i], pairs[2i+1]
	plus  []plusCons // overloaded '+' constraints, resolved by fixpoint

	lenArgs   []int      // slots passed to len(); must resolve to string/slice/map
	strArgs   []int      // slots passed to str(); must resolve to a number, bool, or string
	fieldUses []fieldUse // struct field access/assign, resolved after solve
	indexUses []indexUse // x[i] access/assign, resolved after solve (slice or map)
	rangeUses []rangeUse // for-range loops, resolved after solve

	// monomorphization: each call instantiates a fresh copy of the callee, so a
	// function is specialized per concrete call-site type (deduped at codegen).
	instFn      map[string]*FuncDecl              // instance name -> source function
	instStack   map[string]string                // source name -> instance being generated (recursion)
	instOrder   []string                         // instances in creation order
	instCounter int                              // unique instance ids
	callInst    map[string]map[*Call]string       // enclosing instance -> call node -> callee instance
	closureInst map[string]map[*MakeClosure]string // enclosing instance -> closure node -> lambda instance
	cnameOf     map[string]string                 // instance -> C function name (deduped)
	reps        []string                          // representative instances (one C function each)
	exportSrc   map[string]bool                   // source names declared `export func`
	hasMainFn   bool                              // program defines a main function
	globalSlot  map[string]int                    // package-global name -> type slot
	globalOrder []string                          // package globals in declaration order
}

// IsLocal reports whether name is a parameter or local of the given instance (so
// codegen can tell a shadowing local from a package global of the same name).
func (c *Checker) IsLocal(inst, name string) bool {
	_, ok := c.vars[inst][name]
	return ok
}

// IsGlobal reports whether name is a declared package global.
func (c *Checker) IsGlobal(name string) bool { _, ok := c.globalSlot[name]; return ok }

// GlobalOrder returns the package globals in declaration order.
func (c *Checker) GlobalOrder() []string { return c.globalOrder }

// GlobalCType returns the C type of a package global.
func (c *Checker) GlobalCType(name string) string { return c.ctypeSlot(c.globalSlot[name]) }

// GlobalKind returns the inferred kind of a package global (for zero values).
func (c *Checker) GlobalKind(name string) Kind { return c.kindOf(c.globalSlot[name]) }

// HasMain reports whether the program defines a main function.
func (c *Checker) HasMain() bool { return c.hasMainFn }

// ExportNames returns the source names of every `export func` that survived to a
// representative instance — the names a wasm build exports to the host (each C
// function carries an export_name attribute, so JS sees the clean name).
func (c *Checker) ExportNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, inst := range c.reps {
		src := c.instFn[inst].Name
		if c.exportSrc[src] && !seen[src] {
			seen[src] = true
			out = append(out, src)
		}
	}
	return out
}

// rangeUse defers a for-range: once base resolves, key/val are bound to the
// index+element (slice/string) or key+value (map) types.
type rangeUse struct {
	base   int
	key    int
	val    int
	hasVal bool
}

// indexUse defers x[i]: once base resolves to a slice or map, idx and result
// are unified with the element (slice) or key/value (map) types.
type indexUse struct {
	base   int
	idx    int
	result int
}

type plusCons struct{ l, r, res int }

// funcSig is a function value's signature: parameter slots and a single return
// slot (ret < 0 means void).
type funcSig struct {
	params []int
	ret    int
}

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
	c.mkey = append(c.mkey, -1)
	c.mval = append(c.mval, -1)
	c.fsig = append(c.fsig, nil)
	return len(c.parent) - 1
}

// newFuncSlot makes a KFunc slot with the given signature.
func newFuncSlot(c *Checker, sig *funcSig) int {
	s := newSlot(c, KFunc)
	c.fsig[s] = sig
	return s
}

// funcOf forces slot to be a function value of the given parameter arity and
// returns its signature.
func (c *Checker) funcOf(slot, arity int) (*funcSig, error) {
	r := c.find(slot)
	if c.kind[r] == KFunc && c.fsig[r] != nil {
		return c.fsig[r], nil
	}
	params := make([]int, arity)
	for i := range params {
		params[i] = newSlot(c, KVar)
	}
	sig := &funcSig{params: params, ret: newSlot(c, KVar)}
	if _, err := c.union(slot, newFuncSlot(c, sig)); err != nil {
		return nil, err
	}
	return c.fsig[c.find(slot)], nil
}

// newMapSlot makes a KMap slot with the given key and value slots.
func newMapSlot(c *Checker, keySlot, valSlot int) int {
	s := newSlot(c, KMap)
	c.mkey[s] = keySlot
	c.mval[s] = valSlot
	return s
}

// mapKV forces slot to be a map and returns its key and value slots.
func (c *Checker) mapKV(slot int) (int, int, error) {
	r := c.find(slot)
	if c.kind[r] == KMap && c.mkey[r] >= 0 && c.mval[r] >= 0 {
		return c.mkey[r], c.mval[r], nil
	}
	k := newSlot(c, KVar)
	v := newSlot(c, KVar)
	m := newMapSlot(c, k, v)
	if _, err := c.union(slot, m); err != nil {
		return 0, 0, err
	}
	rr := c.find(slot)
	return c.mkey[rr], c.mval[rr], nil
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
	if strings.HasPrefix(t, "map[") {
		kt, vt, err := splitMapType(t)
		if err != nil {
			return 0, err
		}
		ks, err := c.typeSlot(kt)
		if err != nil {
			return 0, err
		}
		vs, err := c.typeSlot(vt)
		if err != nil {
			return 0, err
		}
		return newMapSlot(c, ks, vs), nil
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
	case "bytes":
		return newSlot(c, KBytes), nil
	case "func":
		return newSlot(c, KFunc), nil // signature filled in by unification
	}
	if _, ok := c.structs[t]; ok {
		return newStructSlot(c, t), nil
	}
	return 0, fmt.Errorf("unknown type %q", t)
}

// splitMapType splits "map[KEY]VAL" into its key and value type strings,
// honoring nested brackets in the key (e.g. map[[]int]string).
func splitMapType(t string) (string, string, error) {
	inner := t[len("map["):]
	depth := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '[':
			depth++
		case ']':
			if depth == 0 {
				return inner[:i], inner[i+1:], nil
			}
			depth--
		}
	}
	return "", "", fmt.Errorf("malformed map type %q", t)
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
	if a == KMap && b == KMap {
		return KMap, nil // key/value slots reconciled by union
	}
	if a == KFunc && b == KFunc {
		return KFunc, nil // signatures reconciled by union
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
	var sigA, sigB *funcSig
	if merged == KFunc {
		sigA, sigB = c.fsig[ra], c.fsig[rb]
		if sigA != nil && sigB != nil && len(sigA.params) != len(sigB.params) {
			return false, fmt.Errorf("function arity mismatch: %d vs %d", len(sigA.params), len(sigB.params))
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
	if merged == KMap {
		mka, mkb := c.mkey[ra], c.mkey[rb]
		mva, mvb := c.mval[ra], c.mval[rb]
		if mka < 0 {
			c.mkey[ra] = mkb
		}
		if mva < 0 {
			c.mval[ra] = mvb
		}
		if mka >= 0 && mkb >= 0 && mka != mkb {
			if _, err := c.union(mka, mkb); err != nil {
				return false, err
			}
		}
		if mva >= 0 && mvb >= 0 && mva != mvb {
			if _, err := c.union(mva, mvb); err != nil {
				return false, err
			}
		}
	}
	if merged == KFunc {
		keep := sigA
		if keep == nil {
			keep = sigB
		}
		c.fsig[ra] = keep
		if sigA != nil && sigB != nil && sigA != sigB {
			for i := range sigA.params {
				if _, err := c.union(sigA.params[i], sigB.params[i]); err != nil {
					return false, err
				}
			}
			if sigA.ret >= 0 && sigB.ret >= 0 {
				if _, err := c.union(sigA.ret, sigB.ret); err != nil {
					return false, err
				}
			}
		}
	}
	return true, nil
}

func (c *Checker) kindOf(slot int) Kind { return c.kind[c.find(slot)] }

// returnArity finds a function's number of return values: the count in the
// first return statement that has values (searching nested blocks), else 0.
func returnArity(body []Stmt) int {
	for _, s := range body {
		switch st := s.(type) {
		case *ReturnStmt:
			if len(st.Vals) > 0 {
				return len(st.Vals)
			}
		case *IfStmt:
			if n := returnArity(st.Then); n > 0 {
				return n
			}
			if n := returnArity(st.Else); n > 0 {
				return n
			}
		case *WhileStmt:
			if n := returnArity(st.Body); n > 0 {
				return n
			}
		case *RangeStmt:
			if n := returnArity(st.Body); n > 0 {
				return n
			}
		case *ArenaStmt:
			if n := returnArity(st.Body); n > 0 {
				return n
			}
		}
	}
	return 0
}

// funcArity is a function's number of return values: its named returns, or the
// count inferred from its return statements.
func funcArity(fn *FuncDecl) int {
	if len(fn.Returns) > 0 {
		return len(fn.Returns)
	}
	return returnArity(fn.Body)
}

// instantiate creates a fresh specialization of a function (new parameter,
// return, and local slots) and generates its body's constraints. A recursive
// call reuses the in-progress instance (monomorphic recursion). The caller
// unifies the call arguments with the instance's parameters.
func (c *Checker) instantiate(name string) (string, error) {
	if cur, ok := c.instStack[name]; ok {
		return cur, nil
	}
	fn := c.funcs[name]
	inst := fmt.Sprintf("%s$%d", name, c.instCounter)
	c.instCounter++
	c.instFn[inst] = fn
	c.instOrder = append(c.instOrder, inst)

	params := make([]int, len(fn.Params))
	env := map[string]int{}
	for i, p := range fn.Params {
		params[i] = newSlot(c, KVar)
		env[p] = params[i]
	}
	c.funcParam[inst] = params
	c.vars[inst] = env
	// return slots: when named, each is also a zero-initialized local in scope
	arity := len(fn.Returns)
	if arity == 0 {
		arity = returnArity(fn.Body)
	}
	rets := make([]int, arity)
	for i := range rets {
		rets[i] = newSlot(c, KVar)
		if i < len(fn.Returns) {
			env[fn.Returns[i]] = rets[i]
			c.localOrder[inst] = append(c.localOrder[inst], fn.Returns[i])
		}
	}
	c.funcRets[inst] = rets
	c.nodeSlot[inst] = map[Node]int{}
	c.callInst[inst] = map[*Call]string{}
	c.closureInst[inst] = map[*MakeClosure]string{}

	prev, had := c.instStack[name]
	c.instStack[name] = inst
	syn := &FuncDecl{Name: inst, Params: fn.Params, Returns: fn.Returns, Body: fn.Body, IsLambda: fn.IsLambda, NumCaptures: fn.NumCaptures}
	for _, s := range fn.Body {
		if err := c.genStmt(syn, s); err != nil {
			return "", err
		}
	}
	if had {
		c.instStack[name] = prev
	} else {
		delete(c.instStack, name)
	}
	return inst, nil
}

// Check infers types for the program, returning an error on a type clash.
func Check(p *Program) (*Checker, error) {
	c := &Checker{
		funcs:      map[string]*FuncDecl{},
		funcParam:  map[string][]int{},
		funcRets:   map[string][]int{},
		vars:       map[string]map[string]int{},
		localOrder: map[string][]string{},
		nodeSlot:   map[string]map[Node]int{},
		structs:    map[string]*TypeDecl{},
		instFn:     map[string]*FuncDecl{},
		instStack:  map[string]string{},
		callInst:   map[string]map[*Call]string{},
		closureInst: map[string]map[*MakeClosure]string{},
		externs:    map[string]*ExternFunc{},
	}
	c.cBool = newSlot(c, KBool)
	c.cString = newSlot(c, KString)
	c.cVoid = newSlot(c, KVoid)
	c.cInt = newSlot(c, KInt)
	c.cFloat = newSlot(c, KFloat)
	c.cBytes = newSlot(c, KBytes)

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

	// 1. register functions
	var exported []string
	c.exportSrc = map[string]bool{}
	for _, fn := range p.Funcs {
		// A user function may not shadow a builtin: at call sites the builtin wins,
		// so the function would be silently ignored (a confusing footgun). Externs
		// may shadow a builtin (intentional — the FFI symbol replaces it).
		if isBuiltinName(fn.Name) {
			return nil, fmt.Errorf("function %q shadows the builtin %q — it would be silently ignored at call sites; rename it", fn.Name, fn.Name)
		}
		c.funcs[fn.Name] = fn
		if fn.Exported {
			exported = append(exported, fn.Name)
			c.exportSrc[fn.Name] = true
		}
	}
	_, hasMain := c.funcs["main"]
	c.hasMainFn = hasMain
	// A program needs an entry point: a main, or — for a library target (wasm) —
	// at least one `export func`. Exported functions are reachability roots too.
	if !hasMain && len(exported) == 0 {
		return nil, fmt.Errorf("no main function defined (and no exported functions)")
	}
	// register foreign (extern) functions; their signatures are fixed, not inferred
	ffiTypeOK := func(t string) bool {
		if t == "" || isFFIScalar(t) {
			return true
		}
		if strings.HasPrefix(t, "*") {
			return true // *Name: deref an MFL int (ptr) to a header C type, by value
		}
		if strings.HasSuffix(t, "*") {
			_, ok := c.structs[strings.TrimSuffix(t, "*")]
			return ok // Name*: inout, pass an MFL cstruct by pointer with writeback
		}
		_, ok := c.structs[t]
		return ok
	}
	// the set of all declared cstruct names, so a cstruct field may be another
	// cstruct (nested by-value structs — e.g. raylib Camera3D holds Vector3s)
	cstructNames := map[string]bool{}
	for _, ed := range p.Externs {
		for _, cs := range ed.Structs {
			cstructNames[cs.Name] = true
		}
	}
	for _, ed := range p.Externs {
		for _, cs := range ed.Structs {
			for _, f := range cs.Fields {
				if !isFFINumeric(f.CType) && !cstructNames[f.CType] && f.CType != "ptr" {
					return nil, fmt.Errorf("cstruct %s field %s: %q is not a numeric C type, a declared cstruct, or ptr", cs.Name, f.Name, f.CType)
				}
			}
		}
		for _, ef := range ed.Funcs {
			f := ef
			if _, dup := c.externs[f.Name]; dup {
				return nil, fmt.Errorf("duplicate extern fn %q", f.Name)
			}
			if _, clash := c.funcs[f.Name]; clash {
				return nil, fmt.Errorf("extern fn %q clashes with a function", f.Name)
			}
			for _, pt := range f.Params {
				if !ffiTypeOK(pt) {
					return nil, fmt.Errorf("extern fn %s: unknown parameter type %q", f.Name, pt)
				}
			}
			if !ffiTypeOK(f.Ret) {
				return nil, fmt.Errorf("extern fn %s: unknown return type %q", f.Name, f.Ret)
			}
			c.externs[f.Name] = &f
		}
	}
	// 1b. package globals: type each one from its initializer, in declaration order
	//     (a later global may reference an earlier one). They live in a synthetic
	//     "$globals" context; references resolve through the globalSlot fallback in
	//     genExpr/AssignStmt, so functions and globals share the same variables.
	c.globalSlot = map[string]int{}
	if len(p.Globals) > 0 {
		gfn := &FuncDecl{Name: "$globals"}
		c.vars["$globals"] = map[string]int{}
		c.nodeSlot["$globals"] = map[Node]int{}
		c.callInst["$globals"] = map[*Call]string{}
		c.closureInst["$globals"] = map[*MakeClosure]string{}
		c.instStack["$globals"] = "$globals"
		for _, g := range p.Globals {
			vs, err := c.genExpr(gfn, g.Init)
			if err != nil {
				return nil, fmt.Errorf("global %s: %w", g.Name, err)
			}
			slot := newSlot(c, KVar)
			c.addPair(slot, vs)
			c.globalSlot[g.Name] = slot
			c.globalOrder = append(c.globalOrder, g.Name)
		}
		delete(c.instStack, "$globals")
	}
	// 2. instantiate from the roots; every reachable function is specialized per
	//    concrete call-site type (monomorphization). main is a root when present;
	//    each `export func` is also a root (kept even if main never calls it), with
	//    its parameter types inferred from the function body.
	if hasMain {
		if _, err := c.instantiate("main"); err != nil {
			return nil, err
		}
	}
	for _, name := range exported {
		if _, err := c.instantiate(name); err != nil {
			return nil, err
		}
	}
	// 3. solve
	if err := c.solve(); err != nil {
		return nil, err
	}
	// 3b. resolve deferred x[i] and struct field uses now that bases have a type
	if err := c.resolveDeferred(); err != nil {
		return nil, err
	}
	// 4. defaults — return slots (if any) default to int like other slots; a
	// function with no return slots is void.
	for i := range c.parent {
		r := c.find(i)
		if c.kind[r] == KVar || c.kind[r] == KNum {
			c.kind[r] = KInt
		}
	}
	// 5. validate len() arguments now that kinds are fully resolved.
	for _, slot := range c.lenArgs {
		if k := c.kindOf(slot); k != KString && k != KSlice && k != KMap && k != KBytes {
			return nil, fmt.Errorf("len: argument must be a string, slice, map, or bytes, got %s", k)
		}
	}
	// 5b. validate str() arguments: a number, bool, or string.
	for _, slot := range c.strArgs {
		if k := c.kindOf(slot); !isNumeric(k) && k != KBool && k != KString {
			return nil, fmt.Errorf("str: argument must be a number, bool, or string, got %s", k)
		}
	}
	// 6. dedup instances by concrete signature and assign C names
	c.finalizeMono()
	return c, nil
}

// finalizeMono deduplicates instances that have identical concrete signatures
// (so a function used twice at the same type yields one C function) and assigns
// each unique instance a C name.
func (c *Checker) finalizeMono() {
	c.cnameOf = map[string]string{}
	repByKey := map[string]string{}
	perSrc := map[string]int{}
	for _, inst := range c.instOrder {
		key := c.instFn[inst].Name + "|" + c.sigString(inst)
		if rep, ok := repByKey[key]; ok {
			c.cnameOf[inst] = c.cnameOf[rep]
			continue
		}
		repByKey[key] = inst
		c.reps = append(c.reps, inst)
		src := c.instFn[inst].Name
		if src == "main" {
			c.cnameOf[inst] = "mfl_main"
		} else {
			c.cnameOf[inst] = fmt.Sprintf("mfl_%s_%d", src, perSrc[src])
			perSrc[src]++
		}
	}
}

// sigString is an instance's canonical concrete signature (for dedup).
func (c *Checker) sigString(inst string) string {
	var b strings.Builder
	b.WriteByte('(')
	for i, p := range c.funcParam[inst] {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(c.typeStringSlot(p))
	}
	b.WriteString(")->(")
	for i, r := range c.funcRets[inst] {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(c.typeStringSlot(r))
	}
	b.WriteByte(')')
	return b.String()
}

func (c *Checker) addPair(a, b int) { c.pairs = append(c.pairs, a, b) }

// checkTypeName validates that a declared type string is known.
func (c *Checker) checkTypeName(t string) error {
	if strings.HasPrefix(t, "[]") {
		return c.checkTypeName(t[2:])
	}
	if strings.HasPrefix(t, "chan ") {
		return c.checkTypeName(strings.TrimPrefix(t, "chan "))
	}
	if strings.HasPrefix(t, "map[") {
		kt, vt, err := splitMapType(t)
		if err != nil {
			return err
		}
		if err := c.checkTypeName(kt); err != nil {
			return err
		}
		return c.checkTypeName(vt)
	}
	switch t {
	case "int", "float", "bool", "string", "bytes", "func":
		return nil
	}
	if _, ok := c.structs[t]; ok {
		return nil
	}
	return fmt.Errorf("unknown type %q", t)
}

// resolveDeferred ties off deferred x[i] index and struct field uses. Each is
// resolved once its base expression has a concrete kind; resolving one may give
// another its type (e.g. m[k].field), so this runs to a fixpoint, re-solving
// after each round.
func (c *Checker) resolveDeferred() error {
	fieldDone := make([]bool, len(c.fieldUses))
	indexDone := make([]bool, len(c.indexUses))
	rangeDone := make([]bool, len(c.rangeUses))
	for {
		progressed := false
		for i, iu := range c.indexUses {
			if indexDone[i] {
				continue
			}
			ok, err := c.tryIndex(iu)
			if err != nil {
				return err
			}
			if ok {
				indexDone[i] = true
				progressed = true
			}
		}
		for i, ru := range c.rangeUses {
			if rangeDone[i] {
				continue
			}
			ok, err := c.tryRange(ru)
			if err != nil {
				return err
			}
			if ok {
				rangeDone[i] = true
				progressed = true
			}
		}
		for i, fu := range c.fieldUses {
			if fieldDone[i] || c.kindOf(fu.base) != KStruct {
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
			fieldDone[i] = true
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
		if !fieldDone[i] {
			return fmt.Errorf("cannot infer struct type for field .%s", fu.field)
		}
	}
	for i := range c.indexUses {
		if !indexDone[i] {
			return fmt.Errorf("cannot index a non-slice/map value")
		}
	}
	for i := range c.rangeUses {
		if !rangeDone[i] {
			return fmt.Errorf("cannot range over this value (need a slice, map, string, or channel)")
		}
	}
	return nil
}

// tryRange resolves one for-range loop once its base kind is known.
func (c *Checker) tryRange(ru rangeUse) (bool, error) {
	switch c.kindOf(ru.base) {
	case KSlice:
		if _, err := c.union(ru.key, c.cInt); err != nil {
			return false, err
		}
		if ru.hasVal {
			e, err := c.sliceElem(ru.base)
			if err != nil {
				return false, err
			}
			if _, err := c.union(ru.val, e); err != nil {
				return false, err
			}
		}
		return true, nil
	case KString:
		if _, err := c.union(ru.key, c.cInt); err != nil {
			return false, err
		}
		if ru.hasVal {
			if _, err := c.union(ru.val, c.cString); err != nil {
				return false, err
			}
		}
		return true, nil
	case KMap:
		ks, vs, err := c.mapKV(ru.base)
		if err != nil {
			return false, err
		}
		if _, err := c.union(ru.key, ks); err != nil {
			return false, err
		}
		if ru.hasVal {
			if _, err := c.union(ru.val, vs); err != nil {
				return false, err
			}
		}
		return true, nil
	case KChan:
		// for v := range ch — the single variable is the received element; the
		// loop ends when the channel is closed and drained.
		if ru.hasVal {
			return false, fmt.Errorf("range over a channel takes a single variable")
		}
		e, err := c.chanElem(ru.base)
		if err != nil {
			return false, err
		}
		if _, err := c.union(ru.key, e); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// tryIndex resolves one x[i]: slice → (idx int, result elem); map → (idx key,
// result value). Returns false if the base kind is not yet known.
func (c *Checker) tryIndex(iu indexUse) (bool, error) {
	switch c.kindOf(iu.base) {
	case KSlice:
		e, err := c.sliceElem(iu.base)
		if err != nil {
			return false, err
		}
		if _, err := c.union(iu.idx, c.cInt); err != nil {
			return false, err
		}
		if _, err := c.union(iu.result, e); err != nil {
			return false, err
		}
		return true, nil
	case KMap:
		ks, vs, err := c.mapKV(iu.base)
		if err != nil {
			return false, err
		}
		if _, err := c.union(iu.idx, ks); err != nil {
			return false, err
		}
		if _, err := c.union(iu.result, vs); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
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
			// `=` to a name that is a package global assigns the global (a local is
			// only created by `:=`, which may also shadow a global).
			if gs, isG := c.globalSlot[st.Name]; isG && st.Op == "=" {
				c.addPair(gs, vs)
				return nil
			}
			slot = newSlot(c, KVar)
			env[st.Name] = slot
			c.localOrder[fn.Name] = append(c.localOrder[fn.Name], st.Name)
		}
		c.addPair(slot, vs)
		return nil
	case *ReturnStmt:
		rets := c.funcRets[fn.Name]
		if len(st.Vals) == 0 {
			// a bare return is fine for a void function or one with named returns
			if len(rets) != 0 && len(fn.Returns) == 0 {
				return fmt.Errorf("%s: bare return in a function that returns %d values", fn.Name, len(rets))
			}
			return nil
		}
		if len(st.Vals) != len(rets) {
			return fmt.Errorf("%s: returns %d values but this return has %d", fn.Name, len(rets), len(st.Vals))
		}
		for i, v := range st.Vals {
			vs, err := c.genExpr(fn, v)
			if err != nil {
				return err
			}
			c.addPair(rets[i], vs)
		}
		return nil
	case *BreakStmt:
		return nil
	case *ContinueStmt:
		return nil
	case *MultiAssign:
		return c.genMultiAssign(fn, st)
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
		is, err := c.genExpr(fn, st.Target.Idx)
		if err != nil {
			return err
		}
		vs, err := c.genExpr(fn, st.Val)
		if err != nil {
			return err
		}
		c.indexUses = append(c.indexUses, indexUse{base: xs, idx: is, result: vs})
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
	case *SelectStmt:
		env := c.vars[fn.Name]
		for i := range st.Cases {
			sc := &st.Cases[i]
			if sc.RecvCh != nil {
				cs, err := c.genExpr(fn, sc.RecvCh)
				if err != nil {
					return err
				}
				eslot, err := c.chanElem(cs)
				if err != nil {
					return err
				}
				if sc.Name != "" && sc.Name != "_" {
					slot, ok := env[sc.Name]
					if !ok {
						slot = newSlot(c, KVar)
						env[sc.Name] = slot
						c.localOrder[fn.Name] = append(c.localOrder[fn.Name], sc.Name)
					}
					c.addPair(slot, eslot)
				}
				if sc.OkName != "" && sc.OkName != "_" {
					slot, ok := env[sc.OkName]
					if !ok {
						slot = newSlot(c, KVar)
						env[sc.OkName] = slot
						c.localOrder[fn.Name] = append(c.localOrder[fn.Name], sc.OkName)
					}
					c.addPair(slot, c.cBool)
				}
			} else {
				cs, err := c.genExpr(fn, sc.SendCh)
				if err != nil {
					return err
				}
				eslot, err := c.chanElem(cs)
				if err != nil {
					return err
				}
				vs, err := c.genExpr(fn, sc.SendVal)
				if err != nil {
					return err
				}
				c.addPair(vs, eslot)
			}
			for _, s := range sc.Body {
				if err := c.genStmt(fn, s); err != nil {
					return err
				}
			}
		}
		for _, s := range st.Default {
			if err := c.genStmt(fn, s); err != nil {
				return err
			}
		}
		return nil
	case *RangeStmt:
		xs, err := c.genExpr(fn, st.X)
		if err != nil {
			return err
		}
		env := c.vars[fn.Name]
		bind := func(name string) int {
			if name == "" || name == "_" {
				return newSlot(c, KVar) // throwaway, not bound to a name
			}
			if slot, ok := env[name]; ok {
				return slot // reuse an existing local (flat function scope)
			}
			slot := newSlot(c, KVar)
			env[name] = slot
			c.localOrder[fn.Name] = append(c.localOrder[fn.Name], name)
			return slot
		}
		keySlot := bind(st.Key)
		valSlot := bind(st.Val)
		c.rangeUses = append(c.rangeUses, rangeUse{base: xs, key: keySlot, val: valSlot, hasVal: st.Val != ""})
		for _, s := range st.Body {
			if err := c.genStmt(fn, s); err != nil {
				return err
			}
		}
		return nil
	case *GoStmt:
		if _, ok := c.funcs[st.Call.Callee]; !ok {
			return fmt.Errorf("go: %q is not a user function", st.Call.Callee)
		}
		_, err := c.genExpr(fn, st.Call)
		return err
	case *ArenaStmt:
		// flat function scope: the body's statements type-check as usual; only
		// allocation lifetime differs at runtime.
		for _, s := range st.Body {
			if err := c.genStmt(fn, s); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("typecheck: unknown statement %T", s)
}

func (c *Checker) genExpr(fn *FuncDecl, e Expr) (int, error) {
	ns := c.nodeSlot[fn.Name]
	if s, ok := ns[e]; ok {
		return s, nil
	}
	slot, err := c.genExprInner(fn, e)
	if err != nil {
		return 0, err
	}
	ns[e] = slot
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
		if s, ok := c.globalSlot[ex.Name]; ok { // a package global, in scope everywhere
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
		if ex.Op == "^" {              // bitwise complement: int-only
			c.addPair(xs, c.cInt)
			return xs, nil
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
		is, err := c.genExpr(fn, ex.Idx)
		if err != nil {
			return 0, err
		}
		res := newSlot(c, KVar)
		c.indexUses = append(c.indexUses, indexUse{base: xs, idx: is, result: res})
		return res, nil
	case *MakeMap:
		ks, err := c.typeSlot(ex.Key)
		if err != nil {
			return 0, err
		}
		vs, err := c.typeSlot(ex.Val)
		if err != nil {
			return 0, err
		}
		return newMapSlot(c, ks, vs), nil
	case *MakeClosure:
		inst, err := c.instantiate(ex.FuncName)
		if err != nil {
			return 0, err
		}
		params := c.funcParam[inst]
		nc := len(ex.Captures)
		env := c.vars[fn.Name]
		for i, capName := range ex.Captures {
			capSlot, ok := env[capName]
			if !ok {
				return 0, fmt.Errorf("closure capture %q is not in scope", capName)
			}
			c.addPair(params[i], capSlot)
		}
		ret := c.cVoid
		switch rets := c.funcRets[inst]; len(rets) {
		case 0:
			ret = c.cVoid
		case 1:
			ret = rets[0]
		default:
			return 0, fmt.Errorf("a function value cannot return multiple values")
		}
		c.closureInst[fn.Name][ex] = inst
		return newFuncSlot(c, &funcSig{params: params[nc:], ret: ret}), nil
	case *CallValue:
		fs, err := c.genExpr(fn, ex.Fn)
		if err != nil {
			return 0, err
		}
		sig, err := c.funcOf(fs, len(ex.Args))
		if err != nil {
			return 0, err
		}
		for i, a := range ex.Args {
			as, err := c.genExpr(fn, a)
			if err != nil {
				return 0, err
			}
			c.addPair(sig.params[i], as)
		}
		return sig.ret, nil
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
	case "%", "&", "|", "^", "<<", ">>":
		// integer-only operators (C's %, &, |, ^, <<, >>); reject non-int operands
		// at type-check time so the user gets a clean MFL error, not leaked cc output.
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

// multiRetBuiltin reports the argument and return type slots for builtins that
// return multiple values via the Go-style `v, err := f()` idiom (err == "" means
// success). This is how error handling reaches the builtin layer: a call like
// http_get gives back (status, body, err) instead of collapsing failure to "".
func (c *Checker) multiRetBuiltin(name string) (args []int, rets []int, ok bool) {
	switch name {
	case "http_get":
		return []int{c.cString}, []int{c.cInt, c.cString, c.cString}, true
	case "http_request":
		return []int{c.cString, c.cString, newSliceSlot(c, c.cString), c.cString}, []int{c.cInt, c.cString, c.cString}, true
	case "json_get":
		return []int{c.cString, c.cString}, []int{c.cString, c.cString}, true
	}
	return nil, nil, false
}

func (c *Checker) genMultiAssign(fn *FuncDecl, st *MultiAssign) error {
	env := c.vars[fn.Name]
	nameSlots := make([]int, len(st.Names))
	for i, n := range st.Names {
		if n == "_" {
			nameSlots[i] = newSlot(c, KVar)
			continue
		}
		slot, ok := env[n]
		if !ok {
			if st.Op == "=" {
				return fmt.Errorf("assignment to undefined variable %q", n)
			}
			slot = newSlot(c, KVar)
			env[n] = slot
			c.localOrder[fn.Name] = append(c.localOrder[fn.Name], n)
		}
		nameSlots[i] = slot
	}

	if len(st.Rhs) == 1 {
		// comma-ok receive: v, ok := <-ch
		if recv, ok := st.Rhs[0].(*Recv); ok {
			if len(st.Names) != 2 {
				return fmt.Errorf("comma-ok receive needs exactly 2 variables: v, ok := <-ch")
			}
			cs, err := c.genExpr(fn, recv.Ch)
			if err != nil {
				return err
			}
			eslot, err := c.chanElem(cs)
			if err != nil {
				return err
			}
			c.addPair(nameSlots[0], eslot)
			c.addPair(nameSlots[1], c.cBool)
			return nil
		}
		// a multi-return builtin (e.g. http_get -> status, body, err)
		if call, ok := st.Rhs[0].(*Call); ok {
			if aks, rks, isMRB := c.multiRetBuiltin(call.Callee); isMRB {
				if len(call.Args) != len(aks) {
					return fmt.Errorf("%s: expected %d args, got %d", call.Callee, len(aks), len(call.Args))
				}
				for i, a := range call.Args {
					as, err := c.genExpr(fn, a)
					if err != nil {
						return err
					}
					c.addPair(as, aks[i])
				}
				if len(rks) != len(st.Names) {
					return fmt.Errorf("%s returns %d values but %d are assigned", call.Callee, len(rks), len(st.Names))
				}
				for i := range rks {
					c.addPair(nameSlots[i], rks[i])
				}
				return nil
			}
		}
		// a single call returning multiple values destructures across the names
		if call, ok := st.Rhs[0].(*Call); ok {
			if srcFn, isUser := c.funcs[call.Callee]; isUser && funcArity(srcFn) >= 2 {
				argSlots := make([]int, len(call.Args))
				for i, a := range call.Args {
					as, err := c.genExpr(fn, a)
					if err != nil {
						return err
					}
					argSlots[i] = as
				}
				inst, err := c.instantiate(call.Callee)
				if err != nil {
					return err
				}
				params := c.funcParam[inst]
				if len(params) != len(call.Args) {
					return fmt.Errorf("%s: expected %d args, got %d", call.Callee, len(params), len(call.Args))
				}
				for i := range params {
					c.addPair(params[i], argSlots[i])
				}
				c.callInst[fn.Name][call] = inst
				rets := c.funcRets[inst]
				if len(rets) != len(st.Names) {
					return fmt.Errorf("%s returns %d values but %d are assigned", call.Callee, len(rets), len(st.Names))
				}
				for i := range rets {
					c.addPair(nameSlots[i], rets[i])
				}
				return nil
			}
		}
		if len(st.Names) != 1 {
			return fmt.Errorf("%d variables but a single value on the right", len(st.Names))
		}
		vs, err := c.genExpr(fn, st.Rhs[0])
		if err != nil {
			return err
		}
		c.addPair(nameSlots[0], vs)
		return nil
	}

	if len(st.Rhs) != len(st.Names) {
		return fmt.Errorf("%d variables but %d values", len(st.Names), len(st.Rhs))
	}
	for i, e := range st.Rhs {
		vs, err := c.genExpr(fn, e)
		if err != nil {
			return err
		}
		c.addPair(nameSlots[i], vs)
	}
	return nil
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

// ffiSlot maps an FFI type name to its checker slot: a scalar's shared slot, a
// fresh struct slot for a cstruct name, or void for "".
func (c *Checker) ffiSlot(t string) int {
	if strings.HasPrefix(t, "*") {
		return c.cInt // *Name: the MFL arg is a pointer held as an int
	}
	if strings.HasSuffix(t, "*") {
		return newStructSlot(c, strings.TrimSuffix(t, "*")) // Name*: the arg is a cstruct value (inout)
	}
	switch t {
	case "":
		return c.cVoid
	case "float", "f32", "f64":
		return c.cFloat
	case "bool":
		return c.cBool
	case "string":
		return c.cString
	}
	if isFFIScalar(t) {
		return c.cInt // every integer width is MFL int
	}
	return newStructSlot(c, t) // a cstruct value
}

// ExternFn returns the foreign-function signature for name, or nil.
func (c *Checker) ExternFn(name string) *ExternFunc { return c.externs[name] }

func (c *Checker) genCall(fn *FuncDecl, ex *Call) (int, error) {
	argSlots := make([]int, len(ex.Args))
	for i, a := range ex.Args {
		s, err := c.genExpr(fn, a)
		if err != nil {
			return 0, err
		}
		argSlots[i] = s
	}
	// An explicit extern declaration shadows a builtin of the same name, so a
	// program that declares e.g. `fn sqrt(float) float` gets its extern rather
	// than the math builtin.
	if ef, ok := c.externs[ex.Callee]; ok {
		if len(argSlots) != len(ef.Params) {
			return 0, fmt.Errorf("%s: expected %d args, got %d", ef.Name, len(ef.Params), len(argSlots))
		}
		for i, pt := range ef.Params {
			c.addPair(argSlots[i], c.ffiSlot(pt))
		}
		return c.ffiSlot(ef.Ret), nil
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
	case "bytes":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("bytes: 1 arg (string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cBytes, nil
	case "bytes_str":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("bytes_str: 1 arg (bytes)")
		}
		c.addPair(argSlots[0], c.cBytes)
		return c.cString, nil
	case "to_hex":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("to_hex: 1 arg (bytes)")
		}
		c.addPair(argSlots[0], c.cBytes)
		return c.cString, nil
	case "from_hex":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("from_hex: 1 arg (string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cBytes, nil
	case "byte_at":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("byte_at: 2 args (bytes, index)")
		}
		c.addPair(argSlots[0], c.cBytes)
		c.addPair(argSlots[1], c.cInt)
		return c.cInt, nil
	case "bytes_sub":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("bytes_sub: 3 args (bytes, start, end)")
		}
		c.addPair(argSlots[0], c.cBytes)
		c.addPair(argSlots[1], c.cInt)
		c.addPair(argSlots[2], c.cInt)
		return c.cBytes, nil
	case "bytes_concat":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("bytes_concat: 2 args (bytes, bytes)")
		}
		c.addPair(argSlots[0], c.cBytes)
		c.addPair(argSlots[1], c.cBytes)
		return c.cBytes, nil
	case "rand_bytes":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("rand_bytes: 1 arg (count)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cBytes, nil
	case "sha256_bytes", "x25519_pub", "ed25519_pub":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("%s: 1 arg (bytes)", ex.Callee)
		}
		c.addPair(argSlots[0], c.cBytes)
		return c.cBytes, nil
	case "hmac_sha256_bytes", "x25519_shared", "ed25519_sign":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("%s: 2 args (bytes, bytes)", ex.Callee)
		}
		c.addPair(argSlots[0], c.cBytes)
		c.addPair(argSlots[1], c.cBytes)
		return c.cBytes, nil
	case "ed25519_verify":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("ed25519_verify: 3 args (pub, msg, sig — all bytes)")
		}
		for _, sl := range argSlots {
			c.addPair(sl, c.cBytes)
		}
		return c.cBool, nil
	case "aes_cbc_encrypt", "aes_cbc_decrypt":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("%s: 3 args (key, iv, data — all bytes)", ex.Callee)
		}
		for _, sl := range argSlots {
			c.addPair(sl, c.cBytes)
		}
		return c.cBytes, nil
	case "aes_gcm_encrypt", "aes_gcm_decrypt":
		if len(argSlots) != 4 {
			return 0, fmt.Errorf("%s: 4 args (key, iv, data, aad — all bytes)", ex.Callee)
		}
		for _, sl := range argSlots {
			c.addPair(sl, c.cBytes)
		}
		return c.cBytes, nil
	case "xeddsa_sign":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("xeddsa_sign: 3 args (priv32, msg, random64 — all bytes)")
		}
		for _, sl := range argSlots {
			c.addPair(sl, c.cBytes)
		}
		return c.cBytes, nil
	case "xeddsa_verify":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("xeddsa_verify: 3 args (pub32, msg, sig64 — all bytes)")
		}
		for _, sl := range argSlots {
			c.addPair(sl, c.cBytes)
		}
		return c.cBool, nil
	case "hkdf_sha256":
		if len(argSlots) != 4 {
			return 0, fmt.Errorf("hkdf_sha256: 4 args (ikm, salt, info — bytes — and length int)")
		}
		c.addPair(argSlots[0], c.cBytes)
		c.addPair(argSlots[1], c.cBytes)
		c.addPair(argSlots[2], c.cBytes)
		c.addPair(argSlots[3], c.cInt)
		return c.cBytes, nil
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
	case "has", "delete":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("%s: 2 args (map, key)", ex.Callee)
		}
		ks, _, err := c.mapKV(argSlots[0])
		if err != nil {
			return 0, err
		}
		c.addPair(argSlots[1], ks)
		if ex.Callee == "has" {
			return c.cBool, nil
		}
		return c.cVoid, nil
	case "keys":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("keys: 1 arg (map)")
		}
		ks, _, err := c.mapKV(argSlots[0])
		if err != nil {
			return 0, err
		}
		return newSliceSlot(c, ks), nil
	case "json":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("json: 1 arg")
		}
		// any value serializes; codegen reads the resolved type
		return c.cString, nil
	case "parse":
		// parse(jsonString, witness) -> a value of the witness's type
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("parse: 2 args (json string, type witness)")
		}
		c.addPair(argSlots[0], c.cString)
		return argSlots[1], nil
	case "http_body":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("http_body: 1 arg")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cString, nil
	case "to_upper", "to_lower", "trim":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("%s: 1 arg", ex.Callee)
		}
		c.addPair(argSlots[0], c.cString)
		return c.cString, nil
	case "contains", "has_prefix", "has_suffix":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("%s: 2 args", ex.Callee)
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return c.cBool, nil
	case "index":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("index: 2 args")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return c.cInt, nil
	case "substr":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("substr: 3 args (string, start, end)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cInt)
		c.addPair(argSlots[2], c.cInt)
		return c.cString, nil
	case "charat":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("charat: 2 args (string, index)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cInt)
		return c.cString, nil
	case "replace":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("replace: 3 args (string, old, new)")
		}
		for _, s := range argSlots {
			c.addPair(s, c.cString)
		}
		return c.cString, nil
	case "split":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("split: 2 args (string, sep)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return newSliceSlot(c, c.cString), nil
	case "join":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("join: 2 args ([]string, sep)")
		}
		c.addPair(argSlots[0], newSliceSlot(c, c.cString))
		c.addPair(argSlots[1], c.cString)
		return c.cString, nil
	case "str":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("str: 1 arg")
		}
		// str accepts a number, bool, or string; the exact emission is picked by
		// the arg's resolved kind at codegen. Validated after solve (strArgs) so
		// we neither force the arg numeric (which rejected bool) nor lose safety.
		c.strArgs = append(c.strArgs, argSlots[0])
		return c.cString, nil
	case "int":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("int: 1 arg")
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		return c.cInt, nil
	case "float":
		// int -> float (and identity on float). The counterpart to int(): MFL
		// has no implicit int->float, so a concrete int (a function return,
		// byte_at, len, ...) needs this to enter float arithmetic.
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("float: 1 arg")
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		return c.cFloat, nil
	case "f64_bits":
		// reinterpret a double's IEEE-754 bits as an int64 (for byte serialization)
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("f64_bits: 1 arg (float)")
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		return c.cInt, nil
	case "f64_from_bits":
		// the inverse: int64 bit pattern -> double
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("f64_from_bits: 1 arg (int)")
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		return c.cFloat, nil
	case "sin", "cos", "tan", "asin", "acos", "atan", "exp", "log", "log2", "log10", "sqrt", "cbrt", "floor", "ceil", "round", "trunc", "abs":
		// native math (libm): numeric in, float out
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("%s: 1 arg", ex.Callee)
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		return c.cFloat, nil
	case "pow", "atan2", "fmod", "hypot":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("%s: 2 args", ex.Callee)
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		c.addPair(argSlots[1], newSlot(c, KNum))
		return c.cFloat, nil
	case "pi":
		if len(argSlots) != 0 {
			return 0, fmt.Errorf("pi: no args")
		}
		return c.cFloat, nil
	case "noise2":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("noise2: 2 args (x, y)")
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		c.addPair(argSlots[1], newSlot(c, KNum))
		return c.cFloat, nil
	case "noise3":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("noise3: 3 args (x, y, z)")
		}
		c.addPair(argSlots[0], newSlot(c, KNum))
		c.addPair(argSlots[1], newSlot(c, KNum))
		c.addPair(argSlots[2], newSlot(c, KNum))
		return c.cFloat, nil
	// raw heap memory: pointers are ints. For building C buffers/structs to hand
	// to a foreign API (e.g. a GPU vertex buffer via the FFI).
	case "alloc":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("alloc: 1 arg (byte count)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cInt, nil
	case "free":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("free: 1 arg (pointer)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cVoid, nil
	case "poke_f32":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("poke_f32: 3 args (ptr, byte offset, value)")
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cInt)
		c.addPair(argSlots[2], newSlot(c, KNum))
		return c.cVoid, nil
	case "poke_i32", "poke_u8", "poke_u16", "poke_ptr":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("%s: 3 args (ptr, byte offset, value)", ex.Callee)
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cInt)
		c.addPair(argSlots[2], c.cInt)
		return c.cVoid, nil
	case "peek_f32":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("peek_f32: 2 args (ptr, byte offset)")
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cInt)
		return c.cFloat, nil
	case "peek_i32":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("peek_i32: 2 args (ptr, byte offset)")
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cInt)
		return c.cInt, nil
	case "ptr_str":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("ptr_str: 1 arg (pointer to NUL-terminated bytes)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cString, nil
	case "listen", "accept":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("%s: 1 arg", ex.Callee)
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cInt, nil
	case "dial":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("dial: 2 args (host string, port int)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cInt)
		return c.cInt, nil
	case "read":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("read: 1 arg")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cString, nil
	case "read_bytes":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("read_bytes: 1 arg (fd)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cBytes, nil
	case "input":
		if len(argSlots) != 0 {
			return 0, fmt.Errorf("input: no args")
		}
		return c.cString, nil
	case "raw_mode":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("raw_mode: 1 arg (on: 1 enable, 0 restore)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cInt, nil
	case "read_key":
		if len(argSlots) != 0 {
			return 0, fmt.Errorf("read_key: no args")
		}
		return c.cString, nil
	case "read_stdin":
		if len(argSlots) != 0 {
			return 0, fmt.Errorf("read_stdin: no args")
		}
		return c.cString, nil
	case "args":
		if len(argSlots) != 0 {
			return 0, fmt.Errorf("args: no args")
		}
		return newSliceSlot(c, c.cString), nil
	case "env":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("env: 1 arg (variable name)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cString, nil
	case "now":
		if len(argSlots) != 0 {
			return 0, fmt.Errorf("now: no args")
		}
		return c.cInt, nil
	case "now_ms":
		if len(argSlots) != 0 {
			return 0, fmt.Errorf("now_ms: no args")
		}
		return c.cInt, nil
	case "time_fields":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("time_fields: 1 arg (unix seconds)")
		}
		c.addPair(argSlots[0], c.cInt)
		return newSliceSlot(c, c.cInt), nil
	case "time_format", "time_format_utc":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("%s: 2 args (unix seconds, strftime pattern)", ex.Callee)
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cString)
		return c.cString, nil
	case "time_make":
		if len(argSlots) != 6 {
			return 0, fmt.Errorf("time_make: 6 args (year, month, day, hour, minute, second)")
		}
		for _, sl := range argSlots {
			c.addPair(sl, c.cInt)
		}
		return c.cInt, nil
	case "parse_int":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("parse_int: 1 arg (string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cInt, nil
	case "parse_float":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("parse_float: 1 arg (string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cFloat, nil
	case "read_file":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("read_file: 1 arg (path)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cString, nil
	case "read_file_bytes":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("read_file_bytes: 1 arg (path)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cBytes, nil
	case "write_bytes":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("write_bytes: 2 args (fd, bytes)")
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cBytes)
		return c.cInt, nil
	case "write_file":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("write_file: 2 args (path, content)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return c.cInt, nil
	case "list_dir":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("list_dir: 1 arg (path)")
		}
		c.addPair(argSlots[0], c.cString)
		return newSliceSlot(c, c.cString), nil
	case "mkdir":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("mkdir: 1 arg (path)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cInt, nil
	case "system":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("system: 1 arg (command string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cInt, nil
	case "https_get":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("https_get: 1 arg (url string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cString, nil
	case "https_post":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("https_post: 2 args (url string, body string)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return c.cString, nil
	case "exit":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("exit: 1 arg (status code int)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cInt, nil
	case "flush":
		if len(argSlots) != 0 {
			return 0, fmt.Errorf("flush: no args")
		}
		return c.cInt, nil
	case "base64_encode", "base64_decode", "url_encode", "url_decode", "sha256":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("%s: 1 arg (string)", ex.Callee)
		}
		c.addPair(argSlots[0], c.cString)
		return c.cString, nil
	case "base64_encode_bytes":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("base64_encode_bytes: 1 arg (bytes)")
		}
		c.addPair(argSlots[0], c.cBytes)
		return c.cString, nil
	case "base64_decode_bytes":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("base64_decode_bytes: 1 arg (string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cBytes, nil
	case "hmac_sha256":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("hmac_sha256: 2 args (key, message)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return c.cString, nil
	case "sqlite_open":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("sqlite_open: 1 arg (path string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cInt, nil
	case "sqlite_exec", "sqlite_query":
		if len(argSlots) != 2 && len(argSlots) != 3 {
			return 0, fmt.Errorf("%s: 2 or 3 args (db int, sql string[, params []string])", ex.Callee)
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cString)
		if len(argSlots) == 3 {
			c.addPair(argSlots[2], newSliceSlot(c, c.cString))
		}
		if ex.Callee == "sqlite_query" {
			return c.cString, nil
		}
		return c.cInt, nil
	case "sqlite_close":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("sqlite_close: 1 arg (db int)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cInt, nil
	case "regex_match":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("regex_match: 2 args (s, pattern)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return c.cBool, nil
	case "regex_find":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("regex_find: 2 args (s, pattern)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return c.cString, nil
	case "regex_replace":
		if len(argSlots) != 3 {
			return 0, fmt.Errorf("regex_replace: 3 args (s, pattern, repl)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		c.addPair(argSlots[2], c.cString)
		return c.cString, nil
	case "regex_groups":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("regex_groups: 2 args (s, pattern)")
		}
		c.addPair(argSlots[0], c.cString)
		c.addPair(argSlots[1], c.cString)
		return newSliceSlot(c, c.cString), nil
	case "http_get":
		return 0, fmt.Errorf("http_get returns 3 values; use: status, body, err := http_get(url)")
	case "http_request":
		return 0, fmt.Errorf("http_request returns 3 values; use: status, body, err := http_request(method, url, headers, body)")
	case "json_get":
		return 0, fmt.Errorf("json_get returns 2 values; use: value, err := json_get(json, path)")
	case "wss_open":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("wss_open: 1 arg (url string)")
		}
		c.addPair(argSlots[0], c.cString)
		return c.cInt, nil
	case "wss_send":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("wss_send: 2 args (conn int, msg string)")
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cString)
		return c.cInt, nil
	case "wss_recv":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("wss_recv: 1 arg (conn int)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cString, nil
	case "wss_send_bin":
		if len(argSlots) != 2 {
			return 0, fmt.Errorf("wss_send_bin: 2 args (conn int, payload bytes)")
		}
		c.addPair(argSlots[0], c.cInt)
		c.addPair(argSlots[1], c.cBytes)
		return c.cInt, nil
	case "wss_recv_bin":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("wss_recv_bin: 1 arg (conn int)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cBytes, nil
	case "wss_close":
		if len(argSlots) != 1 {
			return 0, fmt.Errorf("wss_close: 1 arg (conn int)")
		}
		c.addPair(argSlots[0], c.cInt)
		return c.cInt, nil
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
		// arg is an fd (int) or a channel — codegen dispatches on its kind. Don't
		// pin it to int, so close(ch) type-checks.
		return c.cVoid, nil
	}
	if _, isFunc := c.funcs[ex.Callee]; !isFunc {
		// not a top-level function — maybe a function-valued local variable
		if slot, isVar := c.vars[fn.Name][ex.Callee]; isVar {
			sig, err := c.funcOf(slot, len(argSlots))
			if err != nil {
				return 0, err
			}
			for i := range argSlots {
				c.addPair(sig.params[i], argSlots[i])
			}
			return sig.ret, nil
		}
		return 0, fmt.Errorf("%s: call to undefined function %q", fn.Name, ex.Callee)
	}
	// instantiate a fresh specialization of the callee for this call site
	inst, err := c.instantiate(ex.Callee)
	if err != nil {
		return 0, err
	}
	params := c.funcParam[inst]
	c.callInst[fn.Name][ex] = inst
	if c.funcs[ex.Callee].Variadic {
		nfixed := len(params) - 1
		vparam := params[nfixed] // the variadic parameter is a slice
		if ex.Spread {
			if len(argSlots) != nfixed+1 {
				return 0, fmt.Errorf("%s: expected %d args before the spread, got %d", ex.Callee, nfixed, len(argSlots)-1)
			}
			for i := 0; i < nfixed; i++ {
				c.addPair(params[i], argSlots[i])
			}
			c.addPair(vparam, argSlots[nfixed]) // spread slice IS the variadic slice
		} else {
			if len(argSlots) < nfixed {
				return 0, fmt.Errorf("%s: expected at least %d args, got %d", ex.Callee, nfixed, len(argSlots))
			}
			for i := 0; i < nfixed; i++ {
				c.addPair(params[i], argSlots[i])
			}
			elem, err := c.sliceElem(vparam)
			if err != nil {
				return 0, err
			}
			for i := nfixed; i < len(argSlots); i++ {
				c.addPair(argSlots[i], elem)
			}
		}
	} else {
		if len(params) != len(argSlots) {
			return 0, fmt.Errorf("%s: expected %d args, got %d", ex.Callee, len(params), len(argSlots))
		}
		for i := range params {
			c.addPair(params[i], argSlots[i])
		}
	}
	rets := c.funcRets[inst]
	switch len(rets) {
	case 0:
		return c.cVoid, nil
	case 1:
		return rets[0], nil
	default:
		return 0, fmt.Errorf("%s returns %d values; use a multi-assignment (a, b := %s(...))", ex.Callee, len(rets), ex.Callee)
	}
}

// ---- queries used by codegen ----

func (c *Checker) RetArity(fn string) int { return len(c.funcRets[fn]) }

// RetNames returns the named return identifiers of an instance (empty if none).
func (c *Checker) RetNames(inst string) []string { return c.instFn[inst].Returns }

// ParamElemCType returns the element C type of a slice-typed parameter.
func (c *Checker) ParamElemCType(fn string, i int) string {
	r := c.find(c.funcParam[fn][i])
	if c.kind[r] == KSlice && c.elem[r] >= 0 {
		return c.ctypeSlot(c.elem[r])
	}
	return "int64_t"
}
func (c *Checker) RetKindAt(fn string, i int) Kind { return c.kindOf(c.funcRets[fn][i]) }
func (c *Checker) ParamKind(fn string, i int) Kind {
	return c.kindOf(c.funcParam[fn][i])
}
func (c *Checker) VarKind(fn, name string) Kind { return c.kindOf(c.vars[fn][name]) }
func (c *Checker) Locals(fn string) []string    { return c.localOrder[fn] }

// slotOf returns the slot an expression node was assigned in a given instance.
func (c *Checker) slotOf(inst string, n Node) int { return c.nodeSlot[inst][n] }

func (c *Checker) NodeKind(inst string, n Node) Kind { return c.kindOf(c.slotOf(inst, n)) }

// ElemKindOf returns the element kind of a slice- or channel-typed node.
func (c *Checker) ElemKindOf(inst string, n Node) Kind {
	r := c.find(c.slotOf(inst, n))
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
	case KBytes:
		return "mfl_bytes"
	case KVoid:
		return "void"
	case KSlice:
		return "mfl_slice"
	case KStruct:
		return "mfl_" + c.sname[r]
	case KChan:
		return "mfl_chan*"
	case KMap:
		return "mfl_map*"
	case KFunc:
		return "mfl_closure"
	}
	return "int64_t"
}

func (c *Checker) RetCTypeAt(fn string, i int) string { return c.ctypeSlot(c.funcRets[fn][i]) }
func (c *Checker) ParamCType(fn string, i int) string { return c.ctypeSlot(c.funcParam[fn][i]) }
func (c *Checker) VarCType(fn, name string) string  { return c.ctypeSlot(c.vars[fn][name]) }
func (c *Checker) NodeCType(inst string, n Node) string { return c.ctypeSlot(c.slotOf(inst, n)) }

// ElemCType renders the C type of a slice- or channel-node's element.
func (c *Checker) ElemCType(inst string, n Node) string {
	r := c.find(c.slotOf(inst, n))
	if (c.kind[r] == KSlice || c.kind[r] == KChan) && c.elem[r] >= 0 {
		return c.ctypeSlot(c.elem[r])
	}
	return "int64_t"
}

// ElemTypeString is the MFL type string of a slice/channel element (e.g.
// "string", "int", a struct name) — used to compute channel string offsets.
func (c *Checker) ElemTypeString(inst string, n Node) string {
	r := c.find(c.slotOf(inst, n))
	if (c.kind[r] == KSlice || c.kind[r] == KChan) && c.elem[r] >= 0 {
		return c.typeStringSlot(c.elem[r])
	}
	return "int"
}

// Types returns the declared struct types (codegen emits a C typedef per type).
func (c *Checker) StructTypes() map[string]*TypeDecl { return c.structs }

// IsTopFunc reports whether name is a top-level function (vs a closure value).
func (c *Checker) IsTopFunc(name string) bool { _, ok := c.funcs[name]; return ok }

// ---- monomorphization queries for codegen ----

// Reps returns the representative instances — one C function is emitted per one.
func (c *Checker) Reps() []string { return c.reps }

// CName is an instance's C function name.
func (c *Checker) CName(inst string) string { return c.cnameOf[inst] }

// SrcFunc returns the source function an instance specializes.
func (c *Checker) SrcFunc(inst string) *FuncDecl { return c.instFn[inst] }

// CalleeInst is the callee instance for a call node inside an enclosing instance.
func (c *Checker) CalleeInst(encl string, call *Call) string {
	return c.callInst[encl][call]
}

// CalleeCName is the C name to call for a call node inside an enclosing instance.
func (c *Checker) CalleeCName(encl string, call *Call) string {
	return c.cnameOf[c.callInst[encl][call]]
}

// ClosureCName is the C name of the lambda a MakeClosure builds, in context.
func (c *Checker) ClosureCName(encl string, mc *MakeClosure) string {
	return c.cnameOf[c.closureInst[encl][mc]]
}

// ClosureInst returns the lambda instance a MakeClosure builds, in context.
func (c *Checker) ClosureInst(encl string, mc *MakeClosure) string {
	return c.closureInst[encl][mc]
}

// sigCTypes returns the parameter and return C types of a function-valued slot.
func (c *Checker) sigCTypes(slot int) ([]string, string) {
	r := c.find(slot)
	if c.kind[r] == KFunc && c.fsig[r] != nil {
		sig := c.fsig[r]
		ps := make([]string, len(sig.params))
		for i, p := range sig.params {
			ps[i] = c.ctypeSlot(p)
		}
		return ps, c.ctypeSlot(sig.ret)
	}
	return nil, "int64_t"
}

func (c *Checker) NodeFuncSig(inst string, n Node) ([]string, string) { return c.sigCTypes(c.slotOf(inst, n)) }
func (c *Checker) VarFuncSig(fn, name string) ([]string, string)      { return c.sigCTypes(c.vars[fn][name]) }

// TypeString renders a node's resolved type as a canonical string (int, float,
// bool, string, a struct name, []T, map[K]V) — used to key JSON serializers.
func (c *Checker) TypeString(inst string, n Node) string { return c.typeStringSlot(c.slotOf(inst, n)) }

func (c *Checker) typeStringSlot(slot int) string {
	r := c.find(slot)
	switch c.kind[r] {
	case KFloat:
		return "float"
	case KBool:
		return "bool"
	case KString:
		return "string"
	case KStruct:
		return c.sname[r]
	case KSlice:
		return "[]" + c.typeStringSlot(c.elem[r])
	case KChan:
		return "chan " + c.typeStringSlot(c.elem[r])
	case KMap:
		return "map[" + c.typeStringSlot(c.mkey[r]) + "]" + c.typeStringSlot(c.mval[r])
	case KVoid:
		return "void"
	}
	return "int"
}

// map key/value accessors for a map-typed node (used by codegen).
func (c *Checker) MapKeyKind(inst string, n Node) Kind    { return c.mapPart(inst, n, true, true).(Kind) }
func (c *Checker) MapValCType(inst string, n Node) string { return c.mapPart(inst, n, false, false).(string) }
func (c *Checker) MapKeyCType(inst string, n Node) string { return c.mapPart(inst, n, true, false).(string) }

func (c *Checker) mapPart(inst string, n Node, key bool, asKind bool) interface{} {
	r := c.find(c.slotOf(inst, n))
	slot := -1
	if c.kind[r] == KMap {
		if key {
			slot = c.mkey[r]
		} else {
			slot = c.mval[r]
		}
	}
	if slot < 0 {
		if asKind {
			return KInt
		}
		return "int64_t"
	}
	if asKind {
		return c.kindOf(slot)
	}
	return c.ctypeSlot(slot)
}
