package main

import (
	"fmt"
	"strings"
)

type Builtin func(in *Interp, args []Value) (Value, error)

var builtins = map[string]Builtin{
	"print": func(in *Interp, args []Value) (Value, error) {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = display(a)
		}
		in.out.WriteString(strings.Join(parts, " "))
		return nil, nil
	},
	"println": func(in *Interp, args []Value) (Value, error) {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = display(a)
		}
		in.out.WriteString(strings.Join(parts, " "))
		in.out.WriteByte('\n')
		return nil, nil
	},
	"len": func(in *Interp, args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("len: expected 1 arg")
		}
		s, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("len: expected string")
		}
		return int64(len(s)), nil
	},
	"str": func(in *Interp, args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("str: expected 1 arg")
		}
		return display(args[0]), nil
	},
	"int": func(in *Interp, args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("int: expected 1 arg")
		}
		switch x := args[0].(type) {
		case int64:
			return x, nil
		case float64:
			return int64(x), nil
		}
		return nil, fmt.Errorf("int: cannot convert %T", args[0])
	},
}

func display(v Value) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return fmt.Sprintf("%d", x)
	case float64:
		return fmt.Sprintf("%g", x)
	case string:
		return x
	}
	return fmt.Sprintf("%v", v)
}
