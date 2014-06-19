package main

import (
	"code.google.com/p/go-charset/charset"
	_ "code.google.com/p/go-charset/data"
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

type stateFn func(*lexer) stateFn

const headerStart = "=== "
const headerEnd = " === [Total Buy/Sell Value: "
const eof = -1
const cardStarts = "ABCDEFGHIJKLMNOPQRSTUVWXYZ\u00C6"
const cardSetStart = " ["
const cardSetEnd = "]  "
const digits = "0123456789"

type itemType int

const (
	itemError itemType = iota // error occurred;
	itemEOF
	itemEOL
	itemSetName
	itemCardName
	itemSetPrefix
	itemNumber
	itemBuyPrice
	itemSellPrice
	itemBotName
	itemBotCount
)

// item represents a token returned from the scanner.
type item struct {
	typ itemType // Type, such as itemNumber.
	val string   // Value, such as "23.2".
}

type card struct {
	name      string
	set       string
	buyPrice  int
	sellPrice int
	stock     int
}

type lexer struct {
	name      string // used only for error reports.
	input     string // the string being scanned.
	start     int    // start position of this item.
	lineStart int
	pos       int       // current position in the input.
	width     int       // width of last rune read from input.
	items     chan item // channel of scanned items.
	hasHeader bool      // have we seen a header yet?
	state     stateFn
}

func (i item) String() string {
	switch i.typ {
	case itemEOF:
		return "EOF"
	case itemEOL:
		return "EOL"
	case itemError:
		return i.val
	}
	return fmt.Sprintf("%d: %q", i.typ, i.val)
}

func (l *lexer) next() (r rune) {
	if l.pos >= len(l.input) {
		l.width = 0
		return eof
	}
	r, l.width =
		utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += l.width
	return r
}

// acceptRun consumes a run of runes from the valid set.
func (l *lexer) acceptRun(valid string) {
	for strings.IndexRune(valid, l.next()) >= 0 {
	}
	l.backup()
}

// accept consumes the next rune
// if it's from the valid set.
func (l *lexer) accept(valid string) bool {
	if strings.IndexRune(valid, l.next()) >= 0 {
		return true
	}
	l.backup()
	return false
}

func lexCardPrices(l *lexer) stateFn {
	for {
		switch r := l.next(); {
		case r == ' ':
			l.ignore()
		case '0' <= r && r <= '9':
			l.backup()
			return lexPrice
		case r == '\n':
			return lexStart
		case r == eof:
			l.emit(itemEOF)
			return nil
		}
	}
	return nil
}

func lexSetPrefix(l *lexer) stateFn {
	for {
		if strings.HasPrefix(l.input[l.pos:], cardSetEnd) {
			if l.pos > l.start {
				l.emit(itemSetPrefix)
			}
			l.pos += len(cardSetEnd)
			l.ignore()

			return lexCardPrices
		}
		if l.next() == eof {
			break
		}
	}
	l.emit(itemEOF)
	return nil
}

func lexCardName(l *lexer) stateFn {
	for {
		if strings.HasPrefix(l.input[l.pos:], cardSetStart) {
			if l.pos > l.start {
				l.lineStart = l.start
				l.emit(itemCardName)
			}
			l.pos += len(cardSetStart)
			l.ignore()

			return lexSetPrefix
		}

		if l.next() == eof {
			break
		}
	}
	l.emit(itemEOF)
	return nil
}

func lexStart(l *lexer) stateFn {
	for {
		if strings.HasPrefix(l.input[l.pos:], headerStart) {
			l.pos += len(headerStart)
			l.ignore()
			return lexSetName
		}

		if l.hasHeader && l.accept(cardStarts) {
			l.backup()
			l.ignore()
			return lexCardName
		}

		if l.next() == eof {
			break
		}
	}

	l.emit(itemEOF)
	return nil
}

func lexPrice(l *lexer) stateFn {
	typ := itemSellPrice
	if l.start-l.lineStart <= 44 {
		typ = itemBuyPrice
	}

	l.acceptRun(digits)
	if l.accept(".") {
		l.acceptRun(digits)
	}
	l.emit(typ)

	if typ == itemBuyPrice {
		return lexCardPrices
	}

	return lexBots
}

func lexBots(l *lexer) stateFn {
	for {
		l.acceptRun(" ")
		l.ignore()

		if l.peek() == '\n' {
			l.next()
			l.ignore()
			return lexStart
		}

		return lexBotName
	}
}

func lexBotName(l *lexer) stateFn {
	for {
		if l.accept("mCsptb") {
			l.accept("rs1236s")
			l.emit(itemBotName)
			return lexBotCount
		}
	}
}

func lexBotCount(l *lexer) stateFn {
	for {
		if l.accept("[") {
			l.ignore()
			l.acceptRun(digits)
			l.emit(itemBotCount)
			if l.accept("]") {
				l.ignore()
				return lexBots
			}
		}
	}
}

func lexNumberPair(l *lexer) stateFn {
	lexNumber(l)
	if l.accept("/") {
		l.ignore()
		lexNumber(l)
	}

	return lexSetEndOfLine
}

func lexSetEndOfLine(l *lexer) stateFn {
	for {
		if l.accept("]") {
			if l.accept(" ") {
				l.acceptRun("=")
				if l.next() == '\n' {
					l.emit(itemEOL)
					return lexStart
				}
			}
		}
	}

	return nil
}

func lexNumber(l *lexer) stateFn {
	l.acceptRun(digits)
	l.emit(itemNumber)
	return nil
}

func lexSetName(l *lexer) stateFn {
	for {
		if strings.HasPrefix(l.input[l.pos:], headerEnd) {
			l.emit(itemSetName)
			l.hasHeader = true
			l.pos += len(headerEnd)
			l.ignore()

			return lexNumberPair
		}
		if l.next() == eof {
			break
		}
	}

	l.emit(itemEOF)
	return nil
}

// ignore skips over the pending input before this point.
func (l *lexer) ignore() {
	l.start = l.pos
}

// backup steps back one rune.
// Can be called only once per call of next.
func (l *lexer) backup() {
	l.pos -= l.width
}

// peek returns but does not consume
// the next rune in the input.
func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

func (l *lexer) nextItem() item {
	for {
		select {
		case item := <-l.items:
			return item
		default:
			l.state = l.state(l)
		}
	}
	panic("not reached")
}

func (l *lexer) emit(t itemType) {
	l.items <- item{t, l.input[l.start:l.pos]}
	l.start = l.pos
}

func lex(name, input string) *lexer {
	l := &lexer{
		name:  name,
		input: input,
		state: lexStart,
		items: make(chan item, 2), // Two items sufficient.
	}
	return l
}

func (c *card) save(stmt *sql.Stmt, ts string) {
	_, err := stmt.Exec(c.name, c.set, c.buyPrice, c.sellPrice, c.stock, ts)

	if err != nil {
		log.Fatal(err)
	}
}

func strToFixed(s string) int {
	r := strings.Split(s, ".")

	a, err := strconv.ParseInt(r[0], 10, 32)

	if err != nil {
		log.Fatal(err)
	}

	a *= 1000

	x := 1000

	if len(r) > 1 {
		i := len(r[1])

		for i > 0 {
			x /= 10
			i--
		}

		b, err := strconv.ParseInt(r[1], 10, 32)

		if err != nil {
			log.Fatal(err)
		}

		a += int64(x) * b
	}

	return int(a)
}

func main() {
	db, err := sql.Open("sqlite3", "./card.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	info, err := os.Stat("prices_0.txt")
	if err != nil {
		log.Fatal(err)
	}

	ts := info.ModTime().UTC()

	fi, err := ioutil.ReadFile("prices_0.txt")

	if err != nil {
		fmt.Println("Error fetching: %v\n", err)
		return
	}

	iso_prices, err := charset.NewReader("iso-8859-1", strings.NewReader(string(fi)))

	if err != nil {
		log.Fatal(err)
	}

	prices, err := ioutil.ReadAll(iso_prices)

	if err != nil {
		log.Fatal(err)
	}

	l := lex("start", string(prices))

	c := &card{}

	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	stmt, err := tx.Prepare("insert into card_prices(name, set_name, buy, sell, stock, ts) values(?, ?, ?, ?, ?, ?)")

	if err != nil {
		log.Fatal(err)
	}

	defer stmt.Close()

	for {
		item := l.nextItem()

		switch item.typ {
		case itemCardName:
			if c.name != "" {
				c.save(stmt, ts.String())
				c = &card{}
			}
			c.name = item.val
		case itemSetPrefix:
			c.set = item.val
		case itemBuyPrice:
			c.buyPrice = strToFixed(item.val)
		case itemSellPrice:
			c.sellPrice = strToFixed(item.val)
		case itemBotCount:
			stock, err := strconv.ParseInt(item.val, 10, 32)

			if err != nil {
				log.Fatal(err)
			}

			c.stock += int(stock)
		}

		if item.typ == itemEOF {
			break
		}
	}

	tx.Commit()
}
