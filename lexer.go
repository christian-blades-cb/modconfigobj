package modconfigobj

import (
	"bytes"
	"fmt"
	"io"
	"unicode"
)

type itemType int

const (
	ItemError itemType = iota

	itemComment // includes hash (#)

	ItemKeyword
	ItemValue   // includes quotes, if those exist
	ItemSection // includes brackets

	ItemEOF
)

func (i itemType) String() string {
	switch i {
	case ItemError:
		return "Error"
	case itemComment:
		return "Comment"
	case ItemKeyword:
		return "Keyword"
	case ItemValue:
		return "Value"
	case ItemSection:
		return "Section"
	case ItemEOF:
		return "EOF"
	default:
		return "DOESNOTEXIST"
	}

}

type token struct {
	TokenType itemType
	Position  int64
	len       int64
	Value     string
}

func (t token) String() string {
	return fmt.Sprintf("token %s at %d: \"%s\"", t.TokenType, t.Position, t.Value)
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

type Lexer struct {
	input          Reader
	tokenValBuffer Buffer
	prevRuneSize   int
	Position       int64
	start          int64
	tokenStream    chan token
	state          stateFn
}

func NewLexer(input Reader) *Lexer {
	return &Lexer{
		state:          lexGeneric,
		input:          input,
		tokenValBuffer: bytes.NewBuffer(nil),
		tokenStream:    make(chan token, 3),
	}
}

func (l *Lexer) NextItem() token {
	for {
		select {
		case t := <-l.tokenStream:
			return t
		default:
			l.state = l.state(l)
		}
	}
}

type stateFn func(*Lexer) stateFn

func lexGeneric(l *Lexer) stateFn {
	l.skipWhitespace()
	l.resetTokenBuffer()

	var r rune
	var err error

	for {
		r, err = l.next()
		if err != nil {
			l.resetTokenBuffer()
			l.emit(ItemEOF)
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
			l.emit(ItemError)
			return lexGeneric
		default:
			l.backup()
			return lexKey
		}
	}
}

func lexKey(l *Lexer) stateFn {
	var r rune
	var err error

	l.resetTokenBuffer()

	for {
		r, err = l.next()
		if err != nil {
			l.emit(ItemError)
			l.emit(ItemEOF)
			return nil
		}

		switch r {
		case '\n':
			l.emit(ItemError)
			return lexGeneric
		case '=':
			if l.Position-int64(l.prevRuneSize) == l.start { // empty key?
				l.emit(ItemError)
				return lexGeneric
			}

			l.backup()
			l.emit(ItemKeyword)
			l.next()
			return lexValue
		}
	}
}

func lexValue(l *Lexer) stateFn {
	l.skipWhitespace()
	l.resetTokenBuffer()

	var r rune
	var err error

	for {
		r, err = l.next()
		if err != nil {
			l.emit(ItemValue)
			l.emit(ItemEOF)
			return nil
		}

		switch r {
		case '"', '\'':
			if l.Position-int64(l.prevRuneSize) == l.start {
				l.backup()
				return lexQuotedValue(r, l)
			}
		case '\n':
			l.backup()
			l.emit(ItemValue)
			l.next()
			return lexGeneric
		}
	}
}

func lexQuotedValue(quoteRune rune, l *Lexer) stateFn {
	var err error

	l.resetTokenBuffer()

	numQuotes, err := l.takeRunes(quoteRune, 3)
	if err != nil {
		l.emit(ItemError)
		l.emit(ItemEOF)
		return nil
	}

	switch numQuotes {
	case 1, 3:
		for {
			endQuotes, err := l.takeRunes(quoteRune, numQuotes)
			if err != nil {
				l.emit(ItemError)
				l.emit(ItemEOF)
				return nil
			}
			if endQuotes == numQuotes {
				l.emit(ItemValue)
				return lexGeneric
			}

			_, err = l.next()
			if err != io.EOF {
				l.emit(ItemError)
				l.emit(ItemEOF)
				return nil
			}
		}
	default:
		l.emit(ItemError)
		return lexGeneric
	}
}

func lexSingleQuote(l *Lexer) stateFn {
	return lexQuotedValue('\'', l)
}

func lexDoubleQuote(l *Lexer) stateFn {
	return lexQuotedValue('"', l)
}

func (l *Lexer) acceptRun(accept rune) (numRunes int, err error) {
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

func (l *Lexer) handleUnexpectedEOF(n int) {
	if l.Position == l.start {
		l.emit(ItemError)
		l.Position += int64(n)
		l.emit(ItemEOF)
	}
}

func lexComment(l *Lexer) stateFn {
	var r rune
	var n int
	var err error

	l.start = l.Position
	for {
		r, n, err = l.input.ReadRune()
		if err == io.EOF {
			if l.Position != l.start {
				l.emit(itemComment)
			}
			l.emit(ItemEOF)
			return nil
		} else if err != nil {
			l.emit(ItemError)
			panic(err)
		}

		switch r {
		case '\n':
			if l.Position != l.start {
				l.emit(itemComment)
			}
			l.Position += int64(n)
			return lexGeneric
		default:
			l.consumeRune(r, n)
		}
	}
}

func lexSection(l *Lexer) stateFn {
	var r rune
	var err error
	var sectionDepth int

	l.resetTokenBuffer()

	sectionDepth, err = l.acceptRun('[')
	if sectionDepth == 0 || err != nil {
		l.emit(ItemError)
		return lexGeneric
	}

	var endSectionRun int
	for {
		endSectionRun, err = l.takeRunes(']', sectionDepth)
		if err != nil {
			l.emit(ItemError)
			l.emit(ItemEOF)
			return nil
		}
		if endSectionRun == sectionDepth {
			l.emit(ItemSection)
			return lexGeneric
		}

		r, err = l.next()
		if err != nil {
			l.emit(ItemError)
			l.emit(ItemEOF)
			return nil
		}

		if r == '\n' {
			l.emit(ItemError)
			return lexGeneric
		}
	}
}

func (l *Lexer) emit(t itemType) {
	l.tokenStream <- token{
		TokenType: t,
		Position:  l.start,
		len:       l.Position - l.start,
		Value:     l.tokenValBuffer.String(),
	}

	l.resetTokenBuffer()
}

func (l *Lexer) skipWhitespace() {
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

func (l *Lexer) consumeRune(r rune, n int) {
	l.Position += int64(n)
	l.tokenValBuffer.WriteRune(r)
}

func (l *Lexer) next() (r rune, err error) {
	var size int
	r, size, err = l.input.ReadRune()
	if err != io.EOF && err != nil {
		l.emit(ItemError)
		panic(err)
	}

	l.consumeRune(r, size)
	l.prevRuneSize = size

	return
}

func (l *Lexer) backup() {
	if l.prevRuneSize == 0 {
		panic("backup called before a call to next")
	}

	err := l.input.UnreadRune()
	if err != nil {
		l.emit(ItemError)
		panic(err)
	}

	l.tokenValBuffer.Truncate(l.tokenValBuffer.Len() - l.prevRuneSize)
	l.Position -= int64(l.prevRuneSize)
	l.prevRuneSize = 0
}

func (l *Lexer) resetTokenBuffer() {
	l.start = l.Position
	l.tokenValBuffer.Reset()
}

func (l *Lexer) takeRunes(accept rune, max int) (taken int, err error) {
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
