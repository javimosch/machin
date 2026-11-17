package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
)

// caldavProg composes framework/xml.src + framework/caldav.src + the test app
// (xml.src MUST precede caldav.src — caldav reads its g_xn node store).
func caldavProg(t *testing.T, app string) *Program {
	t.Helper()
	xs, err := os.ReadFile("framework/xml.src")
	if err != nil {
		t.Skip("framework/xml.src not found")
	}
	cs, err := os.ReadFile("framework/caldav.src")
	if err != nil {
		t.Skip("framework/caldav.src not found")
	}
	prog, perr := progFromSrcErr(string(xs) + string(cs) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// caldavReadRequest reads one HTTP request: request line, headers to the blank
// line, then Content-Length body bytes. Returns (method, path, headers, body).
func caldavReadRequest(c net.Conn) (string, string, string, string, error) {
	r := bufio.NewReader(c)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", "", "", "", err
	}
	f := strings.Fields(strings.TrimRight(line, "\r\n"))
	if len(f) < 2 {
		return "", "", "", "", fmt.Errorf("bad request line %q", line)
	}
	var hdrs strings.Builder
	cl := 0
	for {
		h, err := r.ReadString('\n')
		if err != nil {
			return "", "", "", "", err
		}
		h = strings.TrimRight(h, "\r\n")
		if h == "" {
			break
		}
		hdrs.WriteString(h)
		hdrs.WriteString("\n")
		if strings.HasPrefix(strings.ToLower(h), "content-length:") {
			v := strings.TrimSpace(h[strings.Index(h, ":")+1:])
			if n, err := strconv.Atoi(v); err == nil {
				cl = n
			}
		}
	}
	body := ""
	if cl > 0 {
		b := make([]byte, cl)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", "", "", "", err
		}
		body = string(b)
	}
	return f[0], f[1], hdrs.String(), body, nil
}

func caldavRespond(c net.Conn, status int, reason, ctype, body string) {
	fmt.Fprintf(c, "HTTP/1.1 %d %s\r\nContent-Length: %d\r\nContent-Type: %s\r\nConnection: close\r\n\r\n%s",
		status, reason, len(body), ctype, body)
}

const caldavMultistatusOpen = `<?xml version="1.0" encoding="utf-8"?>` +
	`<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`

const caldavMultistatusClose = `</D:multistatus>`

// caldavDiscoveryBody is the Depth:0 PROPFIND answer: principal + home-set.
const caldavDiscoveryBody = caldavMultistatusOpen +
	`<D:response><D:href>/</D:href><D:propstat><D:prop>` +
	`<D:current-user-principal><D:href>/principals/users/alice/</D:href></D:current-user-principal>` +
	`<C:calendar-home-set><D:href>/calendars/alice/</D:href></C:calendar-home-set>` +
	`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>` +
	caldavMultistatusClose

// caldavListBody is the Depth:1 PROPFIND answer on the home collection: the
// collection itself (calendar+collection resourcetype), a second calendar,
// and an event whose displayname prop 404s (second propstat must be skipped).
const caldavListBody = caldavMultistatusOpen +
	`<D:response><D:href>/calendars/alice/</D:href><D:propstat><D:prop>` +
	`<D:displayname>Alice</D:displayname>` +
	`<D:resourcetype><D:collection/><C:calendar/></D:resourcetype>` +
	`<D:getetag>"home-etag"</D:getetag>` +
	`<C:calendar-description>Personal calendars</C:calendar-description>` +
	`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>` +
	`<D:response><D:href>/calendars/alice/work/</D:href><D:propstat><D:prop>` +
	`<D:displayname>Work</D:displayname>` +
	`<D:resourcetype><D:collection/><C:calendar/></D:resourcetype>` +
	`<D:getetag>"work-etag"</D:getetag>` +
	`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>` +
	`<D:response><D:href>/calendars/alice/home/a.ics</D:href>` +
	`<D:propstat><D:prop><D:getetag>"a-etag"</D:getetag></D:prop>` +
	`<D:status>HTTP/1.1 200 OK</D:status></D:propstat>` +
	`<D:propstat><D:prop><D:displayname/></D:prop>` +
	`<D:status>HTTP/1.1 404 Not Found</D:status></D:propstat></D:response>` +
	caldavMultistatusClose

// caldavReportBody is the REPORT calendar-query answer: one object whose
// calendar-data holds TWO VEVENTs — the second has a folded SUMMARY line and
// TZID-parametrized DTSTART/DTEND.
const caldavReportBody = caldavMultistatusOpen +
	`<D:response><D:href>/calendars/alice/home/a.ics</D:href><D:propstat><D:prop>` +
	`<D:getetag>"a-etag"</D:getetag>` +
	`<C:calendar-data>BEGIN:VCALENDAR` + "\n" +
	`VERSION:2.0` + "\n" +
	`PRODID:-//machin-test//EN` + "\n" +
	`BEGIN:VEVENT` + "\n" +
	`UID:evt-1` + "\n" +
	`SUMMARY:Team Standup` + "\n" +
	`DTSTART:20260105T090000Z` + "\n" +
	`DTEND:20260105T093000Z` + "\n" +
	`END:VEVENT` + "\n" +
	`BEGIN:VEVENT` + "\n" +
	`UID:evt-2` + "\n" +
	`SUMMARY:Quarterly rev` + "\n" +
	` iew with the board` + "\n" +
	`DTSTART;TZID=America/New_York:20260106T140000` + "\n" +
	`DTEND;TZID=America/New_York:20260106T150000` + "\n" +
	`END:VEVENT` + "\n" +
	`END:VCALENDAR` +
	`</C:calendar-data>` +
	`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>` +
	caldavMultistatusClose

