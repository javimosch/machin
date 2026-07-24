package main

import (
	"os"
	"strings"
	"testing"
)

// TestSAMLVerify exercises the pure-MFL XML-DSig / SAML signature verification
// in framework/xml.src (the XML c14n arm of issue #484): a minimal XML parser +
// Exclusive Canonicalization (xml-exc-c14n#) + enveloped-signature verification
// on top of the existing x509_pubkey / rsa_verify_jwk_sha256 crypto builtins.
//
// The golden vectors in testdata/saml were produced by signxml (the de-facto
// Python XML-DSig library, over lxml's exclusive c14n) — real, spec-correct
// enveloped RSA-SHA256 signatures. The MFL verifier must accept a genuine
// signature and reject every corruption. testdata/saml/nested.xml wraps the
// signed <Assertion> in a <Response> that declares extra unused namespaces,
// which Exclusive Canonicalization must PRUNE — the crux of "exclusive".
func TestSAMLVerify(t *testing.T) {
	xmlsrc, err := os.ReadFile("framework/xml.src")
	if err != nil {
		t.Skip("framework/xml.src not found")
	}
	// Guard: the golden vectors must exist (produced by signxml, committed).
	for _, f := range []string{"testdata/saml/signed.xml", "testdata/saml/cert.b64", "testdata/saml/nested.xml", "testdata/saml/cert2.b64"} {
		if _, err := os.Stat(f); err != nil {
			t.Skipf("golden vector %s missing", f)
		}
	}

	app := `
func main() {
    xml := read_file("testdata/saml/signed.xml")
    cert := read_file("testdata/saml/cert.b64")
    cert2 := read_file("testdata/saml/cert2.b64")
    // genuine signature verifies (digest + signature)
    println("valid=" + str(saml_verify(xml, cert)))
    // tampering the signed content breaks the reference digest
    println("tampered=" + str(saml_verify(replace(xml, "ada@example.com", "evil@example.com"), cert)))
    // corrupting the signature value breaks the signature check
    println("badsig=" + str(saml_verify(replace(xml, "hTw7", "AAAA"), cert)))
    // a different (untrusted) cert must not verify
    println("wrongcert=" + str(saml_verify(xml, cert2)))
    // exclusive c14n: signed Assertion nested under a Response with extra
    // unused namespaces that must be pruned
    nested := read_file("testdata/saml/nested.xml")
    println("nested=" + str(saml_verify(nested, cert2)))
    println("nested_tampered=" + str(saml_verify(replace(nested, "bob@x.io", "mallory@x.io"), cert2)))
    // no signature at all -> fails closed
    println("nosig=" + str(saml_verify("<a><b>x</b></a>", cert)))
}`

	prog, perr := progFromSrcErr(string(xmlsrc) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"valid=1",
		"tampered=0",
		"badsig=0",
		"wrongcert=0",
		"nested=1",
		"nested_tampered=0",
		"nosig=0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestExcC14nGolden pins the Exclusive Canonicalization output byte-for-byte
// against lxml/signxml's c14n of the same nodes — the highest-risk core, where
// any deviation silently breaks signature verification. It canonicalizes the
// <ds:SignedInfo> (the signature input) and the <saml:Assertion> with the
// enveloped <ds:Signature> removed (the digest input), and compares to the
// committed reference bytes.
func TestExcC14nGolden(t *testing.T) {
	xmlsrc, err := os.ReadFile("framework/xml.src")
	if err != nil {
		t.Skip("framework/xml.src not found")
	}
	wantSI, err := os.ReadFile("testdata/saml/signedinfo_c14n.txt")
	if err != nil {
		t.Skip("golden signedinfo_c14n.txt missing")
	}
	wantAS, err := os.ReadFile("testdata/saml/assertion_c14n.txt")
	if err != nil {
		t.Skip("golden assertion_c14n.txt missing")
	}

	// The MFL driver prints each c14n result on its own line, framed by markers
	// so we can extract it exactly (c14n output itself contains no newlines here).
	app := `
func main() {
    root := xml_parse(read_file("testdata/saml/signed.xml"))
    si := xml_find_local(root, "SignedInfo")
    sig := xml_find_local(root, "Signature")
    g_skip = sig
    println("<<AS>>" + c14n_subtree(root) + "<<END>>")
    g_skip = -1
    println("<<SI>>" + c14n_subtree(si) + "<<END>>")
}`
	prog, perr := progFromSrcErr(string(xmlsrc) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	gotAS := between(out, "<<AS>>", "<<END>>")
	gotSI := between(out, "<<SI>>", "<<END>>")
	if gotAS != string(wantAS) {
		t.Fatalf("Assertion c14n mismatch:\n got: %q\nwant: %q", gotAS, string(wantAS))
	}
	if gotSI != string(wantSI) {
		t.Fatalf("SignedInfo c14n mismatch:\n got: %q\nwant: %q", gotSI, string(wantSI))
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	i += len(a)
	j := strings.Index(s[i:], b)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}
