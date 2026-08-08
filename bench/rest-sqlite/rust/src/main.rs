// A notes REST service over SQLite — create / list / get / delete.
// Needs three crates: tiny_http (server), rusqlite (SQLite), serde_json (JSON).
use std::io::Read;
use rusqlite::Connection;
use serde_json::{json, Value};
use tiny_http::{Header, Request, Response, Server};

fn rows(conn: &Connection, sql: &str, args: &[&dyn rusqlite::ToSql]) -> Vec<Value> {
    let mut st = conn.prepare(sql).unwrap();
    let it = st
        .query_map(args, |r| {
            Ok(json!({
                "id": r.get::<_, i64>(0)?,
                "title": r.get::<_, String>(1)?,
                "body": r.get::<_, String>(2)?
            }))
        })
        .unwrap();
    it.filter_map(|r| r.ok()).collect()
}

fn send(req: Request, code: u16, body: String) {
    let h = Header::from_bytes(&b"Content-Type"[..], &b"application/json"[..]).unwrap();
    let _ = req.respond(Response::from_string(body).with_status_code(code).with_header(h));
}

fn main() {
    let conn = Connection::open("notes.db").unwrap();
    conn.execute(
        "CREATE TABLE IF NOT EXISTS notes(id INTEGER PRIMARY KEY, title TEXT, body TEXT)",
        [],
    )
    .unwrap();
    let server = Server::http("0.0.0.0:18081").unwrap();

    for mut req in server.incoming_requests() {
        let method = req.method().as_str().to_string();
        let path = req.url().to_string();
        let id = path.strip_prefix("/notes/").map(|s| s.to_string());

        match (method.as_str(), id.as_deref()) {
            ("POST", _) if path == "/notes" => {
                let mut b = String::new();
                req.as_reader().read_to_string(&mut b).ok();
                let n: Value = serde_json::from_str(&b).unwrap_or(json!({}));
                conn.execute(
                    "INSERT INTO notes(title,body) VALUES(?,?)",
                    [n["title"].as_str().unwrap_or(""), n["body"].as_str().unwrap_or("")],
                )
                .unwrap();
                let r = rows(&conn, "SELECT id,title,body FROM notes WHERE id=last_insert_rowid()", &[]);
                send(req, 201, serde_json::to_string(&r).unwrap());
            }
            ("GET", None) if path == "/notes" => {
                let r = rows(&conn, "SELECT id,title,body FROM notes ORDER BY id", &[]);
                send(req, 200, serde_json::to_string(&r).unwrap());
            }
            ("GET", Some(i)) => {
                let r = rows(&conn, "SELECT id,title,body FROM notes WHERE id=?", &[&i]);
                if r.is_empty() { send(req, 404, "{}".into()) } else { send(req, 200, serde_json::to_string(&r).unwrap()) }
            }
            ("DELETE", Some(i)) => {
                conn.execute("DELETE FROM notes WHERE id=?", [&i]).unwrap();
                send(req, 200, format!("{{\"deleted\":{}}}", i));
            }
            _ => send(req, 404, "{}".into()),
        }
    }
}
