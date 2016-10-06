package kabuta

import (
	"flag"
	"fmt"
	//	"github.com/derekparker/delve/service/api"
	"github.com/derekparker/delve/service/api"
	"github.com/derekparker/delve/service/rpc2"
	"net/rpc/jsonrpc"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
)

// noopReturner returns empty result.
func noopReturner() gdbMiResponse {
	return gdbMiResponse{}
}

// gdbMiRecord represents GDB/MI2 output record,
// either result (async is false: https://sourceware.org/gdb/onlinedocs/gdb/GDB_002fMI-Result-Records.html#GDB_002fMI-Result-Records), or
// async (async is true: https://sourceware.org/gdb/onlinedocs/gdb/GDB_002fMI-Async-Records.html#GDB_002fMI-Async-Records)
// The communicating to the frontend of stream (https://sourceware.org/gdb/onlinedocs/gdb/GDB_002fMI-Stream-Records.html#GDB_002fMI-Stream-Records)
// records is handled in sendConsoleStreamRecord and sendOutputStreamRecord.
type gdbMiRecord struct {
	// done, running, etc.
	state   string
	results string
	// if not nil, state is assumed to be "error"
	err   error
	async bool
}

// gdbCmd represents either GDB or MI command as described in
// https://ftp.gnu.org/old-gnu/Manuals/gdb-5.1.1/html_node/gdb_213.html#SEC220
type gdbCmd struct {
	// Is this an MI2 (true) or CLI (false) command?
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

// process finds the appropriate method on gdbCmd to process
// the command, or return dontKnowError if not found.
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

	response := retval.Interface().(gdbMiResponse)
	respond(response)
}

// respond responds to the Frontend's request with an error message
func (c *gdbCmd) respond(resp gdbMiRecord) {
	frontReq := c.frontendRequest
	k := frontReq.kabuta
	var response string
	if resp.err == nil {
		if resp.state == "" {
			resp.state = "done"
		}

		if c.isMiCmd {
			if s1 != "" {
				response = s1 + "\n"
			}
			if s2 != "" {
				s2 += ","
			}
			response += f("%s^%s,%s%s\n", frontReq.token, resp.state, resp.s2, frontReq.gdbSummary())
		} else {
			// Temporary
			response = f("%s^%s\n", resp.state, frontReq.token)
			//		c.frontendRequest.kabuta.log("Unsure what to do with respond(%s,%s) in %s", s1, s2, c)
		}
	} else {
		k.log("Error: %s", resp.err.String())
		response = f("%s^error,msg=\"%s\"", resp.token, resp.err.String())
	}
	k.writeToFrontend(response)
}

// dontKnowError is used when we don't know how to handle a given command.
// It is primarily intended as a placeholder during development.
func (c *gdbCmd) dontKnowError() gdbMiResponse {
	return returnErrorf("Don't know how to handle \"%s\"", c)
}

func (c *gdbCmd) sendConsoleStreamRecord(s string, args ...interface{}) {
	c.frontendRequest.kabuta.writeToFrontend("~" + f(s, args))
}

func (c *gdbCmd) sendOutputStreamRecord(s string, args ...interface{}) {
	c.frontendRequest.kabuta.writeToFrontend("@" + f(s, args))
}

// 9*stopped,time={wallclock="0.09920",user="0.03285",system="0.03430",start="1475679819.184591",end="1475679819.283786"},
// reason="breakpoint-hit",commands="no",times="1",bkptno="1",thread-id="2"

// respond responds to the Frontend's request in the proper format
// (including corresponding token and command execution info).

