package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"
)

// init registers the status command.
func init() {
	Register(Command{
		Name:    "status",
		Summary: "report the server state: password, devices, pending invitations",
		Run:     runStatus,
	})
}

// runStatus is the Run function of `pi-ui status`: it reads the state database and
// reports what an operator needs before pairing a device or setting a password. It
// never talks to a running server.
func runStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeStatusUsage(stderr) }
	stateDirFlag := fs.String("state-dir", "", "state directory (PIUI_STATE_DIR)")
	if err := fs.Parse(args); err != nil {
		return Usage(err)
	}
	if fs.NArg() > 0 {
		return Usagef("unexpected argument %q", fs.Arg(0))
	}

	dir, err := resolveStateDir(*stateDirFlag)
	if err != nil {
		return err
	}
	db, service, err := openState(ctx, dir)
	if err != nil {
		return err
	}
	defer db.Close()

	fmt.Fprintln(stdout, "pi-ui status")
	fmt.Fprintf(stdout, "  version         %s\n", Version)
	fmt.Fprintf(stdout, "  state           %s\n", db.Path())
	fmt.Fprintf(stdout, "  admin password  %s\n", yesNo(service.HasAdminPassword(), "set", "not set"))
	devices := service.Devices()
	fmt.Fprintf(stdout, "  devices         %d\n", len(devices))
	for _, device := range devices {
		created := device.CreatedAt.UTC().Format(time.RFC3339)
		seen := "never"
		if !device.LastSeenAt.IsZero() {
			seen = device.LastSeenAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(stdout, "    %-20s %-16s %-9s created %s  last seen %s\n",
			device.ID, device.Name, device.Scope, created, seen)
	}
	fmt.Fprintf(stdout, "  invitations     %d pending\n", service.PendingInvites())
	if len(devices) == 0 && !service.HasAdminPassword() {
		fmt.Fprintln(stdout, "  next            run `pi-ui pair` and scan the QR — or type the code — on the device,")
		fmt.Fprintln(stdout, "                  or set an admin password with `pi-ui auth set-password`")
	}
	return nil
}

// yesNo renders a boolean as a labelled answer.
func yesNo(value bool, yes, no string) string {
	if value {
		return yes
	}
	return no
}

// writeStatusUsage prints the command's own flags.
func writeStatusUsage(w io.Writer) {
	fmt.Fprint(w, "usage: pi-ui status [flags]\n\n"+
		"flags:\n"+
		"  --state-dir <path>     state directory (PIUI_STATE_DIR, default $XDG_STATE_HOME/pi-ui)\n\n"+
		"Reports the devices, the admin password and the pending pairing invitations of\n"+
		"the state database; it does not require a running server.\n")
}
