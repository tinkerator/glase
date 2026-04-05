package glase

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// MaxListCommands is the capacity of a command list.
const MaxListCommands = 256

const (
	JumpTo          uint16 = 0x8001
	EndOfList       uint16 = 0x8002
	DelayTime       uint16 = 0x8004
	MarkTo          uint16 = 0x8005
	JumpSpeed       uint16 = 0x8006
	LaserOnDelay    uint16 = 0x8007
	LaserOffDelay   uint16 = 0x8008
	MarkFrequency   uint16 = 0x800a
	MarkPowerRatio  uint16 = 0x800b
	MarkSpeed       uint16 = 0x800c
	JumpDelay       uint16 = 0x800d
	PolygonDelay    uint16 = 0x800f
	SetCo2FPK       uint16 = 0x801e
	ChangeMarkCount uint16 = 0x8023
	ReadyMark       uint16 = 0x8051
)

var encoder = map[string]uint16{
	"JumpTo":          JumpTo,
	"EndOfList":       EndOfList,
	"DelayTime":       DelayTime,
	"MarkTo":          MarkTo,
	"JumpSpeed":       JumpSpeed,
	"LaserOnDelay":    LaserOnDelay,
	"LaserOffDelay":   LaserOffDelay,
	"MarkFrequency":   MarkFrequency,
	"MarkPowerRatio":  MarkPowerRatio,
	"MarkSpeed":       MarkSpeed,
	"JumpDelay":       JumpDelay,
	"PolygonDelay":    PolygonDelay,
	"SetCo2FPK":       SetCo2FPK,
	"ChangeMarkCount": ChangeMarkCount,
	"ReadyMark":       ReadyMark,
}

// argument types
type argType int

const (
	atDisplacement argType = iota
	atDistance
	atDuration
	atFrequency
	atInt
	atPower
	atSpeed
	atAngle
)

// Detail holds a lookup table for decoding an assembled command.
type Detail struct {
	name   string
	desc   string
	opcode uint16
	args   []argType
}

// decoder holds a decoding map for each supported opcode.
var decoder = map[uint16]Detail{
	JumpTo: {
		name:   "JumpTo",
		desc:   "Move the laser to target a specific point without marking.",
		opcode: JumpTo,
		args:   []argType{atDisplacement, atDisplacement, atAngle, atDistance},
	},
	EndOfList: {
		name:   "EndOfList",
		opcode: EndOfList,
		desc:   "Indicates the end of the command list sequence. Typically used to pad the list to 256 entries.",
	},
	DelayTime: {
		name:   "DelayTime",
		desc:   "This is a delay inserted into the command sequence. Equivalent to time.Sleep()",
		opcode: DelayTime,
		args:   []argType{atDuration},
	},
	MarkTo: {
		name:   "MarkTo",
		desc:   "Move the laser to target a specific point, lasing along the way.",
		opcode: MarkTo,
		args:   []argType{atDisplacement, atDisplacement, atAngle, atDistance},
	},
	JumpSpeed: {
		name:   "JumpSpeed",
		desc:   "Configures future jumps to execute at this speed. Units assumed to be galvo units per millisecond.",
		opcode: JumpSpeed,
		args:   []argType{atSpeed},
	},
	LaserOnDelay: {
		name:   "LaserOnDelay",
		desc:   "Relative to start of mirror motion, delay this number of microseconds to activate laser.",
		opcode: LaserOnDelay,
		args:   []argType{atDuration},
	},
	LaserOffDelay: {
		name:   "LaserOffDelay",
		desc:   "Relative to start of completion of marking motion, delay before turning the laser off.",
		opcode: LaserOffDelay,
		args:   []argType{atDuration},
	},
	MarkFrequency: {
		name:   "MarkFrequency",
		desc:   "Frequency of laser pulses. Less frequent pulses deliver more power.",
		opcode: MarkFrequency,
		args:   []argType{atFrequency},
	},
	MarkPowerRatio: {
		name:   "MarkPowerRatio",
		desc:   "For the UV laser, this is the Q-Pulse width in ns (compare to 1/freqency) for power ratio.",
		opcode: MarkPowerRatio,
		args:   []argType{atPower},
	},
	MarkSpeed: {
		name:   "MarkSpeed",
		desc:   "Configures future marking to execute at this speed. Units assumed to be galvo units per millisecond.",
		opcode: MarkSpeed,
		args:   []argType{atSpeed},
	},
	JumpDelay: {
		name:   "JumpDelay",
		desc:   "Microseconds to wait after a jump to let mirrors settle.",
		opcode: JumpDelay,
		args:   []argType{atDuration},
	},
	PolygonDelay: {
		name:   "PolygonDelay",
		desc:   "Microseconds to wait between segments of a set of connected lines. The laser continues to be on through this delay.",
		opcode: PolygonDelay,
		args:   []argType{atDuration},
	},
	SetCo2FPK: {
		name:   "SetCo2FPK",
		desc:   "Microsecond delay to suppress (First-Pulse-Killer) the initial power surge of the laser when starting to mark.",
		opcode: SetCo2FPK,
		args:   []argType{atDuration, atDuration},
	},
	ChangeMarkCount: {
		name:   "ChangeMarkCount",
		desc:   "Change the number of times a subsequent sequence is executed.",
		opcode: ChangeMarkCount,
		args:   []argType{atInt},
	},
	ReadyMark: {
		name:   "ReadyMark",
		desc:   "Wait for an external signal to start rest of program. Likely the foot pedal.",
		opcode: ReadyMark,
	},
}

