package main

import (
	"fmt"
	"github.com/crazy-goat/go-mesi/mesi"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: program <filename>")
		return
	}

	filename := os.Args[1]
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	fmt.Print(mesi.Parse(string(data), 5, "http://127.0.0.1:8080"))
}
