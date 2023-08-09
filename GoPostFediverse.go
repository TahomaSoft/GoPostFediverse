/* Main code for GoPost to Federate */
// last update 9 Aug 2023


// This file incorporates "by reference" two other files with the 'main' package designator

package main

import (
	"bytes"
	"time"
	"flag"
	"log"
	"net/url"
	"text/template"
	"github.com/SlyMarbo/rss"
)

var (
	yamlfile      *config
	debug         bool
	debugOrDryRun bool
)

type article struct {
	Title, URL, Summary string
}

func main() {
	var (
		configFileName       string
		dryRun               bool
		DesiredTimeOffset    time.Duration
		TimeDifference       time.Duration
		LastProgramRunTime   time.Time
		CurrentTime          time.Time
	)
	flag.StringVar(&configFileName, "c", "gpf.yaml", "the configuration file to use")
	flag.BoolVar(&dryRun, "dry-run", false, "whether to perform a dry run or not")
	flag.BoolVar(&debug, "debug", false, "enable debugging")
	flag.Parse()

	debugOrDryRun = debug || dryRun

	log.Println(configFileName)

	log.Println("gpf starting up...")
	yamlfile = readConfig(configFileName)

	log.Printf("Version: %s\n", yamlfile.Meta.Version)
	log.Printf("Build time: %s\n", yamlfile.Meta.Buildtime)

	var tpls = make(map[string]*template.Template)
	var formats = make(map[string]string)
	for accountIndex, a := range yamlfile.Accounts {
		for feedIndex, f := range a.Feeds {
			tmpl, err := template.New(f.URL).Parse(f.Template)
			if err != nil {
				log.Fatalf("Failed to parse template [%s]. Error: %s", f.Template, err.Error())
			}
			// Default format to "plain", if blank
			if f.Format == "" {
				yamlfile.Accounts[accountIndex].Feeds[feedIndex].Format = "plain"
				f.Format = "plain"
			}
			// log.Printf("Time Jitter", yamlfile.Accounts[accountIndex].Feeds[feedIndex].TimeJitter)
			tpls[f.URL] = tmpl
			formats[f.URL] = f.Format
		}
	}

	for _, a := range yamlfile.Accounts {
		var toot message
		// Get feeds
		log.Printf("Fetching feeds for account [%s]...", a.Name)
		var feeds []*rss.Feed
		for _, source := range a.Feeds {
			toot.feed = source
			if debug {
				log.Printf("source:\n\n%v", source) // same as feedIndex
			}
			feed, err := rss.Fetch(source.URL)
			if err != nil {
				log.Printf("Error fetching %s: %s", source.URL, err.Error())
				continue
			}
			feeds = append(feeds, feed)
			log.Printf("Fetched [%s]", feed.Title)
			// log.Printf("feedno %i", accountIndex)
		}
		if len(feeds) == 0 {
			log.Fatal("Expected at least one feed to successfully fetch.")
		}

		// Loop through feeds
		for _, feed := range feeds {
			// Get feed items
			if len(feed.Items) == 0 {
				log.Printf("Warning: feed %s has no items. \n", feed.Title)
				continue
			}
			// if len(feed.Items) > 1 {
			//	log.Printf("DANGER Warning: feed %s has more than one item: it has %v \n", feed.Title, len(feed.Items))
			//	continue
			// }			

			items := feed.Items
			if len(items) > 1 {
				items = items[:1] //discarding any extra items, I think, by taking a slice from 0:1
				// One feed should really only have one item ?
			}
			base, err := url.Parse(feed.UpdateURL)
			if err != nil {
				log.Fatal("failed parsing update URL of the feed")
			}
			feedLink, _ := url.Parse(feed.Link)
			if err != nil {
				log.Fatal("failed parsing canonical feed URL of the feed")
			}

			// Loop through items
			for feedIndex, item := range items { //simplify this as there is only one item per feed
				// Add time jitter for nws warning feeds
				// If there is no warning, the feed's update time is the same as the time being fetched from the server
				// This creates problems where the nothing burger feed is effectively updated each time and reposted.
				// It is very annoying
				// field time jitter is specified in yaml configuration file with units (seconds, minutes, etc)
				 
				CurrentTime=time.Now()
				DesiredTimeOffset = a.Feeds[feedIndex].TimeJitter
				// log.Println("Current Time: %s TimeOffset: %s \n", CurrentTime, DesiredTimeOffset)
				

				LastProgramRunTime = yamlfile.LastUpdated
				TimeDifference = CurrentTime.Sub(item.Date)
				// LastUpdatedTime = LastUpdatedTime.Add(TimeOffset)
				// log.Println("Time difference is: %s \n", TimeDifference)

				// Skip an item if the feed item is dated before the last time the program is run AND not debug AND feed time is not within TimeOffset of current clock time

				// Print these various time items
				log.Printf("Current Time: %s  LastProgramRunTime: %s LastItemTime: %s", CurrentTime, LastProgramRunTime, item.Date)

				
				// if item.Date.Before(LastProgramRunTime) && !debug && (TimeDifference <= DesiredTimeOffset) {
				//	log.Println("No new items. Skipping.")
				//	continue
				// }
				
				
				
				if item.Date.Before(LastProgramRunTime) && !debug {
					log.Println("No new items. Skipping.")
					continue

				} else if TimeDifference <= DesiredTimeOffset {
					log.Println("Item time is close to fetch time")
					continue

				} else {

					itemLink, err := url.Parse(item.Link)
					if err != nil {
						log.Fatal("failed parsing article URL of the feed item")
					}
					
					// Make sure data looks OK
					// log.Printf("Item Data:\n\tTimestamp: %s\n\tSite URL: %s\n\tFeed Title: %s\n\tItem Title: %s\n\tItem URL: %s\n",
					log.Printf("\n Item Data:\n\tTimestamp: %s\n\tSite URL: %s\n\tFeed Title: %s\n\tItem Title: %s\n\tItem URL: %s\n",
						item.Date, base.ResolveReference(feedLink).String(),
						feed.Title, item.Title, base.ResolveReference(itemLink).String())
					i := article{
						Title:   item.Title,
						Summary: item.Summary,
						URL:     base.ResolveReference(itemLink).String(),
					}
					buf := new(bytes.Buffer)
					err = tpls[base.String()].Execute(buf, i)
					if err != nil {
						log.Fatalf("Error executing template [%v]. Error: %s", tpls[base.String()], err.Error())
					}
					// toot.account = account
					toot.account = a
					toot.content = buf.String()
					if err = toot.post(); err != nil {
						log.Fatalf("Failed to post message \"%s\". Error: %s", buf.String(), err.Error())
					}
				}
			}
		}
	}

	if !debugOrDryRun {
		// update timestamp in config
		yamlfile.updateLastUpdated()
		// save yamlfile configuration, updating last accessed time and tweaking parameters (which i don't like)
		err := yamlfile.Save()
		if err != nil {
			log.Fatalf("Failed to save config to file. Error: %s", err.Error())
		}
	}
}
