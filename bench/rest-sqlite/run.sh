#!/usr/bin/env bash
# Build all three implementations and run the same CRUD smoke test against each.
# machin always builds here; Go needs a network fetch of modernc.org/sqlite the
# first time; Python needs a CPython with the stdlib sqlite3 module.
set +e
cd "$(dirname "$0")"

smoke() { # $1=port $2=label
  local p=$1 l=$2
  echo "  POST   -> $(curl -s -X POST localhost:$p/notes -d '{"title":"a","body":"b"}')"
  curl -s -X POST localhost:$p/notes -d '{"title":"c","body":"d"}' >/dev/null
  echo "  LIST   -> $(curl -s localhost:$p/notes)"
  echo "  GET 1  -> $(curl -s localhost:$p/notes/1)"
  echo "  DELETE -> $(curl -s -X DELETE localhost:$p/notes/1)"
  echo "  MISS   -> HTTP $(curl -s -o /dev/null -w '%{http_code}' localhost:$p/notes/99)"
}

echo "== machin =="
( cd machin && rm -f notes.db && machin encode ../../../framework/machweb.src app.src > app.mfl 2>/dev/null \
  && machin build app.mfl -o notes-machin 2>/dev/null \
  && echo "  built: $(stat -c%s notes-machin) byte static binary" \
  && ./notes-machin >/dev/null 2>&1 & )
sleep 1.5; smoke 18080 machin; pkill -f notes-machin 2>/dev/null

echo "== Go =="
( cd go && rm -f notes.db && go mod tidy >/dev/null 2>&1 && go build -o notes-go . 2>/dev/null \
  && echo "  built: $(stat -c%s notes-go) byte binary" \
  && ./notes-go >/dev/null 2>&1 & )
sleep 2; smoke 18081 go; pkill -f notes-go 2>/dev/null

echo "== Python =="
( cd python && rm -f notes.db && python3 app.py >/dev/null 2>&1 & )
sleep 1.5; smoke 18082 python; pkill -f "python3 app.py" 2>/dev/null

echo "done."
