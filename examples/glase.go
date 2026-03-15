package main

import (
	"flag"
	"log"
	"math"
	"os"
	"time"

	"zappem.net/pub/io/glase"
)

var (
	info   = flag.Bool("info", false, "list the discovered laser devices")
	serial = flag.String("serial", "", "serial numbered device to connect to")
	x      = flag.Int("x", 0, "together with --move set X relative offset to this value")
	y      = flag.Int("y", 0, "together with --move set Y relative offset to this value")
	move   = flag.Bool("move", false, "move the laser to --x --y vs the center point")
	on     = flag.Bool("on", false, "turn on the laser")
	off    = flag.Bool("off", false, "turn off the laser")
	cor    = flag.String("cor", "./jcz150.cor", "correction data file")
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

	conn.GetSerial()
	conn.GetVersion()
	data, err := os.ReadFile(*cor)
	if err != nil {
		log.Fatalf("failed to read %q: %v", *cor, err)
	}
	if err := conn.UploadCorrections(data); err != nil {
		log.Fatalf("failed to upload corrections %q: %v", *cor, err)
	}
	log.Print("correction data loaded")
	conn.EnableLaser()
	conn.SetControlMode(0)
	const steps = 100
	const ang = 2 * math.Pi / steps
	const r = 16384.0
	for i := 0; i < steps; i++ {
		a := ang * float64(i)
		x, y := r*math.Cos(a), r*math.Sin(a)
		conn.GotoYX(y, x)
		time.Sleep(200 * time.Millisecond)
	}
	conn.GetYX()
	conn.GotoYX(0, 0)
}
