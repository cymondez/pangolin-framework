package main

import (
	"fmt"
	"log"
	"os"

	"github.com/cymondez/pangolin-framework/kernel/config"
)

func main() {
	environment := map[string]string{}
	if name, ok := os.LookupEnv("APP_NAME"); ok {
		environment["app.name"] = name
	}
	settings, err := config.NewLoader([]config.Rule{
		{Key: "app.name", Required: true},
	}).Load([]config.Source{
		{Name: "defaults", Values: map[string]string{"app.name": "My App"}},
		{Name: "environment", Values: environment},
	})
	if err != nil {
		log.Fatal(err)
	}
	name, _ := settings.Lookup("app.name")
	fmt.Println("Application:", name)
}
