package main

import (
	"code.google.com/p/go-charset/charset"
	_ "code.google.com/p/go-charset/data"
	"database/sql"
	"flag"
	"fmt"
	"github.com/coopernurse/gorp"
	_ "github.com/mattn/go-sqlite3"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
)

type Card struct {
	Rowid     int64 `db:"Rowid"`
	Name      string
	SetName   string `db:"set_name"`
	BuyPrice  int    `db:"buy"`
	SellPrice int    `db:"sell"`
	Stock     int
	Clean     bool
	Ts        string
}

func checkErr(err error, msg string) {
	if err != nil {
		log.Fatalln(msg, err)
	}
}

func (c *Card) save(dbmap *gorp.DbMap, ts string) {
	c.Ts = ts
	err := dbmap.Insert(c)
	checkErr(err, "Save failed")
}

func (c *Card) same_name(t *Card) bool {
	return c.Name == t.Name && c.SetName == t.SetName
}

func (c *Card) same_details(t *Card) bool {
	return c.same_name(t) && c.BuyPrice == t.BuyPrice &&
		c.SellPrice == t.SellPrice && c.Stock == t.Stock
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

var shouldClean bool
var shouldTrace bool

func init() {
	flag.BoolVar(&shouldClean, "clean", false, "Clean the database instead of loading new prices")
	flag.BoolVar(&shouldTrace, "trace", false, "Trace sql commands")
}

func cleanCard(name, set string, stmt *sql.Stmt) {
	fmt.Println(name, set)
	rows, err := stmt.Query(name, set)

	if err != nil {
		log.Fatal(err)
	}

	defer rows.Close()

	for rows.Next() {
		var rowid, buy, sell, stock int

		err := rows.Scan(&rowid, &buy, &sell, &stock)

		if err != nil {
			log.Fatal(err)
		}

		fmt.Println(rowid, buy, sell, stock)
	}
}

func clean(db *gorp.DbMap) {
	var cards []Card

	_, err := db.Select(&cards, "select rowid,* from card_prices order by name, set_name, ts")

	checkErr(err, "Selecting cards")

	fmt.Printf("Found %d cards\n", len(cards))

	if len(cards) < 1 {
		return
	}

	last_card := cards[0]

	for _, card := range cards[1:] {
		if card.same_name(&last_card) {
			if card.same_details(&last_card) {
				db.Delete(&card)
			}

			if !last_card.Clean {
				last_card.Clean = true
				db.Update(&last_card)
			}
		}

		last_card = card
	}
}

func initDb() *gorp.DbMap {
	db, err := sql.Open("sqlite3", "./card.db")
	checkErr(err, "sql.Open failed")

	dbmap := &gorp.DbMap{Db: db, Dialect: gorp.SqliteDialect{}}

	dbmap.AddTableWithName(Card{}, "card_prices").SetKeys(true, "Rowid")

	err = dbmap.CreateTablesIfNotExists()
	checkErr(err, "Creating tables")

	if shouldTrace {
		dbmap.TraceOn("[gorp]", log.New(os.Stdout, "mtgprices:", log.Lmicroseconds))
	}

	return dbmap
}

func main() {
	flag.Parse()

	dbmap := initDb()
	defer dbmap.Db.Close()

	if shouldClean {
		clean(dbmap)
		return
	}

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

	c := &Card{}

	for {
		item := l.nextItem()

		switch item.typ {
		case itemCardName:
			if c.Name != "" {
				c.save(dbmap, ts.String())
				c = &Card{}
			}
			c.Name = item.val
		case itemSetPrefix:
			c.SetName = item.val
		case itemBuyPrice:
			c.BuyPrice = strToFixed(item.val)
		case itemSellPrice:
			c.SellPrice = strToFixed(item.val)
		case itemBotCount:
			stock, err := strconv.ParseInt(item.val, 10, 32)

			if err != nil {
				log.Fatal(err)
			}

			c.Stock += int(stock)
		}

		if item.typ == itemEOF {
			break
		}
	}
}
