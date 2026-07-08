// A minimal web app following the minato contract: listen on $PORT and serve
// HTTP. Used by the e2e tests and as a template for your own apps.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

const message = "Hello from sample-app! (rev 1)"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		fmt.Fprintln(w, message)
	})
	log.Printf("sample-app listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
