package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <directory>")
		os.Exit(1)
	}
	dir := os.Args[1]

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
}
