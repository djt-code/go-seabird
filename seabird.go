package main

import (
	"log"
	"os"

	"github.com/belak/seabird/bot"

	// Load plugins
	//_ "github.com/belak/seabird/auth"
	_ "github.com/seabird-irc/plugins"
	_ "github.com/seabird-irc/plugins/linkproviders"

	// Load DB drivers
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	conf := os.Getenv("SEABIRD_CONFIG")
	if conf == "" {
		log.Fatalln("$SEABIRD_CONFIG is not defined")
	}

	// Create the bot
	b, err := bot.NewBot(conf)
	if err != nil {
		log.Fatalln(err)
	}

	// Run the bot
	err = b.Run()
	if err != nil {
		log.Fatalln(err)
	}
}
