package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// cmdUFTest is the Stage-3 (sub-slice 1) oracle: it drives the real type-checker
// union-find engine (newSlot / find / union / reconcile) with a scripted sequence
// of operations and dumps the resulting slot table in a canonical form. The MFL
// port (selfhost/check.src) runs the identical script and must produce byte-for-byte
// the same dump. This isolates the engine from constraint generation.
//
// Script grammar (one op per line, whitespace-separated; '#' comments ignored):
//
//	var|num|int|float|bool|string|void|bytes      -> new slot of that kind
//	slice E            -> new KSlice slot, element = slot E
//	chan  E            -> new KChan slot,  element = slot E
//	struct NAME        -> new KStruct slot, name = NAME
//	map K V            -> new KMap slot, key = slot K, value = slot V
//	func P0,P1,...|R    -> new KFunc slot; params = those slots, ret = slot R ('-' = none)
//	                       (use '-' for an empty param list: func -|R)
//	union A B          -> union(slot A, slot B)
//
// A 'dump' line (or end of script) prints the table. Once any union reports a type
// mismatch, processing stops and the dump is emitted with err=1.
func cmdUFTest(args []string) error {
	var src []byte
	var err error
	if len(args) >= 1 {
		src, err = os.ReadFile(args[0])
		if err != nil {
			return err
		}
	} else {
		src, _ = os.ReadFile("/dev/stdin")
	}
	c := &Checker{}
	failed := false

	parseSlot := func(s string) int {
		if s == "-" || s == "" {
			return -1
		}
		n, _ := strconv.Atoi(s)
		return n
	}

	sc := bufio.NewScanner(strings.NewReader(string(src)))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		op := f[0]
		if failed && op != "dump" {
			continue
		}
		switch op {
		case "var":
			newSlot(c, KVar)
		case "num":
			newSlot(c, KNum)
		case "int":
			newSlot(c, KInt)
		case "float":
			newSlot(c, KFloat)
		case "bool":
			newSlot(c, KBool)
		case "string":
			newSlot(c, KString)
		case "void":
			newSlot(c, KVoid)
		case "bytes":
			newSlot(c, KBytes)
		case "slice":
			s := newSlot(c, KSlice)
			c.elem[s] = parseSlot(f[1])
		case "chan":
			s := newSlot(c, KChan)
			c.elem[s] = parseSlot(f[1])
		case "struct":
			s := newSlot(c, KStruct)
			c.sname[s] = f[1]
		case "map":
			s := newSlot(c, KMap)
			c.mkey[s] = parseSlot(f[1])
			c.mval[s] = parseSlot(f[2])
		case "func":
			var params []int
			ret := -1
			if len(f) >= 2 {
				parts := strings.SplitN(f[1], "|", 2)
				if parts[0] != "-" && parts[0] != "" {
					for _, p := range strings.Split(parts[0], ",") {
						params = append(params, parseSlot(p))
					}
				}
				if len(parts) == 2 {
					ret = parseSlot(parts[1])
				}
			}
			newFuncSlot(c, &funcSig{params: params, ret: ret})
		case "union":
			a, b := parseSlot(f[1]), parseSlot(f[2])
			if _, e := c.union(a, b); e != nil {
				failed = true
			}
		case "dump":
			// dumped at end regardless
		default:
			return fmt.Errorf("unknown op: %q", op)
		}
	}
	os.Stdout.WriteString(dumpUF(c, failed))
	return nil
}

// dumpUF renders the slot table canonically: every slot ref printed as its root.
func dumpUF(c *Checker, failed bool) string {
	var b strings.Builder
	canon := func(s int) int {
		if s < 0 {
			return -1
		}
		return c.find(s)
	}
	for i := 0; i < len(c.parent); i++ {
		r := c.find(i)
		k := c.kind[r]
		fmt.Fprintf(&b, "%d root=%d kind=%s", i, r, k.String())
		switch k {
		case KSlice, KChan:
			fmt.Fprintf(&b, " elem=%d", canon(c.elem[r]))
		case KStruct:
			fmt.Fprintf(&b, " sname=%s", c.sname[r])
		case KMap:
			fmt.Fprintf(&b, " mkey=%d mval=%d", canon(c.mkey[r]), canon(c.mval[r]))
		case KFunc:
			sig := c.fsig[r]
			b.WriteString(" fsig=")
			if sig == nil {
				b.WriteString("-")
			} else {
				parts := make([]string, len(sig.params))
				for j, p := range sig.params {
					parts[j] = strconv.Itoa(canon(p))
				}
				fmt.Fprintf(&b, "%s|%d", strings.Join(parts, ","), canon(sig.ret))
			}
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "err=%d\n", boolToInt(failed))
	return b.String()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
