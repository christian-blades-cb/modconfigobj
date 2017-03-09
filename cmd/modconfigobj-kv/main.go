package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/christian-blades-cb/modconfigobj"
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
	lex := modconfigobj.NewLexer(buf)

	printKVs(lex)
}

func printKVs(lex *modconfigobj.Lexer) {
	sectionStack := []string{}
	for {
		t := lex.NextItem()
		switch t.TokenType {
		case modconfigobj.ItemError:
			fmt.Printf("bad token at %d", t.Position)
			os.Exit(2)
		case modconfigobj.ItemSection:
			depth := -1
			for i := 0; i < len(t.Value); i++ {
				if t.Value[i] == '[' {
					depth++
				} else {
					break
				}
			}
			cleanSectionName := strings.TrimSpace(strings.TrimLeft(strings.TrimRight(t.Value, "]"), "["))
			sectionStack = append(sectionStack[:depth], cleanSectionName)
		case modconfigobj.ItemKeyword:
			valueToken := lex.NextItem()
			if valueToken.TokenType != modconfigobj.ItemValue {
				fmt.Printf("unexpected token at %d: %v", valueToken.Position, valueToken)
				os.Exit(2)
			}
			fmt.Printf("%s.%s=%s\n", strings.Join(sectionStack, "."), strings.TrimSpace(t.Value), strings.TrimSpace(valueToken.Value))
		case modconfigobj.ItemEOF:
			return
		}
	}
}
