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
	cor    = flag.String("cor", "./jcz150.cor", "correction data file")
	scale  = flag.Float64("mm2gal", 400.0, "number of galvo units to mm")
	gotoX  = flag.Float64("x", 0.0, "x coordinate at end of run")
	gotoY  = flag.Float64("y", 0.0, "y coordinate at end of run")
	circle = flag.Bool("circle", true, "step out a circle with the laser")
)

func main() {
	flag.Parse()

	conn, err := glase.OpenOmni1()
	if err != nil {
		log.Fatalf("Failed to detect Omni 1 Laser: %v", err)
	}
	defer conn.Close()
	conn.SetMM2Galvo(*scale)
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

	log.Printf("Serial number: %q", conn.GetSerial())
	log.Printf("Version: %q", conn.GetVersion())

	data, err := os.ReadFile(*cor)
	if err != nil {
		log.Fatalf("Failed to read %q: %v", *cor, err)
	}
	if err := conn.UploadCorrections(data); err != nil {
		log.Fatalf("Failed to upload corrections %q: %v", *cor, err)
	}
	log.Printf("Correction data loaded from %q", *cor)

	if err := conn.EnableLaser(); err != nil {
		log.Fatalf("Failed to enable laser: %v", err)
	}
	if err := conn.SetControlMode(0); err != nil {
		log.Fatalf("Failed to set control mode: %v", err)
	}

	if *circle {
		const steps = 100
		const ang = 2 * math.Pi / steps
		const r = 40.0 // mm
		for i := 0; i < steps; i++ {
			a := ang * float64(i)
			x, y := r*math.Cos(a), r*math.Sin(a)
			conn.GotoXY(x, y)
			time.Sleep(200 * time.Millisecond)
		}
	}

	x, y, err := conn.GetXY()
	if err != nil {
		log.Fatalf("failed to read XY: %v", err)
	}
	log.Printf("current XY=(%f, %f)", x, y)
	conn.GotoXY(*gotoX, *gotoY)
	time.Sleep(500 * time.Millisecond)
	x, y, err = conn.GetXY()
	if err != nil {
		log.Fatalf("failed to read XY: %v", err)
	}
	log.Printf("final XY=(%f, %f)", x, y)
}