// See https://sourceware.org/gdb/onlinedocs/gdb/GDB_002fMI-Breakpoint-Commands.html
func (c *gdbCmd) BreakInsert() gdbMiResponse {
	//	      -break-insert [ -t ] [ -h ] [ -f ] [ -d ] [ -a ]
	//         [ -c condition ] [ -i ignore-count ]
	//         [ -p thread-id ] [ location ]
	c.flagSet.Bool("t", false, "")
	c.flagSet.Parse(c.args)

	args := c.flagSet.Args()
	if args == nil || len(args) == 0 {
		return dontKnowError()
	}
	bp, err := newBreakpoint(args[0])
	if err != nil {
		return returnError(err)
	}
	// If Delve is already running, send the breakpoint there.
	k := c.frontendRequest.kabuta
	// Some dummy address for now..
	addr := "0x0000000100000f78"
	funcName := "dummy"
	fileName := bp.fileName
	lineNo := bp.lineNo
	if k.dlvRpcClient != nil {
		bpIn := rpc2.CreateBreakpointIn{Breakpoint: *bp.dlvBreakpoint}
		bpOut := &rpc2.CreateBreakpointOut{}
		err = k.dlvRpcClient.Call("RPCServer.CreateBreakpoint", bpIn, bpOut)
		if err != nil {
			return returnErrorf("Error setting breakpoint %s: %s", bp, err)
		}
		bp.dlvBreakpoint = &bpOut.Breakpoint
		addr = f("%x", bp.dlvBreakpoint.Addr)
		funcName = bp.dlvBreakpoint.FunctionName
		fileName = bp.dlvBreakpoint.File
		lineNo = bp.dlvBreakpoint.Line
		k.log("Set breakpoint %v", bp.dlvBreakpoint)
	}
	c.frontendRequest.kabuta.breakpoints = append(c.frontendRequest.kabuta.breakpoints, bp)
	bpNo := len(c.frontendRequest.kabuta.breakpoints) + 1
	resp := fmt.Sprintf("bkpt={number=\"%d\",type=\"breakpoint\",disp=\"keep\",enabled=\"y\",", bpNo)
	resp += f("addr=\"%s\"", addr)
	resp += f("func=\"%s\",file=\"%s\",line=\"%d\",", funcName, fileName, lineNo)
	resp += f("shlib=\"%s\",times=\"0\"}", c.frontendRequest.kabuta.debugBinaryPath)
	return "", resp, nil
}

func (c *gdbCmd) DataEvaluateExpression() gdbMiResponse {
	dontKnowHowReturner := func() gdbMiResponse {
		return "", "", NewError("Don't know how to evaluate %s", c.argsStr)
	}
	switch c.argsStr {
	case "\"sizeof (void*)\"":
		return "", "value=\"8\"", nil
	default:
		return dontKnowHowReturner()
	}
}

// EnvironmentCd reacts to environment-cd command - it sets
// kabuta's cwd field and adds the directory to GOPATH.
// It then goes over go files in the directory to look for
// package main. That information is stored so that when FileExecAndSymbols
// are called,
func (c *gdbCmd) EnvironmentCd() gdbMiResponse {
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
	// TODO document what's going on here.
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

// ExecRun is invoked in response to exec-run GDB MI command.
// It launches Delve as described in https://github.com/derekparker/delve/tree/master/Documentation/api.
// In particular:
//   1. Binary specified by EnvKabutaDlvPath is run
//   2. It is run directory as determined by FileExecAndSymbols
//   3. It is run with --headless --log --api-version-2 flags.
//   4. It is run with the --listen flag argument set to 127.0.0.1:<PORT> where port is
// the value of EnvKabutaDlvPort.
func (c *gdbCmd) ExecRun() gdbMiResponse {
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
		return returnErrorf("Error running %s in %s: %", cmdLine, k.debugBinaryPackageDir, err)
	}
	k.dlvStderr, err = k.dlvCmd.StderrPipe()
	if err != nil {
		return returnErrorf("Error running %s in %s: %s", cmdLine, k.debugBinaryPackageDir, err)
	}
	k.log("ExecRun(): Launching %s in %s", cmdLine, k.dlvCmd.Dir)
	err = k.dlvCmd.Start()
	k.log("ExecRun(): After Start()")
	if err != nil {
		return "", "", NewError("Error running %s in %s: %s", cmdLine, k.debugBinaryPackageDir, err)
	}
	k.log("ExecRun(): dlv pid: %d", k.dlvCmd.Process.Pid)
	go k.dlvReadLoop(true)
	go k.dlvReadLoop(false)
	var line string
	for {
		k.log("ExecRun(): Waiting for Delve to start...")
		line = <-k.dlvChannel
		k.log("RECEIVED %d bytes FROM DLV CHANNEL\n---------------------\n%s\n---------------------", len(line), line)
		if line == "exec: \"go\": executable file not found in $PATH" {
			return returnErrorf("%s (%s)", line, Environ()["PATH"])
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
			return returnErrorf("Error setting breakpoint %s: %s", bp, err)
		}
		bp.dlvBreakpoint = &bpOut.Breakpoint
		k.log("Set breakpoint %v", bp.dlvBreakpoint)
	}

	return 9 ^ running()
}

