package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
)

func makeHTTPRequest() {
	resp, err := http.Get("https://example.com")
	if err != nil {
		fmt.Println("Error making HTTP request:", err)
		return
	}
	// Process the response here
	// discard the body of the response to free the connection
	io.Copy(io.Discard, resp.Body)
	// close the connection to the server to free the connection
	resp.Body.Close()
}

func createJob() {
	// Create a new job

	// create a job
	makeHTTPRequest()

	// pull the job from the queue
	makeHTTPRequest()

	// process the job
	// TODO

	// complete the job
	makeHTTPRequest()
}

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 10000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			createJob()
		}()
	}
	wg.Wait()
	fmt.Println("All goroutines completed.")
}
