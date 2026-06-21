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
	}
	return "?"
}

func isNumeric(k Kind) bool { return k == KInt || k == KFloat || k == KNum }

type Checker struct {
	parent []int
	kind   []Kind
	elem   []int    // for KSlice/KChan slots: the element slot; -1 otherwise
	sname  []string // for KStruct slots: the struct type name; "" otherwise
	mkey   []int    // for KMap slots: the key slot; -1 otherwise
	mval   []int    // for KMap slots: the value slot; -1 otherwise

	structs map[string]*TypeDecl // declared struct types

	funcs     map[string]*FuncDecl
	funcParam map[string][]int
	funcRets  map[string][]int // one slot per return value (empty = void)

	vars       map[string]map[string]int // func -> var name -> slot
	localOrder map[string][]string       // func -> locals in declaration order
	nodeSlot   map[Node]int

	// shared concrete slots (no inner type vars, safe to share)
	cBool, cString, cVoid, cInt int

	pairs []int      // flattened pairs: pairs[2i], pairs[2i+1]
	plus  []plusCons // overloaded '+' constraints, resolved by fixpoint

	lenArgs   []int      // slots passed to len(); must resolve to string/slice/map
	fieldUses []fieldUse // struct field access/assign, resolved after solve
	indexUses []indexUse // x[i] access/assign, resolved after solve (slice or map)
	rangeUses []rangeUse // for-range loops, resolved after solve
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
	return len(c.parent) - 1
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
		}
	}
	return 0
}

// Check infers types for the program, returning an error on a type clash.
func Check(p *Program) (*Checker, error) {
	c := &Checker{
		funcs:      map[string]*FuncDecl{},
		funcParam:  map[string][]int{},
		funcRets:   map[string][]int{},
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
		rets := make([]int, returnArity(fn.Body))
		for i := range rets {
			rets[i] = newSlot(c, KVar)
		}
		c.funcRets[fn.Name] = rets
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
		if k := c.kindOf(slot); k != KString && k != KSlice && k != KMap {
			return nil, fmt.Errorf("len: argument must be a string, slice, or map, got %s", k)
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
	case "int", "float", "bool", "string":
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
			return fmt.Errorf("cannot range over this value (need a slice, map, or string)")
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
			slot = newSlot(c, KVar)
			env[st.Name] = slot
			c.localOrder[fn.Name] = append(c.localOrder[fn.Name], st.Name)
		}
		c.addPair(slot, vs)
		return nil
	case *ReturnStmt:
		rets := c.funcRets[fn.Name]
		if len(st.Vals) == 0 {
			if len(rets) != 0 {
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
		// a single call returning multiple values destructures across the names
		if call, ok := st.Rhs[0].(*Call); ok {
			if rets, isUser := c.funcRets[call.Callee]; isUser && len(rets) >= 2 {
				if len(rets) != len(st.Names) {
					return fmt.Errorf("%s returns %d values but %d are assigned", call.Callee, len(rets), len(st.Names))
				}
				params := c.funcParam[call.Callee]
				if len(params) != len(call.Args) {
					return fmt.Errorf("%s: expected %d args, got %d", call.Callee, len(params), len(call.Args))
				}
				for i, a := range call.Args {
					as, err := c.genExpr(fn, a)
					if err != nil {
						return err
					}
					c.addPair(params[i], as)
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
	rets := c.funcRets[ex.Callee]
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
func (c *Checker) RetKindAt(fn string, i int) Kind { return c.kindOf(c.funcRets[fn][i]) }
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
	case KMap:
		return "mfl_map*"
	}
	return "int64_t"
}

func (c *Checker) RetCTypeAt(fn string, i int) string { return c.ctypeSlot(c.funcRets[fn][i]) }
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

// TypeString renders a node's resolved type as a canonical string (int, float,
// bool, string, a struct name, []T, map[K]V) — used to key JSON serializers.
func (c *Checker) TypeString(n Node) string { return c.typeStringSlot(c.nodeSlot[n]) }

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
func (c *Checker) MapKeyKind(n Node) Kind  { return c.mapPart(n, true, true).(Kind) }
func (c *Checker) MapValCType(n Node) string { return c.mapPart(n, false, false).(string) }
func (c *Checker) MapKeyCType(n Node) string { return c.mapPart(n, true, false).(string) }

func (c *Checker) mapPart(n Node, key bool, asKind bool) interface{} {
	r := c.find(c.nodeSlot[n])
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
