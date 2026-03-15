# Glase is a package to support a ComMarker Omni 1 5W UV laser

There is no official support for Linux for this laser, so I've
developed this package and cli tool to bridge that gap in a way that
meets my needs.

## How to use

Build and kick the tires (no laser activity from this first example):

```
$ git clone https://github.com/tinkerator/glase.git
$ cd glase
$ go build examples/glase.go
$ ./glase --info
```

- My device is `mfg="BJJCZ" product="USBLMCV4" ...`, so I won't be
  surprised if a different device misbehaves.
- If you are fortunate enough to have more than one of these devices,
  you can specify `--serial=<xxx>` on the command line to operate on a
  specific one.

### First time gotchas

- If this fails with an error about `libusb-1.0` not being found, try
  one of the following and then the `go build examples/glase.go`
  command again:
  - Debian: `sudo apt install libusb-1.0-0-dev pkg-config`
  - Fedora: `sudo dnf install libusb1-devel pkgconfig-pkg-config`
- The first time you run this, you will likely get a `bad access [code -3]` error.
  - To resolve that, create the following file with `sudo nano
/etc/udev/rules.d/99-omni1.rules`, with this content:

```
# This rule was added for the ComMarker Omni 1 5W UV Laser device.
SUBSYSTEM=="usb", ENV{DEVTYPE}=="usb_device", ATTRS{idVendor}=="9588", ATTRS{idProduct}=="9899", MODE="0666"
```
  - Then, to refresh the rules, run `sudo udevadm control --reload-rules` and also `sudo udevadm trigger`.

## References

- The development of this code was inspired by [this
  post](https://hackaday.com/tag/balor/) and benefited greatly from
  the pioneering work, [Balor](https://www.bryce.pw/engraver.html).
- The excellent [meerk40t](https://github.com/meerk40t/meerk40t)
  successor to Balor.
  - This includes the closer relative,
    [galvoplotter](https://github.com/meerk40t/galvoplotter)

## License info

The `glase` package is distributed with the same BSD 3-clause
[license](LICENSE) as that used by
[golang](https://golang.org/LICENSE) itself.

## Reporting bugs

This is a hobby project, so I can't guarantee a fix, but do use the
[github `glase` bug
tracker](https://github.com/tinkerator/glase/issues).
