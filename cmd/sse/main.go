package main

import (
	"log"
	"net/http"

	"github.com/Checksum/go/sse"
)

func main() {
	broker := sse.New(func(req *http.Request) string {
		return "global"
	})
	http.HandleFunc("/", broker.ServeHTTP)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
