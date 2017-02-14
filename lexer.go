package main

import (
	"bytes"
	"fmt"
	"io"
	"unicode"
)

type itemType int

const (
	itemError itemType = iota

	itemComment // includes hash (#)

	itemKeyword
	itemValue   // includes quotes, if those exist
	itemSection // includes brackets

	itemEOF
)

func (i itemType) String() string {
	switch i {
	case itemError:
		return "Error"
	case itemComment:
		return "Comment"
	case itemKeyword:
		return "Keyword"
	case itemValue:
		return "Value"
	case itemSection:
		return "Section"
	case itemEOF:
		return "EOF"
	default:
		return "DOESNOTEXIST"
	}

}

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
	Truncate(n int)
	Len() int
	WriteRune(rune) (int, error)
	String() string
	Reset()
}

type lexer struct {
	input          Reader
	tokenValBuffer Buffer
	prevRuneSize   int
	position       int64
	start          int64
	tokenStream    chan token
	state          stateFn
}

func newLexer(input Reader) *lexer {
	return &lexer{
		state:          lexGeneric,
		input:          input,
		tokenValBuffer: bytes.NewBuffer(nil),
		tokenStream:    make(chan token, 3),
	}
}

func (l *lexer) nextItem() token {
	for {
		select {
		case t := <-l.tokenStream:
			return t
		default:
			l.state = l.state(l)
		}
	}
}

type stateFn func(*lexer) stateFn

func lexGeneric(l *lexer) stateFn {
	l.skipWhitespace()
	l.resetTokenBuffer()

	var r rune
	var err error

	for {
		r, err = l.next()
		if err != nil {
			l.resetTokenBuffer()
			l.emit(itemEOF)
			return nil
		}

		switch r {
		case '[':
			l.backup()
			return lexSection
		case '#':
			l.backup()
			return lexComment
		case '\n':
			return lexGeneric
		case '=':
			l.emit(itemError)
			return lexGeneric
		default:
			l.backup()
			return lexKey
		}
	}
}

func lexKey(l *lexer) stateFn {
	var r rune
	var err error

	l.resetTokenBuffer()

	for {
		r, err = l.next()
		if err != nil {
			l.emit(itemError)
			l.emit(itemEOF)
			return nil
		}

		switch r {
		case '\n':
			l.emit(itemError)
			return lexGeneric
		case '=':
			if l.position-int64(l.prevRuneSize) == l.start { // empty key?
				l.emit(itemError)
				return lexGeneric
			}

			l.backup()
			l.emit(itemKeyword)
			l.next()
			return lexValue
		}
	}
}

func lexValue(l *lexer) stateFn {
	l.skipWhitespace()
	l.resetTokenBuffer()

	var r rune
	var err error

	for {
		r, err = l.next()
		if err != nil {
			l.emit(itemValue)
			l.emit(itemEOF)
			return nil
		}

		switch r {
		case '"', '\'':
			if l.position-int64(l.prevRuneSize) == l.start {
				l.backup()
				return lexQuotedValue(r, l)
			}
		case '\n':
			l.backup()
			l.emit(itemValue)
			l.next()
			return lexGeneric
		}
	}
}

func lexQuotedValue(quoteRune rune, l *lexer) stateFn {
	var err error

	l.resetTokenBuffer()

	numQuotes, err := l.takeRunes(quoteRune, 3)
	if err != nil {
		l.emit(itemError)
		l.emit(itemEOF)
		return nil
	}

	switch numQuotes {
	case 1, 3:
		for {
			endQuotes, err := l.takeRunes(quoteRune, numQuotes)
			if err != nil {
				l.emit(itemError)
				l.emit(itemEOF)
				return nil
			}
			if endQuotes == numQuotes {
				l.emit(itemValue)
				return lexGeneric
			}

			_, err = l.next()
			if err != io.EOF {
				l.emit(itemError)
				l.emit(itemEOF)
				return nil
			}
		}
	default:
		l.emit(itemError)
		return lexGeneric
	}
}

func lexSingleQuote(l *lexer) stateFn {
	return lexQuotedValue('\'', l)
}

func lexDoubleQuote(l *lexer) stateFn {
	return lexQuotedValue('"', l)
}

func (l *lexer) acceptRun(accept rune) (numRunes int, err error) {
	var r rune

	for {
		r, err = l.next()
		if err != nil {
			return
		}

		if r != accept {
			l.backup()
			return
		}

		numRunes++
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
	var err error
	var sectionDepth int

	l.resetTokenBuffer()

	sectionDepth, err = l.acceptRun('[')
	if sectionDepth == 0 || err != nil {
		l.emit(itemError)
		return lexGeneric
	}

	var endSectionRun int
	for {
		endSectionRun, err = l.takeRunes(']', sectionDepth)
		if err != nil {
			l.emit(itemError)
			l.emit(itemEOF)
			return nil
		}
		if endSectionRun == sectionDepth {
			l.emit(itemSection)
			return lexGeneric
		}

		r, err = l.next()
		if err != nil {
			l.emit(itemError)
			l.emit(itemEOF)
			return nil
		}

		if r == '\n' {
			l.emit(itemError)
			return lexGeneric
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

	l.resetTokenBuffer()
}

func (l *lexer) skipWhitespace() {
	var r rune
	var err error

	for {
		r, err = l.next()
		if err != nil {
			if err == io.EOF {
				return
			}
			panic(err)
		}

		if !unicode.IsSpace(r) {
			l.backup()
			l.resetTokenBuffer()
			return
		}
	}
}

func (l *lexer) consumeRune(r rune, n int) {
	l.position += int64(n)
	l.tokenValBuffer.WriteRune(r)
}

func (l *lexer) next() (r rune, err error) {
	var size int
	r, size, err = l.input.ReadRune()
	if err != io.EOF && err != nil {
		l.emit(itemError)
		panic(err)
	}

	l.consumeRune(r, size)
	l.prevRuneSize = size

	return
}

func (l *lexer) backup() {
	if l.prevRuneSize == 0 {
		panic("backup called before a call to next")
	}

	err := l.input.UnreadRune()
	if err != nil {
		l.emit(itemError)
		panic(err)
	}

	l.tokenValBuffer.Truncate(l.tokenValBuffer.Len() - l.prevRuneSize)
	l.position -= int64(l.prevRuneSize)
	l.prevRuneSize = 0
}

func (l *lexer) resetTokenBuffer() {
	l.start = l.position
	l.tokenValBuffer.Reset()
}

func (l *lexer) takeRunes(accept rune, max int) (taken int, err error) {
	var r rune

	for i := 0; i < max; i++ {
		r, err = l.next()
		if err != nil {
			return
		}

		if r != accept {
			l.backup()
			return
		}

		taken++
	}

	return
}
