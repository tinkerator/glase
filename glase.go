// Package glase supports communication with a ComMarker Omni 1 5W UV
// laser.
package glase

import (
	"errors"
	"log"
	"sync"

	"github.com/google/gousb"
)

// Conn is the connection to the laser machine.
type Conn struct {
	mu   sync.Mutex
	ctx  *gousb.Context
	devs []*gousb.Device
}

var (
	// ErrNotFound is returned if no supported device is found.
	ErrNotFound = errors.New("no device found")

	// ErrNotOpen is returned when an attempt is made to use a closed
	// connection.
	ErrNotOpen = errors.New("connection is not established")
)

// OpenOmni1 locates the Omni1 in USB space, and opens a connection to
// it.
func OpenOmni1() (*Conn, error) {
	c := &Conn{
		ctx: gousb.NewContext(),
	}
	var supported = []struct{ vid, pid gousb.ID }{
		{0x9588, 0x9899},
	}
	var err error
	c.devs, err = c.ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		for _, vp := range supported {
			if vp.vid == desc.Vendor && vp.pid == desc.Product {
				log.Printf("Found desc: %#v", desc)
				return true
			}
		}
		return false
	})
	if err != nil || len(c.devs) == 0 {
		c.ctx.Close()
		if err == nil {
			err = ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

// Close closes an open Omni1 connection. It is an error to close a
// device twice.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil {
		return ErrNotOpen
	}
	for _, dev := range c.devs {
		dev.Close()
	}
	err := c.ctx.Close()
	c.ctx = nil
	return err
}
