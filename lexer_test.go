package modconfigobj_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/christian-blades-cb/modconfigobj"
)

const SimpleFile = `
[section]
key = value
`

func Test_SimpleFile(t *testing.T) {
	buf := strings.NewReader(SimpleFile)
	lex := modconfigobj.NewLexer(buf)
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
		case modconfigobj.ItemKey:
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
