//go:build !(js && wasm)

package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// main serves a database file: the web app on local HTTP, or the MCP server
// over stdio or HTTP, or one tool call from the command line.
func main() {
	dbPath := flag.String("db", "../job_search.sqlite3", "path to the SQLite database")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	mcpMode := flag.Bool("mcp", false, "serve MCP over stdin/stdout instead of HTTP")
	mcpHTTP := flag.String("mcp-http", "", "serve MCP over Streamable HTTP on this address (for ChatGPT), for example 127.0.0.1:8765")
	callTool := flag.String("call", "", "call one MCP tool by name, print its JSON result and exit (\"list\" lists the tools)")
	callArgs := flag.String("args", "{}", "JSON arguments for -call")
	callArgsFile := flag.String("args-file", "", "file holding the JSON arguments for -call (for long text such as a résumé)")
	initDB := flag.Bool("init", false, "create the database file and its tables if they do not exist (for a first run)")
	flag.Parse()
	path, e := filepath.Abs(*dbPath)
	if e != nil {
		log.Fatal(e)
	}
	db, e := sql.Open("sqlite3", fileDSN(path, *initDB))
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	if e = db.Ping(); e != nil {
		log.Fatal(e)
	}
	if *initDB {
		if e = initJobs(db); e != nil {
			log.Fatal(e)
		}
	}
	if e = migrateAll(db); e != nil {
		log.Fatal(e)
	}
	dataDir = filepath.Dir(path)
	// Importing would replace the file in place; on the desktop, copy files
	// instead. Export works everywhere.
	registerAll(db, false)
	if *callTool != "" {
		args := []byte(*callArgs)
		if *callArgsFile != "" {
			if args, e = os.ReadFile(*callArgsFile); e != nil {
				log.Fatal(e)
			}
		}
		out, e := callMCPTool(db, *callTool, args)
		fmt.Println(out)
		if e != nil {
			log.Fatal(e)
		}
		return
	}
	if *mcpHTTP != "" {
		log.Fatal(serveMCPHTTP(db, *mcpHTTP))
	}
	if *mcpMode {
		if e = runMCP(db); e != nil {
			log.Fatal(e)
		}
		return
	}
	static, _ := fs.Sub(assets, "static")
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	log.Printf("Job review: http://%s (database %s)", *addr, path)
	log.Fatal(http.ListenAndServe(*addr, localHostOnly(http.DefaultServeMux)))
}