// FileExecAndSymbols is invoked in response to file-exec-and-symbols GDB MI command.
// It determines the directory in which the "main" package being debugged lives
// by examining information created by EnvironmentCd.
func (c *gdbCmd) FileExecAndSymbols() gdbMiResponse {
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

// GdbExit exits the process.
func (c *gdbCmd) GdbExit() gdbMiResponse {
	c.frontendRequest.kabuta.log("Exit command received. The Moor has done his duty, the Moor can go.")
	os.Exit(0)
	return noopReturner()
}

func (c *gdbCmd) GdbSet() gdbMiResponse {
	k := c.frontendRequest.kabuta
	varName := c.args[0]
	dontKnowHowReturner := func() gdbMiResponse {

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
func (c *gdbCmd) GdbShow() gdbMiResponse {
	dontKnowHowReturner := func() gdbMiResponse {
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

func (c *gdbCmd) GdbVersion() gdbMiResponse {
	return strings.Join(miGdbVersion, "\n"), GdbVersionSummary, nil
}

// Noop
func (c *gdbCmd) InferiorTtySet() gdbMiResponse {
	return noopReturner()
}

//11^done,threadno="3",frame={func="threadFunc",optimized="0",args=[{name="id",value="4006"}],file="main.c",fullname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/main.c",line="6",dir="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject",shlibname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/a.out"},threadno="2",frame={func="threadFunc",optimized="0",args=[{name="id",value="4006"}],file="main.c",fullname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/main.c",line="6",dir="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject",shlibname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/a.out"},threadno="1",frame={addr="0x00007fff908a7206",fp="0x00007fff5fbffa00",func="__semwait_signal",optimized="0",args=[],shlibname="/usr/lib/system/libsystem_kernel.dylib"}
func (c *gdbCmd) InfoThreads() gdbMiResponse {
	//	XXX
	// 13^done,threadno="3",
	//frame={func="threadFunc",optimized="0",
	//args=[{name="id",value="4006"}],file="main.c",
	//fullname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/main.c",
	//line="6",dir="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject",
	//shlibname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/a.out"},
	//threadno="2",frame={func="threadFunc",optimized="0",args=[{name="id",value="4006"}],
	//file="main.c",
	//fullname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/main.c",
	//line="6",
	//dir="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject",
	//shlibname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/a.out"},
	//threadno="1",frame={addr="0x00007fff908a146a",fp="0x00007fff5fbff9b0",
	//func="swtch_pri",optimized="0",args=[],shlibname="/usr/lib/system/libsystem_kernel.dylib"}
	var goroutines []*api.Goroutine
	k := c.frontendRequest.kabuta
	gro := rpc2.ListGoroutinesOut{Goroutines: goroutines}
	err := k.dlvRpcClient.Call("RPCServer.Goroutines", rpc2.ListGoroutinesIn{}, &gro)
	if err != nil {
		return returnErrorf("Error listing threads: %s", err)
	}

	threadIds := ""
	threadDetails := ""
	result := ""
	for i, g := range gro.Goroutines {
		if len(result) > 0 {
			result += ","
		}
		result += f("threadno=\"%d\"", i+1)

		err := k.dlvRpcClient.Call("RPCServer.Goroutines", rpc2.ListGoroutinesIn{}, &gro)
		if err != nil {
			return returnErrorf("Error listing threads: %s", err)
		}
	}
}

// Source is a no-op (should it not be?)
func (c *gdbCmd) Source() gdbMiResponse {
	return noopReturner()
}

// StackInfoDepth does nothing at the moment, but returns the depth as specified
// in the command. In other words, if called with
// 11-stack-info-depth 4
// will return
// 11^done,depth="4"
func (c *gdbCmd) StackInfoDepth() gdbMiResponse {
	return gdbMiResponse{s2: f("depth=\"%s\"", c.args[0])}, nil
}

// 12^done,stack=[frame={level="0",addr="0x0000000100000df0",fp="0x0000700000080f00",
// func="threadFunc",optimized="0",file="main.c",
// fullname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/main.c",
// line="6",dir="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject",
// shlibname="/Users/grisha/g/dev/Kabuta/src/github.com/debedb/kabuta/testdata/cdtproject/a.out"},
// frame={level="1",addr="0x00007fff93670c13",fp="0x0000700000080f20",func="_pthread_body",optimized="0",
// shlibname="/usr/lib/system/libsystem_pthread.dylib"},frame={level="2",addr="0x00007fff93670b90",
// fp="0x0000700000080f60",func="_pthread_start",optimized="0",
// shlibname="/usr/lib/system/libsystem_pthread.dylib"},
// frame={level="3",addr="0x00007fff9366e375",fp="0x0000000000000000",
// func="thread_start",optimized="0",
// shlibname="/usr/lib/system/libsystem_pthread.dylib"}],
// time={wallclock="0.00411",user="0.00095",system="0.00129",
// start="1475680391.687718",end="1475680391.691833"}
func (c *gdbCmd) StackListFrames() gdbMiResponse {
	low := 0
	high := 100
	if len(c.args) > 0 {
		low = c.args[0]
		if len(c.args) > 1 {
			high = c.args[1]
		}
	}
	k := c.frontendRequest.kabuta
	err := k.dlvRpcClient.Call("RPCServer.Stacktrace", rpc2.StacktraceIn{Cfg: loadConfig}, &gro)
	if err != nil {
		return "", "", NewError("Error listing threads: %s", err)
	}

	return "", f("depth=\"%s\"", c.args[0]), nil
}

func (c *gdbCmd) StackSelectFrame() gdbMiResponse {

	return dontKnowError()
}

// ThreadListIds actually lists Goroutine IDs, because that is of
// interest to the user.
func (c *gdbCmd) ThreadListIds() gdbMiResponse {
	var goroutines []*api.Goroutine
	k := c.frontendRequest.kabuta
	gro := rpc2.ListGoroutinesOut{Goroutines: goroutines}
	err := k.dlvRpcClient.Call("RPCServer.Goroutines", rpc2.ListGoroutinesIn{}, &gro)
	if err != nil {
		return returnErrorf("Error listing threads: %s", err)
	}

	threadIds := ""
	threadDetails := ""
	for i, g := range gro.Goroutines {
		if i > 0 {
			threadIds += ","
			threadDetails += ","
		}
		k.log("Thread information: %s", String(g))
		tid := f("thread-id=\"%d\"", g.ID)
		threadIds += tid
		// State?
		threadInfo := rpc2.GetThreadOut{}
		err := k.dlvRpcClient.Call("RPCServer.GetThread", rpc2.GetThreadIn{Id: g.ThreadID}, &threadInfo)
		if err != nil {
			return "", "", NewError("Error listing threads: %s", err)
		}

		if threadInfo.Thread.Breakpoint == nil {
			state = "RUNNING"
		} else {
			state = "WAITING"
		}
		threadDetails += f("thread={%s,state=\"%s\",mach-port-number=\"0xffff\",pthread-id=\"%d\",unique-id=\"%d\"}", tid, state, t.GoroutineID, t.GoroutineID)
	}
	result := f("thread-ids={%s},number-of-threads=\"%d\",threads=[%s]", threadIds, len(gro.Goroutines), threadDetails)
	return "", result, nil
}

// Lists threads
func (c *gdbCmd) ThreadListIdsXXX() gdbMiResponse {
	var threads []*api.Thread
	k := c.frontendRequest.kabuta
	lto := rpc2.ListThreadsOut{Threads: threads}
	err := k.dlvRpcClient.Call("RPCServer.ListThreads", rpc2.ListThreadsIn{}, &lto)
	if err != nil {
		return returnErrorf("Error listing threads: %s", err)
	}

	threadIds := ""
	threadDetails := ""
	for i, t := range lto.Threads {
		if i > 0 {
			threadIds += ","
			threadDetails += ","
		}
		k.log("Thread information: %s", String(t))
		tid := f("thread-id=\"%d\"", t.GoroutineID)
		threadIds += tid
		// State?
		var state string
		if t.Breakpoint == nil {
			state = "RUNNING"
		} else {
			state = "WAITING"
		}
		threadDetails += f("thread={%s,state=\"%s\",mach-port-number=\"0xffff\",pthread-id=\"%d\",unique-id=\"%d\"}", tid, state, t.GoroutineID, t.GoroutineID)
	}
	result := f("thread-ids={%s},number-of-threads=\"%d\",threads=[%s]", threadIds, len(lto.Threads), threadDetails)
	return "", result, nil
}
