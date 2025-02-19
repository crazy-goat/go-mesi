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
	"time"

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

func sleep(w http.ResponseWriter, r *http.Request) {
	timeout, _ := strconv.Atoi(r.PathValue("timeout"))
	index := r.PathValue("index")
	time.Sleep(time.Duration(timeout) * time.Second)
	w.Write([]byte(index + " Waited " + strconv.Itoa(timeout)))
}

func returnEsi(w http.ResponseWriter, _ *http.Request) {
	w.Header().Add("Edge-control", "dca=esi")
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:8080/hello\" />]"))
}

func returnEsiNoHeader(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:8080/hello\" />]"))
}

func recursive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Add("Edge-control", "dca=esi")
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:8080/recursive\" />]"))
}

func startHttpServer(wg *sync.WaitGroup) *http.Server {
	srv := &http.Server{Addr: ":8080"}

	http.HandleFunc("/hello", hello)
	http.HandleFunc("/status/code/{id}", statusCode)
	http.HandleFunc("/sleep/{timeout}/{index}", sleep)
	http.HandleFunc("/returnEsi", returnEsi)
	http.HandleFunc("/returnNonEsiHeader", returnEsiNoHeader)
	http.HandleFunc("/recursive", recursive)
	go func() {
		defer wg.Done()
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()
	return srv
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <directory>")
		os.Exit(1)
	}
	dir := os.Args[1]

	httpServerExitDone := &sync.WaitGroup{}

	httpServerExitDone.Add(1)
	srv := startHttpServer(httpServerExitDone)

	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("Error reading directory:", err)
		os.Exit(1)
	}

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
		start := time.Now()

		result := mesi.MESIParse(string(testData), mesi.EsiParserConfig{
			DefaultUrl:    "http://127.0.0.1:8080",
			MaxDepth:      5,
			ParseOnHeader: true,
			Timeout:       5 * time.Second,
		})
		elapsed := time.Since(start)
		expected := string(expectedData)

		// Compare the result with the expected value.
		if result == expected {
			if elapsed < 2*time.Second {
				fmt.Printf("Test %s ok, duration: %s\n", testFileName, elapsed)
			} else {
				fmt.Printf("Test %s failed - took to long, duration: %s\n", testFileName, elapsed)
			}
		} else {
			fmt.Printf("Test %s fail, duration: %s\n", testFileName, elapsed)
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
