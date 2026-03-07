package main

import (
	"log"

	"zappem.net/pub/io/glase"
)

func main() {
	conn, err := glase.OpenOmni1()
	if err != nil {
		log.Fatalf("Failed to detect Omni 1 Laser: %v", err)
	}
	defer conn.Close()

	log.Printf("connection: %#v", *conn)
}
