package kabuta

import (
	"flag"
	"fmt"
	//	"github.com/derekparker/delve/service/api"
	"github.com/derekparker/delve/service/rpc2"
	"net/rpc/jsonrpc"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
)

// gdbCmd represents either GDB or MI command as described in
// https://ftp.gnu.org/old-gnu/Manuals/gdb-5.1.1/html_node/gdb_213.html#SEC220
type gdbCmd struct {
	// Is this a MI2 (true) or CLI (false) command?
	isMiCmd bool
	// The command.
	cmd string
	// The arguments.
	args []string
	// The arguments as a string.
	argsStr string
	// Reference to parent frontendRequest.
	frontendRequest *frontendRequest
	// Command arguments parsed as flags
	flagSet *flag.FlagSet
}

func (c gdbCmd) String() string {
	if c.args != nil && len(c.args) > 0 {
		return f("%s %s", c.cmd, c.argsStr)
	} else {
		return c.cmd
	}

}

// respond responds to the Frontend's request
func (c *gdbCmd) respond(s1 string, s2 string) {
	r := c.frontendRequest
	response := ""
	if c.isMiCmd {
		if s1 != "" {
			response = s1 + "\n"
		}
		if s2 != "" {
			s2 += ","
		}
		response += f("%s^done,%s%s\n", c.frontendRequest.token, s2, r.gdbSummary())
	} else {
		// Temporary
		response = f("%s^done\n", c.frontendRequest.token)
		//		c.frontendRequest.kabuta.log("Unsure what to do with respond(%s,%s) in %s", s1, s2, c)
	}
	r.kabuta.writeToFrontend(response)
}

// Source is a no-op (should it not be?)
func (c *gdbCmd) Source() (string, string, error) {
	return noopReturner()
}