// Assembled is an assembled line.
type Assembled [12]byte

// Disassemble generates a string representation what is known about the assembled command.
func (c *Conn) Disassemble(a Assembled) string {
	cmdargs := make([]uint16, 6)
	binary.Decode(a[:], binary.LittleEndian, cmdargs)
	d, ok := decoder[cmdargs[0]]
	if !ok {
		return fmt.Sprintf("unknown:%v", cmdargs)
	}
	var parts []string
	lastZero := 6
	for i := 5; i > 0; i-- {
		if cmdargs[i] != 0 {
			break
		}
		lastZero = i - 1
	}
	for i := 0; i < 5; i++ {
		if i >= lastZero && i >= len(d.args) {
			break
		}
		v := cmdargs[1+i]
		if i < len(d.args) {
			var s string
			switch d.args[i] {
			case atAngle:
				if v == 0x8000 {
					s = ">= 90 deg"
				} else {
					s = fmt.Sprintf("%.2f deg", float64(v)*90.0/float64(0x8000))
				}
			case atDisplacement:
				x, _ := c.fXY(v, 0)
				s = fmt.Sprintf("%.3f mm", x)
			case atDistance:
				x := float64(v) / c.mm2galvo
				s = fmt.Sprintf("%.3f mm", x)
			case atDuration:
				s = (time.Duration(time.Microsecond) * time.Duration(v)).String()
			case atFrequency:
				s = fmt.Sprintf("%.0f kHz", float64(v)*40/250)
			case atInt:
				s = fmt.Sprintf("%d", v)
			case atPower:
				s = fmt.Sprintf("%.2f ns", float64(v)/20)
			case atSpeed:
				x := float64(v) / c.mm2galvo * 200000 / 256
				s = fmt.Sprintf("%.0f mm/s", x)
			default:
				s = fmt.Sprintf("?%d", v)
			}
			parts = append(parts, s)
		} else {
			parts = append(parts, fmt.Sprintf("?0x%x", v))
		}
	}
	return fmt.Sprint(d.name, "\t", strings.Join(parts, ", "))
}

// ParseListCommand parses a string and generates an Assembled command.
func (c *Conn) ParseListCommand(asm string) (a Assembled, err error) {
	if d, err2 := hex.DecodeString(asm); err2 == nil {
		if len(d) != 12 {
			err = fmt.Errorf("%q is not 12 bytes long", asm)
			return
		}
		copy(a[:], d)
		return
	}
	fs := strings.Fields(asm)
	if len(fs) < 1 {
		err = fmt.Errorf("%q has no fields", asm)
		return
	}
	opcode, ok := encoder[fs[0]]
	if !ok {
		err = fmt.Errorf("%q is not recognized in %q", fs[0], asm)
	}

	err = fmt.Errorf("TODO not done yet %04x with %q", opcode, asm)
	return
}

