const http = require("http");
http.createServer((req, res) => res.end("hello from node\n")).listen(8092);
