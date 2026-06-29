import json
import sqlite3
from http.server import BaseHTTPRequestHandler, HTTPServer

db = sqlite3.connect("notes.db", check_same_thread=False)
db.execute("CREATE TABLE IF NOT EXISTS notes(id INTEGER PRIMARY KEY, title TEXT, body TEXT)")


def row(r):
    return {"id": r[0], "title": r[1], "body": r[2]}


class Handler(BaseHTTPRequestHandler):
    def send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        n = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        cur = db.execute("INSERT INTO notes(title,body) VALUES(?,?)", (n["title"], n["body"]))
        db.commit()
        self.send(201, {"id": cur.lastrowid, "title": n["title"], "body": n["body"]})

    def do_GET(self):
        if self.path == "/notes":
            rows = db.execute("SELECT id,title,body FROM notes ORDER BY id").fetchall()
            self.send(200, [row(r) for r in rows])
        else:
            r = db.execute("SELECT id,title,body FROM notes WHERE id=?", (self.path.split("/")[-1],)).fetchone()
            self.send(200, row(r)) if r else self.send(404, {"error": "not found"})

    def do_DELETE(self):
        i = self.path.split("/")[-1]
        db.execute("DELETE FROM notes WHERE id=?", (i,))
        db.commit()
        self.send(200, {"deleted": int(i)})


HTTPServer(("", 18082), Handler).serve_forever()
