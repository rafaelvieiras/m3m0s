//go:build pprofdebug

package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
)

// Profiling aid, compiled in only with `-tags pprofdebug`: serves
// /debug/pprof on localhost:6060 (loopback only).
func init() {
	go func() { log.Println(http.ListenAndServe("127.0.0.1:6060", nil)) }()
}
