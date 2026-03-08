package main

import (
	"flag"
	"log"

	"zappem.net/pub/io/glase"
)

var (
	info   = flag.Bool("info", false, "list the discovered laser devices")
	serial = flag.String("serial", "", "serial numbered device to connect to")
)

func main() {
	flag.Parse()

	conn, err := glase.OpenOmni1()
	if err != nil {
		log.Fatalf("Failed to detect Omni 1 Laser: %v", err)
	}
	defer conn.Close()

	if *info {
		list, err := conn.ListDevices()
		if err != nil {
			log.Fatalf("Unable to list devices: %v", err)
		}
		for _, s := range list {
			log.Print(s)
		}
		return
	}

	if *serial != "" {
		if err := conn.DeviceBySerial(*serial); err != nil {
			log.Fatalf("No device found with serial number, %q: %v", *serial, err)
		}
	} else {
		if err := conn.DeviceByIndex(0); err != nil {
			log.Fatalf("No 0-device found: %v", err)
		}
	}

	log.Printf("Connected to: %v", conn.String())
}
