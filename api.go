package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

func testGetJobID(resp http.ResponseWriter, r *http.Request) {
	reqVars := mux.Vars(r)
	fmt.Fprintf(resp, "Hello, %s", reqVars["jobID"])
}

func setupRouter() *http.ServeMux {
	r := mux.NewRouter()
	r.HandleFunc("/{jobID}", testGetJobID).Methods("GET")
}

// ServeHTTP sets up the routes and serves HTTP requests. It blocks forever, so
// make sure you call this last.
func ServeHTTP() {
	m := setupRouter()
	s := &http.Server{
		Addr:    ":8080",
		Handler: m,
	}
	log.Fatal(s.ListenAndServe)
}
