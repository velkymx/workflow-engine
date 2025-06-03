package main

import (
	"fmt"
	"jbpmn-engine/db" // Import your db package
)

func main() {
	fmt.Println("Attempting to access db.TimeFormat:")
	fmt.Println(db.TimeFormat) // This line tries to use it
}
