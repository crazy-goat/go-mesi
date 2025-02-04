package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// parse is a mock function that returns the input as is.
func parse(input string) string {
	// Replace this with your actual parsing logic.
	return input
}

func hello(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("Hello World"))
}

func statusCode(w http.ResponseWriter, r *http.Request) {
	code, _ := strconv.Atoi(r.PathValue("id"))
	w.WriteHeader(code)
	w.Write([]byte(http.StatusText(code)))
}

func startHttpServer(wg *sync.WaitGroup) *http.Server {
	srv := &http.Server{Addr: ":8080"}

	http.HandleFunc("/hello", hello)
	http.HandleFunc("/status/code/{id}", statusCode)
	go func() {
		defer wg.Done() // let main know we are done cleaning up

		// always returns error. ErrServerClosed on graceful close
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			// unexpected error. port in use?
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()

	// returning reference so caller can call Shutdown()
	return srv
}

func main() {
	// Check if a directory argument was provided.
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <directory>")
		os.Exit(1)
	}
	dir := os.Args[1]

	httpServerExitDone := &sync.WaitGroup{}

	httpServerExitDone.Add(1)
	srv := startHttpServer(httpServerExitDone)

	// Read all entries in the provided directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("Error reading directory:", err)
		os.Exit(1)
	}

	// Filter for test files that end with ".html" but not ".expected".
	var testFiles []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".html") && !strings.HasSuffix(name, ".expected") {
			testFiles = append(testFiles, entry)
		}
	}

	// Sort the test files lexicographically.
	sort.Slice(testFiles, func(i, j int) bool {
		return testFiles[i].Name() < testFiles[j].Name()
	})

	// Process each test file.
	for _, testFile := range testFiles {
		testFileName := testFile.Name()
		expectedFileName := testFileName + ".expected"

		// Build full paths for the files.
		testFilePath := dir + "/" + testFileName
		expectedFilePath := dir + "/" + expectedFileName

		testData, err := os.ReadFile(testFilePath)
		if err != nil {
			fmt.Printf("Error reading test file %s: %v\n", testFilePath, err)
			continue
		}

		expectedData, err := os.ReadFile(expectedFilePath)
		if err != nil {
			fmt.Printf("Error reading expected file %s: %v\n", expectedFilePath, err)
			continue
		}

		// Call the parse function.
		result := mesi.Parse(string(testData))
		expected := string(expectedData)

		// Compare the result with the expected value.
		if result == expected {
			fmt.Printf("Test %s ok\n", testFileName)
		} else {
			fmt.Printf("Test %s fail\n", testFileName)
			// Generate a diff between expected and result using diffmatchpatch.
			dmp := diffmatchpatch.New()
			diffs := dmp.DiffMain(expected, result, false)
			diffText := dmp.DiffPrettyText(diffs)
			fmt.Println("Diff:")
			fmt.Println(diffText)
		}
	}
	if err := srv.Shutdown(context.TODO()); err != nil {
		panic(err) // failure/timeout shutting down the server gracefully
	}

	httpServerExitDone.Wait()
}
