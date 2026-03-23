// Package glase supports communication with a ComMarker Omni 1 5W UV
// laser.
package glase

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/google/gousb"
)

// Status* bits. Mostly guessed from traces.
const (
	// Command OK
	statusOK uint16 = 1 << 9
	// Accepting more list commands
	statusReady uint16 = 1 << 5
	// Running running commands
	statusRunning uint16 = 1 << 2
	// Ending sequence (?)
	statusEnding uint16 = 1 << 1
)

// Conn is the connection to the laser machine.
type Conn struct {
	mu       sync.Mutex
	mm2galvo float64
	ctx      *gousb.Context
	devs     []*gousb.Device
	selected int
	config   *gousb.Config
	intf     *gousb.Interface
	in       *gousb.InEndpoint
	out      *gousb.OutEndpoint
	x, y     float64
	status   uint16
}

var (
	// ErrNotFound is returned if no supported device is found.
	ErrNotFound = errors.New("no device found")

	// ErrNotOpen is returned when an attempt is made to use a closed
	// connection.
	ErrNotOpen = errors.New("connection is not established")

	// ErrInvalid provided request data is invalid.
	ErrInvalid = errors.New("invalid request")
)

// forgetConfig cleans up any opened interface and config.
func (c *Conn) forgetConfig() {
	if c.config != nil {
		c.intf.Close()
		c.intf = nil
		c.config.Close()
		c.config = nil
		c.out = nil
		c.in = nil
	}
	c.selected = -1
}

// Close closes an open Omni1 connection. It is an error to close a
// device twice.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil {
		return ErrNotOpen
	}
	c.forgetConfig()
	for _, dev := range c.devs {
		dev.Close()
	}
	c.devs = nil
	err := c.ctx.Close()
	c.ctx = nil
	c.selected = -1
	return err
}

// OpenOmni1 locates the Omni1 in USB space, and opens a connection to
// it.
func OpenOmni1() (*Conn, error) {
	c := &Conn{
		ctx:      gousb.NewContext(),
		mm2galvo: mm2galvo,
		selected: -1,
	}
	var supported = []struct{ vid, pid gousb.ID }{
		{0x9588, 0x9899},
	}
	var err error
	c.devs, err = c.ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		for _, vp := range supported {
			if vp.vid == desc.Vendor && vp.pid == desc.Product {
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

func OpenSim() *Conn {
	return &Conn{
		mm2galvo: mm2galvo,
		selected: -1,
	}
}

// String displays something human readable about the connection.
func (c *Conn) String() string {
	if c == nil {
		return "not connected"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.selected == -1 {
		return "no connection config selected"
	}
	return c.intf.String()
}

// openSelectedDevice opens a specific device - caller is holding
// mutex. At the end of this operation, writes to c will be connected,
// and reads from c will be connected.
func (c *Conn) openSelectedDevice(index int) error {
	c.forgetConfig()
	config, err := c.devs[index].Config(1)
	if err != nil {
		return err
	}
	intf, err := config.Interface(0, 0)
	if err != nil {
		config.Close()
		return err
	}
	in, err := intf.InEndpoint(0x88)
	if err != nil {
		intf.Close()
		config.Close()
		return err
	}
	out, err := intf.OutEndpoint(0x2)
	if err != nil {
		intf.Close()
		config.Close()
		return err
	}
	c.selected = index
	c.config = config
	c.intf = intf
	c.in = in
	c.out = out
	return nil
}

// DeviceByIndex focuses the connection on a specified device. Use
// ListDevices to understand the order.
func (c *Conn) DeviceByIndex(index int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= 0 && index < len(c.devs) {
		return c.openSelectedDevice(index)
	}
	return ErrNotFound
}

// DeviceBySerial focuses the connection on a specified device,
// identifying the device by its USB config serial number. Use
// ListDevices to understand which are visible.
func (c *Conn) DeviceBySerial(serial string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, d := range c.devs {
		s, err := d.SerialNumber()
		if err != nil {
			continue
		}
		if s == serial {
			return c.openSelectedDevice(i)
		}
	}
	return ErrNotFound
}

// Read reads from the opened write endpoint of the DeviceBy...()
// selected product device.
func (c *Conn) Read(buf []byte) (int, error) {
	c.mu.Lock()
	in := c.in
	c.mu.Unlock()
	if in == nil {
		return 0, ErrNotOpen
	}
	return in.Read(buf)
}

// Write writes to the opened write endpoint of the DeviceBy...()
// selected product device.
func (c *Conn) Write(buf []byte) (int, error) {
	c.mu.Lock()
	out := c.out
	c.mu.Unlock()
	if out == nil {
		return 0, ErrNotOpen
	}
	return out.Write(buf)
}

// WARNING: this API will likely change. It is just something to clarify which devices
// are which.
func (c *Conn) ListDevices() (list []string, err error) {
	for i, d := range c.devs {
		m, err := d.Manufacturer()
		if err != nil {
			m = "<?>"
		}
		p, err := d.Product()
		if err != nil {
			p = "<?>"
		}
		s, err := d.SerialNumber()
		if err != nil {
			s = "<?>"
		}
		list = append(list, fmt.Sprintf("%3d: mfg=%q product=%q serial=%q", i, m, p, s))
	}
	return
}

// send sends a command to the device where the caller holds the mutex.
func (c *Conn) send(data []byte) error {
	if len(data) != 12 {
		return ErrInvalid
	}
	for from, n := 0, 0; from < len(data); from += n {
		var err error
		n, err = c.out.Write(data[from:])
		if err != nil {
			return err
		}
	}
	return nil
}

func packCmdArgs(cmd Command, args []uint16) ([]byte, error) {
	data := make([]byte, 12)
	binary.Encode(data, binary.LittleEndian, cmd)
	for i, a := range args {
		if i == 5 {
			return nil, ErrInvalid
		}
		binary.Encode(data[2+2*i:], binary.LittleEndian, a)
	}
	return data, nil
}

// Send sends a command to the device.
func (c *Conn) Send(cmd Command, args ...uint16) error {
	data, err := packCmdArgs(cmd, args)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.send(data)
	return nil
}

// Command is the command type for the laser. See cmds.go for known list.
type Command uint16

// Command sends a command to the device blocks and waits for the response.
func (c *Conn) Command(cmd Command, args ...uint16) ([]byte, error) {
	data, err := packCmdArgs(cmd, args)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.send(data)
	data = make([]byte, 512)
	n, err := c.in.Read(data)
	return data[:n], err
}

// Query executes a command and converts the response into an array of
// uint16s.
func (c *Conn) Query(cmd Command, args ...uint16) ([]uint16, error) {
	data, err := c.Command(cmd, args...)
	if err != nil {
		return nil, err
	}
	ans := make([]uint16, len(data)/2)
	binary.Decode(data, binary.LittleEndian, ans)
	if len(ans) == 4 {
		c.mu.Lock()
		c.status = ans[3]
		c.mu.Unlock()
	}
	return ans, nil
}