type List struct {
	c        *Conn
	mu       sync.Mutex
	err      error
	pts      int
	x0, y0   float64
	x1, y1   float64
	commands []Assembled
}

// NewList defines a command list associated with the connection. The
// connection, as needed, provides detailed parameters specific to the
// connected device.
func (c *Conn) NewList() *List {
	return &List{
		c: c,
	}
}

// Stream sends a stream of assembled commands over the returned
// channel. This will mutex Lock the list while streaming.
func (list *List) Stream() <-chan Assembled {
	as := make(chan Assembled)
	go func() {
		defer close(as)
		list.mu.Lock()
		defer list.mu.Unlock()
		for _, a := range list.commands {
			as <- a
		}
	}()
	return as
}

// pack packs a command sequence into a list of commands. It returns
// list, which allows (*List).pack()s to be chained. This method is
// called with the list mutex locked. There is no limit when building
// a list, although rendering the list to the laser packages lists
// with 256 entry sub-lists, padding with an end-of-list command.
func (list *List) pack(cmdargs ...uint16) *List {
	var data Assembled
	binary.Encode(data[:], binary.LittleEndian, cmdargs)
	list.commands = append(list.commands, data)
	return list
}

// Profile holds data on the laser settings to be used.
type Profile struct {
	// MarkFrequency is the kHz frequency.
	MarkFrequency float64
	// SetCo2FPK1 & 2 are initial power-up protections.
	SetCo2FPK1, SetCo2FPK2 time.Duration
	// Q-Power in ns (compare to 1/frequency) for power concentration.
	// 1/40kHz = 25000 ns.
	MarkPowerRatio time.Duration
	// JumpSpeed and MarkSpeed are the mm/sec galvo movement speeds.
	JumpSpeed, MarkSpeed float64
	// LaserOnDelay, LaserOffDelay and PolygonDelay are latencies
	// for dwell time at start, mid points and end of marked
	// lines. If LaserOnDelay == 0, then get low power "pointer"
	// mode.
	LaserOnDelay, LaserOffDelay, PolygonDelay time.Duration
	// JumpDelay is how long to dwell at the corners of successive
	// lines (to allow galvos to settle).
	JumpDelay time.Duration
}

// PointerProfile is for displaying images for alignment etc.
var PointerProfile = Profile{
	JumpSpeed:     8000,
	MarkSpeed:     8000,
	LaserOnDelay:  0,
	LaserOffDelay: 0,
	PolygonDelay:  0,
	JumpDelay:     0,
}

// BasicProfile is for performing a basic burn.
var BasicProfile = Profile{
	MarkFrequency:  40, // kHz ~ period of 25000 ns
	SetCo2FPK1:     50 * time.Microsecond,
	SetCo2FPK2:     1 * time.Microsecond,
	MarkPowerRatio: 1 * time.Nanosecond, // compare to period above.
	JumpSpeed:      4000,                // mm/sec
	MarkSpeed:      200,                 // mm/sec
	LaserOnDelay:   300 * time.Microsecond,
	LaserOffDelay:  100 * time.Microsecond,
	PolygonDelay:   10 * time.Microsecond,
	JumpDelay:      8 * time.Microsecond,
}

func (c *Conn) mmsSpeed(speed float64) uint16 {
	n := uint(speed * 256 / 200000 * c.mm2galvo)
	if n > 0xffff {
		return 0xffff
	}
	return uint16(n)
}

func waitTime(wait time.Duration) uint16 {
	n := uint(wait / time.Microsecond)
	if n > 0xffff {
		n = 0xffff
	}
	return uint16(n)
}

func kHz(freq float64) uint16 {
	if freq < 30 {
		freq = 30
	} else if freq > 150 {
		freq = 150
	}
	n := freq / 40 * 250
	return uint16(n)
}

func powerRatio(power time.Duration) uint16 {
	n := power * 20 / time.Nanosecond
	if n > 100 {
		n = 100 // TODO should explore what range is valid.
	}
	return uint16(n)
}

