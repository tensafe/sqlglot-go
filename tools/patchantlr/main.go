package main

import (
	"flag"
	"log"
	"os"
	"strings"
)

func main() {
	file := flag.String("file", "", "file to patch")
	find := flag.String("find", "", "substring to find")
	repl := flag.String("repl", "", "replacement")
	flag.Parse()

	if *file == "" || *find == "" {
		log.Fatal("usage: -file <path> -find <substr> [-repl <substr>]")
	}
	b, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("read %s: %v", *file, err)
	}
	s := string(b)
	if !strings.Contains(s, *find) {
		return // no-op
	}
	s = strings.ReplaceAll(s, *find, *repl)
	if err := os.WriteFile(*file, []byte(s), 0o644); err != nil {
		log.Fatalf("write %s: %v", *file, err)
	}
}