func (c *gdbCmd) FileExecAndSymbols() (string, string, error) {
	// This will be something like
	// /Users/grisha/g/dev/Romana/core/bin/root
	k := c.frontendRequest.kabuta
	k.debugBinaryPath = c.args[0]
	k.debugBinaryPackageDir = k.debugBinaryToPackageDir[filepath.Base(c.args[0])]
	if k.debugBinaryPackageDir == "" {
		return "", "", NewError("Cannot determine package directory for %s", c.args[0])
	}
	k.log("FileExecAndSymbols(): Package directory: %s", k.debugBinaryPackageDir)
	return noopReturner()
}
func (c *gdbCmd) ExecRun() (string, string, error) {
	var err error
	k := c.frontendRequest.kabuta
	dlv := k.dlvPath
	dlvAddr := f("127.0.0.1:%d", k.dlvPort)
	listenArg := f("--listen=%s", dlvAddr)
	// See https://github.com/derekparker/delve/tree/master/Documentation/api
	k.dlvCmd = exec.Command(dlv, "debug", "--headless", "--log", "--api-version=2", listenArg, "--", k.debugBinaryArgs)
	k.dlvCmd.Dir = k.debugBinaryPackageDir
	k.dlvStdout, err = k.dlvCmd.StdoutPipe()
	cmdLine := strings.Join(k.dlvCmd.Args, " ")
	if err != nil {
		return "", "", NewError("Error running %s in %s: %", cmdLine, k.debugBinaryPackageDir, err)
	}
	k.dlvStderr, err = k.dlvCmd.StderrPipe()
	if err != nil {
		return "", "", NewError("Error running %s in %s: %s", cmdLine, k.debugBinaryPackageDir, err)
	}
	k.log("ExecRun(): Launching %s in %s", cmdLine, k.dlvCmd.Dir)
	err = k.dlvCmd.Start()
	k.log("ExecRun(): After Start()")
	if err != nil {
		return "", "", NewError("Error running %s in %s: %s", cmdLine, k.debugBinaryPackageDir, err)
	}
	k.log("ExecRun(): dlv pid: %d", k.dlvCmd.Process.Pid)
	//	jsonrpc.Dial(", address)
	go k.dlvReadLoop(true)
	go k.dlvReadLoop(false)
	var line string
	for {
		k.log("ExecRun(): Waiting for Delve to start...")
		line = <-k.dlvChannel
		k.log("RECEIVED %d bytes FROM DLV CHANNEL\n---------------------\n%s\n---------------------", len(line), line)
		if line == "exec: \"go\": executable file not found in $PATH" {
			return "", "", NewError("%s (%s)", line, Environ()["PATH"])
		}
		if strings.HasPrefix(line, "API server listening at:") {
			break
		}
	}
	k.dlvRpcClient, err = jsonrpc.Dial("tcp", dlvAddr)
	if err != nil {
		return "", "", NewError("Error connecting to %s: %s", dlvAddr, err)
	}
	// Set breakpoints that have been requested so far.
	for _, bp := range k.breakpoints {
		bpIn := rpc2.CreateBreakpointIn{Breakpoint: *bp.dlvBreakpoint}
		bpOut := &rpc2.CreateBreakpointOut{}
		err = k.dlvRpcClient.Call("RPCServer.CreateBreakpoint", bpIn, bpOut)
		if err != nil {
			return "", "", NewError("Error setting breakpoint %s: %s", bp, err)
		}
		bp.dlvBreakpoint = &bpOut.Breakpoint
		k.log("Set breakpoint %v", bp.dlvBreakpoint)
	}
	return noopReturner()
}
func (c *gdbCmd) DataEvaluateExpression() (string, string, error) {
	dontKnowHowReturner := func() (string, string, error) {
		return "", "", NewError("Don't know how to evaluate %s", c.argsStr)
	}
	switch c.argsStr {
	case "\"sizeof (void*)\"":
		return "", "value=\"8\"", nil
	default:
		return dontKnowHowReturner()
	}
}
func (c *gdbCmd) GdbSet() (string, string, error) {
	k := c.frontendRequest.kabuta
	varName := c.args[0]
	dontKnowHowReturner := func() (string, string, error) {

		return "", "", NewError("Don't know how to set %s", c.argsStr)
	}
	var varName2 string
	switch varName {
	case "breakpoint":
		varName2 = c.args[1]
		switch varName2 {
		case "pending":
			if c.args[2] == "on" {
				k.breakpointPending = true
				return noopReturner()
			} else if c.args[2] == "off" {
				k.breakpointPending = true
				return noopReturner()
			} else {
				return "", "", NewError("Unknown value for breakpoint pending: %s", c.args[2])
			}

		default:
			return dontKnowHowReturner()
		}
	case "args":
		k.debugBinaryArgs = strings.Join(c.args[1:], " ")
		k.log("Binary args: %s", k.debugBinaryArgs)
		return noopReturner()
	case "print":
		varName2 = c.args[1]
		switch varName2 {
		case "sevenbit-strings":
			fallthrough
		case "object":
			// Just do nothing for now...
			return noopReturner()
		default:
			return dontKnowHowReturner()
		}
	case "env":
		argsStr := strings.Join(c.args[1:], " ")
		kv := strings.Split(argsStr, " = ")
		if len(kv) != 2 {
			return "", "", NewError("Bad value for env %s", argsStr)
		}
		os.Setenv(kv[0], kv[1])
		return noopReturner()
	case "charset":
		fallthrough
	case "auto-solib-add":
		return noopReturner()
	default:
		return "", "", NewError("Unknown variable: %s", varName)
	}
	return "", "", nil
}

// GdbShow handles show commands.
func (c *gdbCmd) GdbShow() (string, string, error) {
	dontKnowHowReturner := func() (string, string, error) {
		return "", "", NewError("Don't know how to show %s", strings.Join(c.args, " "))
	}
	switch c.args[0] {
	case "language":
		return "", "value=\"auto; currently c\"", nil
	case "endian":
		return noopReturner()
	default:
		return dontKnowHowReturner()
	}

}

// Noop
func (c *gdbCmd) InferiorTtySet() (string, string, error) {
	return noopReturner()
}

func (c *gdbCmd) dontKnowError() error {
	return NewError("Don't know how to handle \"%s\"", c)
}

// See https://sourceware.org/gdb/onlinedocs/gdb/GDB_002fMI-Breakpoint-Commands.html
func (c *gdbCmd) BreakInsert() (string, string, error) {
	//	      -break-insert [ -t ] [ -h ] [ -f ] [ -d ] [ -a ]
	//         [ -c condition ] [ -i ignore-count ]
	//         [ -p thread-id ] [ location ]
	c.flagSet.Bool("t", false, "")
	c.flagSet.Parse(c.args)

	args := c.flagSet.Args()
	if args == nil || len(args) == 0 {
		return "", "", c.dontKnowError()
	}
	bp, err := newBreakpoint(args[0])
	if err != nil {
		return "", "", err
	}
	c.frontendRequest.kabuta.breakpoints = append(c.frontendRequest.kabuta.breakpoints, bp)
	bpNo := len(c.frontendRequest.kabuta.breakpoints) + 1
	resp := fmt.Sprintf("bkpt={number=\"%d\",type=\"breakpoint\",disp=\"keep\",enabled=\"y\",", bpNo)
	resp += "addr=\"0x0000000100000f78\","
	resp += fmt.Sprintf("func=\"X\",file=\"%s\",line=\"%d\",", bp.fileName, bp.lineNo)
	resp += fmt.Sprintf("shlib=\"%s\",times=\"0\"}", c.frontendRequest.kabuta.debugBinaryPath)
	return "", resp, nil
}

