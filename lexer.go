package main

import (
	"fmt"
	"io"
	"unicode"
)

type itemType int

const (
	itemError itemType = iota

	itemComment // includes hash (#)

	itemKeyword
	itemValue // includes quotes, if those exist
	itemSection

	itemLeftSection  // [
	itemRightSection // ]

	itemEOF
)

type token struct {
	tokenType itemType
	position  int64
	len       int64
	value     string
}

func (t token) String() string {
	return fmt.Sprintf("token %s at %d: \"%s\"", t.tokenType, t.position, t.value)
}

// Reader is an object that can emit single runes
type Reader interface {
	ReadRune() (rune, int, error)
	UnreadRune() error
}

// Buffer supports writing runes, emitting strings, and resetting its contents
type Buffer interface {
	WriteRune(rune) (int, error)
	String() string
	Reset()
}

type lexer struct {
	input          Reader
	tokenValBuffer Buffer
	position       int64
	start          int64
	tokenStream    chan token
}

func (l *lexer) run() {
	for state := lexGeneric; state != nil; {
		state = state(l)
	}
	close(l.tokenStream)
}

type stateFn func(*lexer) stateFn

func lexGeneric(l *lexer) stateFn {
	l.skipWhitespace()
	var r rune
	var n int
	var err error

	for {
		r, n, err = l.input.ReadRune()
		if err == io.EOF {
			l.emit(itemEOF)
			return nil
		} else if err != nil {
			l.emit(itemError)
			panic(err)
		}

		switch r {
		case '[':
			l.input.UnreadRune()
			return lexSection
		case '#':
			l.input.UnreadRune()
			return lexComment
		case '\n':
			l.position += int64(n)
			return lexGeneric
		case '=':
			l.position += int64(n)
			l.emit(itemError)
		default:
			l.position += int64(n)
			return lexKey
		}
	}
}

func lexKey(l *lexer) stateFn {
	var r rune
	var n int
	var err error

	l.start = l.position
	l.tokenValBuffer.Reset()
	for {
		r, n, err = l.input.ReadRune()
		if err == io.EOF {
			l.emit(itemError)
			l.emit(itemEOF)
		} else if err != nil {
			l.emit(itemError)
			panic(err)
		}
		switch r {
		case '\n':
			l.position += int64(n)
			l.emit(itemError)
			return lexGeneric
		case '=':
			if l.position == l.start { // empty key?
				l.position += int64(n)
				l.emit(itemError)
				return lexGeneric
			}
			l.position += int64(n)
			l.emit(itemKeyword)
			l.position += int64(n)
			return lexValue
		default:
			l.position += int64(n)
		}
	}
}

func lexValue(l *lexer) stateFn {
	l.skipWhitespace()

	var r rune
	var n int
	var err error

	l.start = l.position
	l.tokenValBuffer.Reset()

	for {
		r, n, err = l.input.ReadRune()
		if err == io.EOF {
			if l.position == l.start { // no value for key/value pair
				l.handleUnexpectedEOF(n)
				return nil
			} else if err != nil {
				l.emit(itemError)
				panic(err)
			}
		}

		switch r {
		case '"':
			if l.position == l.start { // key = "
				l.input.UnreadRune()
				if err != nil {
					l.emit(itemError)
					panic(err)
				}
				return lexDoubleQuote
			}

			l.consumeRune(r, n)

		case '\'':
			if l.position == l.start { // key = '
				l.input.UnreadRune()
				if err != nil {
					l.emit(itemError)
					panic(err)
				}
				return lexSingleQuote
			}

			l.consumeRune(r, n)

		case '\n':
			if l.position == l.start { // key = <EOL>
				l.position += int64(n)
				l.emit(itemError)
				return lexGeneric
			}

			l.emit(itemValue)
			l.position += int64(n)
			return lexGeneric

		default:
			l.consumeRune(r, n)
		}
	}
}

func lexQuotedValue(quoteRune rune, l *lexer) stateFn {
	var r rune
	var n int
	var err error

	l.start = l.position
	l.tokenValBuffer.Reset()

	numQuotes, err := l.takeRunes(quoteRune, 3)
	if err == io.EOF {
		l.emit(itemError)
		l.emit(itemEOF)
		return nil
	} else if err != nil {
		l.emit(itemValue)
		panic(err)
	}

	switch numQuotes {
	case 1, 3:
		for {
			endQuotes, err := l.takeRunes(quoteRune, numQuotes)
			if err == io.EOF {
				l.emit(itemError)
				l.emit(itemEOF)
				return nil
			} else if err != nil {
				l.emit(itemValue)
				panic(err)
			}

			if endQuotes == numQuotes {
				l.emit(itemValue)
				return lexGeneric
			}

			r, n, err = l.input.ReadRune()
			if err == io.EOF {
				l.emit(itemError)
				l.emit(itemEOF)
				return nil
			} else if err != nil {
				l.emit(itemValue)
				panic(err)
			}

			l.consumeRune(r, n)
		}
	default:
		l.consumeRune(r, n)
		l.emit(itemError)
		return lexGeneric
	}

}

