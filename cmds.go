package glase

import (
	"fmt"
	"log"
	"math"
)

// TODO Observed, but unknown meaning: 0x0003, 0x0008, ?

const (
	DisableLaser         Command = 0x0002
	EnableLaser          Command = 0x0004
	ExecuteList          Command = 0x0005
	SetPwmPulseWidth     Command = 0x0006
	GetVersion           Command = 0x0007
	GetSerialNo          Command = 0x0009 // not clear this is a simple command
	GetListStatus        Command = 0x000a
	GetPositionXY        Command = 0x000c
	GotoXY               Command = 0x000d
	LaserSignalOff       Command = 0x000e
	LaserSignalOn        Command = 0x000f
	WriteCorLine         Command = 0x0010
	ResetList            Command = 0x0012
	RestartList          Command = 0x0013
	WriteCorTable        Command = 0x0015
	SetControlMode       Command = 0x0016
	SetDelayMode         Command = 0x0017
	SetMaxPolyDelay      Command = 0x0018
	SetEndOfList         Command = 0x0019
	SetFirstPulseKiller  Command = 0x001a
	SetLaserMode         Command = 0x001b
	SetTiming            Command = 0x001c
	SetStandby           Command = 0x001d
	SetPwmHalfPeriod     Command = 0x001e
	StopExecute          Command = 0x001f
	StopList             Command = 0x0020
	WritePort            Command = 0x0021
	WriteAnalogPort1     Command = 0x0022
	WriteAnalogPort2     Command = 0x0023
	WriteAnalogPortX     Command = 0x0024
	ReadPort             Command = 0x0025
	SetAxisMotionParam   Command = 0x0026
	SetAxisOriginParam   Command = 0x0027
	AxisGoOrigin         Command = 0x0028
	MoveAxisTo           Command = 0x0029
	GetAxisPos           Command = 0x002a
	GetFlyWaitCount      Command = 0x002b
	GetMarkCount         Command = 0x002d
	SetFpkParam2         Command = 0x002e
	FiberPulseWidth      Command = 0x002f
	FiberGetConfigExtend Command = 0x0030
	InputPort            Command = 0x0031
	SetFlyRes            Command = 0x0032
	Fiber_SetMo          Command = 0x0033
	Fiber_GetStMO_AP     Command = 0x0034
	GetUserData          Command = 0x0036
	GetFlySpeed          Command = 0x0038
	DisableZ             Command = 0x0039
	EnableZ              Command = 0x003a
	SetZData             Command = 0x003b
	SetSPISimmerCurrent  Command = 0x003c
	Reset                Command = 0x0040
	GetMarkTime          Command = 0x0041
	SetFpkParam          Command = 0x0062

	// Empirical - not really sure what this is for.
	PollStepping Command = 0x0069
)

// GetSerial reads the serial number
func (c *Conn) GetSerial() string {
	resp, err := c.Query(GetSerialNo)
	if err != nil {
		log.Fatalf("error reading serial: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sn, err := c.devs[c.selected].SerialNumber()
	if err == nil {
		return fmt.Sprintf("%s-%d.%d", sn, resp[2], resp[1])
	}
	return fmt.Sprintf("?-%d", resp[1:3])
}

// GetVersion reads the FW version of the laser
func (c *Conn) GetVersion() string {
	resp, err := c.Query(GetVersion)
	if err != nil {
		log.Fatalf("error reading version: %v", err)
	}
	return fmt.Sprintf("%d.%d", resp[2], resp[1])
}

// EnableLaser enables the laser
func (c *Conn) EnableLaser() error {
	resp, err := c.Query(EnableLaser)
	if err != nil {
		return fmt.Errorf("error enabling laser: %v", err)
	}
	if resp[3] != 0x220 {
		return fmt.Errorf("bad enable laser response %02x", resp)
	}
	return nil
}

// SetControlMode sets the laser into the specified mode.
func (c *Conn) SetControlMode(mode uint16) error {
	resp, err := c.Query(SetControlMode, mode)
	if err != nil {
		return fmt.Errorf("set control mode error: %v", err)
	}
	if resp[3] != 0x220 {
		return fmt.Errorf("set control mode %02x", resp)
	}
	return nil
}

// convert galvo coordinates to x,y mm coordinates.
func (c *Conn) fXY(ix, iy uint16) (x, y float64) {
	x = float64(int(ix)-0x8000) / c.mm2galvo
	y = float64(int(iy)-0x8000) / c.mm2galvo
	return
}

// GetYX determines the mm coordinate, relative to (0,0), of the
// laser.
func (c *Conn) GetXY() (x, y float64, err error) {
	var resp []uint16
	resp, err = c.Query(GetPositionXY)
	if err != nil {
		return
	}
	if resp[3] != 0x220 {
		err = fmt.Errorf("get xy error %02x", resp)
		return
	}
	c.mu.Lock()
	x, y = c.fXY(resp[2], resp[1])
	c.x, c.y = x, y
	c.mu.Unlock()
	return
}

// iXY converts x,y mm coordinates into galvo coordinates.
func (c *Conn) iXY(x, y float64) (ix, iy uint16) {
	ix = uint16(0x8000 + x*c.mm2galvo)
	iy = uint16(0x8000 + y*c.mm2galvo)
	return
}

// iD computes the distance traveled in galvo units.
func (c *Conn) iD(dx, dy float64) uint16 {
	d := uint(c.mm2galvo * math.Sqrt(dx*dx+dy*dy))
	id := uint16(0xffff)
	if d < 0xffff {
		id = uint16(d)
	}
	return id
}

// GotoXY - conventions for the laser are very backwards.
func (c *Conn) GotoXY(x, y float64) error {
	c.mu.Lock()
	dx, dy := x-c.x, y-c.y
	c.x, c.y = x, y
	iX, iY := c.iXY(x, y)
	id := c.iD(dx, dy)
	c.mu.Unlock()
	_, err := c.Query(GotoXY, iY, iX, 0, id)
	return err
}
