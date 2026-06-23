package main

import (
	"log"

	"github.com/playwright-community/playwright-go"
)

func main() {
	if err := playwright.Install(); err != nil {
		log.Fatalf("failed to install playwright drivers and browsers: %v", err)
	}
	log.Println("playwright drivers and browsers installed successfully")
}