const caldavEventICS = "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:evt-1\nSUMMARY:Team Standup\nEND:VEVENT\nEND:VCALENDAR\n"

// caldavServe plays one scripted CalDAV session per connection. Only
// alice:s3cret gets in — anything else is a 401, so the client's Basic auth
// header is exercised on every call.
func caldavServe(c net.Conn) {
	defer c.Close()
	method, path, hdrs, body, err := caldavReadRequest(c)
	if err != nil {
		return
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	if !strings.Contains(hdrs, "Authorization: "+wantAuth) {
		caldavRespond(c, 401, "Unauthorized", "text/plain", "no")
		return
	}
	switch {
	case method == "PROPFIND" && path == "/":
		caldavRespond(c, 207, "Multi-Status", "application/xml; charset=utf-8", caldavDiscoveryBody)
	case method == "PROPFIND" && path == "/calendars/alice/":
		caldavRespond(c, 207, "Multi-Status", "application/xml; charset=utf-8", caldavListBody)
	case method == "REPORT" && path == "/calendars/alice/":
		caldavRespond(c, 207, "Multi-Status", "application/xml; charset=utf-8", caldavReportBody)
	case method == "GET" && path == "/calendars/alice/home/a.ics":
		caldavRespond(c, 200, "OK", "text/calendar; charset=utf-8", caldavEventICS)
	case method == "PUT" && path == "/calendars/alice/home/new.ics":
		caldavRespond(c, 201, "Created", "text/plain", "")
	case method == "DELETE" && path == "/calendars/alice/home/a.ics":
		caldavRespond(c, 204, "No Content", "text/plain", "")
	case method == "GET" && path == "/missing.ics":
		caldavRespond(c, 404, "Not Found", "text/plain", "no such object")
	default:
		caldavRespond(c, 400, "Bad Request", "text/plain", "unhandled "+method+" "+path+" body="+body)
	}
}

// TestCaldavClient exercises the pure-MFL CalDAV client end to end against a
// scripted loopback server: PROPFIND discovery, Depth:1 listing, REPORT
// calendar-query with VEVENT extraction, GET/PUT/DELETE, plus the failure
// paths (401 auth, 404 object, malformed time-range).
func TestCaldavClient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback available: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go caldavServe(c)
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	app := fmt.Sprintf(`
func main() {
    base := "http://127.0.0.1:%d"
    p, home, ok := caldav_discover(base + "/", "alice", "s3cret")
    println("DISC ok=" + str(ok))
    println("PRINCIPAL " + p)
    println("HOME " + home)
    cals, ok2 := caldav_list(home, "alice", "s3cret")
    println("LIST ok=" + str(ok2))
    println("CALS " + cals)
    evs, ok3 := caldav_query(home, "alice", "s3cret", "20260101T000000Z", "20260201T000000Z")
    println("QUERY ok=" + str(ok3))
    println("EVS " + evs)
    ics, ok4 := caldav_get(base, "alice", "s3cret", "/calendars/alice/home/a.ics")
    println("GET ok=" + str(ok4))
    println("ICS " + ics)
    pok := caldav_put(base, "alice", "s3cret", "/calendars/alice/home/new.ics", "BEGIN:VCALENDAR\nEND:VCALENDAR")
    println("PUT ok=" + str(pok))
    dok := caldav_delete(base, "alice", "s3cret", "/calendars/alice/home/a.ics")
    println("DEL ok=" + str(dok))
    _, gok := caldav_get(base, "alice", "s3cret", "/missing.ics")
    println("GET404 ok=" + str(gok))
    _, aok := caldav_get(base, "bob", "wrong", "/calendars/alice/home/a.ics")
    println("AUTH ok=" + str(aok))
    _, qok := caldav_query(home, "alice", "s3cret", "bogus", "20260201T000000Z")
    println("BADTIME ok=" + str(qok))
}`, port)

	out, err := RunCaptured(caldavProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	origin := fmt.Sprintf("http://127.0.0.1:%d", port)
	for _, want := range []string{
		"DISC ok=1",
		"PRINCIPAL " + origin + "/principals/users/alice/",
		"HOME " + origin + "/calendars/alice/",
		"LIST ok=1",
		`"displayname":"Work"`,
		`"calendar":1`,
		`"collection":1`,
		`"etag":"\"work-etag\""`,
		`"description":"Personal calendars"`,
		origin + "/calendars/alice/work/",
		"QUERY ok=1",
		`"uid":"evt-1"`,
		`"summary":"Team Standup"`,
		`"dtstart":"20260105T090000Z"`,
		// folded SUMMARY unfolded; TZID param dropped from the value
		`"summary":"Quarterly review with the board"`,
		`"dtstart":"20260106T140000"`,
		"GET ok=1",
		"ICS BEGIN:VCALENDAR",
		"PUT ok=1",
		"DEL ok=1",
		"GET404 ok=0",
		"AUTH ok=0",
		"BADTIME ok=0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing expectation %q; got:\n%s", want, out)
		}
	}
}
