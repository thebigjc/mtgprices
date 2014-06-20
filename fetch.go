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
)

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