// Offset returns the number of command slots used so far.
func (list *List) Offset() int {
	list.mu.Lock()
	defer list.mu.Unlock()
	return len(list.commands)
}

// ErrBadReplay is returned by ReplayForm() when an attempt is made to
// duplicate nothing or less than nothing.
var ErrBadReplay = errors.New("unable to make copies of nothing")

// ReplayFrom duplicates a series of commands from a given offset (use
// Unused() at the right time to determine what that is) a specified
// number of times, n. If n is 0 then as many whole copies as will fit
// up to the next MaxListCommands boundary will be made. This function
// edits list in-place and the returned value is the number of copies
// made.
func (list *List) ReplayFrom(base, n int) (int, error) {
	list.mu.Lock()
	defer list.mu.Unlock()
	head := len(list.commands)
	entries := head - base
	if entries <= 0 {
		return 0, ErrBadReplay
	}
	m := head % MaxListCommands
	if n == 0 {
		if m == 0 {
			return 0, nil
		}
		avail := MaxListCommands - m
		if avail < entries {
			return 0, nil
		}
		n = avail / entries
	}
	for m := 0; m < n; m++ {
		list.commands = append(list.commands, list.commands[base:head]...)
	}
	return n, nil
}

// Start inserts a start of lasing sequence to configure the various
// laser settings.
func (list *List) Start(profile Profile) (*List, error) {
	list = list.pack(ReadyMark)
	if profile.MarkFrequency != 0 {
		list = list.pack(MarkFrequency, kHz(profile.MarkFrequency))
	}
	if profile.SetCo2FPK1 != 0 {
		list = list.pack(SetCo2FPK, waitTime(profile.SetCo2FPK1), waitTime(profile.SetCo2FPK2))
	}
	if profile.MarkPowerRatio != 0 {
		list = list.pack(MarkPowerRatio, powerRatio(profile.MarkPowerRatio))
	}
	list = list.pack(JumpSpeed, list.c.mmsSpeed(profile.JumpSpeed))
	list = list.pack(MarkSpeed, list.c.mmsSpeed(profile.MarkSpeed))
	list = list.pack(LaserOnDelay, waitTime(profile.LaserOnDelay))
	list = list.pack(LaserOffDelay, waitTime(profile.LaserOffDelay))
	list = list.pack(PolygonDelay, waitTime(profile.PolygonDelay))
	list = list.pack(JumpDelay, waitTime(profile.JumpDelay))
	return list, nil
}

// DelayJumps inserts a delay after a jump to allow the galvo to
// settle.
func (list *List) DelayJumps(wait time.Duration) *List {
	return list.pack(JumpDelay, waitTime(wait))
}

// Sleep inserts a microsecond delay. Zero is interpreted as 1 usec,
// upper bound is about 65 ms.
func (list *List) Sleep(wait time.Duration) *List {
	list.mu.Lock()
	defer list.mu.Unlock()
	return list.pack(DelayTime, waitTime(wait))
}

// JumpXY Jumps the pointer (laser mostly off) to (x,y) mm coordinates.
func (list *List) JumpXY(x, y float64) *List {
	list.mu.Lock()
	defer list.mu.Unlock()
	dx, dy := x-list.x1, y-list.y1
	id := list.c.iD(dx, dy)
	iX, iY := list.c.iXY(x, y)
	angle := uint16(0)

	list.pts = 1
	list.x0, list.y0 = list.x1, list.y1
	list.x1, list.y1 = x, y

	return list.pack(JumpTo, iY, iX, angle, id)
}

