package main

import "testing"

// Covers parseSelect's case-form branches (discard-receive, comma-ok receive,
// send, default) and its error paths, none of which had direct parser-level
// coverage — the existing select tests in mfl_test.go only exercise the
// comma-ok-receive and plain-receive forms end-to-end.

func TestParseSelectDiscardReceive(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { select { case <-ch: x := 1 } }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	sel, ok := fn.Body[0].(*SelectStmt)
	if !ok {
		t.Fatalf("Body[0] = %T, want *SelectStmt", fn.Body[0])
	}
	if len(sel.Cases) != 1 || sel.Cases[0].Name != "_" || sel.Cases[0].RecvCh == nil {
		t.Errorf("Cases = %#v, want one discard-receive case", sel.Cases)
	}
}

func TestParseSelectSend(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { select { case ch <- 1: x := 1 } }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	sel := fn.Body[0].(*SelectStmt)
	if len(sel.Cases) != 1 || sel.Cases[0].SendCh == nil || sel.Cases[0].SendVal == nil {
		t.Errorf("Cases = %#v, want one send case", sel.Cases)
	}
}

func TestParseSelectDefault(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { select { case <-ch: x := 1  default: y := 2 } }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	sel := fn.Body[0].(*SelectStmt)
	if !sel.HasDefault || len(sel.Default) != 1 {
		t.Errorf("Default = %#v, HasDefault = %v", sel.Default, sel.HasDefault)
	}
}

func TestParseSelectCommaOkReceive(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { select { case v, ok := <-ch: x := 1 } }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	sel := fn.Body[0].(*SelectStmt)
	if len(sel.Cases) != 1 || sel.Cases[0].Name != "v" || sel.Cases[0].OkName != "ok" {
		t.Errorf("Cases = %#v, want v/ok receive case", sel.Cases)
	}
}

func TestParseSelectBadKeyword(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { select { bogus: x := 1 } }`))
	if err == nil {
		t.Fatal("expected error for a select body without case/default")
	}
}

func TestParseSelectMissingOpenBrace(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { select case <-ch: x := 1 } }`))
	if err == nil {
		t.Fatal("expected error for select missing opening brace")
	}
}
