package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
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

	// for {
	// 	t := lex.nextItem()
	// 	if t.tokenType == itemEOF {
	// 		return
	// 	}
	// 	fmt.Println(t)
	// }
	printKVs(lex)
}

func printKVs(lex *lexer) {
	sectionStack := []string{}
	for {
		t := lex.nextItem()
		switch t.tokenType {
		case itemError:
			fmt.Printf("bad token at %d", t.position)
			os.Exit(2)
		case itemSection:
			depth := -1
			for i := 0; i < len(t.value); i++ {
				if t.value[i] == '[' {
					depth++
				} else {
					break
				}
			}
			cleanSectionName := strings.TrimSpace(strings.TrimLeft(strings.TrimRight(t.value, "]"), "["))
			sectionStack = append(sectionStack[:depth], cleanSectionName)
		case itemKeyword:
			valueToken := lex.nextItem()
			if valueToken.tokenType != itemValue {
				fmt.Printf("unexpected token at %d: %v", valueToken.position, valueToken)
				os.Exit(2)
			}
			fmt.Printf("%s.%s=%s\n", strings.Join(sectionStack, "."), strings.TrimSpace(t.value), strings.TrimSpace(valueToken.value))
		case itemEOF:
			return
		}
	}
}
