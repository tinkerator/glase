package glase

import (
	"fmt"
	"log"
)

// TODO Observed, but unknown meaning: 0x0003, 0x0008, ?

const (
	DisableLaser         Command = 0x0002
	EnableLaser                  = 0x0004
	ExecuteList                  = 0x0005
	SetPwmPulseWidth             = 0x0006
	GetVersion                   = 0x0007
	GetSerialNo                  = 0x0009 // not clear this is a simple command
	GetListStatus                = 0x000a
	GetPositionXY                = 0x000c
	GotoXY                       = 0x000d
	LaserSignalOff               = 0x000e
	LaserSignalOn                = 0x000f
	WriteCorLine                 = 0x0010
	ResetList                    = 0x0012
	RestartList                  = 0x0013
	WriteCorTable                = 0x0015
	SetControlMode               = 0x0016
	SetDelayMode                 = 0x0017
	SetMaxPolyDelay              = 0x0018
	SetEndOfList                 = 0x0019
	SetFirstPulseKiller          = 0x001a
	SetLaserMode                 = 0x001b
	SetTiming                    = 0x001c
	SetStandby                   = 0x001d
	SetPwmHalfPeriod             = 0x001e
	StopExecute                  = 0x001f
	StopList                     = 0x0020
	WritePort                    = 0x0021
	WriteAnalogPort1             = 0x0022
	WriteAnalogPort2             = 0x0023
	WriteAnalogPortX             = 0x0024
	ReadPort                     = 0x0025
	SetAxisMotionParam           = 0x0026
	SetAxisOriginParam           = 0x0027
	AxisGoOrigin                 = 0x0028
	MoveAxisTo                   = 0x0029
	GetAxisPos                   = 0x002a
	GetFlyWaitCount              = 0x002b
	GetMarkCount                 = 0x002d
	SetFpkParam2                 = 0x002e
	FiberPulseWidth              = 0x002f
	FiberGetConfigExtend         = 0x0030
	InputPort                    = 0x0031
	SetFlyRes                    = 0x0032
	Fiber_SetMo                  = 0x0033
	Fiber_GetStMO_AP             = 0x0034
	GetUserData                  = 0x0036
	GetFlySpeed                  = 0x0038
	DisableZ                     = 0x0039
	EnableZ                      = 0x003a
	SetZData                     = 0x003b
	SetSPISimmerCurrent          = 0x003c
	Reset                        = 0x0040
	GetMarkTime                  = 0x0041
	SetFpkParam                  = 0x0062

	PollStepping = 0x0069
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
	y = float64(int(resp[1])-0x8000) / c.mm2galvo
	x = float64(int(resp[2])-0x8000) / c.mm2galvo
	c.mu.Unlock()
	return
}

// GotoYX - conventions for the laser are very backwards.
func (c *Conn) GotoXY(x, y float64) error {
	c.mu.Lock()
	iY := uint16(0x8000 + y*c.mm2galvo)
	iX := uint16(0x8000 + x*c.mm2galvo)
	c.mu.Unlock()
	resp, err := c.Query(GotoXY, iY, iX)
	if err != nil {
		return err
	}
	if resp[3] != 0x220 {
		return fmt.Errorf("goto xy error %02x", resp)
	}
	return nil
}
