package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly"
)

type TokenMetadata struct {
	Name            string
	Symbol          string
	ContractAddress string
}

const DefaultMetadataFile = "resources/tokenMetadata.json"

func writeMetadataJson(addressToTokenMetadata map[string]TokenMetadata, filename string) error {
	b, err := json.MarshalIndent(addressToTokenMetadata, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling metadata json: %v", err)
	}

	err = ioutil.WriteFile(filename, b, 0644)
	if err != nil {
		return fmt.Errorf("writing metadata json to file: %v", err)
	}

	return nil
}

const ScrapeLoopPeriod = 1 * time.Hour

func parseSymbol(supplyStr string) string {
	split := strings.Split(supplyStr, " ")
	return split[1]
}

func scrapeTokens(outputFile string) {
	log.Println("starting scrape")

	addressToTokenMetadata := make(map[string]TokenMetadata)

	c := colly.NewCollector()

	// Visit each token cell
	c.OnHTML("#ContentPlaceHolder1_divresult tbody tr td:nth-child(3)", func(e *colly.HTMLElement) {
		tokenPageLink := e.ChildAttr("a[href]", "href")
		c.Visit(e.Request.AbsoluteURL(tokenPageLink))
	})

	numPagesCh := make(chan int)
	c.OnHTML("div.col-sm-6 b:nth-child(2)", func(e *colly.HTMLElement) {
		go func() {
			numPages, err := strconv.Atoi(e.Text)
			if err != nil {
				log.Printf("error parsing num pages from text %s: %v", e.Text, err)

				numPagesCh <- 1
				return
			}

			// NOTE: Unfortunately this sends something into the channel on every single page
			numPagesCh <- numPages
		}()
	})

	//Visit each token page and store scraped info
	c.OnHTML("html", func(e *colly.HTMLElement) {
		path := e.Request.URL.Path

		// NOTE: This will break if we visit other types of page with path /token/.*
		isTokenPage, _ := regexp.MatchString(`/token/.*`, path)
		if isTokenPage {
			name := e.ChildText(".breadcrumbs #address")

			supplyStr := e.ChildText("#ContentPlaceHolder1_divSummary tbody tr:nth-child(1) td.tditem")
			symbol := parseSymbol(supplyStr)

			contractAddress := e.ChildText("#ContentPlaceHolder1_trContract td.tditem a")
			contractAddress = strings.ToLower(contractAddress)

			log.Printf("scraper processed token: %s\n", name)

			addressToTokenMetadata[contractAddress] = TokenMetadata{
				Name:            name,
				Symbol:          symbol,
				ContractAddress: contractAddress,
			}
		}
	})

	// Rate limiting to avoid request throttling from Etherscan
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*etherscan.*",
		Parallelism: 1,
		RandomDelay: 2 * time.Second,
	})

	// Kick off scrape by getting number of pages
	c.Visit("https://etherscan.io/tokens?p=1")

	numPages := <-numPagesCh

	// NOTE: Starts at '2' because page 1 is visited prior to this
	for i := 2; i <= numPages; i++ {
		c.Visit(fmt.Sprintf("https://etherscan.io/tokens?p=%d", i))
	}

	writeMetadataJson(addressToTokenMetadata, outputFile)
}

var metadataFile = flag.String("m", DefaultMetadataFile, "metadataFile")

func main() {
	flag.Parse()

	scrapeTokens(*metadataFile)
}