func lexSingleQuote(l *lexer) stateFn {
	var r rune
	var n int
	var err error

	l.start = l.position
	l.tokenValBuffer.Reset()

	numQuotes, err := l.takeRunes('"', 3)
	if err == io.EOF {
		l.emit(itemError)
		l.emit(itemEOF)
		return nil
	} else if err != nil {
		l.emit(itemValue)
		panic(err)
	}

	switch numQuotes {
	case 1, 3:
		for {
			endQuotes, err := l.takeRunes('\'', numQuotes)
			if err == io.EOF {
				l.emit(itemError)
				l.emit(itemEOF)
				return nil
			} else if err != nil {
				l.emit(itemValue)
				panic(err)
			}

			if endQuotes == numQuotes {
				l.emit(itemValue)
				return lexGeneric
			}

			r, n, err = l.input.ReadRune()
			if err == io.EOF {
				l.emit(itemError)
				l.emit(itemEOF)
				return nil
			} else if err != nil {
				l.emit(itemValue)
				panic(err)
			}

			l.consumeRune(r, n)
		}
	default:
		l.consumeRune(r, n)
		l.emit(itemError)
		return lexGeneric
	}

}

func lexDoubleQuote(l *lexer) stateFn {
	var r rune
	var n int
	var err error

	l.start = l.position
	l.tokenValBuffer.Reset()

	numQuotes, err := l.takeRunes('"', 3)
	if err == io.EOF {
		l.emit(itemError)
		l.emit(itemEOF)
		return nil
	} else if err != nil {
		l.emit(itemValue)
		panic(err)
	}

	switch numQuotes {
	case 1, 3:
		for {
			endQuotes, err := l.takeRunes('"', numQuotes)
			if err == io.EOF {
				l.emit(itemError)
				l.emit(itemEOF)
				return nil
			} else if err != nil {
				l.emit(itemValue)
				panic(err)
			}

			if endQuotes == numQuotes {
				l.emit(itemValue)
				return lexGeneric
			}

			r, n, err = l.input.ReadRune()
			if err == io.EOF {
				l.emit(itemError)
				l.emit(itemEOF)
				return nil
			} else if err != nil {
				l.emit(itemValue)
				panic(err)
			}

			l.consumeRune(r, n)
		}
	default:
		l.consumeRune(r, n)
		l.emit(itemError)
		return lexGeneric
	}
}

func (l *lexer) acceptRun(accept rune) (numRunes int, err error) {
	var r rune
	var n int

	for {
		r, n, err = l.input.ReadRune()
		if err != nil {
			return
		}

		if r == accept {
			numRunes++
			l.consumeRune(r, n)
		} else {
			l.input.UnreadRune()
			return
		}
	}
}

func (l *lexer) handleUnexpectedEOF(n int) {
	if l.position == l.start {
		l.emit(itemError)
		l.position += int64(n)
		l.emit(itemEOF)
	}
}

func lexComment(l *lexer) stateFn {
	var r rune
	var n int
	var err error

	l.start = l.position
	for {
		r, n, err = l.input.ReadRune()
		if err == io.EOF {
			if l.position != l.start {
				l.emit(itemComment)
			}
			l.emit(itemEOF)
			return nil
		} else if err != nil {
			l.emit(itemError)
			panic(err)
		}

		switch r {
		case '\n':
			if l.position != l.start {
				l.emit(itemComment)
			}
			l.position += int64(n)
			return lexGeneric
		default:
			l.consumeRune(r, n)
		}
	}
}

func lexSection(l *lexer) stateFn {
	var r rune
	var n int
	var err error

	l.start = l.position
	sectionDepth := 0
	startName := false

	for {
		r, n, err = l.input.ReadRune()
		if err == io.EOF {
			l.emit(itemError)
			l.emit(itemEOF)
			return nil
		} else if err != nil {
			l.emit(itemError)
			panic(err)
		}

		if unicode.IsSpace(r) {
			startName = true
			if err := l.input.UnreadRune(); err != nil {
				panic(err)
			}
			l.position++
		}

		switch r {
		case ']':
			if startName {
				l.emit(itemSection)
			}

			l.consumeRune(r, n)

			if sectionDepth < 1 {
				l.emit(itemError)
				return lexGeneric
			}

			l.emit(itemRightSection)
			sectionDepth--

			if sectionDepth == 0 {
				return lexGeneric
			}

		case '[':
			if startName {
				l.consumeRune(r, n)
				continue
			}

			l.consumeRune(r, n)
			l.emit(itemLeftSection)
			sectionDepth++

		case '\n':
			l.consumeRune(r, n)
			l.emit(itemError)
			return lexGeneric

		default:
			startName = true
			l.consumeRune(r, n)
		}
	}
}

func (l *lexer) emit(t itemType) {
	l.tokenStream <- token{
		tokenType: t,
		position:  l.start,
		len:       l.position - l.start,
		value:     l.tokenValBuffer.String(),
	}

	l.start = l.position
	l.tokenValBuffer.Reset()
}

func (l *lexer) skipWhitespace() {
	for {
		r, n, err := l.input.ReadRune()
		if err != nil {
			panic(err)
		}
		if !unicode.IsSpace(r) {
			break
		}

		l.position += int64(n)
	}
	if err := l.input.UnreadRune(); err != nil {
		panic(err)
	}
	l.start = l.position
}

func (l *lexer) consumeRune(r rune, n int) {
	l.position += int64(n)
	l.tokenValBuffer.WriteRune(r)
}

func (l *lexer) takeRunes(accept rune, max int) (taken int, err error) {
	var r rune
	var n int

	for i := 0; i < max; i++ {
		r, n, err = l.input.ReadRune()
		if err != nil {
			return
		}
		if r != accept {
			err = l.input.UnreadRune()
			return
		}
		l.consumeRune(r, n)
		taken++
	}

	return
}
