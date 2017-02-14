package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
)

func main() {
	flag.Parse()
	filename := flag.Arg(0)

	if filename == "" {
		fmt.Println("must supply filename")
		os.Exit(1)
	}

	fd, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer fd.Close()

	buf := bufio.NewReader(fd)
	lex := newLexer(buf)

	for {
		t := lex.nextItem()
		if t.tokenType == itemEOF {
			return
		}
		fmt.Println(t)
	}
}
