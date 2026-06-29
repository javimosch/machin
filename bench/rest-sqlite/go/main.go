package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type Note struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

var db *sql.DB

func handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == "POST" && r.URL.Path == "/notes":
		var n Note
		json.NewDecoder(r.Body).Decode(&n)
		res, _ := db.Exec("INSERT INTO notes(title,body) VALUES(?,?)", n.Title, n.Body)
		id, _ := res.LastInsertId()
		n.ID = int(id)
		json.NewEncoder(w).Encode(n)
	case r.Method == "GET" && r.URL.Path == "/notes":
		rows, _ := db.Query("SELECT id,title,body FROM notes ORDER BY id")
		defer rows.Close()
		out := []Note{}
		for rows.Next() {
			var n Note
			rows.Scan(&n.ID, &n.Title, &n.Body)
			out = append(out, n)
		}
		json.NewEncoder(w).Encode(out)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/notes/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/notes/"))
		var n Note
		err := db.QueryRow("SELECT id,title,body FROM notes WHERE id=?", id).Scan(&n.ID, &n.Title, &n.Body)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(n)
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/notes/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/notes/"))
		db.Exec("DELETE FROM notes WHERE id=?", id)
		json.NewEncoder(w).Encode(map[string]int{"deleted": id})
	default:
		http.NotFound(w, r)
	}
}

func main() {
	var err error
	db, err = sql.Open("sqlite", "notes.db")
	if err != nil {
		log.Fatal(err)
	}
	db.Exec("CREATE TABLE IF NOT EXISTS notes(id INTEGER PRIMARY KEY, title TEXT, body TEXT)")
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(":18081", nil))
}
