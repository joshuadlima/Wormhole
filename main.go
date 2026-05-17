package main

import (
	"github.com/joho/godotenv"
	"github.com/joshuadlima/Wormhole/cmd"
)

func main() {
	godotenv.Load()
	cmd.Execute()
}
