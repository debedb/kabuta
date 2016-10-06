package kabuta

import (
	"sync"
)

const (
	EnvKabutaLogFile = "KABUTA_LOG_FILE"
	EnvKabutaDlvPath = "KABUTA_DLV_PATH"
	EnvKabutaDlvPort = "KABUTA_DLV_PORT"
	EnvKabutaPath    = "KABUTA_PATH"
	// Init file, looked for in user's home directory, that can override environment
	// variables.
	KabutaInitFile        = ".kabutainit"
	DefaultKabutaLogFile  = "kabuta.log"
	DefaultDlvPort        = "8181"
	DlvVersionOutputStart = "Delve Debugger"
	// MI2 command. First group is token (to include in response).
	RegexpMiCmd = "([0-9]+)-(.*)"
	// GDB CLI command. First group is token (to include in response).
	RegexpCliCmd = "([0-9]+)(.*)"

	GdbPrompt = "(gdb)\n"

	// Copied from my gdb's output
	GdbVersion = `GNU gdb 6.3.50.20050815-cvs (Wed Nov 26 07:47:26 UTC 2014)
Copyright 2004 Free Software Foundation, Inc.
GDB is free software, covered by the GNU General Public License, and you are
welcome to change it and/or distribute copies of it under certain conditions.
Type "show copying" to see the conditions.
There is absolutely no warranty for GDB.  Type "show warranty" for details.
This GDB was configured as "--host=i686-apple-darwin14.0.0 --target=".`
	GdbVersionSummary = `version="6.3.50.20050815-cvs",rc_version="unknown",target="",build-date="Wed Nov 26 07:47:26 UTC 2014"`
)

var (
	miGdbVersion = []string{
		"~\"GNU gdb 6.3.50.20050815-cvs (Wed Nov 26 07:47:26 UTC 2014)\\n\"",
		"~\"Copyright 2004 Free Software Foundation, Inc.\\n\"",
		"~\"GDB is free software, covered by the GNU General Public License, and you are\\n\"",
		"~\"welcome to change it and/or distribute copies of it under certain conditions.\\n\"",
		"~\"Type \"show copying\" to see the conditions.\\n\"",
		"~\"There is absolutely no warranty for GDB.  Type \"show warranty\" for details.\\n\"",
		"~\"This GDB was configured as \\\"--host=i686-apple-darwin14.0.0 --target=\\\".\"",
	}
	wg  sync.WaitGroup
	err error
)
