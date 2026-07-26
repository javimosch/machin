package main

import "testing"

// Regression: a JSON `null` VALUE must be consumed by the value parser, not just
// ignored. Every mfl_js_* parser used to return its zero value WITHOUT advancing
// the cursor when the value wasn't the shape it expected, so `null` left the
// cursor parked on the literal. The enclosing object's field loop then looked for
// its separating comma, found `n`, and broke out — silently dropping every
// REMAINING field of that object (and derailing the enclosing parsers too).
//
// This is a data-loss bug against real APIs: OpenAI/OpenRouter return exactly
// {"content":null,"tool_calls":[...]} on a tool call, so the tool_calls array
// (and the sibling usage block) vanished. mfl_js_null() now consumes the literal
// in every value parser: scalars, strings, slices, maps, and structs.

func TestParseNullDoesNotDropFollowingFields(t *testing.T) {
	got := runProg(t,
		`type T struct { a string  b string  n int }`,
		`func main() {
			// null in the FIRST field must not eat the ones after it
			x := parse("{\"a\":null,\"b\":\"hello\",\"n\":42}", T{})
			println("b=" + x.b)
			println("n=" + str(x.n))
			// a null int/bool position likewise
			y := parse("{\"a\":\"x\",\"n\":null,\"b\":\"tail\"}", T{})
			println("y.a=" + y.a)
			println("y.n=" + str(y.n))
			println("y.b=" + y.b)
		}`)
	want := "b=hello\nn=42\ny.a=x\ny.n=0\ny.b=tail\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// The OpenAI tool-call response shape, verbatim: a null `content` followed by a
// populated `tool_calls` array of nested structs. Before the fix this parsed as
// zero tool calls, so a tool-calling client silently saw "no tools requested".
func TestParseNullContentKeepsToolCallsArray(t *testing.T) {
	got := runProg(t,
		`type FnCall struct { name string  arguments string }`,
		`type ToolCall struct { id string  function FnCall }`,
		`type RMsg struct { content string  tool_calls []ToolCall }`,
		`type RChoice struct { message RMsg }`,
		`type RUsage struct { total_tokens int }`,
		`type RResp struct { choices []RChoice  usage RUsage }`,
		`func main() {
			s := "{\"choices\":[{\"message\":{\"content\":null,\"tool_calls\":[{\"id\":\"call_1\",\"function\":{\"name\":\"calc\",\"arguments\":\"{}\"}}]}}],\"usage\":{\"total_tokens\":7}}"
			r := parse(s, RResp{})
			println("tc=" + str(len(r.choices[0].message.tool_calls)))
			println("id=" + r.choices[0].message.tool_calls[0].id)
			println("name=" + r.choices[0].message.tool_calls[0].function.name)
			println("content.len=" + str(len(r.choices[0].message.content)))
			println("tokens=" + str(r.usage.total_tokens))
		}`)
	want := "tc=1\nid=call_1\nname=calc\ncontent.len=0\ntokens=7\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// A null in place of a whole composite (slice / struct / map) must also be
// consumed, yielding the empty value and leaving later fields intact.
func TestParseNullCompositeFields(t *testing.T) {
	got := runProg(t,
		`type Inner struct { x string }`,
		`type C struct { items []string  in Inner  tail string }`,
		`func main() {
			a := parse("{\"items\":null,\"tail\":\"kept\"}", C{})
			println("items=" + str(len(a.items)) + " tail=" + a.tail)
			b := parse("{\"in\":null,\"tail\":\"kept2\"}", C{})
			println("in.x.len=" + str(len(b.in.x)) + " tail=" + b.tail)
		}`)
	want := "items=0 tail=kept\nin.x.len=0 tail=kept2\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