// GdbExit exits the process.
func (c *gdbCmd) GdbExit() (string, string, error) {
	c.frontendRequest.kabuta.log("Exit command received. The Moor has done his duty, the Moor can go.")
	os.Exit(0)
	return noopReturner()
}

func (c *gdbCmd) GdbVersion() (string, string, error) {
	return strings.Join(miGdbVersion, "\n"), GdbVersionSummary, nil
}

// EnvironmentCd reacts to environment-cd command - it sets
// kabuta's cwd field and adds the directory to GOPATH.
// It then goes over go files in the directory to look for
// package main. That information is stored so that when FileExecAndSymbols
// are called,
func (c *gdbCmd) EnvironmentCd() (string, string, error) {
	cwd := c.args[0]
	k := c.frontendRequest.kabuta
	k.log("Changing dir to %s", cwd)
	k.cwd = cwd
	gopath := Environ()["GOPATH"]
	if gopath != "" {
		gopath += ":"
	}
	gopath += cwd
	err := os.Setenv("GOPATH", cwd)
	if err != nil {
		return "", "", NewError("Error setting GOPATH to \"%s\"", cwd)
	}
	grep := fmt.Sprintf("grep -r '^package main$'  %s | grep -v vendor", cwd)
	grepCmd := exec.Command("/bin/bash", "-c", grep)
	out, err := grepCmd.CombinedOutput()
	if err != nil {
		return "", "", NewError("Error running %s: %s", grep, err)
	}
	outLines := strings.Split(string(out), "\n")
	for _, line := range outLines {
		fnamePkg := strings.Split(line, ":")
		fname := fnamePkg[0]
		if !strings.HasSuffix(fname, ".go") {
			continue
		}
		dir := filepath.Dir(fname)
		base := filepath.Base(dir)
		if k.debugBinaryToPackageDir[base] == "" {
			k.debugBinaryToPackageDir[base] = dir
		} else {
			k.log("Trying to set %s as dir for %s, but already have %s, will skip", dir, base, k.debugBinaryToPackageDir[base])
		}

	}
	return noopReturner()
}

func (c *gdbCmd) process() {
	if c.args != nil && len(c.args) > 0 {
		c.flagSet = flag.NewFlagSet(c.cmd, flag.ContinueOnError)
		if err != nil {
			c.frontendRequest.kabuta.log("Error parsing arguments %s for %s: %s", c.cmd, c.argsStr, err)
		}
	}
	c.frontendRequest.kabuta.log("Command: %s", *c)
	var cmdSep string
	if c.isMiCmd {
		cmdSep = "-"
	} else {
		cmdSep = " "
	}
	cmdElts := strings.Split(c.cmd, cmdSep)
	methodName := ""
	for _, e := range cmdElts {
		e = strings.ToUpper(string(e[0])) + e[1:]
		methodName += e
	}

	method, mExist := reflect.TypeOf(c).MethodByName(methodName)
	if !mExist {
		msg := f("Unknown command %s: no method to process it \"%s\" found.", c, methodName)
		c.respondError(msg)
		return
	}

	self := reflect.ValueOf(c)
	args := []reflect.Value{self}
	retval := method.Func.Call(args)
	//	c.frontendRequest.kabuta.log("Calling method %s, got %v", method.Name, retval)
	if len(retval) != 3 {
		c.respondError("Method %s for command %s was expected to return 2 values, returned %v", methodName, c.cmd, retval)
	}

	errVal := retval[2].Interface()
	if errVal != nil {
		c.respondError("Error occurred: %s", errVal.(error))
	} else {
		c.respond(retval[0].Interface().(string), retval[1].Interface().(string))
	}
}

// respond responds to the Frontend's request with an error message
func (c *gdbCmd) respondError(s string, args ...interface{}) {
	if args != nil && len(args) > 0 {
		s = f(s, args...)
	}
	r := c.frontendRequest
	r.kabuta.log("Error: %s", s)
	response := f("%s^error,msg=\"%s\"", r.token, s)
	r.kabuta.writeToFrontend(response)
}

func noopReturner() (string, string, error) {
	return "", "", nil
}
