package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"time"

	"careme/internal/mnfoodclubstub"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	publicURL := flag.String("public-url", "http://localhost:8080", "externally reachable URL, including path prefix")
	flag.Parse()
	u, err := url.Parse(*publicURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" || u.Fragment != "" {
		log.Fatal("invalid public URL")
	}
	server := &http.Server{Addr: *addr, Handler: mnfoodclubstub.New(*publicURL), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