// lineXY marks a line recursively not drawing any segment greater
// than 10mm in length. This function is called locked.
func (list *List) lineXY(x, y float64) *List {
	var angle uint16
	dx1, dy1 := x-list.x1, y-list.y1

	if dx1*dx1+dy1*dy1 > 100.0 {
		midX, midY := 0.5*(list.x1+x), 0.5*(list.y1+y)
		return list.lineXY(midX, midY).lineXY(x, y)
	}

	if list.pts > 1 {
		// Enough history to compute angle, which is
		// proportional to any acute subtended angle between
		// two successive line segments (reset by any jump).
		dx0, dy0 := list.x1-list.x0, list.y1-list.y0
		d02xd12 := (dx0*dx0 + dy0*dy0) * (dx1*dx1 + dy1*dy1)
		if d02xd12 > 0 {
			dot := (dx0*dx1 + dy0*dy1)
			if theta := math.Acos(dot/math.Sqrt(d02xd12)) / math.Pi * 180.0; theta < 90 {
				angle = uint16(math.Round(theta / 90 * 32768))
			} else {
				angle = 0x8000
			}

		} // else angle = 0
	}
	id := list.c.iD(dx1, dy1)
	iX, iY := list.c.iXY(x, y)

	list.pts++
	list.x0, list.y0 = list.x1, list.y1
	list.x1, list.y1 = x, y

	return list.pack(MarkTo, iY, iX, angle, id)
}

// MarkXY moves the laser enabled (laser on) to (x,y) mm coordinates.
func (list *List) MarkXY(x, y float64) *List {
	list.mu.Lock()
	defer list.mu.Unlock()
	return list.lineXY(x, y)
}

// Run executes a list. Optionally loop, repeating the list until
// canceled via context. Note: the laser control is operating with at
// least a double buffer, and where the commands overflow the first
// buffer, the ExecuteList command is not issued until the 2nd buffer
// has been filled for the first time.
func (list *List) Run(ctx context.Context, loop bool) error {
	var eol Assembled
	if n, err := binary.Encode(eol[:], binary.LittleEndian, EndOfList); err != nil || n != 2 {
		return err
	}

	// Execute the code via this buffer.
	buf := make([]byte, len(eol)*MaxListCommands)
	as := list.Stream()

	if _, err := list.c.Query(ResetList); err != nil {
		return fmt.Errorf("failed to reset list: %v", err)
	}

	unstarted := true
	restart := true
	chunk := 0
	var x, y float64
	for ended := false; !ended; chunk++ {
		if _, err := list.c.Query(GetVersion); err != nil {
			return fmt.Errorf("failed to query laser: %v", err)
		}
		var status uint16
		select {
		case <-ctx.Done():
			ended = true
			// Drain channel of remaining commands.
			if as != nil {
				for range as {
				}
				as = nil
			}
			continue
		case <-time.After(50 * time.Millisecond):
			list.c.mu.Lock()
			status = list.c.status
			list.c.mu.Unlock()
		}
		if (status & statusReady) == 0 {
			continue
		}
		if unstarted {
			var err error
			if x, y, err = list.c.GetXY(); err != nil {
				return err
			}
			list.c.GotoXY(x, y)
			unstarted = false
		}
		if as == nil {
			as = list.Stream()
			chunk = 0
			restart = true
		}
		done := false // Capture that the end of the list commands was reached.
		for i := 0; i < MaxListCommands; i++ {
			offset := i * len(eol)
			if as == nil {
				done = true
			} else if a, ok := <-as; !ok {
				done = true
				as = nil
			} else {
				copy(buf[offset:], a[:])
			}
			if done {
				copy(buf[offset:], eol[:])
			}
		}
		n, err := list.c.Write(buf)
		if err != nil {
			return err
		}
		if n != len(buf) {
			return fmt.Errorf("unable to load data len=%d, wrote=%d", len(buf), n)
		}
		list.c.Query(SetEndOfList)
		if restart && (done || chunk > 0) {
			// Take advantage of double buffering for longer lists, by
			// only executing after the 2nd buffer is filled.
			list.c.Query(ExecuteList)
			restart = false
		}
		if done && !loop {
			break
		}
	}
	// Wait for completion
	for {
		if _, err := list.c.Query(GetVersion); err != nil {
			return fmt.Errorf("error polling laser: %v", err)
		}
		list.c.mu.Lock()
		status := list.c.status
		list.c.mu.Unlock()
		if status == (statusOK | statusReady) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	list.c.Query(SetControlMode, 0)
	return list.c.GotoXY(x, y)
}
